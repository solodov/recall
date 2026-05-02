package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/solodov/recall/internal/normalize"
	"github.com/solodov/recall/internal/orchestrator"
	"github.com/solodov/recall/internal/rank"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestWriteHumanRendersFormatTitleAndOrderedDetails(t *testing.T) {
	useLocalZone(t, "TEST", 2*60*60)
	result := providerResult("jira", issueResult())
	var output bytes.Buffer

	if err := WriteHuman(&output, result, HumanOptions{}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	text := stripOSC8(output.String())
	for _, want := range []string{
		"[jira:issue:content] Results",
		"  OPS-42 Fix checkout total",
		"    status: In Review",
		"    updated at: 2026-04-28T11:30:00",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output %q does not contain %q", text, want)
		}
	}
	for _, unwanted := range []string{"ticket:", "summary:", "updated_at", " TEST"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("human output %q contains renderer noise %q", text, unwanted)
		}
	}
}

func TestWriteHumanRendersSlackTimestampTitle(t *testing.T) {
	useLocalZone(t, "TEST", 2*60*60)
	messageTime := time.Date(2026, 4, 29, 10, 15, 30, 123456000, time.UTC)
	message := &searchv1.SearchResponse_Result{
		Id:       "message:C1:1777467330.123456",
		Selector: "message:content",
		Fields: []*searchv1.SearchResponse_Result_Field{
			timestampField("timestamp", messageTime),
			textField("snippet", "matched message text"),
			textField("author", "fixture-bot"),
		},
		Targets: []*searchv1.OpenTarget{uriTarget("https://example.invalid/archives/C1/p1777467330123456")},
		Group: &searchv1.SearchGroup{
			Key:     "channel:C1",
			Title:   "#fixtures",
			Targets: []*searchv1.OpenTarget{uriTarget("https://example.invalid/archives/C1")},
		},
		Format: format([]string{"timestamp", "snippet"}, nil),
	}
	result := providerResult("slack", message)
	var output bytes.Buffer

	if err := WriteHuman(&output, result, HumanOptions{}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	rawText := output.String()
	text := stripOSC8(rawText)
	for _, want := range []string{
		"[slack:message:content] #fixtures",
		"2026-04-29 12:15:30: matched message text",
		"    author: fixture-bot",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("grouped message output %q does not contain %q", text, want)
		}
	}
	if !strings.Contains(rawText, "type=uri") || strings.Contains(rawText, "timestamp=") {
		t.Fatalf("grouped message output %q should open URI without display timestamp parameters", rawText)
	}
}

func TestWriteHumanRendersRipgrepLineRowsFromFields(t *testing.T) {
	result := providerResult("code", codeLineResult())
	var output bytes.Buffer

	if err := WriteHuman(&output, result, HumanOptions{}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	rawText := output.String()
	text := stripOSC8(rawText)
	for _, want := range []string{
		"[code:file:content] styleguide/kotlin/formatting.md",
		"     51: fun createSampleItem(flavor: Flavor): SampleItem = when(flavor) {",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("grouped code output %q does not contain %q", text, want)
		}
	}
	for _, unwanted := range []string{"line: 51", "snippet:", "(file:content)", "file:///workspace/codebase/styleguide/kotlin/formatting.md"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("grouped code output %q contains noisy metadata %q", text, unwanted)
		}
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

func TestWriteHumanFallbacksWhenFormatAbsent(t *testing.T) {
	useLocalZone(t, "TEST", 2*60*60)
	result := providerResult("notes", &searchv1.SearchResponse_Result{
		Id:       "note:1",
		Selector: "note:content",
		Fields: []*searchv1.SearchResponse_Result_Field{
			textField("title", "Loose hit"),
			textField("snippet", "More context"),
			timestampField("updated_at", time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC)),
		},
		Targets: []*searchv1.OpenTarget{fileTarget("/tmp/loose.md", 0, 0)},
	})
	var output bytes.Buffer

	if err := WriteHuman(&output, result, HumanOptions{Ungrouped: true}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	text := stripOSC8(output.String())
	for _, want := range []string{
		"[notes] Loose hit (note:content)",
		"  snippet: More context",
		"  updated at: 2026-04-28T11:30:00",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("fallback output %q does not contain %q", text, want)
		}
	}
}

