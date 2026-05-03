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
  transports {
    stdio {
      command: "recall-example-provider"
      args: "--fixture"
      env {
        key: "RECALL_EXAMPLE_FIXTURE"
        value: "builtin"
      }
    }
  }
}
providers {
  id: "remote-source"
  enabled: true
  weight: 1.0
  timeout_ms: 2500
  default_limit: 30
  transports {
    grpc {
      endpoint: "dns:///source-search.internal:443"
    }
  }
}
openers {
  id: "code-editor"
  sources: "example"
  selectors: "file:content"
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
	if providers[0].GetId() != "example" || providers[0].GetTransports()[0].GetStdio().GetCommand() != "recall-example-provider" {
		t.Fatalf("first provider did not round trip as stdio example: %#v", providers[0])
	}
	if providers[1].GetId() != "remote-source" || providers[1].GetTransports()[0].GetGrpc().GetEndpoint() != "dns:///source-search.internal:443" {
		t.Fatalf("second provider did not round trip as grpc remote-source: %#v", providers[1])
	}
	if len(cfg.GetOpeners()) != 1 || cfg.GetOpeners()[0].GetId() != "code-editor" || cfg.GetOpeners()[0].GetCommand() != "editor" {
		t.Fatalf("opener did not round trip: %#v", cfg.GetOpeners())
	}
}

func TestLoadFileMergesConfigDFragmentsInLexicalOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txtpb")
	if err := os.WriteFile(path, []byte(providerConfigBlock("core")), 0o600); err != nil {
		t.Fatalf("write base config: %v", err)
	}
	fragmentDir := filepath.Join(dir, "config.d")
	if err := os.Mkdir(fragmentDir, 0o700); err != nil {
		t.Fatalf("mkdir config.d: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fragmentDir, "20-zeta.txtpb"), []byte(providerConfigBlock("zeta")), 0o600); err != nil {
		t.Fatalf("write zeta fragment: %v", err)
	}
	alphaPath := filepath.Join(fragmentDir, "10-alpha.txtpb")
	if err := os.WriteFile(alphaPath, []byte(providerConfigBlock("alpha")), 0o600); err != nil {
		t.Fatalf("write alpha fragment: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fragmentDir, "README.md"), []byte("ignored"), 0o600); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	loaded, err := LoadFileWithLocations(path)
	if err != nil {
		t.Fatalf("LoadFileWithLocations returned error: %v", err)
	}

	providers := loaded.Config.GetProviders()
	if len(providers) != 3 {
		t.Fatalf("provider count = %d, want base plus two fragments", len(providers))
	}
	for index, want := range []string{"core", "alpha", "zeta"} {
		if providers[index].GetId() != want {
			t.Fatalf("providers[%d].id = %q, want %q", index, providers[index].GetId(), want)
		}
	}
	if location := loaded.ProviderLocations["alpha"]; location.Path != alphaPath || location.Line != 1 || location.Column != 1 {
		t.Fatalf("alpha location = %#v, want fragment path line 1 column 1", location)
	}
}

func TestLoadFileMergesConfigDFragmentSymlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.txtpb")
	if err := os.WriteFile(path, []byte(providerConfigBlock("core")), 0o600); err != nil {
		t.Fatalf("write base config: %v", err)
	}
	fragmentDir := filepath.Join(dir, "config.d")
	if err := os.Mkdir(fragmentDir, 0o700); err != nil {
		t.Fatalf("mkdir config.d: %v", err)
	}
	targetPath := filepath.Join(dir, "work-fragment.txtpb")
	if err := os.WriteFile(targetPath, []byte(providerConfigBlock("linked-work")), 0o600); err != nil {
		t.Fatalf("write linked fragment target: %v", err)
	}
	if err := os.Symlink(targetPath, filepath.Join(fragmentDir, "10-work.txtpb")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	providers := cfg.GetProviders()
	if len(providers) != 2 {
		t.Fatalf("provider count = %d, want base plus linked fragment", len(providers))
	}
	if providers[0].GetId() != "core" || providers[1].GetId() != "linked-work" {
		t.Fatalf("providers = %#v, want core then linked-work", providers)
	}
}

func TestLoadFileAcceptsConfigDirectoryPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.txtpb"), []byte(providerConfigBlock("core")), 0o600); err != nil {
		t.Fatalf("write base config: %v", err)
	}

	cfg, err := LoadFile(dir)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if len(cfg.GetProviders()) != 1 || cfg.GetProviders()[0].GetId() != "core" {
		t.Fatalf("providers = %#v, want core provider from directory config", cfg.GetProviders())
	}
}

func TestLoadFileWithLocationsRecordsProviderBlockLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txtpb")
	lines := []string{
		"# synthetic registry",
		"providers {",
		`  id: "example"`,
		"  enabled: true",
		"  weight: 1.0",
		"  timeout_ms: 1500",
		"  default_limit: 30",
		`  transports { stdio { command: "provider" } }`,
		"}",
		"",
		"providers {",
		`  id: "remote-source"`,
		"  enabled: true",
		"  weight: 1.0",
		"  timeout_ms: 2500",
		"  default_limit: 30",
		`  transports { grpc { endpoint: "dns:///source-search.internal:443" } }`,
		"}",
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loaded, err := LoadFileWithLocations(path)
	if err != nil {
		t.Fatalf("LoadFileWithLocations returned error: %v", err)
	}
	if loaded.Config.GetProviders()[0].GetId() != "example" {
		t.Fatalf("config did not parse providers: %#v", loaded.Config.GetProviders())
	}
	for id, wantLine := range map[string]uint32{"example": 2, "remote-source": 11} {
		location := loaded.ProviderLocations[id]
		if location.Path != path || location.Line != wantLine || location.Column != 1 {
			t.Fatalf("location for %s = %#v, want path %q line %d column 1", id, location, path, wantLine)
		}
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
  transports { stdio { command: "recall-example-provider" } }
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

func providerConfigBlock(id string) string {
	return `providers {
  id: "` + id + `"
  enabled: true
  weight: 1.0
  timeout_ms: 1500
  default_limit: 30
  transports { stdio { command: "provider" } }
}
`
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
				Transports: []*configv1.Transport{
					{
						Transport: &configv1.Transport_Stdio{
							Stdio: &configv1.StdioTransport{
								Command: "",
								Env: map[string]string{
									"bad-env": "value",
								},
							},
						},
					},
				},
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
		"providers[0].transports[0].stdio.command",
		"bad-env",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("Validate error %q does not contain %q", message, want)
		}
	}
}
