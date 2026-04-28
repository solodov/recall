package orchestrator

import (
	"context"
	"errors"
	"testing"

	"recall/internal/searchclient"
	configv1 "recall/proto/recall/config/v1"
	searchv1 "recall/proto/recall/search/v1"
)

func TestSearchSelectsEnabledProvidersAndBuildsRequests(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{
		provider("org", true, 10),
		provider("disabled", false, 20),
		provider("mail", true, 30),
	}}
	factory := &recordingFactory{}

	result, err := Search(context.Background(), cfg, "alice meeting", Options{ClientFactory: factory.New})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(result.Responses) != 2 {
		t.Fatalf("response count = %d, want 2", len(result.Responses))
	}
	if result.Responses[0].ProviderID != "org" || result.Responses[1].ProviderID != "mail" {
		t.Fatalf("responses were not returned in config order: %#v", result.Responses)
	}
	if len(factory.requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(factory.requests))
	}
	if factory.requests["org"].GetQuery() != "alice meeting" || factory.requests["org"].GetLimit() != 10 {
		t.Fatalf("org request = %#v", factory.requests["org"])
	}
	if factory.requests["mail"].GetQuery() != "alice meeting" || factory.requests["mail"].GetLimit() != 30 {
		t.Fatalf("mail request = %#v", factory.requests["mail"])
	}
}

func TestSearchRoutesRequestedSourcesAndLimitOverride(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{
		provider("org", true, 10),
		provider("mail", true, 30),
	}}
	factory := &recordingFactory{}

	result, err := Search(context.Background(), cfg, "deploy", Options{
		Sources:       []string{"mail"},
		Limit:         5,
		ClientFactory: factory.New,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if len(result.Responses) != 1 || result.Responses[0].ProviderID != "mail" {
		t.Fatalf("responses = %#v, want only mail", result.Responses)
	}
	if _, exists := factory.requests["org"]; exists {
		t.Fatal("org was called despite source filter")
	}
	if factory.requests["mail"].GetLimit() != 5 {
		t.Fatalf("mail limit = %d, want override 5", factory.requests["mail"].GetLimit())
	}
}

func TestSearchKeepsPartialFailuresSeparate(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{
		provider("ok", true, 10),
		provider("bad", true, 10),
	}}
	factory := &recordingFactory{failures: map[string]error{"bad": errors.New("boom")}}

	result, err := Search(context.Background(), cfg, "query", Options{ClientFactory: factory.New})
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

	result, err := Search(context.Background(), cfg, "query", Options{ClientFactory: factory.New})
	if err == nil {
		t.Fatal("Search succeeded with all providers failed")
	}
	if result == nil || len(result.Failures) != 1 {
		t.Fatalf("result = %#v, want one failure", result)
	}
}

func TestSearchRejectsUnknownSource(t *testing.T) {
	cfg := &configv1.RecallConfig{Providers: []*configv1.Provider{provider("org", true, 10)}}

	_, err := Search(context.Background(), cfg, "query", Options{Sources: []string{"missing"}, ClientFactory: (&recordingFactory{}).New})
	if err == nil {
		t.Fatal("Search succeeded with unknown source")
	}
}

type recordingFactory struct {
	requests map[string]*searchv1.SearchRequest
	failures map[string]error
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
	return &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{{
		Id:    client.providerID + ":1",
		Kind:  "note",
		Title: client.providerID + " result",
	}}}, nil
}

func provider(id string, enabled bool, limit uint32) *configv1.Provider {
	return &configv1.Provider{
		Id:           id,
		Enabled:      enabled,
		Weight:       1,
		TimeoutMs:    1500,
		DefaultLimit: limit,
		Transport:    &configv1.Provider_Stdio{Stdio: &configv1.StdioTransport{Command: "provider-" + id}},
	}
}