func TestWriteHumanSkipsMissingFormatKeys(t *testing.T) {
	result := providerResult("jira", &searchv1.SearchResponse_Result{
		Id:       "issue:OPS-43",
		Selector: "issue:content",
		Fields: []*searchv1.SearchResponse_Result_Field{
			textField("summary", "Investigate refund state"),
			hiddenTextField("raw_payload", "machine-only state"),
			textField("status", "Open"),
		},
		Targets: []*searchv1.OpenTarget{uriTarget("https://example.invalid/browse/OPS-43")},
		Format:  format([]string{"missing_title", "raw_payload", "summary"}, []string{"missing_detail", "raw_payload", "status"}),
	})
	var output bytes.Buffer

	if err := WriteHuman(&output, result, HumanOptions{}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	text := stripOSC8(output.String())
	for _, want := range []string{"  Investigate refund state", "    status: Open"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing-key output %q does not contain %q", text, want)
		}
	}
	for _, unwanted := range []string{"missing", "raw payload", "machine-only state"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("missing-key output %q rendered hidden or unknown format key %q", text, unwanted)
		}
	}
}

func TestWriteHumanFallbackSkipsHiddenFields(t *testing.T) {
	result := providerResult("notes", &searchv1.SearchResponse_Result{
		Id:       "note:hidden-fields",
		Selector: "note:content",
		Fields: []*searchv1.SearchResponse_Result_Field{
			hiddenTextField("raw_payload", "machine-only payload"),
			textField("title", "Visible note"),
			hiddenTextField("debug_url", "https://example.invalid/debug"),
			textField("snippet", "Visible context"),
		},
	})
	var output bytes.Buffer

	if err := WriteHuman(&output, result, HumanOptions{Ungrouped: true}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	text := stripOSC8(output.String())
	for _, want := range []string{"[notes] Visible note (note:content)", "  snippet: Visible context"} {
		if !strings.Contains(text, want) {
			t.Fatalf("hidden-field fallback output %q does not contain %q", text, want)
		}
	}
	for _, unwanted := range []string{"raw payload", "machine-only payload", "debug url", "https://example.invalid/debug"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("hidden-field fallback output %q rendered hidden field %q", text, unwanted)
		}
	}
}

