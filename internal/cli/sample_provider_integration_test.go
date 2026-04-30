package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSampleProviderExercisesStdioSearchAPI(t *testing.T) {
	providerBinary := buildSampleProvider(t)
	xdgConfigHome := t.TempDir()
	configDir := filepath.Join(xdgConfigHome, "recall")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.txtpb")
	config := `
providers {
  id: "example"
  enabled: true
  weight: 1.0
  timeout_ms: 5000
  default_limit: 10
  transports {
    stdio {
      command: "` + providerBinary + `"
    }
  }
}
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write recall config: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	t.Setenv("HOME", t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := App{Stdout: &stdout, Stderr: &stderr}
	if err := app.Run(context.Background(), []string{"--format", "json", "rollout"}); err != nil {
		t.Fatalf("recall sample-provider search failed: %v\nstderr: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var payload struct {
		Responses []struct {
			ProviderID string `json:"provider_id"`
			Response   struct {
				Hits []struct {
					ID       string `json:"id"`
					Selector string `json:"selector"`
					Title    string `json:"title"`
					Targets  []struct {
						URI struct {
							URI string `json:"uri"`
						} `json:"uri"`
						File struct {
							Path string `json:"path"`
						} `json:"file"`
					} `json:"targets"`
					Group struct {
						Key   string `json:"key"`
						Title string `json:"title"`
					} `json:"group"`
				} `json:"hits"`
			} `json:"response"`
		} `json:"responses"`
		BlendedHits []struct {
			ProviderID   string  `json:"provider_id"`
			ProviderRank int     `json:"provider_rank"`
			BlendedScore float64 `json:"blended_score"`
			Hit          struct {
				ID string `json:"id"`
			} `json:"hit"`
		} `json:"blended_hits"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal recall JSON output: %v\n%s", err, stdout.String())
	}
	if len(payload.Responses) != 1 || payload.Responses[0].ProviderID != "example" {
		t.Fatalf("responses = %#v, want one example provider response", payload.Responses)
	}
	hits := payload.Responses[0].Response.Hits
	if len(hits) != 1 {
		t.Fatalf("provider hit count = %d, want 1", len(hits))
	}
	if hits[0].ID != "example:rollout-note" || hits[0].Selector != "note:content" || hits[0].Title != "Sample rollout note" {
		t.Fatalf("sample provider hit did not preserve search contract fields: %#v", hits[0])
	}
	if len(hits[0].Targets) < 2 || hits[0].Targets[0].File.Path == "" || hits[0].Targets[1].URI.URI == "" {
		t.Fatalf("sample provider hit did not exercise primary and secondary open targets: %#v", hits[0].Targets)
	}
	if hits[0].Group.Key != "fixture:procedures" || hits[0].Group.Title != "Procedure notes" {
		t.Fatalf("sample provider hit did not exercise grouping: %#v", hits[0].Group)
	}
	if len(payload.BlendedHits) != 1 || payload.BlendedHits[0].ProviderID != "example" || payload.BlendedHits[0].ProviderRank != 1 || payload.BlendedHits[0].Hit.ID != "example:rollout-note" {
		t.Fatalf("blended hits = %#v, want one ranked example hit", payload.BlendedHits)
	}
	if payload.BlendedHits[0].BlendedScore <= 0 {
		t.Fatalf("blended score = %f, want positive", payload.BlendedHits[0].BlendedScore)
	}

	stdout.Reset()
	stderr.Reset()
	if err := app.Run(context.Background(), []string{"-ls"}); err != nil {
		t.Fatalf("recall sample-provider list failed: %v\nstderr: %s", err, stderr.String())
	}
	listOutput := stdout.String()
	for _, want := range []string{"SELECTORS", "example:note:content", "example:event:content"} {
		if !strings.Contains(listOutput, want) {
			t.Fatalf("provider list output %q does not contain %q", listOutput, want)
		}
	}
}

func buildSampleProvider(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	binaryPath := filepath.Join(t.TempDir(), "recall-example-provider")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/recall-example-provider")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build recall-example-provider: %v\n%s", err, string(output))
	}
	return binaryPath
}
