// Package orchestrator coordinates query fan-out across configured providers.
package orchestrator

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/solodov/recall/internal/normalize"
	"github.com/solodov/recall/internal/rank"
	"github.com/solodov/recall/internal/runtime"
	"github.com/solodov/recall/internal/searchclient"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

// ClientFactory creates a typed search client for a provider selected from the
// operator registry.
type ClientFactory func(*configv1.Provider) (searchclient.Client, error)

// Options controls recall-level selector routing without changing provider-owned
// query semantics.
type Options struct {
	// Selectors restrict query fan-out using source[:object[:match]] selectors.
	// Empty means all enabled providers with no provider-side selector hints.
	Selectors []string

	// Limit overrides provider default_limit when non-zero.
	Limit uint32

	// ClientFactory is injectable so tests and future diagnostics can exercise
	// orchestration without launching provider processes.
	ClientFactory ClientFactory
}

// Result contains successful provider responses and independent provider
// failures from one query fan-out.
type Result struct {
	Responses      []ProviderResponse
	BlendedResults []rank.Result
	Failures       []ProviderFailure
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
// query plus provider-local selector hints and limit, and returns responses in
// config order.
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

	selected, err := selectProviders(cfg.GetProviders(), options.Selectors)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, errors.New("no enabled providers selected")
	}
	run.Span().Set("provider_count", len(selected), "selector_filter_count", len(options.Selectors))
	run.Log().InfoContext(ctx, "dispatching recall search", "provider_count", len(selected))

	indexedResults := make(chan indexedProviderResult, len(selected))
	var wg sync.WaitGroup
	for index, selection := range selected {
		wg.Add(1)
		go func(index int, selection providerSelection) {
			defer wg.Done()
			indexedResults <- searchOneProvider(run, index, selection, query, options.Limit, clientFactory)
		}(index, selection)
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
		result.Responses = append(result.Responses, normalize.FilterSelectors(item.response, item.selectorFilters))
	}
	if len(result.Responses) == 0 && len(result.Failures) > 0 {
		return result, errors.New("all selected providers failed")
	}
	result.BlendedResults = rank.Blend(result.Responses, providerWeights(selected))
	run.Span().Set("response_count", len(result.Responses), "failure_count", len(result.Failures), "blended_result_count", len(result.BlendedResults))
	return result, nil
}

// NewDefaultClientFactory creates real transport-backed clients.
func NewDefaultClientFactory() ClientFactory {
	return func(provider *configv1.Provider) (searchclient.Client, error) {
		return searchclient.NewProviderClient(provider, searchclient.ProviderClientOptions{})
	}
}

func searchOneProvider(run runtime.Context, index int, selection providerSelection, query string, limitOverride uint32, clientFactory ClientFactory) indexedProviderResult {
	provider := selection.Provider
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
	request := &searchv1.SearchRequest{Query: query, SelectorHints: append([]string{}, selection.SelectorHints...)}
	if limit > 0 {
		request.Limit = proto.Uint32(limit)
	}
	var response *searchv1.SearchResponse
	if err := span.Measure("provider_call", func() error {
		var err error
		response, err = client.Search(run.Std(), request)
		return err
	}, "limit", limit, "selector_hint_count", len(selection.SelectorHints)); err != nil {
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
	span.Set("result_count", len(normalized.Results), "warning_count", len(normalized.Warnings))
	run.Log().InfoContext(run.Std(), "provider search completed", "result_count", len(normalized.Results), "warning_count", len(normalized.Warnings))
	return indexedProviderResult{index: index, response: normalized, selectorFilters: append([]string{}, selection.SelectorFilters...)}
}

func selectProviders(providers []*configv1.Provider, selectors []string) ([]providerSelection, error) {
	wanted, err := selectorFilter(selectors)
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
			return nil, fmt.Errorf("selector source %q is not configured", source)
		}
		if !provider.GetEnabled() {
			return nil, fmt.Errorf("selector source %q is configured but disabled", source)
		}
	}

	selected := make([]providerSelection, 0, len(providers))
	for _, provider := range providers {
		if provider == nil || !provider.GetEnabled() {
			continue
		}
		filter, restricted := wanted[provider.GetId()]
		if len(wanted) > 0 && !restricted {
			continue
		}
		selected = append(selected, providerSelection{
			Provider:        provider,
			SelectorHints:   append([]string{}, filter.hints...),
			SelectorFilters: append([]string{}, filter.filters...),
		})
	}
	return selected, nil
}

func providerWeights(selections []providerSelection) map[string]float64 {
	weights := make(map[string]float64, len(selections))
	for _, selection := range selections {
		if selection.Provider == nil {
			continue
		}
		weights[selection.Provider.GetId()] = selection.Provider.GetWeight()
	}
	return weights
}

func selectorFilter(selectors []string) (map[string]selectorSelection, error) {
	wanted := map[string]selectorSelection{}
	seen := map[string]bool{}
	for _, selectorList := range selectors {
		for _, raw := range strings.Split(selectorList, ",") {
			selector, err := parseSelector(raw)
			if err != nil {
				return nil, err
			}
			if selector.Source == "" {
				continue
			}
			canonical := selector.String()
			if seen[canonical] {
				return nil, fmt.Errorf("selector %q was requested more than once", canonical)
			}
			seen[canonical] = true

			selection := wanted[selector.Source]
			if selector.Local == "" {
				selection.all = true
				selection.hints = nil
				selection.filters = nil
			} else if !selection.all {
				selection.hints = append(selection.hints, selector.Local)
				selection.filters = append(selection.filters, selector.Local)
			}
			wanted[selector.Source] = selection
		}
	}
	return wanted, nil
}

func parseSelector(value string) (Selector, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Selector{}, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) > 3 {
		return Selector{}, fmt.Errorf("selector %q must have form source[:object[:match]]", value)
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return Selector{}, fmt.Errorf("selector %q must not contain empty segments", value)
		}
	}
	selector := Selector{Source: parts[0]}
	if len(parts) > 1 {
		selector.Local = strings.Join(parts[1:], ":")
	}
	return selector, nil
}

// Selector is one concrete value in recall's source:object:match selector taxonomy.
type Selector struct {
	Source string
	Local  string
}

func (selector Selector) String() string {
	if selector.Local == "" {
		return selector.Source
	}
	return selector.Source + ":" + selector.Local
}

type selectorSelection struct {
	all     bool
	hints   []string
	filters []string
}

type providerSelection struct {
	Provider        *configv1.Provider
	SelectorHints   []string
	SelectorFilters []string
}

type indexedProviderResult struct {
	index           int
	response        ProviderResponse
	failure         ProviderFailure
	selectorFilters []string
}
