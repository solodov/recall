// Package cli implements recall's query-first command-line boundary.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"recall/internal/config"
	"recall/internal/orchestrator"
	"recall/internal/render"
	"recall/internal/runtime"
	configv1 "recall/proto/recall/config/v1"
)

// ConfigLoader loads the operator-owned provider registry for a recall run.
type ConfigLoader func() (*configv1.RecallConfig, error)

// SearchRunner executes the provider fan-out for a query.
type SearchRunner func(runtime.Context, *configv1.RecallConfig, string, orchestrator.Options) (*orchestrator.Result, error)

// RuntimeFactory builds the command runtime after flags have selected log paths
// and stderr verbosity.
type RuntimeFactory func(context.Context, RuntimeOptions) (runtime.Context, error)

// RuntimeOptions contains command-line controlled debugging sinks.
type RuntimeOptions struct {
	LogPaths runtime.LogPaths
	LogLevel string
}

// App contains command dependencies so the query-first CLI can be tested
// without launching real provider processes.
type App struct {
	Stdout io.Writer
	Stderr io.Writer

	LoadConfig ConfigLoader
	Search     SearchRunner
	NewRuntime RuntimeFactory
}

// Run parses recall's root search command, loads provider config, dispatches
// the query, and renders provider-agnostic results.
func (app App) Run(ctx context.Context, args []string) (runErr error) {
	stdout := app.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := app.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	search := app.Search
	if search == nil {
		search = orchestrator.Search
	}

	parsed, err := parseArgs(args, stderr)
	if err != nil {
		return err
	}
	runtimeOptions, err := parsed.runtimeOptions()
	if err != nil {
		return err
	}
	run, err := app.newRuntime(ctx, runtimeOptions)
	if err != nil {
		return err
	}
	run = run.WithLogMeta("command", "search")
	run, span := run.StartOperation("recall.search", "query", parsed.query)
	defer func() {
		span.RecordError(runErr)
		span.End()
	}()

	var cfg *configv1.RecallConfig
	if err := span.Measure("load_config", func() error {
		var err error
		cfg, err = app.loadConfig(parsed.configPath)
		return err
	}); err != nil {
		return err
	}
	var result *orchestrator.Result
	searchErr := span.Measure("search", func() error {
		var err error
		result, err = search(run, cfg, parsed.query, orchestrator.Options{
			Sources: parsed.sources,
			Limit:   parsed.limit,
			Kinds:   parsed.kinds,
		})
		return err
	})
	if searchErr != nil && result == nil {
		return searchErr
	}
	if result != nil {
		var renderErr error
		renderErr = span.Measure("render", func() error {
			switch parsed.format {
			case outputFormatJSON:
				return render.WriteJSON(stdout, result)
			case outputFormatHuman:
				if err := render.WriteHuman(stdout, result, render.HumanOptions{Grouped: parsed.grouped}); err != nil {
					return err
				}
				renderFailures(stderr, result.Failures)
			}
			return nil
		}, "format", string(parsed.format), "grouped", parsed.grouped)
		return errors.Join(renderErr, searchErr)
	}
	return searchErr
}

type parsedArgs struct {
	query       string
	configPath  string
	logPath     string
	perfLogPath string
	logLevel    string
	sources     []string
	limit       uint32
	kinds       []string
	grouped     bool
	format      outputFormat
}

type outputFormat string

const (
	outputFormatHuman outputFormat = "human"
	outputFormatJSON  outputFormat = "json"
)

func parseArgs(args []string, stderr io.Writer) (parsedArgs, error) {
	flags := flag.NewFlagSet("recall", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to the provider registry txtpb file")
	logPath := flags.String("log-path", "", "path to the main rotated log file")
	perfLogPath := flags.String("perf-log-path", "", "path to the rotated performance trace log file")
	logLevel := flags.String("log-level", "off", "also print logs to stderr at level: debug|info|warn|error|off")
	var sources stringListFlag
	flags.Var(&sources, "source", "comma-separated provider IDs to query")
	var kinds stringListFlag
	flags.Var(&kinds, "kind", "comma-separated result kinds to keep after provider search")
	limit := flags.Uint("limit", 0, "override per-provider result limit")
	grouped := flags.Bool("grouped", false, "group output by source and provider group")
	format := flags.String("format", string(outputFormatHuman), "output format: human or json")
	if err := flags.Parse(args); err != nil {
		return parsedArgs{}, err
	}

	query := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if query == "" {
		return parsedArgs{}, errors.New("usage: recall [--config path] [--log-path path] [--perf-log-path path] [--log-level level] [--source provider[,provider] ...] [--limit n] QUERY")
	}
	if *limit > uint(^uint32(0)) {
		return parsedArgs{}, fmt.Errorf("--limit %d exceeds uint32 maximum", *limit)
	}
	parsedFormat := outputFormat(*format)
	switch parsedFormat {
	case outputFormatHuman, outputFormatJSON:
	default:
		return parsedArgs{}, fmt.Errorf("unsupported --format %q; use human or json", *format)
	}
	return parsedArgs{query: query, configPath: *configPath, logPath: *logPath, perfLogPath: *perfLogPath, logLevel: *logLevel, sources: sources, limit: uint32(*limit), kinds: kinds, grouped: *grouped, format: parsedFormat}, nil
}

func (parsed parsedArgs) runtimeOptions() (RuntimeOptions, error) {
	logPaths, err := runtime.DefaultLogPaths()
	if err != nil {
		return RuntimeOptions{}, err
	}
	if strings.TrimSpace(parsed.logPath) != "" {
		logPaths.Main = parsed.logPath
	}
	if strings.TrimSpace(parsed.perfLogPath) != "" {
		logPaths.Perf = parsed.perfLogPath
	}
	return RuntimeOptions{LogPaths: logPaths, LogLevel: parsed.logLevel}, nil
}

func (app App) newRuntime(ctx context.Context, options RuntimeOptions) (runtime.Context, error) {
	if app.NewRuntime != nil {
		return app.NewRuntime(ctx, options)
	}
	return runtime.NewWithLogPaths(ctx, options.LogPaths, options.LogLevel)
}

func (app App) loadConfig(configPath string) (*configv1.RecallConfig, error) {
	if app.LoadConfig != nil {
		return app.LoadConfig()
	}
	if strings.TrimSpace(configPath) != "" {
		return config.LoadFile(configPath)
	}
	return config.LoadDefault()
}

func renderFailures(writer io.Writer, failures []orchestrator.ProviderFailure) {
	for _, failure := range failures {
		if failure.Err == nil {
			continue
		}
		fmt.Fprintf(writer, "[%s] provider failed: %v\n", failure.ProviderID, failure.Err)
	}
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}
