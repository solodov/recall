// Package searchclient exposes a typed SearchProvider client over configured
// transports.
package searchclient

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/solodov/recall/internal/stdiorpc"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	SearchService                = searchv1.SearchProviderService
	SearchMethod                 = searchv1.SearchProviderSearchMethod
	SearchFullMethod             = searchv1.SearchProviderSearchPath
	ListCapabilitiesMethod       = searchv1.SearchProviderListCapabilitiesMethod
	ListCapabilitiesFullMethod   = searchv1.SearchProviderListCapabilitiesPath

	defaultGRPCDialTimeout = 5 * time.Second
)

// Client is the transport-independent boundary used by recall's orchestrator.
type Client interface {
	Search(context.Context, *searchv1.SearchRequest) (*searchv1.SearchResponse, error)
	ListCapabilities(context.Context, *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error)
}

// ProviderClientOptions controls transport details that are not operator-owned
// provider registry fields.
type ProviderClientOptions struct {
	GRPCDialOptions []grpc.DialOption
}

var errTransportDial = errors.New("transport dial failed")

// NewProviderClient binds one provider registry entry to the typed search
// client expected by recall's orchestrator. Transports are tried in config
// order, and only dial failures fall through to the next transport.
func NewProviderClient(provider *configv1.Provider, options ProviderClientOptions) (Client, error) {
	if provider == nil {
		return nil, errors.New("provider is nil")
	}
	if len(provider.GetTransports()) == 0 {
		return nil, fmt.Errorf("provider %q has no transports", provider.GetId())
	}

	timeout := time.Duration(provider.GetTimeoutMs()) * time.Millisecond
	dialFailures := []error{}
	for index, transport := range provider.GetTransports() {
		client, err := newTransportClient(provider.GetId(), index, transport, timeout, options)
		if err == nil {
			return client, nil
		}
		if errors.Is(err, errTransportDial) {
			dialFailures = append(dialFailures, err)
			continue
		}
		return nil, err
	}

	return nil, fmt.Errorf("provider %q: no transports could be dialed: %w", provider.GetId(), errors.Join(dialFailures...))
}

// newTransportClient creates a client for one configured transport entry.
func newTransportClient(providerID string, index int, transport *configv1.Transport, timeout time.Duration, options ProviderClientOptions) (Client, error) {
	location := fmt.Sprintf("provider %q transports[%d]", providerID, index)
	if transport == nil {
		return nil, fmt.Errorf("%s is nil", location)
	}

	switch transport := transport.GetTransport().(type) {
	case *configv1.Transport_Stdio:
		client, err := NewStdioClient(providerID, transport.Stdio, timeout)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", location, err)
		}
		return client, nil
	case *configv1.Transport_Grpc:
		client, err := NewGRPCClient(transport.Grpc.GetEndpoint(), timeout, options.GRPCDialOptions...)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", location, err)
		}
		return client, nil
	case nil:
		return nil, fmt.Errorf("%s must set exactly one of stdio or grpc", location)
	default:
		return nil, fmt.Errorf("%s has unsupported transport %T", location, transport)
	}
}

// StdioClient calls SearchProvider.Search over one-shot stdio RPC processes.
type StdioClient struct {
	providerID string
	transport  *configv1.StdioTransport
	timeout    time.Duration
}

// NewStdioClient creates a typed search client after checking that the stdio
// command can be resolved by this process.
func NewStdioClient(providerID string, transport *configv1.StdioTransport, timeout time.Duration) (*StdioClient, error) {
	if strings.TrimSpace(providerID) == "" {
		return nil, errors.New("provider id is required")
	}
	if transport == nil {
		return nil, fmt.Errorf("provider %q stdio transport is nil", providerID)
	}
	command := transport.GetCommand()
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("provider %q stdio command is required", providerID)
	}
	if _, err := exec.LookPath(command); err != nil {
		return nil, fmt.Errorf("%w: provider %q stdio command %q: %w", errTransportDial, providerID, command, err)
	}
	return &StdioClient{
		providerID: providerID,
		transport:  transport,
		timeout:    timeout,
	}, nil
}

