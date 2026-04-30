package exampleprovider

import (
	"bytes"
	"context"
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
	if len(response.GetHits()) != 1 || response.GetHits()[0].GetId() != "example:planning-session" {
		t.Fatalf("unexpected textproto hits: %#v", response.GetHits())
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
	response, err := New().Search(context.Background(), &searchv1.SearchRequest{Query: "example", SelectorHints: []string{"note"}})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(response.GetHits()) != 2 {
		t.Fatalf("hit count = %d, want note-only fixture hits", len(response.GetHits()))
	}
	for _, hit := range response.GetHits() {
		if hit.GetSelector() != "note:content" {
			t.Fatalf("hit selector = %q, want note:content", hit.GetSelector())
		}
	}
}

func TestSearchWithoutLimitReturnsEveryMatch(t *testing.T) {
	response, err := New().Search(context.Background(), &searchv1.SearchRequest{Query: "example"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(response.GetHits()) != 3 {
		t.Fatalf("hit count = %d, want all fixture hits", len(response.GetHits()))
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
	hits := response.GetHits()
	if len(hits) != 1 {
		t.Fatalf("hit count = %d, want 1", len(hits))
	}
	hit := hits[0]
	if hit.GetId() != "example:rollout-note" {
		t.Fatalf("hit id = %q, want example:rollout-note", hit.GetId())
	}
	if hit.GetSelector() != "note:content" || hit.GetTitle() != "Sample rollout note" || hit.GetSnippet() == "" {
		t.Fatalf("hit missing required display fields: %#v", hit)
	}
	if hit.Score == nil {
		t.Fatal("hit score is nil")
	}
	if len(hit.GetTargets()) < 2 || hit.GetTargets()[0].GetFile().GetPath() == "" || hit.GetTargets()[1].GetUri().GetUri() == "" {
		t.Fatalf("hit targets do not exercise primary and secondary open targets: %#v", hit.GetTargets())
	}
	if hit.GetGroup().GetKey() == "" || hit.GetGroup().GetTitle() == "" || len(hit.GetGroup().GetTargets()) == 0 {
		t.Fatalf("hit group does not exercise grouping fields: %#v", hit.GetGroup())
	}
	if hit.GetOccurredAt() == nil || !hit.GetOccurredAt().IsValid() {
		t.Fatalf("occurred_at is invalid: %#v", hit.GetOccurredAt())
	}
	if len(response.GetWarnings()) != 1 || response.GetWarnings()[0].GetCode() != "example_fixture" {
		t.Fatalf("warnings = %#v, want example_fixture warning", response.GetWarnings())
	}
}
