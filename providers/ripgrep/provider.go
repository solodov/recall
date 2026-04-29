package ripgrep

import (
	"context"
	"fmt"

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
		Pattern:        query.Pattern,
		Roots:          resolution.Roots,
		FileTypes:      query.FileTypes,
		ExcludedScopes: query.ExcludedScopes,
		Limit:          limit,
	})
	if err != nil {
		return nil, err
	}
	warnings := append([]*searchv1.Warning{}, resolution.Warnings...)
	warnings = append(warnings, result.Warnings...)
	return MatchesToSearchResponse(result.Matches, warnings, HitOptions{Roots: resolution.Roots}), nil
}

func (provider *Provider) resolveRoots() (RootResolution, error) {
	resolver := provider.rootResolver
	if resolver.WorkDir == "" {
		resolver.WorkDir = provider.workDir
	}
	return resolver.ResolveRoots(provider.roots)
}
