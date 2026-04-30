package ripgrep

import (
	"context"
	"fmt"
	"strings"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	recallprovider "github.com/solodov/recall/provider"
)

// MatchRunner executes a parsed ripgrep search and returns structured matches.
type MatchRunner interface {
	Run(context.Context, RunOptions) (RunResult, error)
}

// Options configures the first-party ripgrep search provider.
type Options struct {
	Roots        []string
	RipgrepPath  string
	WorkDir      string
	RootResolver RootResolver
	Runner       MatchRunner
}

// Provider searches one or more code roots through ripgrep.
type Provider struct {
	roots        []string
	ripgrepPath  string
	workDir      string
	rootResolver RootResolver
	runner       MatchRunner
}

// New constructs a ripgrep-backed recall provider.
func New(options Options) *Provider {
	return &Provider{
		roots:        append([]string{}, options.Roots...),
		ripgrepPath:  options.RipgrepPath,
		workDir:      options.WorkDir,
		rootResolver: options.RootResolver,
		runner:       options.Runner,
	}
}

// ListCapabilities advertises the ripgrep-backed search surfaces without
// touching configured roots or invoking ripgrep.
func (provider *Provider) ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	return &searchv1.ListCapabilitiesResponse{Surfaces: []*searchv1.SearchSurface{
		{Selector: SelectorFileName, Title: "File names", Description: "Search root-relative file paths by name"},
		{Selector: SelectorFileContent, Title: "File contents", Description: "Search matching lines in file contents"},
	}}, nil
}

// Search parses the provider-owned query, skips missing configured roots, runs
// ripgrep against existing roots, and maps matches into recall hits.
func (provider *Provider) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("search request is nil")
	}
	query, err := ParseQuery(request.GetQuery())
	if err != nil {
		return nil, err
	}
	query.Selectors = restrictSearchSelectors(query.Selectors, recallprovider.RequestedSelectors(request))
	if len(query.Selectors) == 0 {
		return &searchv1.SearchResponse{}, nil
	}
	resolution, err := provider.resolveRoots()
	if err != nil {
		return nil, err
	}
	if len(resolution.Roots) == 0 {
		return &searchv1.SearchResponse{Warnings: resolution.Warnings}, nil
	}

	limit, _ := recallprovider.RequestedLimit(request)
	runner := provider.runner
	if runner == nil {
		runner = Runner{Binary: provider.ripgrepPath}
	}
	result, err := runner.Run(ctx, RunOptions{
		Pattern:     query.Pattern,
		Roots:       resolution.Roots,
		Selectors:   query.Selectors,
		FileTypes:   query.FileTypes,
		PathFilters: query.PathFilters,
		Limit:       limit,
	})
	if err != nil {
		return nil, err
	}
	warnings := append([]*searchv1.Warning{}, resolution.Warnings...)
	warnings = append(warnings, result.Warnings...)
	return SearchResponseFromRunResult(result, warnings, HitOptions{Roots: resolution.Roots}), nil
}

func (provider *Provider) resolveRoots() (RootResolution, error) {
	resolver := provider.rootResolver
	if resolver.WorkDir == "" {
		resolver.WorkDir = provider.workDir
	}
	return resolver.ResolveRoots(provider.roots)
}

func restrictSearchSelectors(selectors []SearchSelector, hints map[string]bool) []SearchSelector {
	if len(hints) == 0 {
		return selectors
	}
	filtered := make([]SearchSelector, 0, len(selectors))
	for _, selector := range selectors {
		if matchesSearchSelectorHint(selector, hints) {
			filtered = append(filtered, selector)
		}
	}
	return filtered
}

func matchesSearchSelectorHint(selector SearchSelector, hints map[string]bool) bool {
	value := string(selector)
	return hints[value] || selectorMatchesHint(value, hints)
}

func selectorMatchesHint(selector string, hints map[string]bool) bool {
	for hint := range hints {
		if selector == hint || strings.HasPrefix(selector, hint+":") {
			return true
		}
	}
	return false
}
