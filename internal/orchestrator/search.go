// Package orchestrator coordinates query fan-out across configured providers.
package orchestrator

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"recall/internal/normalize"
	"recall/internal/rank"
	"recall/internal/runtime"
	"recall/internal/searchclient"
	configv1 "recall/proto/recall/config/v1"
	searchv1 "recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

// ClientFactory creates a typed search client for a provider selected from the
// operator registry.
type ClientFactory func(*configv1.Provider) (searchclient.Client, error)

// Options controls recall-level provider routing without changing the provider
// SearchRequest contract.
type Options struct {
	// Sources restricts query fan-out to specific provider IDs. Empty means all
	// enabled providers.
	Sources []string

	// Limit overrides provider default_limit when non-zero.
	Limit uint32

	// Kinds post-filters normalized hits by provider kind after providers have
	// searched their own query semantics. It is never sent in SearchRequest.
	Kinds []string

	// ClientFactory is injectable so tests and future diagnostics can exercise
	// orchestration without launching provider processes.
	ClientFactory ClientFactory
}

// Result contains successful provider responses and independent provider
// failures from one query fan-out.
type Result struct {
	Responses   []ProviderResponse
	BlendedHits []rank.Hit
	Failures    []ProviderFailure
}

// ProviderResponse is one successful provider response after validation and
// annotation with recall's configured provider identity.
type ProviderResponse = normalize.ProviderResponse

// ProviderFailure records one provider-specific failure without discarding
// successful results from other providers.
type ProviderFailure struct {
	ProviderID string
	Err        error
}

// Search loads the selected enabled providers from cfg, sends each the same
// query plus provider-local limit, and returns responses in config order.
func Search(run runtime.Context, cfg *configv1.RecallConfig, query string, options Options) (*Result, error) {
	ctx := run.Std()
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("query must be non-empty")
	}
	if cfg == nil {
		return nil, errors.New("recall config is nil")
	}
	clientFactory := options.ClientFactory
	if clientFactory == nil {
		clientFactory = NewDefaultClientFactory()
	}

	selected, err := selectProviders(cfg.GetProviders(), options.Sources)
	if err != nil {
		return nil, err
	}
	kindFilter, err := listFilter(options.Kinds, "kind")
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, errors.New("no enabled providers selected")
	}
	run.Span().Set("provider_count", len(selected), "source_filter_count", len(options.Sources), "kind_filter_count", len(options.Kinds))
	run.Log().InfoContext(ctx, "dispatching recall search", "provider_count", len(selected))

	indexedResults := make(chan indexedProviderResult, len(selected))
	var wg sync.WaitGroup
	for index, provider := range selected {
		wg.Add(1)
		go func(index int, provider *configv1.Provider) {
			defer wg.Done()
			indexedResults <- searchOneProvider(run, index, provider, query, options.Limit, clientFactory)
		}(index, provider)
	}
	wg.Wait()
	close(indexedResults)

	ordered := make([]indexedProviderResult, 0, len(selected))
	for result := range indexedResults {
		ordered = append(ordered, result)
	}
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].index < ordered[right].index
	})

	result := &Result{}
	for _, item := range ordered {
		if item.failure.Err != nil {
			result.Failures = append(result.Failures, item.failure)
			continue
		}
		result.Responses = append(result.Responses, normalize.FilterKinds(item.response, kindFilter))
	}
	if len(result.Responses) == 0 && len(result.Failures) > 0 {
		return result, errors.New("all selected providers failed")
	}
	result.BlendedHits = rank.Blend(result.Responses, providerWeights(selected))
	run.Span().Set("response_count", len(result.Responses), "failure_count", len(result.Failures), "blended_hit_count", len(result.BlendedHits))
	return result, nil
}

// NewDefaultClientFactory creates real transport-backed clients.
func NewDefaultClientFactory() ClientFactory {
	return func(provider *configv1.Provider) (searchclient.Client, error) {
		return searchclient.NewProviderClient(provider, searchclient.ProviderClientOptions{})
	}
}

