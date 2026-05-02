// Package cli implements recall's query-first command-line boundary.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/solodov/recall/internal/config"
	"github.com/solodov/recall/internal/orchestrator"
	"github.com/solodov/recall/internal/render"
	"github.com/solodov/recall/internal/runtime"
	"github.com/solodov/recall/internal/searchclient"
	"github.com/solodov/recall/internal/tui"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/protobuf/proto"
)

// ConfigLoader loads the operator-owned provider registry for a recall run.
type ConfigLoader func() (*configv1.RecallConfig, error)

// SearchRunner executes the provider fan-out for a query.
type SearchRunner func(runtime.Context, *configv1.RecallConfig, string, orchestrator.Options) (*orchestrator.Result, error)

// RuntimeFactory builds the command runtime after flags have selected log paths
// and stderr verbosity.
type RuntimeFactory func(context.Context, RuntimeOptions) (runtime.Context, error)

// CapabilityLister returns provider-advertised selectors for list output.
type CapabilityLister func(context.Context, *configv1.RecallConfig) (map[string]ProviderCapabilities, error)

// TUIRunner starts the interactive search frontend.
type TUIRunner func(context.Context, tui.Options) error

// ProviderCapabilities contains one provider's advertised selector surfaces.
type ProviderCapabilities struct {
	Selectors []string
	Err       error
}

// RuntimeOptions contains command-line controlled debugging sinks.
type RuntimeOptions struct {
	LogPaths runtime.LogPaths
	LogLevel string
}

// App contains command dependencies so the query-first CLI can be tested
// without launching real provider processes.
type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	LoadConfig       ConfigLoader
	Search           SearchRunner
	ListCapabilities CapabilityLister
	NewRuntime       RuntimeFactory
	RunTUI           TUIRunner
}

// Run parses recall commands, loads provider config, dispatches searches, and
// renders provider-agnostic output.
func (app App) Run(ctx context.Context, args []string) error {
	stdin := app.Stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	stdout := app.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := app.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	options := commandOptions{logLevel: "off", format: string(outputFormatHuman), grouped: true}
	cmd := app.newRootCommand(stdin, stdout, stderr, &options)
	cmd.SetArgs(expandListSourcesAlias(args))
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(stdin)
	return cmd.ExecuteContext(ctx)
}

// expandListSourcesAlias rewrites recall's two-letter -ls alias before Cobra
// parses flags. pflag shorthands are single characters, so this keeps -s
// available for --selector while still offering a compact list-sources spelling.
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
	case "--config", "--log-path", "--perf-log-path", "--log-level", "--selector", "--limit", "--format", "-s", "-l", "-f":
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
	selectors   stringListFlag
	limit       uint32
	grouped     bool
	format      string
	listSources bool
}

