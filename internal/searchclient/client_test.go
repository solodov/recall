package searchclient

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solodov/recall/internal/stdiorpc"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

func TestStdioClientSearchUsesTypedSearchPath(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "provider-log")
	transport := helperTransport(logPath)
	client, err := NewStdioClient("example", transport, time.Second)
	if err != nil {
		t.Fatalf("NewStdioClient returned error: %v", err)
	}

	response, err := client.Search(context.Background(), &searchv1.SearchRequest{Query: "sample note", Limit: proto.Uint32(3)})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	hits := response.GetHits()
	if len(hits) != 1 {
		t.Fatalf("hit count = %d, want 1", len(hits))
	}
	if hits[0].GetId() != "example:sample note" || hits[0].GetTitle() != "Result for sample note" {
		t.Fatalf("unexpected hit: %#v", hits[0])
	}

	log := readHelperLog(t, logPath)
	for _, want := range []string{
		"path=/recall.search.v1.SearchProvider/Search",
		"format=protobuf_binary",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("helper log %q does not contain %q", log, want)
		}
	}
}

func TestNewProviderClientCreatesTransportSpecificClients(t *testing.T) {
	stdioProvider := &configv1.Provider{
		Id:         "example",
		TimeoutMs:  1500,
		Transports: []*configv1.Transport{stdioTransport(&configv1.StdioTransport{Command: os.Args[0]})},
	}

	stdioClient, err := NewProviderClient(stdioProvider, ProviderClientOptions{})
	if err != nil {
		t.Fatalf("NewProviderClient(stdio) returned error: %v", err)
	}
	if _, ok := stdioClient.(*StdioClient); !ok {
		t.Fatalf("stdio provider client has type %T, want *StdioClient", stdioClient)
	}

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	defer server.Stop()
	go func() { _ = server.Serve(listener) }()
	grpcProvider := &configv1.Provider{
		Id:         "remote",
		TimeoutMs:  1500,
		Transports: []*configv1.Transport{grpcTransport("passthrough:///remote")},
	}
	grpcClient, err := NewProviderClient(grpcProvider, ProviderClientOptions{GRPCDialOptions: []grpc.DialOption{
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}})
	if err != nil {
		t.Fatalf("NewProviderClient(grpc) returned error: %v", err)
	}
	if _, ok := grpcClient.(*GRPCClient); !ok {
		t.Fatalf("grpc provider client has type %T, want *GRPCClient", grpcClient)
	}
	if closer, ok := grpcClient.(*GRPCClient); ok {
		_ = closer.Close()
	}
}

func TestNewProviderClientFallsBackAfterGRPCDialFailure(t *testing.T) {
	dialAttempts := 0
	provider := &configv1.Provider{
		Id:        "fallback",
		TimeoutMs: 50,
		Transports: []*configv1.Transport{
			grpcTransport("passthrough:///missing"),
			stdioTransport(&configv1.StdioTransport{Command: os.Args[0]}),
		},
	}

	client, err := NewProviderClient(provider, ProviderClientOptions{GRPCDialOptions: []grpc.DialOption{
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			dialAttempts++
			return nil, errors.New("dial failed")
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}})
	if err != nil {
		t.Fatalf("NewProviderClient returned error: %v", err)
	}
	if dialAttempts == 0 {
		t.Fatal("gRPC fallback transport was not dialed")
	}
	if _, ok := client.(*StdioClient); !ok {
		t.Fatalf("fallback client has type %T, want *StdioClient", client)
	}
}

func TestNewProviderClientFallsBackAfterStdioDialFailure(t *testing.T) {
	provider := &configv1.Provider{
		Id:        "fallback",
		TimeoutMs: 50,
		Transports: []*configv1.Transport{
			stdioTransport(&configv1.StdioTransport{Command: filepath.Join(t.TempDir(), "missing-provider")}),
			stdioTransport(&configv1.StdioTransport{Command: os.Args[0]}),
		},
	}

	client, err := NewProviderClient(provider, ProviderClientOptions{})
	if err != nil {
		t.Fatalf("NewProviderClient returned error: %v", err)
	}
	if _, ok := client.(*StdioClient); !ok {
		t.Fatalf("fallback client has type %T, want *StdioClient", client)
	}
}

