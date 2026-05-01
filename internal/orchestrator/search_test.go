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

func TestSearchRoutesRequestedSelectorsAndLimitOverride(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{
		provider("source-a", true, 10),
		provider("source-b", true, 30),
	}}
	factory := &recordingFactory{}

	result, err := Search(testRuntime(), cfg, "sample", Options{
		Selectors:     []string{"source-b"},
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
		t.Fatal("source-a was called despite selector source filter")
	}
	if factory.requests["source-b"].GetLimit() != 5 {
		t.Fatalf("source-b limit = %d, want override 5", factory.requests["source-b"].GetLimit())
	}
}

func TestSearchAppliesSelectorAsPostFilterAndProviderHint(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{provider("example", true, 10)}}
	factory := &recordingFactory{results: map[string][]*searchv1.SearchResponse_Result{
		"example": {
			result("example:note", "note:content", "Note"),
			result("example:event", "event:content", "Event"),
		},
	}}

	result, err := Search(testRuntime(), cfg, "sample query", Options{Selectors: []string{"example:event:content"}, ClientFactory: factory.New})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	request := factory.requests["example"]
	if request.GetQuery() != "sample query" || request.GetLimit() != 10 {
		t.Fatalf("provider request = %#v, want original query and limit", request)
	}
	if !reflect.DeepEqual(request.GetSelectorHints(), []string{"event:content"}) {
		t.Fatalf("selector hints = %#v, want event:content", request.GetSelectorHints())
	}
	if len(result.Responses) != 1 || len(result.Responses[0].Results) != 1 {
		t.Fatalf("responses = %#v, want one filtered result", result.Responses)
	}
	if result.Responses[0].Results[0].Result.GetId() != "example:event" {
		t.Fatalf("filtered result = %#v, want event result", result.Responses[0].Results[0].Result)
	}
	if len(result.BlendedResults) != 1 || result.BlendedResults[0].Normalized.Result.GetId() != "example:event" {
		t.Fatalf("blended results = %#v, want only event result", result.BlendedResults)
	}
}

func TestSearchMatchesSelectorPrefixes(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{provider("code", true, 10)}}
	factory := &recordingFactory{results: map[string][]*searchv1.SearchResponse_Result{
		"code": {
			result("code:path", "file:name", "Path"),
			result("code:content", "file:content", "Content"),
			result("code:note", "note:content", "Note"),
		},
	}}

	result, err := Search(testRuntime(), cfg, "sample query", Options{Selectors: []string{"code:file"}, ClientFactory: factory.New})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(result.Responses) != 1 || len(result.Responses[0].Results) != 2 {
		t.Fatalf("responses = %#v, want file results", result.Responses)
	}
	got := result.Responses[0].Results[0].Result.GetId() + "," + result.Responses[0].Results[1].Result.GetId()
	if got != "code:path,code:content" {
		t.Fatalf("filtered ids = %q, want file results", got)
	}
	if !reflect.DeepEqual(factory.requests["code"].GetSelectorHints(), []string{"file"}) {
		t.Fatalf("selector hints = %#v, want file prefix", factory.requests["code"].GetSelectorHints())
	}
}

func TestSearchRejectsDuplicateSelectorFilter(t *testing.T) {
	_, err := Search(testRuntime(), &configv1.RecallConfig{Providers: []*configv1.Provider{provider("code", true, 10)}}, "query", Options{Selectors: []string{"code:file:name,code:file:name"}, ClientFactory: (&recordingFactory{}).New})
	if err == nil || !strings.Contains(err.Error(), "selector") {
		t.Fatalf("Search error = %v, want duplicate selector error", err)
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

	_, err := Search(testRuntime(), cfg, "query", Options{Selectors: []string{"missing"}, ClientFactory: (&recordingFactory{}).New})
	if err == nil {
		t.Fatal("Search succeeded with unknown source")
	}
}

type recordingFactory struct {
	requests map[string]*searchv1.SearchRequest
	failures map[string]error
	results  map[string][]*searchv1.SearchResponse_Result
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
	if results := client.factory.results[client.providerID]; len(results) > 0 {
		return &searchv1.SearchResponse{Results: results}, nil
	}
	return &searchv1.SearchResponse{Results: []*searchv1.SearchResponse_Result{
		result(client.providerID+":1", "note:content", client.providerID+" result"),
	}}, nil
}

func (client fakeClient) ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	return &searchv1.ListCapabilitiesResponse{}, nil
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

func result(id string, selector string, title string) *searchv1.SearchResponse_Result {
	return &searchv1.SearchResponse_Result{
		Id:       id,
		Selector: selector,
		Fields: []*searchv1.SearchResponse_Result_Field{{
			Key:   "title",
			Value: &searchv1.SearchResponse_Result_Field_Text{Text: title},
		}},
	}
}
