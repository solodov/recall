package gh

import (
	"context"
	"fmt"
	"strings"

	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
	recallprovider "github.com/solodov/recall/provider"
)

// SearchRunner executes one GitHub search domain and returns raw API items.
type SearchRunner interface {
	Search(context.Context, Domain, string, int) ([]Item, error)
}

// Options configures the first-party GitHub search provider.
type Options struct {
	Domains    []Domain
	GitHubPath string
	Runner     SearchRunner
}

// Provider searches GitHub through the gh command. It intentionally performs no
// default search unless recall supplies selector hints, keeping remote API usage opt-in.
type Provider struct {
	domains    []Domain
	gitHubPath string
	runner     SearchRunner
}

// New constructs a GitHub-backed recall provider.
func New(options Options) (*Provider, error) {
	domains, err := normalizeDomains(options.Domains)
	if err != nil {
		return nil, err
	}
	return &Provider{
		domains:    domains,
		gitHubPath: options.GitHubPath,
		runner:     options.Runner,
	}, nil
}

// ListCapabilities advertises configured GitHub selectors without calling GitHub.
func (provider *Provider) ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	surfaces := make([]*searchv1.SearchSurface, 0, len(provider.domains))
	for _, domain := range provider.domains {
		surfaces = append(surfaces, surfaceForDomain(domain))
	}
	return &searchv1.ListCapabilitiesResponse{Surfaces: surfaces}, nil
}

// Search runs only configured domains requested through advisory selector hints
// and maps GitHub results into recall URI hits.
func (provider *Provider) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("search request is nil")
	}
	domains := provider.domainsFromHints(recallprovider.RequestedSelectors(request))
	if len(domains) == 0 {
		return &searchv1.SearchResponse{}, nil
	}
	query := strings.TrimSpace(request.GetQuery())
	if query == "" {
		return nil, fmt.Errorf("github search query is required when selector hints request github domains")
	}

	limit, _ := recallprovider.RequestedLimit(request)
	runner := provider.runner
	if runner == nil {
		runner = Runner{Binary: provider.gitHubPath}
	}

	hits := []*searchv1.SearchHit{}
	for _, domain := range domains {
		domainLimit := remainingLimit(limit, len(hits))
		if limit > 0 && domainLimit == 0 {
			break
		}
		items, err := runner.Search(ctx, domain, queryForDomain(domain, query), domainLimit)
		if err != nil {
			return nil, err
		}
		hits = append(hits, HitsFromItems(domain, items)...)
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return &searchv1.SearchResponse{Hits: hits}, nil
}

func (provider *Provider) domainsFromHints(hints map[string]bool) []Domain {
	if len(hints) == 0 {
		return nil
	}
	domains := make([]Domain, 0, len(provider.domains))
	for _, domain := range provider.domains {
		if selectorMatchesHint(string(domain), hints) {
			domains = append(domains, domain)
		}
	}
	return domains
}

func selectorMatchesHint(selector string, hints map[string]bool) bool {
	for hint := range hints {
		if selector == hint || strings.HasPrefix(selector, hint+":") {
			return true
		}
	}
	return false
}

func surfaceForDomain(domain Domain) *searchv1.SearchSurface {
	switch domain {
	case DomainCode:
		return &searchv1.SearchSurface{Selector: string(domain), Title: "GitHub code", Description: "Search code files on GitHub"}
	case DomainCommit:
		return &searchv1.SearchSurface{Selector: string(domain), Title: "Commits", Description: "Search GitHub commit messages"}
	case DomainIssue:
		return &searchv1.SearchSurface{Selector: string(domain), Title: "Issues", Description: "Search GitHub issue titles and bodies"}
	case DomainPR:
		return &searchv1.SearchSurface{Selector: string(domain), Title: "Pull requests", Description: "Search GitHub pull request titles and bodies"}
	case DomainRepo:
		return &searchv1.SearchSurface{Selector: string(domain), Title: "Repositories", Description: "Search GitHub repository names and descriptions"}
	default:
		return &searchv1.SearchSurface{Selector: string(domain), Title: string(domain)}
	}
}

func queryForDomain(domain Domain, query string) string {
	switch domain {
	case DomainIssue:
		return query + " type:issue"
	case DomainPR:
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