func TestWriteHumanIgnoresDuplicateFormatKeys(t *testing.T) {
	result := providerResult("jira", &searchv1.SearchResponse_Result{
		Id:       "issue:OPS-44",
		Selector: "issue:content",
		Fields: []*searchv1.SearchResponse_Result_Field{
			textField("ticket", "OPS-44"),
			textField("summary", "Fix duplicate charge"),
			textField("status", "Open"),
		},
		Targets: []*searchv1.OpenTarget{uriTarget("https://example.invalid/browse/OPS-44")},
		Format:  format([]string{"ticket", "ticket", "summary"}, []string{"summary", "status", "status"}),
	})
	var output bytes.Buffer

	if err := WriteHuman(&output, result, HumanOptions{}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	text := stripOSC8(output.String())
	if strings.Count(text, "OPS-44") != 1 || strings.Count(text, "Fix duplicate charge") != 1 || strings.Count(text, "status: Open") != 1 {
		t.Fatalf("duplicate-key output %q did not render each selected field once", text)
	}
	if strings.Contains(text, "summary:") {
		t.Fatalf("duplicate-key output %q rendered a title field again as a detail", text)
	}
}

func TestWriteHumanGroupedLinksSourceLabelToProviderConfig(t *testing.T) {
	result := providerResult("code", codeLineResult())
	var output bytes.Buffer

	if err := WriteHuman(&output, result, HumanOptions{ProviderConfigTargets: map[string]*searchv1.OpenTarget{
		"code": fileTarget("/workspace/config/recall.txtpb", 17, 1),
	}}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}

	rawText := output.String()
	if !strings.Contains(stripOSC8(rawText), "[code:file:content] styleguide/kotlin/formatting.md") {
		t.Fatalf("grouped output %q does not keep source label shape", rawText)
	}
	wantURL := "recall://open?column=1&line=17&path=%2Fworkspace%2Fconfig%2Frecall.txtpb&selector=file%3Acontent&source=code&type=file&v=1"
	if !strings.Contains(rawText, wantURL) || !strings.Contains(rawText, groupLabelStyle+"[code:file:content]"+resetStyle) {
		t.Fatalf("grouped output %q does not link styled source label to config target %q", rawText, wantURL)
	}
}

func TestOpenURLDoesNotDuplicateURIScheme(t *testing.T) {
	openURL := OpenURL("notes", "entry:content", uriTarget("org-protocol:/roam-node?node=89808715-6315-4484-B726-DFC9F4F2345D"))

	if strings.Contains(openURL, "scheme=") {
		t.Fatalf("recall URL %q contains redundant scheme query parameter", openURL)
	}
	if !strings.Contains(openURL, "uri=org-protocol%3A%2Froam-node%3Fnode%3D89808715-6315-4484-B726-DFC9F4F2345D") {
		t.Fatalf("recall URL %q does not preserve encoded original URI", openURL)
	}
}

func TestWriteJSONPreservesStructuredFieldsAndFailures(t *testing.T) {
	issue := issueResult()
	issue.Fields = append(issue.Fields, hiddenTextField("internal_token", "preserved but hidden"))
	result := providerResult("jira", issue)
	result.BlendedResults = []rank.Result{{Normalized: result.Responses[0].Results[0], BlendedScore: 0.5}}
	result.Failures = []orchestrator.ProviderFailure{{ProviderID: "bad", Err: assertErr("boom")}}

	var human bytes.Buffer
	if err := WriteHuman(&human, result, HumanOptions{}); err != nil {
		t.Fatalf("WriteHuman returned error: %v", err)
	}
	if strings.Contains(stripOSC8(human.String()), "internal token") || strings.Contains(stripOSC8(human.String()), "preserved but hidden") {
		t.Fatalf("human output %q rendered a field not selected by format", stripOSC8(human.String()))
	}

	var output bytes.Buffer
	if err := WriteJSON(&output, result); err != nil {
		t.Fatalf("WriteJSON returned error: %v", err)
	}

	var payload struct {
		Responses []struct {
			ProviderID string `json:"provider_id"`
			Response   struct {
				Results []struct {
					ID       string `json:"id"`
					Selector string `json:"selector"`
					Fields   []struct {
						Key    string `json:"key"`
						Text   string `json:"text"`
						Hidden bool   `json:"hidden"`
					} `json:"fields"`
					Format struct {
						TitleFields  []string `json:"title_fields"`
						DetailFields []string `json:"detail_fields"`
					} `json:"format"`
				} `json:"results"`
				Warnings []struct {
					Message string `json:"message"`
					Code    string `json:"code"`
				} `json:"warnings"`
			} `json:"response"`
		} `json:"responses"`
		BlendedResults []struct {
			ProviderID string `json:"provider_id"`
			Result     struct {
				ID string `json:"id"`
			} `json:"result"`
		} `json:"blended_results"`
		Failures []struct {
			ProviderID string `json:"provider_id"`
			Error      string `json:"error"`
		} `json:"failures"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal JSON output: %v\n%s", err, output.String())
	}

	if len(payload.Responses) != 1 || payload.Responses[0].ProviderID != "jira" {
		t.Fatalf("responses metadata = %#v", payload.Responses)
	}
	results := payload.Responses[0].Response.Results
	if len(results) != 1 || results[0].ID != "issue:OPS-42" || results[0].Selector != "issue:content" {
		t.Fatalf("results were not preserved: %#v", results)
	}
	if !containsField(results[0].Fields, "internal_token", "preserved but hidden", true) {
		t.Fatalf("JSON fields did not preserve hidden field: %#v", results[0].Fields)
	}
	if len(results[0].Format.TitleFields) != 2 || results[0].Format.TitleFields[0] != "ticket" || results[0].Format.DetailFields[1] != "updated_at" {
		t.Fatalf("format was not preserved: %#v", results[0].Format)
	}
	if payload.Responses[0].Response.Warnings[0].Code != "fixture_warning" {
		t.Fatalf("warnings were not preserved: %#v", payload.Responses[0].Response.Warnings)
	}
	if len(payload.BlendedResults) != 1 || payload.BlendedResults[0].Result.ID != "issue:OPS-42" {
		t.Fatalf("blended results were not preserved: %#v", payload.BlendedResults)
	}
	if len(payload.Failures) != 1 || payload.Failures[0].ProviderID != "bad" || payload.Failures[0].Error != "boom" {
		t.Fatalf("failures = %#v", payload.Failures)
	}
}

func issueResult() *searchv1.SearchResponse_Result {
	return &searchv1.SearchResponse_Result{
		Id:       "issue:OPS-42",
		Selector: "issue:content",
		Fields: []*searchv1.SearchResponse_Result_Field{
			textField("ticket", "OPS-42"),
			textField("summary", "Fix checkout total"),
			textField("status", "In Review"),
			timestampField("updated_at", time.Date(2026, 4, 28, 9, 30, 0, 0, time.UTC)),
		},
		Targets: []*searchv1.OpenTarget{uriTarget("https://example.invalid/browse/OPS-42")},
		Format:  format([]string{"ticket", "summary"}, []string{"status", "updated_at"}),
	}
}

func codeLineResult() *searchv1.SearchResponse_Result {
	return &searchv1.SearchResponse_Result{
		Id:       "file_content:/workspace/codebase/styleguide/kotlin/formatting.md:51:11",
		Selector: "file:content",
		Fields: []*searchv1.SearchResponse_Result_Field{
			integerField("line", 51),
			textField("snippet", "fun createSampleItem(flavor: Flavor): SampleItem = when(flavor) {"),
		},
		Targets: []*searchv1.OpenTarget{fileTarget("/workspace/codebase/styleguide/kotlin/formatting.md", 51, 11)},
		Group: &searchv1.SearchGroup{
			Key:     "styleguide/kotlin/formatting.md",
			Title:   "styleguide/kotlin/formatting.md",
			Targets: []*searchv1.OpenTarget{fileTarget("/workspace/codebase/styleguide/kotlin/formatting.md", 0, 0)},
		},
		Format: format([]string{"line", "snippet"}, nil),
	}
}

func providerResult(providerID string, results ...*searchv1.SearchResponse_Result) *orchestrator.Result {
	warning := &searchv1.SearchResponse_Warning{Message: "fixture warning", Code: proto.String("fixture_warning")}
	response := orchestrator.ProviderResponse{
		ProviderID: providerID,
		Results:    make([]normalize.Result, 0, len(results)),
		Warnings:   []normalize.Warning{{ProviderID: providerID, Warning: warning}},
		Raw:        &searchv1.SearchResponse{Results: results, Warnings: []*searchv1.SearchResponse_Warning{warning}},
	}
	for index, result := range results {
		response.Results = append(response.Results, normalize.Result{ProviderID: providerID, ProviderRank: index + 1, Result: result})
	}
	return &orchestrator.Result{Responses: []orchestrator.ProviderResponse{response}}
}

func format(titleFields []string, detailFields []string) *searchv1.SearchResponse_Result_Format {
	return &searchv1.SearchResponse_Result_Format{TitleFields: titleFields, DetailFields: detailFields}
}

func textField(key string, value string) *searchv1.SearchResponse_Result_Field {
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Text{Text: value},
	}
}

func integerField(key string, value int64) *searchv1.SearchResponse_Result_Field {
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Integer{Integer: value},
	}
}

func hiddenTextField(key string, value string) *searchv1.SearchResponse_Result_Field {
	field := textField(key, value)
	field.Hidden = proto.Bool(true)
	return field
}

func timestampField(key string, value time.Time) *searchv1.SearchResponse_Result_Field {
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Timestamp{Timestamp: timestamppb.New(value)},
	}
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

func containsField(fields []struct {
	Key    string `json:"key"`
	Text   string `json:"text"`
	Hidden bool   `json:"hidden"`
}, key string, text string, hidden bool) bool {
	for _, field := range fields {
		if field.Key == key && field.Text == text && field.Hidden == hidden {
			return true
		}
	}
	return false
}

func useLocalZone(t *testing.T, name string, offsetSeconds int) {
	t.Helper()
	previous := time.Local
	time.Local = time.FixedZone(name, offsetSeconds)
	t.Cleanup(func() { time.Local = previous })
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
