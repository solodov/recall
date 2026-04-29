// Package cli implements recall's query-first command-line boundary.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/solodov/recall/internal/config"
	"github.com/solodov/recall/internal/orchestrator"
	"github.com/solodov/recall/internal/render"
	"github.com/solodov/recall/internal/runtime"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
	"github.com/spf13/cobra"
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

// Run parses recall commands, loads provider config, dispatches searches, and
// renders provider-agnostic output.
func (app App) Run(ctx context.Context, args []string) error {
	stdout := app.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := app.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	options := commandOptions{logLevel: "off", format: string(outputFormatHuman)}
	cmd := app.newRootCommand(stdout, stderr, &options)
	cmd.SetArgs(expandListSourcesAlias(args))
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(strings.NewReader(""))
	return cmd.ExecuteContext(ctx)
}

// expandListSourcesAlias rewrites recall's two-letter -ls alias before Cobra
// parses flags. pflag shorthands are single characters, so this keeps -s
// available for --source while still offering a compact list-sources spelling.
func expandListSourcesAlias(args []string) []string {
	normalized := append([]string{}, args...)
	for i := 0; i < len(normalized); i++ {
		arg := normalized[i]
		switch {
		case arg == "--":
			return normalized
		case arg == "-ls":
			normalized[i] = "--list-sources"
			continue
		case arg == "-" || !strings.HasPrefix(arg, "-"):
			return normalized
		case flagConsumesNextValue(arg):
			i++
		}
	}
	return normalized
}

// flagConsumesNextValue reports whether the pre-parser should skip the next
// argument because the current flag owns it as a value.
func flagConsumesNextValue(arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}
	switch arg {
	case "--config", "--log-path", "--perf-log-path", "--log-level", "--source", "--kind", "--limit", "--format", "-s", "-l", "-f":
		return true
	default:
		return false
	}
}

type commandOptions struct {
	query       string
	configPath  string
	logPath     string
	perfLogPath string
	logLevel    string
	sources     stringListFlag
	limit       uint32
	kinds       stringListFlag
	grouped     bool
	format      string
	listSources bool
}

