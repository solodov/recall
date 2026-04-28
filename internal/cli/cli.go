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
	loadConfig := app.LoadConfig
	if loadConfig == nil {
		loadConfig = config.LoadDefault
	}
	search := app.Search
	if search == nil {
		search = orchestrator.Search
	}

	parsed, err := parseArgs(args, stderr)
	if err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	result, err := search(ctx, cfg, parsed.query, orchestrator.Options{
		Sources: parsed.sources,
		Limit:   parsed.limit,
	})
	if result != nil {
		renderResult(stdout, result)
		renderFailures(stderr, result.Failures)
	}
	return err
}

type parsedArgs struct {
	query   string
	sources []string
	limit   uint32
}

func parseArgs(args []string, stderr io.Writer) (parsedArgs, error) {
	flags := flag.NewFlagSet("recall", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var sources stringListFlag
	flags.Var(&sources, "source", "comma-separated provider IDs to query")
	limit := flags.Uint("limit", 0, "override per-provider result limit")
	if err := flags.Parse(args); err != nil {
		return parsedArgs{}, err
	}

	query := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if query == "" {
		return parsedArgs{}, errors.New("usage: recall [--source provider[,provider] ...] [--limit n] QUERY")
	}
	if *limit > uint(^uint32(0)) {
		return parsedArgs{}, fmt.Errorf("--limit %d exceeds uint32 maximum", *limit)
	}
	return parsedArgs{query: query, sources: sources, limit: uint32(*limit)}, nil
}

func renderResult(writer io.Writer, result *orchestrator.Result) {
	for _, providerResponse := range result.Responses {
		for _, hit := range providerResponse.Response.GetHits() {
			fmt.Fprintf(writer, "[%s] %s", providerResponse.ProviderID, hit.GetTitle())
			if kind := strings.TrimSpace(hit.GetKind()); kind != "" {
				fmt.Fprintf(writer, " (%s)", kind)
			}
			fmt.Fprintln(writer)
			if snippet := strings.TrimSpace(hit.GetSnippet()); snippet != "" {
				fmt.Fprintf(writer, "  %s\n", snippet)
			}
		}
		for _, warning := range providerResponse.Response.GetWarnings() {
			fmt.Fprintf(writer, "[%s] warning: %s\n", providerResponse.ProviderID, warning.GetMessage())
		}
	}
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
