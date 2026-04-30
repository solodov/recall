package orchestrator

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/solodov/recall/internal/runtime"
	"github.com/solodov/recall/internal/searchclient"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
)

func TestSearchSelectsEnabledProvidersAndBuildsRequests(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{
		provider("source-a", true, 10),
		provider("disabled", false, 20),
		provider("source-b", true, 30),
	}}
	factory := &recordingFactory{}

	result, err := Search(testRuntime(), cfg, "sample query", Options{ClientFactory: factory.New})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(result.Responses) != 2 {
		t.Fatalf("response count = %d, want 2", len(result.Responses))
	}
	if result.Responses[0].ProviderID != "source-a" || result.Responses[1].ProviderID != "source-b" {
		t.Fatalf("responses were not returned in config order: %#v", result.Responses)
	}
	if len(factory.requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(factory.requests))
	}
	if factory.requests["source-a"].GetQuery() != "sample query" || factory.requests["source-a"].GetLimit() != 10 {
		t.Fatalf("source-a request = %#v", factory.requests["source-a"])
	}
	if factory.requests["source-b"].GetQuery() != "sample query" || factory.requests["source-b"].GetLimit() != 30 {
		t.Fatalf("source-b request = %#v", factory.requests["source-b"])
	}
}

func TestSearchRoutesRequestedSourcesAndLimitOverride(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{
		provider("source-a", true, 10),
		provider("source-b", true, 30),
	}}
	factory := &recordingFactory{}

	result, err := Search(testRuntime(), cfg, "sample", Options{
		Sources:       []string{"source-b"},
		Limit:         5,
		ClientFactory: factory.New,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(result.Responses) != 1 || result.Responses[0].ProviderID != "source-b" {
		t.Fatalf("responses = %#v, want only source-b", result.Responses)
	}
	if _, exists := factory.requests["source-a"]; exists {
		t.Fatal("source-a was called despite source filter")
	}
	if factory.requests["source-b"].GetLimit() != 5 {
		t.Fatalf("source-b limit = %d, want override 5", factory.requests["source-b"].GetLimit())
	}
}

func TestSearchAppliesKindAsPostFilterAndProviderHint(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{provider("example", true, 10)}}
	factory := &recordingFactory{hits: map[string][]*searchv1.SearchHit{
		"example": {
			{Id: "example:note", Selector: "note", Title: "Note"},
			{Id: "example:event", Selector: "event", Title: "Event"},
		},
	}}

	result, err := Search(testRuntime(), cfg, "sample query", Options{Kinds: []string{"event"}, ClientFactory: factory.New})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	request := factory.requests["example"]
	if request.GetQuery() != "sample query" || request.GetLimit() != 10 {
		t.Fatalf("provider request = %#v, want original query and limit", request)
	}
	if !reflect.DeepEqual(request.GetSelectorHints(), []string{"event"}) {
		t.Fatalf("kind hints = %#v, want event", request.GetSelectorHints())
	}
	if len(result.Responses) != 1 || len(result.Responses[0].Hits) != 1 {
		t.Fatalf("responses = %#v, want one filtered hit", result.Responses)
	}
	if result.Responses[0].Hits[0].Hit.GetId() != "example:event" {
		t.Fatalf("filtered hit = %#v, want event hit", result.Responses[0].Hits[0].Hit)
	}
	if len(result.BlendedHits) != 1 || result.BlendedHits[0].Normalized.Hit.GetId() != "example:event" {
		t.Fatalf("blended hits = %#v, want only event hit", result.BlendedHits)
	}
}

