package gh

import (
	"context"
	"reflect"
	"testing"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	"google.golang.org/protobuf/proto"
)

func TestProviderListCapabilitiesAdvertisesConfiguredSelectors(t *testing.T) {
	provider, err := New(Options{Selectors: []Selector{SelectorIssue, SelectorPR}})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	response, err := provider.ListCapabilities(context.Background(), &searchv1.ListCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("ListCapabilities returned error: %v", err)
	}
	selectors := []string{}
	for _, surface := range response.GetSurfaces() {
		selectors = append(selectors, surface.GetSelector())
	}
	if !reflect.DeepEqual(selectors, []string{string(SelectorIssue), string(SelectorPR)}) {
		t.Fatalf("selectors = %#v, want issue and PR", selectors)
	}
}

func TestProviderSearchDoesNothingWithoutSelectorHints(t *testing.T) {
	runner := &recordingRunner{}
	provider, err := New(Options{Runner: runner})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	response, err := provider.Search(context.Background(), &searchv1.SearchRequest{Query: "parser"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if runner.called {
		t.Fatal("runner was called without selector hints")
	}
	if len(response.GetResults()) != 0 {
		t.Fatalf("results = %#v, want none", response.GetResults())
	}
}

func TestProviderSearchRunsSupportedHintedSelectors(t *testing.T) {
	runner := &recordingRunner{items: map[Selector][]Item{
		SelectorPR: {{Number: 7, Title: "Improve parser", State: "open", HTMLURL: "https://github.com/example/project/pull/7", RepositoryURL: "https://api.github.com/repos/example/project", UpdatedAt: "2026-04-29T10:00:00Z"}},
	}}
	provider, err := New(Options{Selectors: []Selector{SelectorIssue, SelectorPR}, Runner: runner})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	response, err := provider.Search(context.Background(), &searchv1.SearchRequest{Query: "parser", Limit: proto.Uint32(5), SelectorHints: []string{string(SelectorPR), string(SelectorCode)}})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !reflect.DeepEqual(runner.calls, []runnerCall{{Selector: SelectorPR, Query: "parser type:pr", Limit: 5}}) {
		t.Fatalf("runner calls = %#v, want only PR search", runner.calls)
	}
	if len(response.GetResults()) != 1 {
		t.Fatalf("result count = %d, want 1", len(response.GetResults()))
	}
	result := response.GetResults()[0]
	if result.GetSelector() != string(SelectorPR) || integerFieldValue(t, result, "number") != 7 || textFieldValue(t, result, "title") != "Improve parser" || result.GetGroup().GetTitle() != "example/project" {
		t.Fatalf("result = %#v, want mapped PR", result)
	}
	if !reflect.DeepEqual(result.GetFormat().GetTitleFields(), []string{"number", "title"}) || !reflect.DeepEqual(result.GetFormat().GetDetailFields(), []string{"state", "updated_at"}) {
		t.Fatalf("format = %#v, want number/title plus state/update", result.GetFormat())
	}
}

func TestProviderSearchReturnsNoResultsForUnsupportedHints(t *testing.T) {
	runner := &recordingRunner{}
	provider, err := New(Options{Selectors: []Selector{SelectorIssue}, Runner: runner})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	response, err := provider.Search(context.Background(), &searchv1.SearchRequest{Query: "parser", SelectorHints: []string{string(SelectorRepo)}})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if runner.called {
		t.Fatal("runner was called for unconfigured hinted selector")
	}
	if len(response.GetResults()) != 0 {
		t.Fatalf("results = %#v, want none", response.GetResults())
	}
}

func TestProviderSearchRequiresQueryWhenHintsRequestSearch(t *testing.T) {
	provider, err := New(Options{Runner: &recordingRunner{}})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = provider.Search(context.Background(), &searchv1.SearchRequest{SelectorHints: []string{string(SelectorIssue)}})
	if err == nil {
		t.Fatal("Search succeeded without query")
	}
}

type runnerCall struct {
	Selector Selector
	Query    string
	Limit    int
}

type recordingRunner struct {
	called bool
	calls  []runnerCall
	items  map[Selector][]Item
	err    error
}

func (runner *recordingRunner) Search(_ context.Context, selector Selector, query string, limit int) ([]Item, error) {
	runner.called = true
	runner.calls = append(runner.calls, runnerCall{Selector: selector, Query: query, Limit: limit})
	return append([]Item{}, runner.items[selector]...), runner.err
}

func textFieldValue(t *testing.T, result *searchv1.SearchResponse_Result, key string) string {
	t.Helper()
	for _, field := range result.GetFields() {
		if field.GetKey() == key {
			return field.GetText()
		}
	}
	t.Fatalf("missing text field %q in %#v", key, result.GetFields())
	return ""
}

func integerFieldValue(t *testing.T, result *searchv1.SearchResponse_Result, key string) int64 {
	t.Helper()
	for _, field := range result.GetFields() {
		if field.GetKey() == key {
			return field.GetInteger()
		}
	}
	t.Fatalf("missing integer field %q in %#v", key, result.GetFields())
	return 0
}

func timestampFieldValue(t *testing.T, result *searchv1.SearchResponse_Result, key string) any {
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