func (app App) newRootCommand(stdout io.Writer, stderr io.Writer, options *commandOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recall [flags] QUERY",
		Short: "Search configured personal-search providers",
		Long: strings.TrimSpace(`recall searches configured personal-search providers, such as code, notes, calendars, or mail providers. It sends your query to enabled providers, blends provider-local ranks, and renders one combined result list.

The root command is query-first: all positional arguments are joined into the provider query. Put recall flags before the query when the query contains provider-owned operators like -in:test. Use -ls/--list-sources to see which corpora are configured.

Source vs kind:
  --source/-s selects which providers or corpora to query, such as code.
  --kind filters result types returned by providers, such as code_match or note.`),
		Example: strings.TrimSpace(`recall -ls
recall sample
recall -s code "foo -in:test"
recall --source code -g "foo -in:test"
recall --kind code_match -l 20 router
recall -f json sample
recall --config ./examples/config.txtpb sample`),
		Args: func(cmd *cobra.Command, args []string) error {
			if options.listSources && len(args) > 0 {
				return errors.New("--list-sources cannot be combined with a query")
			}
			if len(args) == 0 && !options.listSources {
				return missingQueryError()
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.listSources {
				cfg, err := app.loadConfig(options.configPath)
				if err != nil {
					return err
				}
				return renderProviders(stdout, cfg)
			}
			return app.runSearch(cmd.Context(), stdout, stderr, *options, args)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&options.configPath, "config", "", "provider registry path")
	cmd.PersistentFlags().StringVar(&options.logPath, "log-path", "", "main rotated log path")
	cmd.PersistentFlags().StringVar(&options.perfLogPath, "perf-log-path", "", "performance trace log path")
	cmd.PersistentFlags().StringVar(&options.logLevel, "log-level", "off", "also print logs to stderr at level: debug|info|warn|error|off")
	cmd.Flags().VarP(&options.sources, "source", "s", "comma-separated provider IDs/corpora to query")
	cmd.Flags().Var(&options.kinds, "kind", "comma-separated result kinds to keep after provider search, e.g. code_match or note")
	cmd.Flags().Uint32VarP(&options.limit, "limit", "l", 0, "override per-provider result limit")
	cmd.Flags().BoolVarP(&options.grouped, "grouped", "g", false, "group human output by source and provider group")
	cmd.Flags().StringVarP(&options.format, "format", "f", string(outputFormatHuman), "output format: human or json")
	cmd.Flags().BoolVar(&options.listSources, "list-sources", false, "list configured sources/providers and exit (alias: -ls)")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func missingQueryError() error {
	return errors.New(strings.TrimSpace(`missing query

recall searches the corpora configured in your provider registry, such as code,
notes, calendars, or mail. It sends your query to enabled providers, blends their
provider-local rankings, and renders one combined result list.

Common commands:
  recall -ls                               list configured corpora/providers
  recall sample                            search all enabled providers
  recall -s code "foo -in:test"             search only the code corpus
  recall --source code -g "foo"             group code matches by provider group
  recall --kind code_match -l 20 foo        show up to 20 code matches per provider
  recall -f json sample                    emit machine-readable results

Source vs kind:
  --source/-s selects providers/corpora before search, such as code.
  --kind filters provider result types after search, such as code_match or note.

Tips:
  Use provider IDs from "recall -ls" with --source/-s.
  Put recall flags before the query; provider operators like -in:test belong in the query.
  Run "recall --help" for all flags and examples.`))
}

func (app App) runSearch(ctx context.Context, stdout io.Writer, stderr io.Writer, options commandOptions, args []string) (runErr error) {
	search := app.Search
	if search == nil {
		search = orchestrator.Search
	}

	options.query = strings.TrimSpace(strings.Join(args, " "))
	if options.query == "" {
		return missingQueryError()
	}
	parsedFormat := outputFormat(options.format)
	switch parsedFormat {
	case outputFormatHuman, outputFormatJSON:
	default:
		return fmt.Errorf("unsupported --format %q; use human or json", options.format)
	}

	runtimeOptions, err := options.runtimeOptions()
	if err != nil {
		return err
	}
	run, err := app.newRuntime(ctx, runtimeOptions)
	if err != nil {
		return err
	}
	run = run.WithLogMeta("command", "search")
	run, span := run.StartOperation("recall.search", "query", options.query)
	defer func() {
		span.RecordError(runErr)
		span.End()
	}()

	var cfg *configv1.RecallConfig
	if err := span.Measure("load_config", func() error {
		var err error
		cfg, err = app.loadConfig(options.configPath)
		return err
	}); err != nil {
		return err
	}
	var result *orchestrator.Result
	searchErr := span.Measure("search", func() error {
		var err error
		result, err = search(run, cfg, options.query, orchestrator.Options{
			Sources: options.sources,
			Limit:   options.limit,
			Kinds:   options.kinds,
		})
		return err
	})
	if searchErr != nil && result == nil {
		return searchErr
	}
	if result != nil {
		var renderErr error
		renderErr = span.Measure("render", func() error {
			switch parsedFormat {
			case outputFormatJSON:
				return render.WriteJSON(stdout, result)
			case outputFormatHuman:
				return render.WriteHuman(stdout, result, render.HumanOptions{Grouped: options.grouped})
			}
			return nil
		}, "format", string(parsedFormat), "grouped", options.grouped)
		return errors.Join(renderErr, searchErr)
	}
	return searchErr
}

type outputFormat string

const (
	outputFormatHuman outputFormat = "human"
	outputFormatJSON  outputFormat = "json"
)

func (options commandOptions) runtimeOptions() (RuntimeOptions, error) {
	logPaths, err := runtime.DefaultLogPaths()
	if err != nil {
		return RuntimeOptions{}, err
	}
	if strings.TrimSpace(options.logPath) != "" {
		logPaths.Main = options.logPath
	}
	if strings.TrimSpace(options.perfLogPath) != "" {
		logPaths.Perf = options.perfLogPath
	}
	return RuntimeOptions{LogPaths: logPaths, LogLevel: options.logLevel}, nil
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

func renderProviders(writer io.Writer, cfg *configv1.RecallConfig) error {
	if cfg == nil || len(cfg.GetProviders()) == 0 {
		_, err := fmt.Fprintln(writer, "No providers configured.")
		return err
	}

	tabWriter := tabwriter.NewWriter(writer, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tabWriter, "ID\tSTATUS\tWEIGHT\tLIMIT\tTIMEOUT\tTRANSPORT\tTARGET"); err != nil {
		return err
	}
	for _, provider := range cfg.GetProviders() {
		if provider == nil {
			continue
		}
		status := "disabled"
		if provider.GetEnabled() {
			status = "enabled"
		}
		transport, target := providerTransport(provider)
		if _, err := fmt.Fprintf(tabWriter, "%s\t%s\t%.2f\t%d\t%dms\t%s\t%s\n", provider.GetId(), status, provider.GetWeight(), provider.GetDefaultLimit(), provider.GetTimeoutMs(), transport, target); err != nil {
			return err
		}
	}
	return tabWriter.Flush()
}

func providerTransport(provider *configv1.Provider) (string, string) {
	switch transport := provider.GetTransport().(type) {
	case *configv1.Provider_Stdio:
		return "stdio", commandLine(transport.Stdio.GetCommand(), transport.Stdio.GetArgs())
	case *configv1.Provider_Grpc:
		return "grpc", transport.Grpc.GetEndpoint()
	default:
		return "unknown", ""
	}
}

func commandLine(command string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	if command != "" {
		parts = append(parts, quoteShellish(command))
	}
	for _, arg := range args {
		parts = append(parts, quoteShellish(arg))
	}
	return strings.Join(parts, " ")
}

func quoteShellish(value string) string {
	if value == "" {
		return strconv.Quote(value)
	}
	for _, r := range value {
		if unicode.IsSpace(r) || strings.ContainsRune("'\"\\$`", r) {
			return strconv.Quote(value)
		}
	}
	return value
}

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func (values *stringListFlag) Type() string {
	return "stringList"
}
