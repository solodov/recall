// Package provider exposes the public Go SDK for implementing recall search providers.
package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/solodov/recall/internal/stdiorpc"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/proto"
)

// Provider is implemented by Go providers that serve recall search requests
// and advertise their provider-local selectors for discovery.
type Provider interface {
	Searcher
	CapabilityLister
}

// Searcher serves recall search requests.
type Searcher interface {
	Search(context.Context, *searchv1.SearchRequest) (*searchv1.SearchResponse, error)
}

// CapabilityLister advertises provider-local selectors without running a search.
type CapabilityLister interface {
	ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error)
}

// SearchFunc adapts a function into a Searcher.
type SearchFunc func(context.Context, *searchv1.SearchRequest) (*searchv1.SearchResponse, error)

// Search calls fn(ctx, request).
func (fn SearchFunc) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	if fn == nil {
		return nil, errors.New("recall provider search function is nil")
	}
	return fn(ctx, request)
}

// CapabilityFunc adapts a function into a CapabilityLister.
type CapabilityFunc func(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error)

// ListCapabilities calls fn(ctx, request).
func (fn CapabilityFunc) ListCapabilities(ctx context.Context, request *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	if fn == nil {
		return nil, errors.New("recall provider capability function is nil")
	}
	return fn(ctx, request)
}

// ServeOptions supplies process-local streams and provider-specific remaining
// arguments for one stdio RPC invocation.
type ServeOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Args   []string
}

// ServeSearch serves one SearchProvider.Search stdio RPC using process streams
// and os.Args when ServeOptions fields are left empty.
func ServeSearch(ctx context.Context, provider Provider) error {
	return ServeSearchWithOptions(ctx, provider, ServeOptions{})
}

// ServeSearchWithOptions serves one SearchProvider stdio RPC. The final
// argument must be a SearchProvider method path; stdin is decoded as protobuf
// binary or textproto and stdout mirrors that format.
func ServeSearchWithOptions(ctx context.Context, provider Provider, options ServeOptions) error {
	if provider == nil {
		return errors.New("recall provider is nil")
	}
	return stdiorpc.ServeOne(ctx, stdiorpc.ServeOptions{
		Stdin:  options.Stdin,
		Stdout: options.Stdout,
		Args:   options.Args,
		Handlers: map[stdiorpc.MethodKey]stdiorpc.UnaryHandler{
			{Service: searchv1.SearchProviderService, Method: searchv1.SearchProviderSearchMethod}: {
				NewRequest: func() proto.Message { return &searchv1.SearchRequest{} },
				Handle: func(ctx context.Context, message proto.Message) (proto.Message, error) {
					request, ok := message.(*searchv1.SearchRequest)
					if !ok {
						return nil, fmt.Errorf("unexpected search request type %T", message)
					}
					return provider.Search(ctx, request)
				},
			},
			{Service: searchv1.SearchProviderService, Method: searchv1.SearchProviderListCapabilitiesMethod}: {
				NewRequest: func() proto.Message { return &searchv1.ListCapabilitiesRequest{} },
				Handle: func(ctx context.Context, message proto.Message) (proto.Message, error) {
					request, ok := message.(*searchv1.ListCapabilitiesRequest)
					if !ok {
						return nil, fmt.Errorf("unexpected list capabilities request type %T", message)
					}
					return provider.ListCapabilities(ctx, request)
				},
			},
		},
	})
}

// RequestedLimit returns a positive caller-specified limit when one is present.
// A missing or zero limit means the provider should return every reasonable match.
func RequestedLimit(request *searchv1.SearchRequest) (int, bool) {
	if request == nil || request.Limit == nil || request.GetLimit() == 0 {
		return 0, false
	}
	return int(request.GetLimit()), true
}

// RequestedSelectors returns non-empty advisory selector hints supplied by
// recall. An empty result means the caller did not request provider-side
// selector narrowing.
func RequestedSelectors(request *searchv1.SearchRequest) map[string]bool {
	requested := map[string]bool{}
	if request == nil {
		return requested
	}
	for _, hint := range request.GetSelectorHints() {
		hint = strings.TrimSpace(hint)
		if hint == "" {
			continue
		}
		requested[hint] = true
	}
	return requested
}
