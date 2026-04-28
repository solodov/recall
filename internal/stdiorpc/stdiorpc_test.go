package stdiorpc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	configv1 "recall/proto/recall/config/v1"
	rpcv1 "recall/proto/recall/rpc/v1"

	"google.golang.org/protobuf/proto"
)

func TestDiscoverCapabilitiesUsesBinaryBootstrapMetadata(t *testing.T) {
	counterPath := filepath.Join(t.TempDir(), "provider-count")
	transport := helperTransport(counterPath)

	capabilities, err := DiscoverCapabilities(context.Background(), "example", transport, time.Second)
	if err != nil {
		t.Fatalf("DiscoverCapabilities returned error: %v", err)
	}

	if got := capabilities.GetPreferredEncoding(); got != rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY {
		t.Fatalf("preferred encoding = %s, want binary", got.String())
	}
	if len(capabilities.GetSupportedEncodings()) != 2 {
		t.Fatalf("supported encoding count = %d, want 2", len(capabilities.GetSupportedEncodings()))
	}
	if got := helperInvocationCount(t, counterPath); got != 1 {
		t.Fatalf("helper invocation count = %d, want 1", got)
	}
}

func TestCapabilityClientCachesByProviderID(t *testing.T) {
	counterPath := filepath.Join(t.TempDir(), "provider-count")
	transport := helperTransport(counterPath)
	client := NewCapabilityClient()

	first, err := client.Get(context.Background(), "example", transport, time.Second)
	if err != nil {
		t.Fatalf("first Get returned error: %v", err)
	}
	first.SupportedEncodings = nil

	second, err := client.Get(context.Background(), "example", transport, time.Second)
	if err != nil {
		t.Fatalf("second Get returned error: %v", err)
	}
	if len(second.GetSupportedEncodings()) != 2 {
		t.Fatalf("second supported encoding count = %d, want cached clone with 2", len(second.GetSupportedEncodings()))
	}
	if got := helperInvocationCount(t, counterPath); got != 1 {
		t.Fatalf("helper invocation count = %d, want 1", got)
	}
}

func TestSelectPayloadEncodingPrefersBinaryForNormalCalls(t *testing.T) {
	capabilities := &rpcv1.StdioRpcCapabilitiesResponse{
		SupportedEncodings: []rpcv1.PayloadEncoding{
			rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO,
			rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
		},
		PreferredEncoding: rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO,
	}

	encoding, err := SelectPayloadEncoding(capabilities, PreferBinary)
	if err != nil {
		t.Fatalf("SelectPayloadEncoding returned error: %v", err)
	}
	if encoding != rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY {
		t.Fatalf("encoding = %s, want binary", encoding.String())
	}
}

func TestSelectPayloadEncodingCanPreferTextprotoForDiagnostics(t *testing.T) {
	capabilities := &rpcv1.StdioRpcCapabilitiesResponse{
		SupportedEncodings: []rpcv1.PayloadEncoding{
			rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
			rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO,
		},
		PreferredEncoding: rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
	}

	encoding, err := SelectPayloadEncoding(capabilities, PreferTextproto)
	if err != nil {
		t.Fatalf("SelectPayloadEncoding returned error: %v", err)
	}
	if encoding != rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO {
		t.Fatalf("encoding = %s, want textproto", encoding.String())
	}
}

func TestValidateCapabilitiesRejectsUnsupportedValues(t *testing.T) {
	capabilities := &rpcv1.StdioRpcCapabilitiesResponse{
		SupportedEncodings: []rpcv1.PayloadEncoding{
			rpcv1.PayloadEncoding_PAYLOAD_ENCODING_UNSPECIFIED,
		},
		PreferredEncoding: rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
	}

	err := ValidateCapabilities(capabilities)
	if err == nil {
		t.Fatal("ValidateCapabilities succeeded with unsupported values")
	}
	message := err.Error()
	for _, want := range []string{"supported_encodings", "preferred_encoding"} {
		if !strings.Contains(message, want) {
			t.Fatalf("ValidateCapabilities error %q does not contain %q", message, want)
		}
	}
}

func TestEncodingMetadataRoundTrip(t *testing.T) {
	value, err := EncodingEnvValue(rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO)
	if err != nil {
		t.Fatalf("EncodingEnvValue returned error: %v", err)
	}
	if value != EncodingProtobufTextproto {
		t.Fatalf("env value = %q, want %q", value, EncodingProtobufTextproto)
	}

	encoding, err := ParseEncodingEnvValue(value)
	if err != nil {
		t.Fatalf("ParseEncodingEnvValue returned error: %v", err)
	}
	if encoding != rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO {
		t.Fatalf("encoding = %s, want textproto", encoding.String())
	}
}

func TestCapabilityProviderHelper(t *testing.T) {
	if os.Getenv("RECALL_STDIO_RPC_HELPER") != "1" {
		return
	}
	serveCapabilityHelper(t)
}

func helperTransport(counterPath string) *configv1.StdioTransport {
	return &configv1.StdioTransport{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestCapabilityProviderHelper", "--"},
		Env: map[string]string{
			"RECALL_STDIO_RPC_HELPER":       "1",
			"RECALL_STDIO_RPC_COUNTER_PATH": counterPath,
		},
	}
}

func helperInvocationCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read helper counter: %v", err)
	}
	return strings.Count(string(data), "1\n")
}

func serveCapabilityHelper(t *testing.T) {
	t.Helper()
	if got := os.Getenv(EnvService); got != ControlService {
		t.Fatalf("%s = %q, want %q", EnvService, got, ControlService)
	}
	if got := os.Getenv(EnvMethod); got != ControlGetCapabilities {
		t.Fatalf("%s = %q, want %q", EnvMethod, got, ControlGetCapabilities)
	}
	if got := os.Getenv(EnvEncoding); got != EncodingProtobufBinary {
		t.Fatalf("%s = %q, want %q", EnvEncoding, got, EncodingProtobufBinary)
	}

	if counterPath := os.Getenv("RECALL_STDIO_RPC_COUNTER_PATH"); counterPath != "" {
		file, err := os.OpenFile(counterPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatalf("open helper counter: %v", err)
		}
		if _, err := file.WriteString("1\n"); err != nil {
			_ = file.Close()
			t.Fatalf("write helper counter: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("close helper counter: %v", err)
		}
	}

	stdin, err := os.ReadFile("/dev/stdin")
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	request := &rpcv1.StdioRpcCapabilitiesRequest{}
	if err := proto.Unmarshal(stdin, request); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	response := &rpcv1.StdioRpcCapabilitiesResponse{
		SupportedEncodings: []rpcv1.PayloadEncoding{
			rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
			rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO,
		},
		PreferredEncoding: rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
	}
	responseBytes, err := proto.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if _, err := os.Stdout.Write(responseBytes); err != nil {
		t.Fatalf("write stdout: %v", err)
	}
	os.Exit(0)
}
