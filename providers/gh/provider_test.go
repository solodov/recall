package gh

import (
	"context"
	"reflect"
	"testing"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	"google.golang.org/protobuf/proto"
)

func TestProviderSearchDoesNothingWithoutKindHints(t *testing.T) {
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
		t.Fatal("runner was called without kind hints")
	}
	if len(response.GetHits()) != 0 {
		t.Fatalf("hits = %#v, want none", response.GetHits())
	}
}

func TestProviderSearchRunsSupportedHintedDomains(t *testing.T) {
	runner := &recordingRunner{items: map[Domain][]Item{
		DomainPR: {{Number: 7, Title: "Improve parser", State: "open", HTMLURL: "https://github.com/example/project/pull/7", RepositoryURL: "https://api.github.com/repos/example/project", UpdatedAt: "2026-04-29T10:00:00Z"}},
	}}
	provider, err := New(Options{Domains: []Domain{DomainIssue, DomainPR}, Runner: runner})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	response, err := provider.Search(context.Background(), &searchv1.SearchRequest{Query: "parser", Limit: proto.Uint32(5), KindHints: []string{"pr", "code"}})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !reflect.DeepEqual(runner.calls, []runnerCall{{Domain: DomainPR, Query: "parser type:pr", Limit: 5}}) {
		t.Fatalf("runner calls = %#v, want only PR search", runner.calls)
	}
	if len(response.GetHits()) != 1 {
		t.Fatalf("hit count = %d, want 1", len(response.GetHits()))
	}
	hit := response.GetHits()[0]
	if hit.GetKind() != "pr" || hit.GetTitle() != "#7 Improve parser" || hit.GetGroup().GetTitle() != "example/project" {
		t.Fatalf("hit = %#v, want mapped PR", hit)
	}
}

func TestProviderSearchReturnsNoHitsForUnsupportedHints(t *testing.T) {
	runner := &recordingRunner{}
	provider, err := New(Options{Domains: []Domain{DomainIssue}, Runner: runner})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	response, err := provider.Search(context.Background(), &searchv1.SearchRequest{Query: "parser", KindHints: []string{"repo"}})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if runner.called {
		t.Fatal("runner was called for unconfigured hinted domain")
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

	_, err = provider.Search(context.Background(), &searchv1.SearchRequest{KindHints: []string{"issue"}})
	if err == nil {
		t.Fatal("Search succeeded without query")
	}
}

type runnerCall struct {
	Domain Domain
	Query  string
	Limit  int
}

type recordingRunner struct {
	called bool
	calls  []runnerCall
	items  map[Domain][]Item
	err    error
}

func (runner *recordingRunner) Search(_ context.Context, domain Domain, query string, limit int) ([]Item, error) {
	runner.called = true
	runner.calls = append(runner.calls, runnerCall{Domain: domain, Query: query, Limit: limit})
	return append([]Item{}, runner.items[domain]...), runner.err
}
