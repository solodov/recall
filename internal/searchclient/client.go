// Package searchclient exposes a typed SearchProvider client over configured
// transports.
package searchclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"recall/internal/stdiorpc"
	configv1 "recall/proto/recall/config/v1"
	rpcv1 "recall/proto/recall/rpc/v1"
	searchv1 "recall/proto/recall/search/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	SearchService    = "recall.search.v1.SearchProvider"
	SearchMethod     = "Search"
	SearchFullMethod = "/recall.search.v1.SearchProvider/Search"
)

// Client is the transport-independent boundary used by recall's orchestrator.
type Client interface {
	Search(context.Context, *searchv1.SearchRequest) (*searchv1.SearchResponse, error)
}

// ProviderClientOptions controls transport details that are not operator-owned
// provider registry fields.
type ProviderClientOptions struct {
	CapabilityClient   *stdiorpc.CapabilityClient
	EncodingPreference stdiorpc.EncodingPreference
	GRPCDialOptions    []grpc.DialOption
}

// NewProviderClient binds one provider registry entry to the typed search
// client expected by recall's orchestrator.
func NewProviderClient(provider *configv1.Provider, options ProviderClientOptions) (Client, error) {
	if provider == nil {
		return nil, errors.New("provider is nil")
	}
	timeout := time.Duration(provider.GetTimeoutMs()) * time.Millisecond
	switch transport := provider.GetTransport().(type) {
	case *configv1.Provider_Stdio:
		return NewStdioClient(provider.GetId(), transport.Stdio, timeout, options.CapabilityClient, options.EncodingPreference)
	case *configv1.Provider_Grpc:
		return NewGRPCClient(transport.Grpc.GetEndpoint(), timeout, options.GRPCDialOptions...)
	case nil:
		return nil, fmt.Errorf("provider %q has no transport", provider.GetId())
	default:
		return nil, fmt.Errorf("provider %q has unsupported transport %T", provider.GetId(), transport)
	}
}

// StdioClient calls SearchProvider.Search over one-shot stdio RPC processes.
type StdioClient struct {
	providerID         string
	transport          *configv1.StdioTransport
	timeout            time.Duration
	capabilityClient   *stdiorpc.CapabilityClient
	encodingPreference stdiorpc.EncodingPreference
}

// NewStdioClient creates a typed search client backed by the generic stdio RPC
// runner and provider capability discovery.
func NewStdioClient(providerID string, transport *configv1.StdioTransport, timeout time.Duration, capabilityClient *stdiorpc.CapabilityClient, encodingPreference stdiorpc.EncodingPreference) (*StdioClient, error) {
	if strings.TrimSpace(providerID) == "" {
		return nil, errors.New("provider id is required")
	}
	if transport == nil {
		return nil, fmt.Errorf("provider %q stdio transport is nil", providerID)
	}
	if strings.TrimSpace(transport.GetCommand()) == "" {
		return nil, fmt.Errorf("provider %q stdio command is required", providerID)
	}
	if capabilityClient == nil {
		capabilityClient = stdiorpc.NewCapabilityClient()
	}
	return &StdioClient{
		providerID:         providerID,
		transport:          transport,
		timeout:            timeout,
		capabilityClient:   capabilityClient,
		encodingPreference: encodingPreference,
	}, nil
}

// Search discovers the provider's supported stdio payload encodings, chooses a
// mutually supported encoding, then invokes SearchProvider.Search.
func (client *StdioClient) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("search provider %q: request is nil", client.providerID)
	}

	capabilities, err := client.capabilityClient.Get(ctx, client.providerID, client.transport, client.timeout)
	if err != nil {
		return nil, err
	}
	encoding, err := stdiorpc.SelectPayloadEncoding(capabilities, client.encodingPreference)
	if err != nil {
		return nil, fmt.Errorf("search provider %q: select stdio payload encoding: %w", client.providerID, err)
	}

	response := &searchv1.SearchResponse{}
	metadata := stdiorpc.CallMetadata{
		Service:  SearchService,
		Method:   SearchMethod,
		Encoding: encoding,
	}
	if err := stdiorpc.CallUnary(ctx, client.transport, client.timeout, metadata, request, response); err != nil {
		return nil, fmt.Errorf("search provider %q: stdio Search call: %w", client.providerID, err)
	}
	return response, nil
}

// SelectedEncoding returns the encoding recall would use for the current cached
// or freshly discovered stdio capabilities.
func (client *StdioClient) SelectedEncoding(ctx context.Context) (rpcv1.PayloadEncoding, error) {
	capabilities, err := client.capabilityClient.Get(ctx, client.providerID, client.transport, client.timeout)
	if err != nil {
		return rpcv1.PayloadEncoding_PAYLOAD_ENCODING_UNSPECIFIED, err
	}
	return stdiorpc.SelectPayloadEncoding(capabilities, client.encodingPreference)
}

type grpcInvoker interface {
	Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error
}

// GRPCClient calls SearchProvider.Search over gRPC using the standard protobuf
// method name for recall.search.v1.
type GRPCClient struct {
	endpoint string
	timeout  time.Duration
	invoker  grpcInvoker
	close    func() error
}

// NewGRPCClient creates a typed search client for providers reachable over
// gRPC. The current config schema has no TLS fields, so the default transport
// credentials are intentionally local/insecure until config grows that policy.
func NewGRPCClient(endpoint string, timeout time.Duration, dialOptions ...grpc.DialOption) (*GRPCClient, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("grpc endpoint is required")
	}
	if len(dialOptions) == 0 {
		dialOptions = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}
	conn, err := grpc.NewClient(endpoint, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("create grpc client for %q: %w", endpoint, err)
	}
	return &GRPCClient{
		endpoint: endpoint,
		timeout:  timeout,
		invoker:  conn,
		close:    conn.Close,
	}, nil
}

// Search invokes the fully-qualified SearchProvider.Search method with the
// provider timeout applied as an RPC deadline.
func (client *GRPCClient) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("grpc search provider %q: request is nil", client.endpoint)
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if client.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, client.timeout)
		defer cancel()
	}

	response := &searchv1.SearchResponse{}
	if err := client.invoker.Invoke(callCtx, SearchFullMethod, request, response); err != nil {
		return nil, fmt.Errorf("grpc search provider %q: Search call: %w", client.endpoint, err)
	}
	return response, nil
}

// Close releases the underlying gRPC connection when this client owns one.
func (client *GRPCClient) Close() error {
	if client.close == nil {
		return nil
	}
	return client.close()
}

func newGRPCClientWithInvoker(endpoint string, timeout time.Duration, invoker grpcInvoker) *GRPCClient {
	return &GRPCClient{endpoint: endpoint, timeout: timeout, invoker: invoker}
}
