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

func TestWriteHumanDefaultsToGroupedTerminalLayout(t *testing.T) {
	var output bytes.Buffer
	result := renderFixtureResult()

	if err := WriteHuman(&output, result, HumanOptions{}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	rawText := output.String()
	text := stripOSC8(rawText)
	for _, want := range []string{
		"[example:note] Procedure notes",
		"  Sample rollout note 2026-04-28T09:30:00Z",
		"    matched rollout context",
		"    actions: https file",
		"[example:note] Results",
		"  Loose hit",
		"[example] warning: fixture warning",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output %q does not contain %q", text, want)
		}
	}
	for _, unwanted := range []string{"# example", "## Procedure notes", "(note)"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("human output %q contains terminal noise %q", text, unwanted)
		}
	}
	if !strings.Contains(rawText, "recall://open?") || !strings.Contains(rawText, "type=file") {
		t.Fatalf("human output %q does not contain recall OSC8 target", rawText)
	}
}

func TestWriteHumanUngroupedRendersOpenTargetsMetadataAndWarnings(t *testing.T) {
	var output bytes.Buffer
	result := renderFixtureResult()

	if err := WriteHuman(&output, result, HumanOptions{Ungrouped: true}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	text := stripOSC8(output.String())
	for _, want := range []string{
		"[example] Sample rollout note (note) 2026-04-28T09:30:00Z",
		"matched rollout context",
		"actions: https file",
		"[example] warning: fixture warning",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ungrouped output %q does not contain %q", text, want)
		}
	}
}

func TestWriteHumanGroupedSuppressesGroupFileAction(t *testing.T) {
	var output bytes.Buffer
	result := orgEntryResult()

	if err := WriteHuman(&output, result, HumanOptions{}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	text := stripOSC8(output.String())
	for _, want := range []string{"[org:org_entry] notes.org", "  Matched org entry"} {
		if !strings.Contains(text, want) {
			t.Fatalf("grouped org output %q does not contain %q", text, want)
		}
	}
	if strings.Contains(text, "actions: file") || strings.Contains(text, "actions:") {
		t.Fatalf("grouped org output %q contains redundant file action", text)
	}
}

func TestWriteHumanGroupedLinksSourceLabelToProviderConfig(t *testing.T) {
	var output bytes.Buffer
	result := codeMatchResult()

	if err := WriteHuman(&output, result, HumanOptions{ProviderConfigTargets: map[string]*searchv1.OpenTarget{
		"code": fileTarget("/workspace/config/recall.txtpb", 17, 1),
	}}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	rawText := output.String()
	if !strings.Contains(stripOSC8(rawText), "[code:content] styleguide/kotlin/formatting.md") {
		t.Fatalf("grouped output %q does not keep source label shape", rawText)
	}
	wantURL := "recall://open?column=1&kind=code_match&line=17&path=%2Fworkspace%2Fconfig%2Frecall.txtpb&source=code&type=file&v=1"
	if !strings.Contains(rawText, wantURL) || !strings.Contains(rawText, groupLabelStyle+"[code:content]"+resetStyle) {
		t.Fatalf("grouped output %q does not link styled source label to config target %q", rawText, wantURL)
	}
}

func TestWriteHumanGroupedRendersFileLinesWithLinkedSnippets(t *testing.T) {
	var output bytes.Buffer
	result := codeMatchResult()

	if err := WriteHuman(&output, result, HumanOptions{}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	rawText := output.String()
	text := stripOSC8(rawText)
	for _, want := range []string{
		"[code:content] styleguide/kotlin/formatting.md",
		"     51: fun createSampleItem(flavor: Flavor): SampleItem = when(flavor) {",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("grouped code output %q does not contain %q", text, want)
		}
	}
	for _, unwanted := range []string{"# code", "## styleguide/kotlin/formatting.md", "[code] styleguide/kotlin/formatting.md:51:11", "file:///workspace/codebase/styleguide/kotlin/formatting.md", "(code_match)"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("grouped code output %q contains noisy metadata %q", text, unwanted)
		}
	}
	groupURL := "recall://open?kind=code_match&path=%2Fworkspace%2Fcodebase%2Fstyleguide%2Fkotlin%2Fformatting.md&source=code&type=file&v=1"
	if !strings.Contains(rawText, groupURL) {
		t.Fatalf("grouped code output %q does not contain file group recall target %q", rawText, groupURL)
	}
	if !strings.Contains(rawText, "recall://open?") || !strings.Contains(rawText, "line=51") || !strings.Contains(rawText, "column=11") {
		t.Fatalf("grouped code output %q does not contain line recall target", rawText)
	}
	if !strings.Contains(rawText, groupTitleStyle+"styleguide/kotlin/formatting.md"+resetStyle) {
		t.Fatalf("grouped code output %q does not style group title", rawText)
	}
	if !strings.Contains(rawText, lineNumberStyle+"   51:"+resetStyle) {
		t.Fatalf("grouped code output %q does not style line number", rawText)
	}
}

func TestWriteHumanUngroupedRendersCodeMatchesCompactly(t *testing.T) {
	var output bytes.Buffer
	result := codeMatchResult()

	if err := WriteHuman(&output, result, HumanOptions{Ungrouped: true}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	rawText := output.String()
	text := stripOSC8(rawText)
	for _, want := range []string{
		"[code] styleguide/kotlin/formatting.md:51:11",
		"  fun createSampleItem(flavor: Flavor): SampleItem = when(flavor) {",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("code output %q does not contain %q", text, want)
		}
	}
	for _, unwanted := range []string{"file:///workspace/codebase/styleguide/kotlin/formatting.md", "(code_match)"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("code output %q contains noisy metadata %q", text, unwanted)
		}
	}
	if !strings.Contains(rawText, "recall://open?") || !strings.Contains(rawText, "path=%2Fworkspace%2Fcodebase%2Fstyleguide%2Fkotlin%2Fformatting.md") {
		t.Fatalf("code output %q does not contain file recall target", rawText)
	}
}

