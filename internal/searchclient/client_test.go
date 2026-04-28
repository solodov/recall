package searchclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"recall/internal/stdiorpc"
	configv1 "recall/proto/recall/config/v1"
	rpcv1 "recall/proto/recall/rpc/v1"
	searchv1 "recall/proto/recall/search/v1"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func TestStdioClientSearchUsesTypedSearchMetadata(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "provider-log")
	transport := helperTransport(logPath)
	client, err := NewStdioClient("example", transport, time.Second, stdiorpc.NewCapabilityClient(), stdiorpc.PreferBinary)
	if err != nil {
		t.Fatalf("NewStdioClient returned error: %v", err)
	}

	response, err := client.Search(context.Background(), &searchv1.SearchRequest{Query: "deploy notes", Limit: 3})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	hits := response.GetHits()
	if len(hits) != 1 {
		t.Fatalf("hit count = %d, want 1", len(hits))
	}
	if hits[0].GetId() != "example:deploy notes" || hits[0].GetTitle() != "Result for deploy notes" {
		t.Fatalf("unexpected hit: %#v", hits[0])
	}

	log := readHelperLog(t, logPath)
	for _, want := range []string{
		"recall.rpc.v1.StdioRpcControl/GetCapabilities/protobuf_binary",
		"recall.search.v1.SearchProvider/Search/protobuf_binary",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("helper log %q does not contain %q", log, want)
		}
	}
}

func TestStdioClientCanSelectTextprotoForDiagnostics(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "provider-log")
	transport := helperTransport(logPath)
	client, err := NewStdioClient("example", transport, time.Second, stdiorpc.NewCapabilityClient(), stdiorpc.PreferTextproto)
	if err != nil {
		t.Fatalf("NewStdioClient returned error: %v", err)
	}

	response, err := client.Search(context.Background(), &searchv1.SearchRequest{Query: "alice", Limit: 1})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(response.GetHits()) != 1 {
		t.Fatalf("hit count = %d, want 1", len(response.GetHits()))
	}

	log := readHelperLog(t, logPath)
	if !strings.Contains(log, "recall.search.v1.SearchProvider/Search/protobuf_textproto") {
		t.Fatalf("helper log %q does not show textproto search call", log)
	}
}

func TestNewProviderClientCreatesTransportSpecificClients(t *testing.T) {
	capabilityClient := stdiorpc.NewCapabilityClient()
	stdioProvider := &configv1.Provider{
		Id:        "example",
		TimeoutMs: 1500,
		Transport: &configv1.Provider_Stdio{Stdio: &configv1.StdioTransport{Command: os.Args[0]}},
	}

	stdioClient, err := NewProviderClient(stdioProvider, ProviderClientOptions{CapabilityClient: capabilityClient})
	if err != nil {
		t.Fatalf("NewProviderClient(stdio) returned error: %v", err)
	}
	if _, ok := stdioClient.(*StdioClient); !ok {
		t.Fatalf("stdio provider client has type %T, want *StdioClient", stdioClient)
	}

	grpcProvider := &configv1.Provider{
		Id:        "remote",
		TimeoutMs: 1500,
		Transport: &configv1.Provider_Grpc{Grpc: &configv1.GrpcTransport{Endpoint: "passthrough:///remote"}},
	}
	grpcClient, err := NewProviderClient(grpcProvider, ProviderClientOptions{GRPCDialOptions: []grpc.DialOption{grpc.WithContextDialer(nil)}})
	if err == nil {
		if closer, ok := grpcClient.(*GRPCClient); ok {
			_ = closer.Close()
		}
		return
	}
}

func TestGRPCClientInvokesSearchFullMethodWithDeadline(t *testing.T) {
	invoker := &recordingInvoker{}
	client := newGRPCClientWithInvoker("dns:///provider", 50*time.Millisecond, invoker)

	response, err := client.Search(context.Background(), &searchv1.SearchRequest{Query: "alice", Limit: 2})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if invoker.method != SearchFullMethod {
		t.Fatalf("method = %q, want %q", invoker.method, SearchFullMethod)
	}
	if invoker.request.GetQuery() != "alice" || invoker.request.GetLimit() != 2 {
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
	response.Hits = []*searchv1.SearchHit{{Id: "grpc-hit", Kind: "note", Title: "gRPC hit"}}
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
	service := os.Getenv(stdiorpc.EnvService)
	method := os.Getenv(stdiorpc.EnvMethod)
	encoding := os.Getenv(stdiorpc.EnvEncoding)
	if logPath := os.Getenv("RECALL_SEARCHCLIENT_HELPER_LOG"); logPath != "" {
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatalf("open helper log: %v", err)
		}
		if _, err := file.WriteString(service + "/" + method + "/" + encoding + "\n"); err != nil {
			_ = file.Close()
			t.Fatalf("write helper log: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("close helper log: %v", err)
		}
	}

	stdin, err := os.ReadFile("/dev/stdin")
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	switch service + "/" + method {
	case stdiorpc.ControlService + "/" + stdiorpc.ControlGetCapabilities:
		if encoding != stdiorpc.EncodingProtobufBinary {
			t.Fatalf("capability encoding = %q, want binary", encoding)
		}
		request := &rpcv1.StdioRpcCapabilitiesRequest{}
		if err := proto.Unmarshal(stdin, request); err != nil {
			t.Fatalf("unmarshal capabilities request: %v", err)
		}
		writeBinaryHelperResponse(t, &rpcv1.StdioRpcCapabilitiesResponse{
			SupportedEncodings: []rpcv1.PayloadEncoding{
				rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
				rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO,
			},
			PreferredEncoding: rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
		})
	case SearchService + "/" + SearchMethod:
		request := &searchv1.SearchRequest{}
		switch encoding {
		case stdiorpc.EncodingProtobufBinary:
			if err := proto.Unmarshal(stdin, request); err != nil {
				t.Fatalf("unmarshal binary search request: %v", err)
			}
		case stdiorpc.EncodingProtobufTextproto:
			if err := prototext.Unmarshal(stdin, request); err != nil {
				t.Fatalf("unmarshal textproto search request: %v", err)
			}
		default:
			t.Fatalf("search encoding = %q", encoding)
		}
		response := &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{{
			Id:      "example:" + request.GetQuery(),
			Kind:    "note",
			Title:   "Result for " + request.GetQuery(),
			Snippet: proto.String("limit observed"),
		}}}
		writeEncodedHelperResponse(t, encoding, response)
	default:
		t.Fatalf("unexpected RPC %s/%s", service, method)
	}
	os.Exit(0)
}

func writeBinaryHelperResponse(t *testing.T, message proto.Message) {
	t.Helper()
	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatalf("marshal binary response: %v", err)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		t.Fatalf("write binary response: %v", err)
	}
}

func writeEncodedHelperResponse(t *testing.T, encoding string, message proto.Message) {
	t.Helper()
	var (
		data []byte
		err  error
	)
	switch encoding {
	case stdiorpc.EncodingProtobufBinary:
		data, err = proto.Marshal(message)
	case stdiorpc.EncodingProtobufTextproto:
		data, err = prototext.Marshal(message)
	default:
		t.Fatalf("unsupported helper response encoding %q", encoding)
	}
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		t.Fatalf("write response: %v", err)
	}
}