func TestNewProviderClientDoesNotFallbackAfterInvalidTransport(t *testing.T) {
	provider := &configv1.Provider{
		Id:        "invalid",
		TimeoutMs: 50,
		Transports: []*configv1.Transport{
			grpcTransport(""),
			stdioTransport(&configv1.StdioTransport{Command: os.Args[0]}),
		},
	}

	_, err := NewProviderClient(provider, ProviderClientOptions{})
	if err == nil || !strings.Contains(err.Error(), "grpc endpoint is required") {
		t.Fatalf("NewProviderClient error = %v, want invalid grpc endpoint", err)
	}
}

func TestGRPCClientInvokesSearchFullMethodWithDeadline(t *testing.T) {
	invoker := &recordingInvoker{}
	client := newGRPCClientWithInvoker("dns:///provider", 50*time.Millisecond, invoker)

	response, err := client.Search(context.Background(), &searchv1.SearchRequest{Query: "fixture", Limit: proto.Uint32(2)})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if invoker.method != SearchFullMethod {
		t.Fatalf("method = %q, want %q", invoker.method, SearchFullMethod)
	}
	if invoker.request.GetQuery() != "fixture" || invoker.request.GetLimit() != 2 {
		t.Fatalf("request = %#v", invoker.request)
	}
	if !invoker.sawDeadline {
		t.Fatal("gRPC search call did not receive a deadline")
	}
	if len(response.GetHits()) != 1 || response.GetHits()[0].GetId() != "grpc-hit" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestSearchProviderHelper(t *testing.T) {
	if os.Getenv("RECALL_SEARCHCLIENT_HELPER") != "1" {
		return
	}
	serveSearchProviderHelper(t)
}

type recordingInvoker struct {
	method      string
	request     *searchv1.SearchRequest
	sawDeadline bool
}

func (invoker *recordingInvoker) Invoke(ctx context.Context, method string, args any, reply any, _ ...grpc.CallOption) error {
	invoker.method = method
	request, ok := args.(*searchv1.SearchRequest)
	if !ok {
		return nil
	}
	invoker.request = proto.Clone(request).(*searchv1.SearchRequest)
	_, invoker.sawDeadline = ctx.Deadline()
	response := reply.(*searchv1.SearchResponse)
	response.Hits = []*searchv1.SearchHit{{Id: "grpc-hit", Selector: "note", Title: "gRPC hit"}}
	return nil
}

func helperTransport(logPath string) *configv1.StdioTransport {
	return &configv1.StdioTransport{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestSearchProviderHelper", "--"},
		Env: map[string]string{
			"RECALL_SEARCHCLIENT_HELPER":     "1",
			"RECALL_SEARCHCLIENT_HELPER_LOG": logPath,
		},
	}
}

func stdioTransport(transport *configv1.StdioTransport) *configv1.Transport {
	return &configv1.Transport{Transport: &configv1.Transport_Stdio{Stdio: transport}}
}

func grpcTransport(endpoint string) *configv1.Transport {
	return &configv1.Transport{Transport: &configv1.Transport_Grpc{Grpc: &configv1.GrpcTransport{Endpoint: endpoint}}}
}

func readHelperLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read helper log: %v", err)
	}
	return string(data)
}

func serveSearchProviderHelper(t *testing.T) {
	t.Helper()
	rpcPath := os.Args[len(os.Args)-1]
	stdin, err := os.ReadFile("/dev/stdin")
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	request := &searchv1.SearchRequest{}
	format, err := stdiorpc.UnmarshalPayloadAuto(stdin, request)
	if err != nil {
		t.Fatalf("unmarshal search request: %v", err)
	}
	if logPath := os.Getenv("RECALL_SEARCHCLIENT_HELPER_LOG"); logPath != "" {
		log := strings.Join([]string{
			"path=" + rpcPath,
			"format=" + string(format),
			"query=" + request.GetQuery(),
		}, "\n") + "\n"
		if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
			t.Fatalf("write helper log: %v", err)
		}
	}

	response := &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{{
		Id:      "example:" + request.GetQuery(),
		Selector:    "note",
		Title:   "Result for " + request.GetQuery(),
		Snippet: proto.String("limit observed"),
	}}}
	responseBytes, err := stdiorpc.MarshalPayload(format, response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if _, err := os.Stdout.Write(responseBytes); err != nil {
		t.Fatalf("write response: %v", err)
	}
	os.Exit(0)
}
