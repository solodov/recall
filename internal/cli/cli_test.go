package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"recall/internal/normalize"
	"recall/internal/orchestrator"
	runtimepkg "recall/internal/runtime"
	configv1 "recall/proto/recall/config/v1"
	searchv1 "recall/proto/recall/search/v1"
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
			return &orchestrator.Result{Responses: []orchestrator.ProviderResponse{{
				ProviderID: "example",
				Hits: []normalize.Hit{{
					ProviderID:   "example",
					ProviderRank: 1,
					Hit: &searchv1.SearchHit{
						Id:      "example:1",
						Kind:    "note",
						Title:   "Example result",
						Snippet: stringPtr("matched text"),
					},
				}},
			}}}, nil
		},
	}
	stdout := app.Stdout.(*bytes.Buffer)

	if err := app.Run(context.Background(), []string{"--source", "example,mail", "--limit", "7", "alice", "meeting"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if receivedQuery != "alice meeting" {
		t.Fatalf("query = %q, want joined query", receivedQuery)
	}
	if got := strings.Join(receivedOptions.Sources, ","); got != "example,mail" {
		t.Fatalf("sources = %q, want example,mail", got)
	}
	if receivedOptions.Limit != 7 {
		t.Fatalf("limit = %d, want 7", receivedOptions.Limit)
	}
	if len(receivedOptions.Kinds) != 0 {
		t.Fatalf("kinds = %#v, want none", receivedOptions.Kinds)
	}
	output := stdout.String()
	for _, want := range []string{"[example] Example result (note)", "matched text"} {
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
  stdio { command: "provider" }
}
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var providerCount int
	app := App{
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		NewRuntime: newTestRuntime,
		Search: func(_ runtimepkg.Context, cfg *configv1.RecallConfig, _ string, _ orchestrator.Options) (*orchestrator.Result, error) {
			providerCount = len(cfg.GetProviders())
			return &orchestrator.Result{}, nil
		},
	}

	if err := app.Run(context.Background(), []string{"--config", configPath, "query"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if providerCount != 1 {
		t.Fatalf("provider count = %d, want config loaded from --config", providerCount)
	}
}

func TestRunPassesKindAsRecallPostFilterOption(t *testing.T) {
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

	if err := app.Run(context.Background(), []string{"--kind", "event,email", "alice"}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := strings.Join(receivedOptions.Kinds, ","); got != "event,email" {
		t.Fatalf("kinds = %q, want event,email", got)
	}
}

func TestRunReportsPartialProviderFailuresOnStderr(t *testing.T) {
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
	if !strings.Contains(stderr.String(), "[bad] provider failed: boom") {
		t.Fatalf("stderr = %q, want provider failure", stderr.String())
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
	if loaded {
		t.Fatal("config was loaded before query validation")
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

func stringPtr(value string) *string {
	return &value
}
