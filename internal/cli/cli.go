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
	configv1 "recall/proto/recall/config/v1"
)

// ConfigLoader loads the operator-owned provider registry for a recall run.
type ConfigLoader func() (*configv1.RecallConfig, error)

// SearchRunner executes the provider fan-out for a query.
type SearchRunner func(context.Context, *configv1.RecallConfig, string, orchestrator.Options) (*orchestrator.Result, error)

// App contains command dependencies so the query-first CLI can be tested
// without launching real provider processes.
type App struct {
	Stdout io.Writer
	Stderr io.Writer

	LoadConfig ConfigLoader
	Search     SearchRunner
}

// Run parses recall's root search command, loads provider config, dispatches
// the query, and renders provider-agnostic results.
func (app App) Run(ctx context.Context, args []string) error {
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

	cfg, err := app.loadConfig(parsed.configPath)
	if err != nil {
		return err
	}
	result, err := search(ctx, cfg, parsed.query, orchestrator.Options{
		Sources: parsed.sources,
		Limit:   parsed.limit,
		Kinds:   parsed.kinds,
	})
	if result != nil {
		var renderErr error
		switch parsed.format {
		case outputFormatJSON:
			renderErr = render.WriteJSON(stdout, result)
		case outputFormatHuman:
			renderErr = render.WriteHuman(stdout, result, render.HumanOptions{Grouped: parsed.grouped})
			renderFailures(stderr, result.Failures)
		}
		return errors.Join(renderErr, err)
	}
	return err
}

type parsedArgs struct {
	query      string
	configPath string
	sources    []string
	limit      uint32
	kinds      []string
	grouped    bool
	format     outputFormat
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
		return parsedArgs{}, errors.New("usage: recall [--config path] [--source provider[,provider] ...] [--limit n] QUERY")
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
	return parsedArgs{query: query, configPath: *configPath, sources: sources, limit: uint32(*limit), kinds: kinds, grouped: *grouped, format: parsedFormat}, nil
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
