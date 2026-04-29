package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/solodov/recall/internal/normalize"
	"github.com/solodov/recall/internal/orchestrator"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestWriteHumanRendersNamedURIsMetadataAndWarnings(t *testing.T) {
	var output bytes.Buffer
	result := renderFixtureResult()

	if err := WriteHuman(&output, result, HumanOptions{}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"[example] Deploy notes <file:///tmp/deploy.md> (note) 2026-04-28T09:30:00Z",
		"matched deploy context",
		"actions: web=https://example.invalid/deploy source=file:///tmp/source.md",
		"[example] warning: fixture warning",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output %q does not contain %q", text, want)
		}
	}
}

func TestWriteHumanGroupedUsesSourceAndProviderGroups(t *testing.T) {
	var output bytes.Buffer
	result := renderFixtureResult()

	if err := WriteHuman(&output, result, HumanOptions{Grouped: true}); err != nil {
		t.Fatalf("WriteHuman grouped returned error: %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"# example",
		"## Runbooks <file:///tmp/runbooks>",
		"  [example] Deploy notes <file:///tmp/deploy.md> (note) 2026-04-28T09:30:00Z",
		"## Ungrouped",
		"  [example] Loose hit <file:///tmp/loose.md> (note)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("grouped output %q does not contain %q", text, want)
		}
	}
}

func TestWriteJSONPreservesProviderResponsesAndFailures(t *testing.T) {
	var output bytes.Buffer
	result := renderFixtureResult()
	result.Failures = []orchestrator.ProviderFailure{{ProviderID: "bad", Err: assertErr("boom")}}

	if err := WriteJSON(&output, result); err != nil {
		t.Fatalf("WriteJSON returned error: %v", err)
	}

	var payload struct {
		Responses []struct {
			ProviderID string `json:"provider_id"`
			Response   struct {
				Hits []struct {
					ID         string `json:"id"`
					Kind       string `json:"kind"`
					Title      string `json:"title"`
					Snippet    string `json:"snippet"`
					OccurredAt string `json:"occurred_at"`
					Uris       []struct {
						Name string `json:"name"`
						URI  string `json:"uri"`
					} `json:"uris"`
					Group struct {
						Key   string `json:"key"`
						Title string `json:"title"`
					} `json:"group"`
				} `json:"hits"`
				Warnings []struct {
					Message string `json:"message"`
					Code    string `json:"code"`
				} `json:"warnings"`
			} `json:"response"`
		} `json:"responses"`
		Failures []struct {
			ProviderID string `json:"provider_id"`
			Error      string `json:"error"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal JSON output: %v\n%s", err, output.String())
	}

	if len(payload.Responses) != 1 || payload.Responses[0].ProviderID != "example" {
		t.Fatalf("responses metadata = %#v", payload.Responses)
	}
	hits := payload.Responses[0].Response.Hits
	if len(hits) != 2 {
		t.Fatalf("hit count = %d, want 2", len(hits))
	}
	if hits[0].ID != "deploy" || hits[0].Uris[0].URI != "file:///tmp/deploy.md" || hits[0].Group.Key != "runbooks" {
		t.Fatalf("first hit did not preserve fields: %#v", hits[0])
	}
	if payload.Responses[0].Response.Warnings[0].Code != "fixture_warning" {
		t.Fatalf("warnings were not preserved: %#v", payload.Responses[0].Response.Warnings)
	}
	if len(payload.Failures) != 1 || payload.Failures[0].ProviderID != "bad" || payload.Failures[0].Error != "boom" {
		t.Fatalf("failures = %#v", payload.Failures)
	}
}

func renderFixtureResult() *orchestrator.Result {
	occurredAt := timestamppb.New(time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC))
	deployHit := &searchv1.SearchHit{
		Id:         "deploy",
		Kind:       "note",
		Title:      "Deploy notes",
		Snippet:    proto.String("matched deploy context"),
		Score:      proto.Float64(1.2),
		OccurredAt: occurredAt,
		Uris: []*searchv1.NamedUri{
			{Name: "open", Uri: "file:///tmp/deploy.md"},
			{Name: "web", Uri: "https://example.invalid/deploy"},
			{Name: "source", Uri: "file:///tmp/source.md"},
		},
		Group: &searchv1.SearchGroup{
			Key:   "runbooks",
			Title: "Runbooks",
			Uris:  []*searchv1.NamedUri{{Name: "open", Uri: "file:///tmp/runbooks"}},
		},
	}
	looseHit := &searchv1.SearchHit{
		Id:    "loose",
		Kind:  "note",
		Title: "Loose hit",
		Uris:  []*searchv1.NamedUri{{Name: "open", Uri: "file:///tmp/loose.md"}},
	}
	warning := &searchv1.Warning{Message: "fixture warning", Code: proto.String("fixture_warning")}
	raw := &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{deployHit, looseHit}, Warnings: []*searchv1.Warning{warning}}
	return &orchestrator.Result{Responses: []orchestrator.ProviderResponse{{
		ProviderID: "example",
		Hits: []normalize.Hit{
			{ProviderID: "example", ProviderRank: 1, Hit: deployHit},
			{ProviderID: "example", ProviderRank: 2, Hit: looseHit},
		},
		Warnings: []normalize.Warning{{ProviderID: "example", Warning: warning}},
		Raw:      raw,
	}}}
}

type assertErr string

func (err assertErr) Error() string { return string(err) }