func (app App) newRootCommand(stdin io.Reader, stdout io.Writer, stderr io.Writer, options *commandOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recall [flags] [QUERY]",
		Short: "Search configured personal-search providers",
		Long: strings.TrimSpace(`recall searches configured personal-search providers, such as code, notes, calendars, or mail providers. It sends your query to enabled providers, blends provider-local ranks, and renders one combined result list.

The root command is query-first: all positional arguments are joined into the provider query. With no query in an interactive terminal, recall opens the search TUI. Put recall flags before the query when the query contains provider-owned operators like -in:test. Use -ls/--list-sources to see which corpora are configured.

Selectors:
  --selector/-s selects providers or provider surfaces, such as code or code:file:content.`),
		Example: strings.TrimSpace(`recall
recall -ls
recall sample
recall -s code "foo -in:test"
recall -s code:file:name router
recall -s code:file:content -l 20 router
recall -f json sample
recall --config ./examples/config.txtpb sample`),
		Args: func(cmd *cobra.Command, args []string) error {
			if options.listSources && len(args) > 0 {
				return errors.New("--list-sources cannot be combined with a query")
			}
			if len(args) == 0 && !options.listSources && !app.canStartTUI(stdin, stdout) {
				return missingQueryError()
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.listSources {
				loaded, err := app.loadConfigWithLocations(options.configPath)
				if err != nil {
					return err
				}
				capabilities, err := app.listCapabilities(cmd.Context(), loaded.Config)
				if err != nil {
					return err
				}
				return renderProviders(stdout, loaded.Config, capabilities)
			}
			if len(args) == 0 {
				return app.runTUI(cmd.Context(), stdin, stdout, *options)
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
	cmd.Flags().VarP(&options.selectors, "selector", "s", "comma-separated selectors to query, e.g. code, code:file:name, or code:file:content")
	cmd.Flags().Uint32VarP(&options.limit, "limit", "l", 0, "override per-provider result limit")
	cmd.Flags().BoolVarP(&options.grouped, "grouped", "g", true, "group human output by source and provider group (default; use --grouped=false for flat output)")
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
  recall                                   open the interactive search TUI in a terminal
  recall -ls                               list configured corpora/providers
  recall sample                            search all enabled providers
  recall -s code "foo -in:test"             search only the code corpus
  recall -s code:file:name foo              show matching file paths in code
  recall -s code:file:content -l 20 foo     show up to 20 content matches per provider
  recall -f json sample                    emit machine-readable results

Selectors:
  --selector/-s selects providers/corpora before search, such as code, or provider surfaces such as code:file:content.

Tips:
  Use provider IDs from "recall -ls" with --selector/-s.
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

	var loaded config.LoadedConfig
	if err := span.Measure("load_config", func() error {
		var err error
		loaded, err = app.loadConfigWithLocations(options.configPath)
		return err
	}); err != nil {
		return err
	}
	cfg := loaded.Config
	var result *orchestrator.Result
	searchErr := span.Measure("search", func() error {
		var err error
		result, err = search(run, cfg, options.query, orchestrator.Options{
			Selectors: options.selectors,
			Limit:     options.limit,
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
				return render.WriteHuman(stdout, result, render.HumanOptions{
					Ungrouped:             !options.grouped,
					ProviderConfigTargets: providerConfigTargets(loaded.ProviderLocations),
				})
			}
			return nil
		}, "format", string(parsedFormat), "grouped", options.grouped)
		return errors.Join(renderErr, searchErr)
	}
	return searchErr
}

func (app App) runTUI(ctx context.Context, stdin io.Reader, stdout io.Writer, options commandOptions) error {
	runtimeOptions, err := options.runtimeOptions()
	if err != nil {
		return err
	}
	run, err := app.newRuntime(ctx, runtimeOptions)
	if err != nil {
		return err
	}
	loaded, err := app.loadConfigWithLocations(options.configPath)
	if err != nil {
		return err
	}
	search := app.Search
	if search == nil {
		search = orchestrator.Search
	}
	runner := app.RunTUI
	if runner == nil {
		runner = tui.Run
	}
	return runner(ctx, tui.Options{
		Config:  loaded.Config,
		Runtime: run,
		Search:  tui.SearchFunc(search),
		Input:   stdin,
		Output:  stdout,
	})
}

func (app App) canStartTUI(stdin io.Reader, stdout io.Writer) bool {
	if app.RunTUI != nil {
		return true
	}
	input, inputOK := stdin.(*os.File)
	output, outputOK := stdout.(*os.File)
	return inputOK && outputOK && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
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
	loaded, err := app.loadConfigWithLocations(configPath)
	if err != nil {
		return nil, err
	}
	return loaded.Config, nil
}

func (app App) loadConfigWithLocations(configPath string) (config.LoadedConfig, error) {
	if app.LoadConfig != nil {
		cfg, err := app.LoadConfig()
		if err != nil {
			return config.LoadedConfig{}, err
		}
		return config.LoadedConfig{Config: cfg}, nil
	}
	if strings.TrimSpace(configPath) != "" {
		return config.LoadFileWithLocations(configPath)
	}
	return config.LoadDefaultWithLocations()
}

func (app App) listCapabilities(ctx context.Context, cfg *configv1.RecallConfig) (map[string]ProviderCapabilities, error) {
	lister := app.ListCapabilities
	if lister == nil {
		lister = listProviderCapabilities
	}
	return lister(ctx, cfg)
}

func listProviderCapabilities(ctx context.Context, cfg *configv1.RecallConfig) (map[string]ProviderCapabilities, error) {
	if cfg == nil {
		return nil, errors.New("recall config is nil")
	}
	capabilities := make(map[string]ProviderCapabilities, len(cfg.GetProviders()))
	for _, provider := range cfg.GetProviders() {
		if provider == nil || strings.TrimSpace(provider.GetId()) == "" || !provider.GetEnabled() {
			continue
		}
		providerID := provider.GetId()
		client, err := searchclient.NewProviderClient(provider, searchclient.ProviderClientOptions{})
		if err != nil {
			capabilities[providerID] = ProviderCapabilities{Err: err}
			continue
		}
		response, err := client.ListCapabilities(ctx, &searchv1.ListCapabilitiesRequest{})
		if closer, ok := client.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		if err != nil {
			capabilities[providerID] = ProviderCapabilities{Err: err}
			continue
		}
		capabilities[providerID] = ProviderCapabilities{Selectors: fullSelectors(providerID, response.GetSurfaces())}
	}
	return capabilities, nil
}

func fullSelectors(providerID string, surfaces []*searchv1.SearchSurface) []string {
	selectors := make([]string, 0, len(surfaces))
	for _, surface := range surfaces {
		if surface == nil {
			continue
		}
		selector := strings.TrimSpace(surface.GetSelector())
		if selector == "" {
			continue
		}
		selectors = append(selectors, providerID+":"+selector)
	}
	return selectors
}

func providerConfigTargets(locations map[string]config.Location) map[string]*searchv1.OpenTarget {
	if len(locations) == 0 {
		return nil
	}
	targets := make(map[string]*searchv1.OpenTarget, len(locations))
	for providerID, location := range locations {
		if strings.TrimSpace(location.Path) == "" {
			continue
		}
		file := &searchv1.FileTarget{Path: location.Path}
		if location.Line > 0 {
			file.Line = proto.Uint32(location.Line)
		}
		if location.Column > 0 {
			file.Column = proto.Uint32(location.Column)
		}
		targets[providerID] = &searchv1.OpenTarget{Target: &searchv1.OpenTarget_File{File: file}}
	}
	return targets
}

func renderProviders(writer io.Writer, cfg *configv1.RecallConfig, capabilities map[string]ProviderCapabilities) error {
	if cfg == nil || len(cfg.GetProviders()) == 0 {
		_, err := fmt.Fprintln(writer, "No providers configured.")
		return err
	}

	tabWriter := tabwriter.NewWriter(writer, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tabWriter, "ID\tSTATUS\tWEIGHT\tLIMIT\tTIMEOUT\tTRANSPORTS\tTARGETS\tSELECTORS"); err != nil {
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
		if _, err := fmt.Fprintf(tabWriter, "%s\t%s\t%.2f\t%d\t%dms\t%s\t%s\t%s\n", provider.GetId(), status, provider.GetWeight(), provider.GetDefaultLimit(), provider.GetTimeoutMs(), transport, target, providerSelectors(provider, capabilities)); err != nil {
			return err
		}
	}
	return tabWriter.Flush()
}

func providerSelectors(provider *configv1.Provider, capabilities map[string]ProviderCapabilities) string {
	if provider == nil {
		return ""
	}
	if !provider.GetEnabled() {
		return "disabled"
	}
	capability, ok := capabilities[provider.GetId()]
	if !ok {
		return "unknown"
	}
	if capability.Err != nil {
		return "unavailable"
	}
	if len(capability.Selectors) == 0 {
		return "none"
	}
	return strings.Join(capability.Selectors, ",")
}

func providerTransport(provider *configv1.Provider) (string, string) {
	if provider == nil || len(provider.GetTransports()) == 0 {
		return "unknown", ""
	}

	names := make([]string, 0, len(provider.GetTransports()))
	targets := make([]string, 0, len(provider.GetTransports()))
	for _, transport := range provider.GetTransports() {
		name, target := transportSummary(transport)
		names = append(names, name)
		if target != "" {
			targets = append(targets, target)
		}
	}
	return strings.Join(names, ","), strings.Join(targets, " -> ")
}

func transportSummary(transport *configv1.Transport) (string, string) {
	if transport == nil {
		return "unknown", ""
	}
	switch transport := transport.GetTransport().(type) {
	case *configv1.Transport_Stdio:
		return "stdio", commandLine(transport.Stdio.GetCommand(), transport.Stdio.GetArgs())
	case *configv1.Transport_Grpc:
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