func TestSearchExpandsPathAndContentKindAliases(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{provider("code", true, 10)}}
	factory := &recordingFactory{hits: map[string][]*searchv1.SearchHit{
		"code": {
			{Id: "code:path", Selector: "path_match", Title: "Path"},
			{Id: "code:content", Selector: "code_match", Title: "Content"},
			{Id: "code:note", Selector: "note", Title: "Note"},
		},
	}}

	result, err := Search(testRuntime(), cfg, "sample query", Options{Kinds: []string{"path,content"}, ClientFactory: factory.New})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(result.Responses) != 1 || len(result.Responses[0].Hits) != 2 {
		t.Fatalf("responses = %#v, want path and content hits", result.Responses)
	}
	got := result.Responses[0].Hits[0].Hit.GetId() + "," + result.Responses[0].Hits[1].Hit.GetId()
	if got != "code:path,code:content" {
		t.Fatalf("filtered ids = %q, want path and content", got)
	}
	if !reflect.DeepEqual(factory.requests["code"].GetSelectorHints(), []string{"path", "path_match", "content", "code_match"}) {
		t.Fatalf("kind hints = %#v, want expanded path/content hints", factory.requests["code"].GetSelectorHints())
	}
}

func TestSearchRejectsDuplicateKindFilter(t *testing.T) {
	_, err := Search(testRuntime(), &configv1.RecallConfig{Providers: []*configv1.Provider{provider("code", true, 10)}}, "query", Options{Kinds: []string{"path,path"}, ClientFactory: (&recordingFactory{}).New})
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("Search error = %v, want duplicate kind error", err)
	}
}

func TestSearchKeepsPartialFailuresSeparate(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{
		provider("ok", true, 10),
		provider("bad", true, 10),
	}}
	factory := &recordingFactory{failures: map[string]error{"bad": errors.New("boom")}}

	result, err := Search(testRuntime(), cfg, "query", Options{ClientFactory: factory.New})
	if err != nil {
		t.Fatalf("Search returned error despite partial success: %v", err)
	}

	if len(result.Responses) != 1 || result.Responses[0].ProviderID != "ok" {
		t.Fatalf("responses = %#v, want only ok", result.Responses)
	}
	if len(result.Failures) != 1 || result.Failures[0].ProviderID != "bad" {
		t.Fatalf("failures = %#v, want bad failure", result.Failures)
	}
}

func TestSearchFailsWhenAllSelectedProvidersFail(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{provider("bad", true, 10)}}
	factory := &recordingFactory{failures: map[string]error{"bad": errors.New("boom")}}

	result, err := Search(testRuntime(), cfg, "query", Options{ClientFactory: factory.New})
	if err == nil {
		t.Fatal("Search succeeded with all providers failed")
	}
	if result == nil || len(result.Failures) != 1 {
		t.Fatalf("result = %#v, want one failure", result)
	}
}

func TestSearchRejectsUnknownSource(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{provider("source-a", true, 10)}}

	_, err := Search(testRuntime(), cfg, "query", Options{Sources: []string{"missing"}, ClientFactory: (&recordingFactory{}).New})
	if err == nil {
		t.Fatal("Search succeeded with unknown source")
	}
}

type recordingFactory struct {
	requests map[string]*searchv1.SearchRequest
	failures map[string]error
	hits     map[string][]*searchv1.SearchHit
}

func (factory *recordingFactory) New(provider *configv1.Provider) (searchclient.Client, error) {
	if factory.requests == nil {
		factory.requests = map[string]*searchv1.SearchRequest{}
	}
	providerID := provider.GetId()
	return fakeClient{
		providerID: providerID,
		factory:    factory,
	}, nil
}

type fakeClient struct {
	providerID string
	factory    *recordingFactory
}

func (client fakeClient) Search(_ context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	client.factory.requests[client.providerID] = request
	if err := client.factory.failures[client.providerID]; err != nil {
		return nil, err
	}
	if hits := client.factory.hits[client.providerID]; len(hits) > 0 {
		return &searchv1.SearchResponse{Hits: hits}, nil
	}
	return &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{{
		Id:    client.providerID + ":1",
		Selector:  "note",
		Title: client.providerID + " result",
	}}}, nil
}

func testRuntime() runtime.Context {
	return runtime.New(context.Background(), nil)
}

func provider(id string, enabled bool, limit uint32) *configv1.Provider {
	return &configv1.Provider{
		Id:           id,
		Enabled:      enabled,
		Weight:       1,
		TimeoutMs:    1500,
		DefaultLimit: limit,
		Transports: []*configv1.Transport{{Transport: &configv1.Transport_Stdio{Stdio: &configv1.StdioTransport{
			Command: "provider-" + id,
		}}}},
	}
}