// Search invokes SearchProvider.Search over a one-shot stdio process.
func (client *StdioClient) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("search provider %q: request is nil", client.providerID)
	}

	response := &searchv1.SearchResponse{}
	method := stdiorpc.MethodKey{Service: SearchService, Method: SearchMethod}
	if err := stdiorpc.CallUnary(ctx, client.transport, client.timeout, method, request, response); err != nil {
		return nil, fmt.Errorf("search provider %q: stdio Search call: %w", client.providerID, err)
	}
	return response, nil
}

// ListCapabilities invokes SearchProvider.ListCapabilities over a one-shot stdio process.
func (client *StdioClient) ListCapabilities(ctx context.Context, request *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	if request == nil {
		request = &searchv1.ListCapabilitiesRequest{}
	}

	response := &searchv1.ListCapabilitiesResponse{}
	method := stdiorpc.MethodKey{Service: SearchService, Method: ListCapabilitiesMethod}
	if err := stdiorpc.CallUnary(ctx, client.transport, client.timeout, method, request, response); err != nil {
		return nil, fmt.Errorf("search provider %q: stdio ListCapabilities call: %w", client.providerID, err)
	}
	return response, nil
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

// NewGRPCClient creates a typed search client after establishing gRPC
// connectivity. The current config schema has no TLS fields, so the default
// transport credentials are intentionally local/insecure until config grows
// that policy.
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
	if err := dialGRPCConnection(context.Background(), conn, timeout); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: dial grpc endpoint %q: %w", errTransportDial, endpoint, err)
	}
	return &GRPCClient{
		endpoint: endpoint,
		timeout:  timeout,
		invoker:  conn,
		close:    conn.Close,
	}, nil
}

// dialGRPCConnection waits until gRPC reports a usable connection, so provider
// fallback only advances when this transport cannot be reached.
func dialGRPCConnection(ctx context.Context, conn *grpc.ClientConn, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultGRPCDialTimeout
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		state := conn.GetState()
		switch state {
		case connectivity.Ready:
			return nil
		case connectivity.Idle:
			conn.Connect()
		case connectivity.TransientFailure:
			return fmt.Errorf("connection entered %s", state)
		case connectivity.Shutdown:
			return errors.New("connection shut down")
		}
		if !conn.WaitForStateChange(ctx, state) {
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("connection remained %s", state)
		}
	}
}

// Search invokes the fully-qualified SearchProvider.Search method with the
// provider timeout applied as an RPC deadline.
func (client *GRPCClient) Search(ctx context.Context, request *searchv1.SearchRequest) (*searchv1.SearchResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("grpc search provider %q: request is nil", client.endpoint)
	}

	response := &searchv1.SearchResponse{}
	if err := client.invoke(ctx, SearchFullMethod, request, response); err != nil {
		return nil, fmt.Errorf("grpc search provider %q: Search call: %w", client.endpoint, err)
	}
	return response, nil
}

// ListCapabilities invokes the fully-qualified SearchProvider.ListCapabilities method.
func (client *GRPCClient) ListCapabilities(ctx context.Context, request *searchv1.ListCapabilitiesRequest) (*searchv1.ListCapabilitiesResponse, error) {
	if request == nil {
		request = &searchv1.ListCapabilitiesRequest{}
	}

	response := &searchv1.ListCapabilitiesResponse{}
	if err := client.invoke(ctx, ListCapabilitiesFullMethod, request, response); err != nil {
		return nil, fmt.Errorf("grpc search provider %q: ListCapabilities call: %w", client.endpoint, err)
	}
	return response, nil
}

func (client *GRPCClient) invoke(ctx context.Context, method string, request any, response any) error {
	callCtx := ctx
	var cancel context.CancelFunc
	if client.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, client.timeout)
		defer cancel()
	}
	return client.invoker.Invoke(callCtx, method, request, response)
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