func TestRecallOpenURLDoesNotDuplicateURIScheme(t *testing.T) {
	openURL := recallOpenURL("notes", "org_node", uriTarget("org-protocol:/roam-node?node=89808715-6315-4484-B726-DFC9F4F2345D"))

	if strings.Contains(openURL, "scheme=") {
		t.Fatalf("recall URL %q contains redundant scheme query parameter", openURL)
	}
	if !strings.Contains(openURL, "uri=org-protocol%3A%2Froam-node%3Fnode%3D89808715-6315-4484-B726-DFC9F4F2345D") {
		t.Fatalf("recall URL %q does not preserve encoded original URI", openURL)
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
					Targets    []struct {
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
	if hits[0].ID != "rollout" || hits[0].Targets[0].File.Path != "/tmp/rollout.md" || hits[0].Group.Key != "procedures" {
		t.Fatalf("first hit did not preserve fields: %#v", hits[0])
	}
	if payload.Responses[0].Response.Warnings[0].Code != "fixture_warning" {
		t.Fatalf("warnings were not preserved: %#v", payload.Responses[0].Response.Warnings)
	}
	if len(payload.Failures) != 1 || payload.Failures[0].ProviderID != "bad" || payload.Failures[0].Error != "boom" {
		t.Fatalf("failures = %#v", payload.Failures)
	}
}

func orgEntryResult() *orchestrator.Result {
	hit := &searchv1.SearchHit{
		Id:    "org:entry",
		Selector:  "org_entry",
		Title: "Matched org entry",
		Targets: []*searchv1.OpenTarget{
			uriTarget("org-protocol:/roam-node?node=89808715-6315-4484-B726-DFC9F4F2345D"),
			fileTarget("/tmp/notes.org", 12, 1),
		},
		Group: &searchv1.SearchGroup{
			Key:     "file:/tmp/notes.org",
			Title:   "notes.org",
			Targets: []*searchv1.OpenTarget{fileTarget("/tmp/notes.org", 0, 0)},
		},
	}
	return &orchestrator.Result{Responses: []orchestrator.ProviderResponse{{
		ProviderID: "org",
		Hits: []normalize.Hit{{
			ProviderID:   "org",
			ProviderRank: 1,
			Hit:          hit,
		}},
		Raw: &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{hit}},
	}}}
}

func codeMatchResult() *orchestrator.Result {
	hit := &searchv1.SearchHit{
		Id:      "code_match:/workspace/codebase/styleguide/kotlin/formatting.md:51:11",
		Selector:    "code_match",
		Title:   "styleguide/kotlin/formatting.md:51:11",
		Snippet: proto.String("fun createSampleItem(flavor: Flavor): SampleItem = when(flavor) {"),
		Targets: []*searchv1.OpenTarget{fileTarget("/workspace/codebase/styleguide/kotlin/formatting.md", 51, 11)},
		Group: &searchv1.SearchGroup{
			Key:     "styleguide/kotlin/formatting.md",
			Title:   "styleguide/kotlin/formatting.md",
			Targets: []*searchv1.OpenTarget{fileTarget("/workspace/codebase/styleguide/kotlin/formatting.md", 0, 0)},
		},
	}
	return &orchestrator.Result{Responses: []orchestrator.ProviderResponse{{
		ProviderID: "code",
		Hits: []normalize.Hit{{
			ProviderID:   "code",
			ProviderRank: 1,
			Hit:          hit,
		}},
		Raw: &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{hit}},
	}}}
}