func searchOneProvider(run runtime.Context, index int, provider *configv1.Provider, query string, limitOverride uint32, clientFactory ClientFactory) indexedProviderResult {
	providerID := provider.GetId()
	run = run.WithLogMeta("provider_id", providerID)
	run, span := run.StartOperation("provider.search", "provider_id", providerID)
	defer span.End()

	client, err := clientFactory(provider)
	if err != nil {
		span.RecordError(err)
		run.Log().ErrorContext(run.Std(), "create provider client", "err", err)
		return indexedProviderResult{index: index, failure: ProviderFailure{ProviderID: providerID, Err: err}}
	}
	if closer, ok := client.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}

	limit := provider.GetDefaultLimit()
	if limitOverride != 0 {
		limit = limitOverride
	}
	request := &searchv1.SearchRequest{Query: query}
	if limit > 0 {
		request.Limit = proto.Uint32(limit)
	}
	var response *searchv1.SearchResponse
	if err := span.Measure("provider_call", func() error {
		var err error
		response, err = client.Search(run.Std(), request)
		return err
	}, "limit", limit); err != nil {
		span.RecordError(err)
		run.Log().WarnContext(run.Std(), "provider search failed", "err", err)
		return indexedProviderResult{index: index, failure: ProviderFailure{ProviderID: providerID, Err: err}}
	}
	if response == nil {
		err := errors.New("provider returned nil response")
		span.RecordError(err)
		return indexedProviderResult{index: index, failure: ProviderFailure{ProviderID: providerID, Err: err}}
	}
	normalized, err := normalize.SearchResponse(providerID, response)
	if err != nil {
		err = fmt.Errorf("invalid provider response: %w", err)
		span.RecordError(err)
		run.Log().WarnContext(run.Std(), "provider response rejected", "err", err)
		return indexedProviderResult{index: index, failure: ProviderFailure{ProviderID: providerID, Err: err}}
	}
	span.Set("hit_count", len(normalized.Hits), "warning_count", len(normalized.Warnings))
	run.Log().InfoContext(run.Std(), "provider search completed", "hit_count", len(normalized.Hits), "warning_count", len(normalized.Warnings))
	return indexedProviderResult{index: index, response: normalized}
}

func selectProviders(providers []*configv1.Provider, sources []string) ([]*configv1.Provider, error) {
	wanted, err := sourceFilter(sources)
	if err != nil {
		return nil, err
	}

	configured := make(map[string]*configv1.Provider, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		configured[provider.GetId()] = provider
	}
	for source := range wanted {
		provider, exists := configured[source]
		if !exists {
			return nil, fmt.Errorf("source %q is not configured", source)
		}
		if !provider.GetEnabled() {
			return nil, fmt.Errorf("source %q is configured but disabled", source)
		}
	}

	selected := make([]*configv1.Provider, 0, len(providers))
	for _, provider := range providers {
		if provider == nil || !provider.GetEnabled() {
			continue
		}
		if len(wanted) > 0 && !wanted[provider.GetId()] {
			continue
		}
		selected = append(selected, provider)
	}
	return selected, nil
}

func providerWeights(providers []*configv1.Provider) map[string]float64 {
	weights := make(map[string]float64, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		weights[provider.GetId()] = provider.GetWeight()
	}
	return weights
}

func sourceFilter(sources []string) (map[string]bool, error) {
	return listFilter(sources, "source")
}

func listFilter(values []string, label string) (map[string]bool, error) {
	wanted := map[string]bool{}
	for _, valueList := range values {
		for _, value := range strings.Split(valueList, ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if wanted[value] {
				return nil, fmt.Errorf("%s %q was requested more than once", label, value)
			}
			wanted[value] = true
		}
	}
	return wanted, nil
}

type indexedProviderResult struct {
	index    int
	response ProviderResponse
	failure  ProviderFailure
}
