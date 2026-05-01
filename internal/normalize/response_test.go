package normalize

import (
	"math"
	"strings"
	"testing"
	"time"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSearchResponseAnnotatesValidatedResultsAndWarnings(t *testing.T) {
	response := &searchv1.SearchResponse{
		Results: []*searchv1.SearchResponse_Result{
			{
				Id:       "result-1",
				Selector: "note:content",
				Fields: []*searchv1.SearchResponse_Result_Field{
					textField("summary", "First result"),
					integerField("rank", 1),
					timestampField("updated_at", time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)),
				},
				Score: proto.Float64(1.23),
				Targets: []*searchv1.OpenTarget{
					fileTarget("/tmp/first.md", 12, 3),
				},
				Group: &searchv1.SearchGroup{
					Key:     "group-1",
					Title:   "Group one",
					Targets: []*searchv1.OpenTarget{fileTarget("/tmp", 0, 0)},
				},
			},
		},
		Warnings: []*searchv1.SearchResponse_Warning{{
			Message: "stale index",
			Code:    proto.String("stale_index"),
		}},
	}

	normalized, err := SearchResponse("example", response)
	if err != nil {
		t.Fatalf("SearchResponse returned error: %v", err)
	}

	if normalized.ProviderID != "example" {
		t.Fatalf("ProviderID = %q, want example", normalized.ProviderID)
	}
	if len(normalized.Results) != 1 {
		t.Fatalf("result count = %d, want 1", len(normalized.Results))
	}
	if normalized.Results[0].ProviderID != "example" || normalized.Results[0].ProviderRank != 1 {
		t.Fatalf("normalized result metadata = %#v", normalized.Results[0])
	}
	if normalized.Results[0].Result == response.Results[0] {
		t.Fatal("normalized result reused provider-owned pointer")
	}
	if len(normalized.Warnings) != 1 || normalized.Warnings[0].ProviderID != "example" {
		t.Fatalf("normalized warnings = %#v", normalized.Warnings)
	}
	if normalized.Raw == response {
		t.Fatal("normalized raw response reused provider-owned pointer")
	}
}

func TestSearchResponseAllowsZeroResults(t *testing.T) {
	normalized, err := SearchResponse("example", &searchv1.SearchResponse{})
	if err != nil {
		t.Fatalf("SearchResponse returned error for empty success: %v", err)
	}
	if len(normalized.Results) != 0 || len(normalized.Warnings) != 0 {
		t.Fatalf("normalized empty response = %#v", normalized)
	}
}

func TestSearchResponseRejectsMalformedResultsFieldsGroupsTargetsAndWarnings(t *testing.T) {
	response := &searchv1.SearchResponse{
		Results: []*searchv1.SearchResponse_Result{
			{
				Id:       "",
				Selector: "",
				Fields: []*searchv1.SearchResponse_Result_Field{
					textField("", "missing key"),
					textField("summary", "first"),
					integerField("summary", 2),
					{Key: "unset"},
					timestampFieldRaw("bad_time", &timestamppb.Timestamp{Seconds: 253402300800}),
				},
				Targets: []*searchv1.OpenTarget{
					{Target: &searchv1.OpenTarget_Uri{Uri: &searchv1.UriTarget{Uri: "relative/path"}}},
					{Target: &searchv1.OpenTarget_File{File: &searchv1.FileTarget{Path: "relative/path", Column: proto.Uint32(2)}}},
				},
				Group: &searchv1.SearchGroup{
					Key:     "",
					Title:   "",
					Targets: []*searchv1.OpenTarget{{Target: &searchv1.OpenTarget_Uri{Uri: &searchv1.UriTarget{Uri: ""}}}},
				},
			},
		},
		Warnings: []*searchv1.SearchResponse_Warning{{Message: "", Code: proto.String("")}},
	}

	err := firstError(SearchResponse("example", response))
	if err == nil {
		t.Fatal("SearchResponse succeeded for malformed response")
	}
	message := err.Error()
	for _, want := range []string{
		"results[0].id",
		"results[0].selector",
		"results[0].fields[0].key",
		"results[0].fields[2].key \"summary\" duplicates results[0].fields[1]",
		"results[0].fields[3].value is required",
		"results[0].fields[4].timestamp is invalid",
		"results[0].targets[0].uri.uri must include a scheme",
		"results[0].targets[1].file.path must be absolute",
		"results[0].targets[1].file.column requires line",
		"results[0].group.key",
		"results[0].group.title",
		"results[0].group.targets[0].uri.uri",
		"warnings[0].message",
		"warnings[0].code",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("SearchResponse error %q does not contain %q", message, want)
		}
	}
}

