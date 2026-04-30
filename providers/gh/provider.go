package gh

import (
	"context"
	"fmt"
	"strings"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	recallprovider "github.com/solodov/recall/provider"
)

// SearchRunner executes one GitHub search selector and returns raw API items.
type SearchRunner interface {
	Search(context.Context, Selector, string, int) ([]Item, error)
}

// Options configures the first-party GitHub search provider.
type Options struct {
	Selectors  []Selector
	GitHubPath string
	Runner     SearchRunner
}

// Provider searches GitHub through the gh command. It intentionally performs no
// default search unless recall supplies selector hints, keeping remote API usage opt-in.
type Provider struct {
	selectors  []Selector
	gitHubPath string
	runner     SearchRunner
}

// New constructs a GitHub-backed recall provider.
func New(options Options) (*Provider, error) {
	selectors, err := normalizeSelectors(options.Selectors)
	if err != nil {
		return nil, err
	}
	return &Provider{
		selectors:  selectors,
		gitHubPath: options.GitHubPath,
		runner:     options.Runner,
	}, nil
}

// ListCapabilities advertises configured GitHub selectors without calling GitHub.
func (provider *Provider) ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	surfaces := make([]*searchv1.SearchSurface, 0, len(provider.selectors))
	for _, selector := range provider.selectors {
		surfaces = append(surfaces, surfaceForSelector(selector))
	}
	return &searchv1.ListCapabilitiesResponse{Surfaces: surfaces}, nil
}

// Search runs only configured selectors requested through advisory selector
// hints and maps GitHub results into recall URI hits.
func (provider *Provider) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("search request is nil")
	}
	selectors := provider.selectorsFromHints(recallprovider.RequestedSelectors(request))
	if len(selectors) == 0 {
		return &searchv1.SearchResponse{}, nil
	}
	query := strings.TrimSpace(request.GetQuery())
	if query == "" {
		return nil, fmt.Errorf("github search query is required when selector hints request github surfaces")
	}

	limit, _ := recallprovider.RequestedLimit(request)
	runner := provider.runner
	if runner == nil {
		runner = Runner{Binary: provider.gitHubPath}
	}

	hits := []*searchv1.SearchHit{}
	for _, selector := range selectors {
		selectorLimit := remainingLimit(limit, len(hits))
		if limit > 0 && selectorLimit == 0 {
			break
		}
		items, err := runner.Search(ctx, selector, queryForSelector(selector, query), selectorLimit)
		if err != nil {
			return nil, err
		}
		hits = append(hits, HitsFromItems(selector, items)...)
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return &searchv1.SearchResponse{Hits: hits}, nil
}

func (provider *Provider) selectorsFromHints(hints map[string]bool) []Selector {
	if len(hints) == 0 {
		return nil
	}
	selectors := make([]Selector, 0, len(provider.selectors))
	for _, selector := range provider.selectors {
		if selectorMatchesHint(string(selector), hints) {
			selectors = append(selectors, selector)
		}
	}
	return selectors
}

func selectorMatchesHint(selector string, hints map[string]bool) bool {
	for hint := range hints {
		if selector == hint || strings.HasPrefix(selector, hint+":") {
			return true
		}
	}
	return false
}

func surfaceForSelector(selector Selector) *searchv1.SearchSurface {
	switch selector {
	case SelectorCode:
		return &searchv1.SearchSurface{Selector: string(selector), Title: "GitHub code", Description: "Search code files on GitHub"}
	case SelectorCommit:
		return &searchv1.SearchSurface{Selector: string(selector), Title: "Commits", Description: "Search GitHub commit messages"}
	case SelectorIssue:
		return &searchv1.SearchSurface{Selector: string(selector), Title: "Issues", Description: "Search GitHub issue titles and bodies"}
	case SelectorPR:
		return &searchv1.SearchSurface{Selector: string(selector), Title: "Pull requests", Description: "Search GitHub pull request titles and bodies"}
	case SelectorRepo:
		return &searchv1.SearchSurface{Selector: string(selector), Title: "Repositories", Description: "Search GitHub repository names and descriptions"}
	default:
		return &searchv1.SearchSurface{Selector: string(selector), Title: string(selector)}
	}
}

func queryForSelector(selector Selector, query string) string {
	switch selector {
	case SelectorIssue:
		return query + " type:issue"
	case SelectorPR:
		return query + " type:pr"
	default:
		return query
	}
}

func remainingLimit(limit int, used int) int {
	if limit <= 0 {
		return 0
	}
	remaining := limit - used
	if remaining < 0 {
		return 0
	}
	return remaining
}
