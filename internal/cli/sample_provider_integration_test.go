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
	configPath := filepath.Join(configDir, "00-example.txtpb")
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
				Results []struct {
					ID       string `json:"id"`
					Selector string `json:"selector"`
					Fields   []struct {
						Key       string `json:"key"`
						Text      string `json:"text"`
						Timestamp string `json:"timestamp"`
					} `json:"fields"`
					Targets []struct {
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
					Format struct {
						TitleFields  []string `json:"title_fields"`
						DetailFields []string `json:"detail_fields"`
					} `json:"format"`
				} `json:"results"`
			} `json:"response"`
		} `json:"responses"`
		BlendedResults []struct {
			ProviderID   string  `json:"provider_id"`
			ProviderRank int     `json:"provider_rank"`
			BlendedScore float64 `json:"blended_score"`
			Result       struct {
				ID string `json:"id"`
			} `json:"result"`
		} `json:"blended_results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal recall JSON output: %v\n%s", err, stdout.String())
	}
	if len(payload.Responses) != 1 || payload.Responses[0].ProviderID != "example" {
		t.Fatalf("responses = %#v, want one example provider response", payload.Responses)
	}
	results := payload.Responses[0].Response.Results
	if len(results) != 1 {
		t.Fatalf("provider result count = %d, want 1", len(results))
	}
	result := results[0]
	if result.ID != "rollout-note" || result.Selector != "note:content" || fieldText(result.Fields, "title") != "Sample rollout note" {
		t.Fatalf("sample provider result did not preserve search contract fields: %#v", result)
	}
	if fieldText(result.Fields, "snippet") == "" || fieldTimestamp(result.Fields, "updated_at") == "" {
		t.Fatalf("sample provider result did not expose snippet and updated_at fields: %#v", result.Fields)
	}
	if len(result.Format.TitleFields) != 1 || result.Format.TitleFields[0] != "title" || len(result.Format.DetailFields) != 2 || result.Format.DetailFields[0] != "updated_at" || result.Format.DetailFields[1] != "snippet" {
		t.Fatalf("sample provider result did not expose display format: %#v", result.Format)
	}
	if len(result.Targets) < 2 || result.Targets[0].File.Path == "" || result.Targets[1].URI.URI == "" {
		t.Fatalf("sample provider result did not exercise primary and secondary open targets: %#v", result.Targets)
	}
	if result.Group.Key != "fixture:procedures" || result.Group.Title != "Procedure notes" {
		t.Fatalf("sample provider result did not exercise grouping: %#v", result.Group)
	}
	if len(payload.BlendedResults) != 1 || payload.BlendedResults[0].ProviderID != "example" || payload.BlendedResults[0].ProviderRank != 1 || payload.BlendedResults[0].Result.ID != "rollout-note" {
		t.Fatalf("blended results = %#v, want one ranked example result", payload.BlendedResults)
	}
	if payload.BlendedResults[0].BlendedScore <= 0 {
		t.Fatalf("blended score = %f, want positive", payload.BlendedResults[0].BlendedScore)
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

func fieldText(fields []struct {
	Key       string `json:"key"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}, key string) string {
	for _, field := range fields {
		if field.Key == key {
			return field.Text
		}
	}
	return ""
}

func fieldTimestamp(fields []struct {
	Key       string `json:"key"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}, key string) string {
	for _, field := range fields {
		if field.Key == key {
			return field.Timestamp
		}
	}
	return ""
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
