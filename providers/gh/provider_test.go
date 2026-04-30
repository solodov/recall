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
	if len(response.GetHits()) != 0 {
		t.Fatalf("hits = %#v, want none", response.GetHits())
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
	if len(response.GetHits()) != 1 {
		t.Fatalf("hit count = %d, want 1", len(response.GetHits()))
	}
	hit := response.GetHits()[0]
	if hit.GetSelector() != string(SelectorPR) || hit.GetTitle() != "#7 Improve parser" || hit.GetGroup().GetTitle() != "example/project" {
		t.Fatalf("hit = %#v, want mapped PR", hit)
	}
}

func TestProviderSearchReturnsNoHitsForUnsupportedHints(t *testing.T) {
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
	if len(response.GetHits()) != 0 {
		t.Fatalf("hits = %#v, want none", response.GetHits())
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