func TestSearchResponseRejectsResultWithoutFields(t *testing.T) {
	response := &searchv1.SearchResponse{Results: []*searchv1.SearchResponse_Result{{
		Id:       "result",
		Selector: "note:content",
	}}}

	err := firstError(SearchResponse("example", response))
	if err == nil || !strings.Contains(err.Error(), "results[0].fields") {
		t.Fatalf("SearchResponse field error = %v", err)
	}
}

func TestSearchResponseRejectsNonFiniteScore(t *testing.T) {
	response := &searchv1.SearchResponse{Results: []*searchv1.SearchResponse_Result{{
		Id:       "result",
		Selector: "note:content",
		Fields:   []*searchv1.SearchResponse_Result_Field{textField("title", "Result")},
		Score:    proto.Float64(math.NaN()),
	}}}

	err := firstError(SearchResponse("example", response))
	if err == nil || !strings.Contains(err.Error(), "score") {
		t.Fatalf("SearchResponse score error = %v", err)
	}
}

func TestFieldHelpersDecodeTypedFields(t *testing.T) {
	updatedAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	result := &searchv1.SearchResponse_Result{Fields: []*searchv1.SearchResponse_Result_Field{
		textField("title", "Example"),
		integerField("line", 42),
		timestampField("updated_at", updatedAt),
	}}

	fields := Fields(result)
	if len(fields) != 3 {
		t.Fatalf("field count = %d, want 3", len(fields))
	}
	if fields[0].Kind != FieldKindText || fields[0].Text != "Example" {
		t.Fatalf("text field = %#v", fields[0])
	}
	if fields[1].Kind != FieldKindInteger || fields[1].Integer != 42 {
		t.Fatalf("integer field = %#v", fields[1])
	}
	if fields[2].Kind != FieldKindTimestamp || !fields[2].Timestamp.Equal(updatedAt) {
		t.Fatalf("timestamp field = %#v", fields[2])
	}
	field, ok := FieldByKey(result, "line")
	if !ok || field.Integer != 42 {
		t.Fatalf("FieldByKey(line) = %#v %v, want integer 42", field, ok)
	}
}

func TestFilterSelectorsKeepsOnlyRequestedSelectorsAfterProviderSearch(t *testing.T) {
	noteResult := result("note-1", "note:content", "Note")
	eventResult := result("event-1", "event:content", "Event")
	warning := &searchv1.SearchResponse_Warning{Message: "provider warning"}
	response := ProviderResponse{
		ProviderID: "example",
		Results: []Result{
			{ProviderID: "example", ProviderRank: 1, Result: noteResult},
			{ProviderID: "example", ProviderRank: 2, Result: eventResult},
		},
		Warnings: []Warning{{ProviderID: "example", Warning: warning}},
		Raw:      &searchv1.SearchResponse{Results: []*searchv1.SearchResponse_Result{noteResult, eventResult}, Warnings: []*searchv1.SearchResponse_Warning{warning}},
	}

	filtered := FilterSelectors(response, []string{"event"})

	if len(filtered.Results) != 1 || filtered.Results[0].Result.GetId() != "event-1" {
		t.Fatalf("filtered results = %#v, want only event result", filtered.Results)
	}
	if len(filtered.Warnings) != 1 || filtered.Warnings[0].Warning.GetMessage() != "provider warning" {
		t.Fatalf("filtered warnings = %#v, want warnings preserved", filtered.Warnings)
	}
	if len(filtered.Raw.GetResults()) != 1 || filtered.Raw.GetResults()[0].GetId() != "event-1" {
		t.Fatalf("filtered raw response = %#v, want only event result", filtered.Raw)
	}
}

func result(id string, selector string, title string) *searchv1.SearchResponse_Result {
	return &searchv1.SearchResponse_Result{
		Id:       id,
		Selector: selector,
		Fields:   []*searchv1.SearchResponse_Result_Field{textField("title", title)},
	}
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

func timestampField(key string, value time.Time) *searchv1.SearchResponse_Result_Field {
	return timestampFieldRaw(key, timestamppb.New(value))
}

func timestampFieldRaw(key string, value *timestamppb.Timestamp) *searchv1.SearchResponse_Result_Field {
	return &searchv1.SearchResponse_Result_Field{
		Key:   key,
		Value: &searchv1.SearchResponse_Result_Field_Timestamp{Timestamp: value},
	}
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

func firstError(_ ProviderResponse, err error) error {
	return err
}
