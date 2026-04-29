package stdiorpc

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	configv1 "github.com/solodov/recall/proto/recall/config/v1"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

var searchMethod = MethodKey{Service: "recall.search.v1.SearchProvider", Method: "Search"}

func TestMethodPathRoundTrip(t *testing.T) {
	path, err := searchMethod.Path()
	if err != nil {
		t.Fatalf("Path returned error: %v", err)
	}
	if path != "/recall.search.v1.SearchProvider/Search" {
		t.Fatalf("path = %q", path)
	}

	parsed, err := ParsePath(path)
	if err != nil {
		t.Fatalf("ParsePath returned error: %v", err)
	}
	if parsed != searchMethod {
		t.Fatalf("parsed = %#v, want %#v", parsed, searchMethod)
	}
}

func TestServeOneDecodesTextprotoAndMirrorsTextproto(t *testing.T) {
	requestBytes, err := prototext.Marshal(&searchv1.SearchRequest{Query: "deploy", Limit: proto.Uint32(3)})
	if err != nil {
		t.Fatalf("marshal textproto request: %v", err)
	}
	stdout := serveSearch(t, requestBytes, PayloadFormatTextproto, []string{mustPath(t, searchMethod)})

	response := &searchv1.SearchResponse{}
	if err := prototext.Unmarshal(stdout, response); err != nil {
		t.Fatalf("unmarshal textproto response: %v", err)
	}
	if response.GetHits()[0].GetId() != "example:deploy" {
		t.Fatalf("response = %#v, want deploy hit", response)
	}
}

func TestServeOneDecodesBinaryAndMirrorsBinary(t *testing.T) {
	requestBytes, err := proto.Marshal(&searchv1.SearchRequest{Query: "alice", Limit: proto.Uint32(2)})
	if err != nil {
		t.Fatalf("marshal binary request: %v", err)
	}
	stdout := serveSearch(t, requestBytes, PayloadFormatBinary, []string{"-test.run=ignored", "--", mustPath(t, searchMethod)})

	response := &searchv1.SearchResponse{}
	if err := proto.Unmarshal(stdout, response); err != nil {
		t.Fatalf("unmarshal binary response: %v", err)
	}
	if response.GetHits()[0].GetId() != "example:alice" {
		t.Fatalf("response = %#v, want alice hit", response)
	}
}

func TestCallUnaryAppendsRPCPathAndUsesBinaryPayload(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "provider-log")
	transport := helperTransport(logPath)
	response := &searchv1.SearchResponse{}

	err := CallUnary(context.Background(), transport, time.Second, searchMethod, &searchv1.SearchRequest{Query: "deploy", Limit: proto.Uint32(1)}, response)
	if err != nil {
		t.Fatalf("CallUnary returned error: %v", err)
	}
	if response.GetHits()[0].GetId() != "example:deploy" {
		t.Fatalf("response = %#v, want deploy hit", response)
	}

	log := readHelperLog(t, logPath)
	for _, want := range []string{
		"path=/recall.search.v1.SearchProvider/Search",
		"query=deploy",
		"format=protobuf_binary",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("helper log %q does not contain %q", log, want)
		}
	}
}

func TestCallUnaryRejectsInvalidMethod(t *testing.T) {
	transport := &configv1.StdioTransport{Command: os.Args[0]}
	err := CallUnary(context.Background(), transport, time.Second, MethodKey{Service: "", Method: "Search"}, &searchv1.SearchRequest{}, &searchv1.SearchResponse{})
	if err == nil || !strings.Contains(err.Error(), "service") {
		t.Fatalf("CallUnary error = %v, want service validation", err)
	}
}

func TestStdioRPCHelper(t *testing.T) {
	if os.Getenv("RECALL_STDIO_RPC_HELPER") != "1" {
		return
	}
	serveCallUnaryHelper(t)
}

func serveSearch(t *testing.T, requestBytes []byte, wantFormat PayloadFormat, args []string) []byte {
	t.Helper()
	var stdout bytes.Buffer
	err := ServeOne(context.Background(), ServeOptions{
		Stdin:  bytes.NewReader(requestBytes),
		Stdout: &stdout,
		Args:   args,
		Handlers: map[MethodKey]UnaryHandler{
			searchMethod: {
				NewRequest: func() proto.Message { return &searchv1.SearchRequest{} },
				Handle: func(_ context.Context, message proto.Message) (proto.Message, error) {
					request := message.(*searchv1.SearchRequest)
					if request.GetLimit() == 0 {
						t.Fatalf("request limit = 0, want decoded request")
					}
					return &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{{
						Id:    "example:" + request.GetQuery(),
						Kind:  "note",
						Title: "Result",
					}}}, nil
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ServeOne returned error: %v", err)
	}

	probe := &searchv1.SearchResponse{}
	if err := UnmarshalPayload(wantFormat, stdout.Bytes(), probe); err != nil {
		t.Fatalf("response was not encoded as %s: %v", wantFormat, err)
	}
	return stdout.Bytes()
}

func helperTransport(logPath string) *configv1.StdioTransport {
	return &configv1.StdioTransport{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestStdioRPCHelper", "--"},
		Env: map[string]string{
			"RECALL_STDIO_RPC_HELPER":     "1",
			"RECALL_STDIO_RPC_HELPER_LOG": logPath,
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

func serveCallUnaryHelper(t *testing.T) {
	t.Helper()
	rpcPath := os.Args[len(os.Args)-1]
	requestBytes, err := os.ReadFile("/dev/stdin")
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	request := &searchv1.SearchRequest{}
	format, err := UnmarshalPayloadAuto(requestBytes, request)
	if err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if rpcPath != mustPath(t, searchMethod) {
		t.Fatalf("rpc path = %q, want search path", rpcPath)
	}
	if logPath := os.Getenv("RECALL_STDIO_RPC_HELPER_LOG"); logPath != "" {
		log := strings.Join([]string{
			"path=" + rpcPath,
			"query=" + request.GetQuery(),
			"format=" + string(format),
		}, "\n") + "\n"
		if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
			t.Fatalf("write helper log: %v", err)
		}
	}

	responseBytes, err := MarshalPayload(format, &searchv1.SearchResponse{Hits: []*searchv1.SearchHit{{
		Id:    "example:" + request.GetQuery(),
		Kind:  "note",
		Title: "Result",
	}}})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if _, err := os.Stdout.Write(responseBytes); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	os.Exit(0)
}

func mustPath(t *testing.T, method MethodKey) string {
	t.Helper()
	path, err := method.Path()
	if err != nil {
		t.Fatalf("Path returned error: %v", err)
	}
	return path
}