func renderFixtureResult() *orchestrator.Result {
	occurredAt := timestamppb.New(time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC))
	rolloutHit := &searchv1.SearchHit{
		Id:         "rollout",
		Selector:       "note",
		Title:      "Sample rollout note",
		Snippet:    proto.String("matched rollout context"),
		Score:      proto.Float64(1.2),
		OccurredAt: occurredAt,
		Targets: []*searchv1.OpenTarget{
			fileTarget("/tmp/rollout.md", 0, 0),
			uriTarget("https://example.invalid/rollout"),
			fileTarget("/tmp/source.md", 0, 0),
		},
		Group: &searchv1.SearchGroup{
			Key:     "procedures",
			Title:   "Procedure notes",
			Targets: []*searchv1.OpenTarget{fileTarget("/tmp/procedures", 0, 0)},
		},
	}
	looseHit := &searchv1.SearchHit{
		Id:      "loose",
		Selector:    "note",
		Title:   "Loose hit",
		Targets: []*searchv1.OpenTarget{fileTarget("/tmp/loose.md", 0, 0)},
	}
	warning := &searchv1.Warning{Message: "fixture warning", Code: proto.String("fixture_warning")}
	raw := &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{rolloutHit, looseHit}, Warnings: []*searchv1.Warning{warning}}
	return &orchestrator.Result{Responses: []orchestrator.ProviderResponse{{
		ProviderID: "example",
		Hits: []normalize.Hit{
			{ProviderID: "example", ProviderRank: 1, Hit: rolloutHit},
			{ProviderID: "example", ProviderRank: 2, Hit: looseHit},
		},
		Warnings: []normalize.Warning{{ProviderID: "example", Warning: warning}},
		Raw:      raw,
	}}}
}

func uriTarget(uri string) *searchv1.OpenTarget {
	return &searchv1.OpenTarget{Target: &searchv1.OpenTarget_Uri{Uri: &searchv1.UriTarget{Uri: uri}}}
}

func fileTarget(path string, line uint32, column uint32) *searchv1.OpenTarget {
	target := &searchv1.FileTarget{Path: path}
	if line > 0 {
		target.Line = proto.Uint32(line)
	}
	if column > 0 {
		target.Column = proto.Uint32(column)
	}
	return &searchv1.OpenTarget{Target: &searchv1.OpenTarget_File{File: target}}
}

func stripOSC8(text string) string {
	for {
		start := strings.Index(text, "\x1b]8;;")
		if start == -1 {
			return stripSGR(text)
		}
		end := strings.Index(text[start:], "\x1b\\")
		if end == -1 {
			return stripSGR(text)
		}
		text = text[:start] + text[start+end+2:]
	}
}

func stripSGR(text string) string {
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

type assertErr string

func (err assertErr) Error() string { return string(err) }
