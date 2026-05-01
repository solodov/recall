package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solodov/recall/internal/normalize"
	"github.com/solodov/recall/internal/orchestrator"
	runtimepkg "github.com/solodov/recall/internal/runtime"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
)

func TestRunLoadsConfigSearchesAndRendersResults(t *testing.T) {
	cfg := &configv1.RecallConfig{}
	var receivedQuery string
	var receivedOptions orchestrator.Options
	app := App{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		LoadConfig: func() (*configv1.RecallConfig, error) {
			return cfg, nil
		},
		NewRuntime: newTestRuntime,
		Search: func(_ runtimepkg.Context, gotCfg *configv1.RecallConfig, query string, options orchestrator.Options) (*orchestrator.Result, error) {
			if gotCfg != cfg {
				t.Fatalf("Search received cfg %#v, want injected cfg", gotCfg)
			}
			receivedQuery = query
			receivedOptions = options
			result := testSearchResult("example:1", "note:content", "Example result", "matched text")
			return &orchestrator.Result{Responses: []orchestrator.ProviderResponse{{
				ProviderID: "example",
				Results: []normalize.Result{{
					ProviderID:   "example",
					ProviderRank: 1,
					Result:       result,
				}},
			}}}, nil
		},
	}
	stdout := app.Stdout.(*bytes.Buffer)

	if err := app.Run(context.Background(), []string{"--selector", "source-a,source-b", "--limit", "7", "sample", "query"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if receivedQuery != "sample query" {
		t.Fatalf("query = %q, want joined query", receivedQuery)
	}
	if got := strings.Join(receivedOptions.Selectors, ","); got != "source-a,source-b" {
		t.Fatalf("selectors = %q, want source-a,source-b", got)
	}
	if receivedOptions.Limit != 7 {
		t.Fatalf("limit = %d, want 7", receivedOptions.Limit)
	}
	output := stripTerminalEscapes(stdout.String())
	for _, want := range []string{"[example:note:content] Results", "  Example result", "    snippet: matched text"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout %q does not contain %q", output, want)
		}
	}
}

func TestRunLoadsExplicitConfigPath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.txtpb")
	if err := os.WriteFile(configPath, []byte(`
providers {
  id: "configured"
  enabled: true
  weight: 1.0
  timeout_ms: 1500
  default_limit: 10
  transports { stdio { command: "provider" } }
}
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	stdout := &bytes.Buffer{}
	var providerCount int
	app := App{
		Stdout:     stdout,
		Stderr:     &bytes.Buffer{},
		NewRuntime: newTestRuntime,
		Search: func(_ runtimepkg.Context, cfg *configv1.RecallConfig, _ string, _ orchestrator.Options) (*orchestrator.Result, error) {
			providerCount = len(cfg.GetProviders())
			result := testSearchResult("configured:1", "note:content", "Configured result", "")
			return &orchestrator.Result{Responses: []orchestrator.ProviderResponse{{
				ProviderID: "configured",
				Results: []normalize.Result{{
					ProviderID:   "configured",
					ProviderRank: 1,
					Result:       result,
				}},
			}}}, nil
		},
	}

	if err := app.Run(context.Background(), []string{"--config", configPath, "query"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if providerCount != 1 {
		t.Fatalf("provider count = %d, want config loaded from --config", providerCount)
	}
	wantPath := strings.ReplaceAll(configPath, "/", "%2F")
	wantURL := "recall://open?column=1&line=2&path=" + wantPath + "&selector=note%3Acontent&source=configured&type=file&v=1"
	if !strings.Contains(stdout.String(), wantURL) {
		t.Fatalf("stdout %q does not contain config source link %q", stdout.String(), wantURL)
	}
}

func TestRunPassesSelectorsAsRecallRoutingOption(t *testing.T) {
	cfg := &configv1.RecallConfig{}
	var receivedOptions orchestrator.Options
	app := App{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		LoadConfig: func() (*configv1.RecallConfig, error) {
			return cfg, nil
		},
		NewRuntime: newTestRuntime,
		Search: func(_ runtimepkg.Context, _ *configv1.RecallConfig, _ string, options orchestrator.Options) (*orchestrator.Result, error) {
			receivedOptions = options
			return &orchestrator.Result{}, nil
		},
	}

	if err := app.Run(context.Background(), []string{"-s", "code:file:name", "--selector", "notes:note:content", "sample"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := strings.Join(receivedOptions.Selectors, ","); got != "code:file:name,notes:note:content" {
		t.Fatalf("selectors = %q, want code:file:name,notes:note:content", got)
	}
}

func TestRunAcceptsSourceFormatAndLimitShorthands(t *testing.T) {
	cfg := &configv1.RecallConfig{}
	stdout := &bytes.Buffer{}
	var receivedQuery string
	var receivedOptions orchestrator.Options
	app := App{
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
		LoadConfig: func() (*configv1.RecallConfig, error) {
			return cfg, nil
		},
		NewRuntime: newTestRuntime,
		Search: func(_ runtimepkg.Context, _ *configv1.RecallConfig, query string, options orchestrator.Options) (*orchestrator.Result, error) {
			receivedQuery = query
			receivedOptions = options
			return &orchestrator.Result{}, nil
		},
	}

	if err := app.Run(context.Background(), []string{"-s", "code", "-f", "json", "-l", "3", "sample", "type:kotlin"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if receivedQuery != "sample type:kotlin" {
		t.Fatalf("query = %q, want provider query", receivedQuery)
	}
	if got := strings.Join(receivedOptions.Selectors, ","); got != "code" {
		t.Fatalf("selectors = %q, want code", got)
	}
	if receivedOptions.Limit != 3 {
		t.Fatalf("limit = %d, want shorthand limit", receivedOptions.Limit)
	}
	if !strings.Contains(stdout.String(), "\"responses\"") {
		t.Fatalf("stdout = %q, want JSON output", stdout.String())
	}
}

func TestRunSuppressesProviderFailureDetailsOnDefaultHumanOutput(t *testing.T) {
	stderr := &bytes.Buffer{}
	app := App{
		Stdout:     &bytes.Buffer{},
		Stderr:     stderr,
		LoadConfig: func() (*configv1.RecallConfig, error) { return &configv1.RecallConfig{}, nil },
		NewRuntime: newTestRuntime,
		Search: func(runtimepkg.Context, *configv1.RecallConfig, string, orchestrator.Options) (*orchestrator.Result, error) {
			return &orchestrator.Result{Failures: []orchestrator.ProviderFailure{{ProviderID: "bad", Err: errors.New("boom")}}}, errors.New("all selected providers failed")
		},
	}

	err := app.Run(context.Background(), []string{"query"})
	if err == nil {
		t.Fatal("Run succeeded despite search error")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want no provider diagnostics by default", stderr.String())
	}
}

func TestRunRejectsMissingQueryBeforeLoadingConfig(t *testing.T) {
	loaded := false
	app := App{
		Stderr: &bytes.Buffer{},
		LoadConfig: func() (*configv1.RecallConfig, error) {
			loaded = true
			return &configv1.RecallConfig{}, nil
		},
	}

	err := app.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("Run succeeded without query")
	}
	for _, want := range []string{"missing query", "configured in your provider registry", "recall -ls", "--selector/-s selects", "provider operators like -in:test"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing-query error = %q, want %q", err.Error(), want)
		}
	}
	if loaded {
		t.Fatal("config was loaded before query validation")
	}
}

func TestRunHelpShowsExamplesAndProviderListing(t *testing.T) {
	stdout := &bytes.Buffer{}
	loaded := false
	app := App{
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
		LoadConfig: func() (*configv1.RecallConfig, error) {
			loaded = true
			return &configv1.RecallConfig{}, nil
		},
	}

	if err := app.Run(context.Background(), []string{"--help"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"recall searches configured personal-search providers", "code, notes, calendars", "Selectors:", "--selector/-s selects", "Examples:", "recall -ls", "-s code", "code:file:name", "-f json", "-l 20", "--list-sources", "alias: -ls", "-g, --grouped", "default; use --grouped=false"} {
		if !strings.Contains(output, want) {
			t.Fatalf("help output %q does not contain %q", output, want)
		}
	}
	if loaded {
		t.Fatal("config was loaded for --help")
	}
}

func TestRunTreatsDashPrefixedQueryTermsAsQuery(t *testing.T) {
	cfg := &configv1.RecallConfig{}
	var receivedQuery string
	app := App{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		LoadConfig: func() (*configv1.RecallConfig, error) {
			return cfg, nil
		},
		NewRuntime: newTestRuntime,
		Search: func(_ runtimepkg.Context, _ *configv1.RecallConfig, query string, _ orchestrator.Options) (*orchestrator.Result, error) {
			receivedQuery = query
			return &orchestrator.Result{}, nil
		},
	}

	if err := app.Run(context.Background(), []string{"foo", "-in:test", "-ls"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if receivedQuery != "foo -in:test -ls" {
		t.Fatalf("query = %q, want provider-owned operators preserved", receivedQuery)
	}
}

func TestRunListsConfiguredProviders(t *testing.T) {
	stdout := &bytes.Buffer{}
	searched := false
	app := App{
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
		LoadConfig: func() (*configv1.RecallConfig, error) {
			return &configv1.RecallConfig{Providers: []*configv1.Provider{
				{
					Id:           "code",
					Enabled:      true,
					Weight:       1.5,
					TimeoutMs:    5000,
					DefaultLimit: 50,
					Transports: []*configv1.Transport{
						{
							Transport: &configv1.Transport_Stdio{
								Stdio: &configv1.StdioTransport{
									Command: "recall-ripgrep-provider",
									Args:    []string{"--root", "/repo/code"},
								},
							},
						},
					},
				},
				{
					Id:           "remote",
					Enabled:      false,
					Weight:       1,
					TimeoutMs:    1000,
					DefaultLimit: 10,
					Transports:   []*configv1.Transport{grpcConfigTransport("dns:///search:443")},
				},
			}}, nil
		},
		Search: func(runtimepkg.Context, *configv1.RecallConfig, string, orchestrator.Options) (*orchestrator.Result, error) {
			searched = true
			return nil, nil
		},
		ListCapabilities: func(_ context.Context, cfg *configv1.RecallConfig) (map[string]ProviderCapabilities, error) {
			if len(cfg.GetProviders()) != 2 {
				t.Fatalf("provider count = %d, want 2", len(cfg.GetProviders()))
			}
			return map[string]ProviderCapabilities{
				"code": {Selectors: []string{"code:file:name", "code:file:content"}},
			}, nil
		},
	}

	if err := app.Run(context.Background(), []string{"--config", "ignored.txtpb", "-ls"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if searched {
		t.Fatal("search runner was called for providers command")
	}
	output := stdout.String()
	for _, want := range []string{"ID", "SELECTORS", "code", "enabled", "1.50", "50", "5000ms", "stdio", "recall-ripgrep-provider --root /repo/code", "code:file:name,code:file:content", "remote", "disabled", "grpc", "dns:///search:443"} {
		if !strings.Contains(output, want) {
			t.Fatalf("provider list output %q does not contain %q", output, want)
		}
	}
}

func TestRunRejectsListSourcesWithQuery(t *testing.T) {
	app := App{Stderr: &bytes.Buffer{}}

	err := app.Run(context.Background(), []string{"--list-sources", "query"})
	if err == nil || !strings.Contains(err.Error(), "--list-sources cannot be combined with a query") {
		t.Fatalf("Run error = %v, want list-sources/query conflict", err)
	}
}

func TestRunPropagatesConfigLoadFailure(t *testing.T) {
	wantErr := errors.New("missing config")
	app := App{
		Stderr:     &bytes.Buffer{},
		NewRuntime: newTestRuntime,
		LoadConfig: func() (*configv1.RecallConfig, error) { return nil, wantErr },
	}

	err := app.Run(context.Background(), []string{"query"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run error = %v, want %v", err, wantErr)
	}
}

func newTestRuntime(ctx context.Context, _ RuntimeOptions) (runtimepkg.Context, error) {
	return runtimepkg.New(ctx, nil), nil
}

func grpcConfigTransport(endpoint string) *configv1.Transport {
	return &configv1.Transport{Transport: &configv1.Transport_Grpc{Grpc: &configv1.GrpcTransport{Endpoint: endpoint}}}
}

func testSearchResult(id string, selector string, title string, snippet string) *searchv1.SearchResponse_Result {
	fields := []*searchv1.SearchResponse_Result_Field{{
		Key:   "title",
		Value: &searchv1.SearchResponse_Result_Field_Text{Text: title},
	}}
	if snippet != "" {
		fields = append(fields, &searchv1.SearchResponse_Result_Field{
			Key:   "snippet",
			Value: &searchv1.SearchResponse_Result_Field_Text{Text: snippet},
		})
	}
	return &searchv1.SearchResponse_Result{
		Id:       id,
		Selector: selector,
		Fields:   fields,
		Format:   &searchv1.SearchResponse_Result_Format{TitleFields: []string{"title"}},
	}
}

func stripTerminalEscapes(text string) string {
	for {
		start := strings.Index(text, "\x1b]8;;")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], "\x1b\\")
		if end == -1 {
			break
		}
		text = text[:start] + text[start+end+2:]
	}
	for {
		start := strings.Index(text, "\x1b[")
		if start == -1 {
			return text
		}
		end := strings.Index(text[start:], "m")
		if end == -1 {
			return text
		}
		text = text[:start] + text[start+end+1:]
	}
}
