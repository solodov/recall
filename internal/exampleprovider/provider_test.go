package exampleprovider

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"recall/internal/searchclient"
	"recall/internal/stdiorpc"
	searchv1 "recall/proto/recall/search/v1"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestServeSearchBinaryExercisesContractFields(t *testing.T) {
	stdout := serveOne(t, stdiorpc.PayloadFormatBinary, &searchv1.SearchRequest{Query: "deploy", Limit: proto.Uint32(5)})

	response := &searchv1.SearchResponse{}
	if err := proto.Unmarshal(stdout, response); err != nil {
		t.Fatalf("unmarshal search response: %v", err)
	}
	assertExampleSearchResponse(t, response)
}

func TestServeSearchTextprotoAutoDetectsAndMirrorsInput(t *testing.T) {
	requestBytes, err := prototext.Marshal(&searchv1.SearchRequest{Query: "alice meeting"})
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
	if len(response.GetHits()) != 1 || response.GetHits()[0].GetId() != "example:alice-meeting" {
		t.Fatalf("unexpected textproto hits: %#v", response.GetHits())
	}
}

func TestSearchRejectsInvalidRequest(t *testing.T) {
	_, err := New().Search(context.Background(), &searchv1.SearchRequest{Query: "", Limit: proto.Uint32(1)})
	if err == nil || !strings.Contains(err.Error(), "query") {
		t.Fatalf("empty query error = %v, want query validation", err)
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

func serveOne(t *testing.T, format stdiorpc.PayloadFormat, request proto.Message) []byte {
	t.Helper()
	requestBytes, err := stdiorpc.MarshalPayload(format, request)
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
	path, err := (stdiorpc.MethodKey{Service: searchclient.SearchService, Method: searchclient.SearchMethod}).Path()
	if err != nil {
		t.Fatalf("Path returned error: %v", err)
	}
	return path
}

func assertExampleSearchResponse(t *testing.T, response *searchv1.SearchResponse) {
	t.Helper()
	hits := response.GetHits()
	if len(hits) != 1 {
		t.Fatalf("hit count = %d, want 1", len(hits))
	}
	hit := hits[0]
	if hit.GetId() != "example:deploy-notes" {
		t.Fatalf("hit id = %q, want example:deploy-notes", hit.GetId())
	}
	if hit.GetKind() != "note" || hit.GetTitle() != "Deploy notes" || hit.GetSnippet() == "" {
		t.Fatalf("hit missing required display fields: %#v", hit)
	}
	if hit.Score == nil {
		t.Fatal("hit score is nil")
	}
	if len(hit.GetUris()) < 2 || hit.GetUris()[0].GetName() != "open" || hit.GetUris()[0].GetUri() == "" {
		t.Fatalf("hit URIs do not exercise named primary/secondary actions: %#v", hit.GetUris())
	}
	if hit.GetGroup().GetKey() == "" || hit.GetGroup().GetTitle() == "" || len(hit.GetGroup().GetUris()) == 0 {
		t.Fatalf("hit group does not exercise grouping fields: %#v", hit.GetGroup())
	}
	if hit.GetOccurredAt() == nil || !hit.GetOccurredAt().IsValid() {
		t.Fatalf("occurred_at is invalid: %#v", hit.GetOccurredAt())
	}
	if len(response.GetWarnings()) != 1 || response.GetWarnings()[0].GetCode() != "example_fixture" {
		t.Fatalf("warnings = %#v, want example_fixture warning", response.GetWarnings())
	}
}
