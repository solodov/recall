package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	configv1 "github.com/solodov/recall/proto/recall/config/v1"
)

func TestDefaultPathUsesXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	t.Setenv("HOME", "/tmp/home")

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath returned error: %v", err)
	}

	want := filepath.Join("/tmp/xdg", "recall", "config.txtpb")
	if path != want {
		t.Fatalf("DefaultPath() = %q, want %q", path, want)
	}
}

func TestDefaultPathFallsBackToHomeConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home")

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath returned error: %v", err)
	}

	want := filepath.Join("/tmp/home", ".config", "recall", "config.txtpb")
	if path != want {
		t.Fatalf("DefaultPath() = %q, want %q", path, want)
	}
}

func TestLoadFileParsesProviderAvailabilityRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txtpb")
	contents := `
providers {
  id: "example"
  enabled: true
  weight: 1.0
  timeout_ms: 1500
  default_limit: 30
  stdio {
    command: "recall-example-provider"
    args: "--fixture"
    env {
      key: "RECALL_EXAMPLE_FIXTURE"
      value: "builtin"
    }
  }
}
providers {
  id: "remote-source"
  enabled: true
  weight: 1.0
  timeout_ms: 2500
  default_limit: 30
  grpc {
    endpoint: "dns:///source-search.internal:443"
  }
}
openers {
  id: "code-editor"
  sources: "example"
  kinds: "code_match"
  target_types: "file"
  command: "editor"
  args: "+{line}"
  args: "{path}"
}
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}

	providers := cfg.GetProviders()
	if len(providers) != 2 {
		t.Fatalf("provider count = %d, want 2", len(providers))
	}
	if providers[0].GetId() != "example" || providers[0].GetStdio().GetCommand() != "recall-example-provider" {
		t.Fatalf("first provider did not round trip as stdio example: %#v", providers[0])
	}
	if providers[1].GetId() != "remote-source" || providers[1].GetGrpc().GetEndpoint() != "dns:///source-search.internal:443" {
		t.Fatalf("second provider did not round trip as grpc remote-source: %#v", providers[1])
	}
	if len(cfg.GetOpeners()) != 1 || cfg.GetOpeners()[0].GetId() != "code-editor" || cfg.GetOpeners()[0].GetCommand() != "editor" {
		t.Fatalf("opener did not round trip: %#v", cfg.GetOpeners())
	}
}

func TestValidateRejectsProtocolOwnedFieldsBySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txtpb")
	contents := `
providers {
  id: "example"
  enabled: true
  service: "recall.search.v1.SearchProvider"
  weight: 1.0
  timeout_ms: 1500
  default_limit: 30
  stdio { command: "recall-example-provider" }
}
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile succeeded with service field in config")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadFile error = %q, want unknown field", err)
	}
}

func TestValidateRejectsInvalidOpener(t *testing.T) {
	err := Validate(&configv1.RecallConfig{Openers: []*configv1.Opener{{
		Id:          "bad opener",
		TargetTypes: []string{"unknown"},
	}}})
	if err == nil {
		t.Fatal("Validate succeeded with invalid opener")
	}
	message := err.Error()
	for _, want := range []string{"openers[0].id", "openers[0].command", "openers[0].target_types[0]"} {
		if !strings.Contains(message, want) {
			t.Fatalf("Validate error %q does not contain %q", message, want)
		}
	}
}

func TestValidateRejectsInvalidProviderConfig(t *testing.T) {
	cfg := &configv1.RecallConfig{
		Providers: []*configv1.Provider{
			{
				Id:           "bad env",
				Enabled:      true,
				Weight:       0,
				TimeoutMs:    0,
				DefaultLimit: 0,
				Transport: &configv1.Provider_Stdio{Stdio: &configv1.StdioTransport{
					Command: "",
					Env: map[string]string{
						"bad-env": "value",
					},
				}},
			},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate succeeded with invalid config")
	}
	message := err.Error()
	for _, want := range []string{
		"providers[0].id",
		"providers[0].weight",
		"providers[0].timeout_ms",
		"providers[0].default_limit",
		"providers[0].stdio.command",
		"bad-env",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("Validate error %q does not contain %q", message, want)
		}
	}
}
