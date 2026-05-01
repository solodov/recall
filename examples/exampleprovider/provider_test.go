package exampleprovider

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestServeSearchBinaryExercisesContractFields(t *testing.T) {
	stdout := serveOneBinary(t, &searchv1.SearchRequest{Query: "rollout", Limit: proto.Uint32(5)})

	response := &searchv1.SearchResponse{}
	if err := proto.Unmarshal(stdout, response); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	assertExampleSearchResponse(t, response)
}

func TestServeSearchTextprotoAutoDetectsAndMirrorsInput(t *testing.T) {
	requestBytes, err := prototext.Marshal(&searchv1.SearchRequest{Query: "planning session"})
	if err != nil {
		t.Fatalf("marshal textproto request: %v", err)
	}

	var stdout bytes.Buffer
	provider := New()
	err = provider.Serve(context.Background(), bytes.NewReader(requestBytes), &stdout, []string{searchPath(t)})
	if err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	response := &searchv1.SearchResponse{}
	if err := prototext.Unmarshal(stdout.Bytes(), response); err != nil {
		t.Fatalf("unmarshal textproto search response: %v", err)
	}
	if len(response.GetResults()) != 1 || response.GetResults()[0].GetId() != "planning-session" {
		t.Fatalf("unexpected textproto results: %#v", response.GetResults())
	}
	if got := resultText(t, response.GetResults()[0], "summary"); got != "Fixture planning session" {
		t.Fatalf("summary field = %q", got)
	}
}

func TestServeListCapabilitiesTextproto(t *testing.T) {
	var stdout bytes.Buffer
	provider := New()
	err := provider.Serve(context.Background(), bytes.NewReader(nil), &stdout, []string{searchv1.SearchProviderListCapabilitiesPath})
	if err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}
	response := &searchv1.ListCapabilitiesResponse{}
	if err := prototext.Unmarshal(stdout.Bytes(), response); err != nil {
		t.Fatalf("unmarshal textproto capabilities response: %v", err)
	}
	if len(response.GetSurfaces()) != 2 || response.GetSurfaces()[0].GetSelector() != "note:content" || response.GetSurfaces()[1].GetSelector() != "event:content" {
		t.Fatalf("surfaces = %#v, want note and event content", response.GetSurfaces())
	}
}

func TestSearchRejectsInvalidRequest(t *testing.T) {
	_, err := New().Search(context.Background(), &searchv1.SearchRequest{Query: "", Limit: proto.Uint32(1)})
	if err == nil || !strings.Contains(err.Error(), "query") {
		t.Fatalf("empty query error = %v, want query validation", err)
	}
}

func TestSearchUsesSelectorHints(t *testing.T) {
	response, err := New().Search(context.Background(), &searchv1.SearchRequest{Query: "a", SelectorHints: []string{"note"}})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(response.GetResults()) != 2 {
		t.Fatalf("result count = %d, want note-only fixture results", len(response.GetResults()))
	}
	for _, result := range response.GetResults() {
		if result.GetSelector() != "note:content" {
			t.Fatalf("result selector = %q, want note:content", result.GetSelector())
		}
	}
}

func TestSearchWithoutLimitReturnsEveryMatch(t *testing.T) {
	response, err := New().Search(context.Background(), &searchv1.SearchRequest{Query: "a"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(response.GetResults()) != 3 {
		t.Fatalf("result count = %d, want all fixture results", len(response.GetResults()))
	}
}

func serveOneBinary(t *testing.T, request proto.Message) []byte {
	t.Helper()
	requestBytes, err := proto.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var stdout bytes.Buffer
	provider := New()
	if err := provider.Serve(context.Background(), bytes.NewReader(requestBytes), &stdout, []string{searchPath(t)}); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}
	return stdout.Bytes()
}

func searchPath(t *testing.T) string {
	t.Helper()
	return searchv1.SearchProviderSearchPath
}

func assertExampleSearchResponse(t *testing.T, response *searchv1.SearchResponse) {
	t.Helper()
	results := response.GetResults()
	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	result := results[0]
	if result.GetId() != "rollout-note" {
		t.Fatalf("result id = %q, want rollout-note", result.GetId())
	}
	if result.GetSelector() != "note:content" || resultText(t, result, "title") != "Sample rollout note" || resultText(t, result, "snippet") == "" {
		t.Fatalf("result missing required display fields: %#v", result)
	}
	if result.Score == nil {
		t.Fatal("result score is nil")
	}
	if !reflect.DeepEqual(result.GetFormat().GetTitleFields(), []string{"title"}) || !reflect.DeepEqual(result.GetFormat().GetDetailFields(), []string{"updated_at", "snippet"}) {
		t.Fatalf("format = %#v, want title plus updated/snippet details", result.GetFormat())
	}
	if resultTimestamp(t, result, "updated_at") == nil {
		t.Fatalf("updated_at timestamp field is missing or invalid: %#v", result.GetFields())
	}
	if len(result.GetTargets()) < 2 || result.GetTargets()[0].GetFile().GetPath() == "" || result.GetTargets()[1].GetUri().GetUri() == "" {
		t.Fatalf("result targets do not exercise primary and secondary open targets: %#v", result.GetTargets())
	}
	if result.GetGroup().GetKey() == "" || result.GetGroup().GetTitle() == "" || len(result.GetGroup().GetTargets()) == 0 {
		t.Fatalf("result group does not exercise grouping fields: %#v", result.GetGroup())
	}
	if len(response.GetWarnings()) != 1 || response.GetWarnings()[0].GetCode() != "example_fixture" {
		t.Fatalf("warnings = %#v, want example_fixture warning", response.GetWarnings())
	}
}

func resultText(t *testing.T, result *searchv1.SearchResponse_Result, key string) string {
	t.Helper()
	for _, field := range result.GetFields() {
		if field.GetKey() == key {
			return field.GetText()
		}
	}
	t.Fatalf("missing text field %q in %#v", key, result.GetFields())
	return ""
}

func resultTimestamp(t *testing.T, result *searchv1.SearchResponse_Result, key string) proto.Message {
	t.Helper()
	for _, field := range result.GetFields() {
		if field.GetKey() == key {
			if timestamp := field.GetTimestamp(); timestamp != nil && timestamp.IsValid() {
				return timestamp
			}
		}
	}
	return nil
}
