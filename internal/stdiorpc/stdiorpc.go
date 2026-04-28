// Package stdiorpc implements recall's one-shot stdio RPC control path.
package stdiorpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	configv1 "recall/proto/recall/config/v1"
	rpcv1 "recall/proto/recall/rpc/v1"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

const (
	EnvService  = "RECALL_RPC_SERVICE"
	EnvMethod   = "RECALL_RPC_METHOD"
	EnvEncoding = "RECALL_RPC_ENCODING"

	EncodingProtobufBinary    = "protobuf_binary"
	EncodingProtobufTextproto = "protobuf_textproto"

	ControlService         = "recall.rpc.v1.StdioRpcControl"
	ControlGetCapabilities = "GetCapabilities"
)

// EncodingPreference tells recall which mutually supported payload encoding to
// prefer when a provider advertises more than one option.
type EncodingPreference int

const (
	// PreferBinary selects protobuf binary for normal searches when available.
	PreferBinary EncodingPreference = iota

	// PreferTextproto selects protobuf text format for diagnostics when available.
	PreferTextproto
)

// CallMetadata is delivered to stdio RPC servers out-of-band for each unary
// call so stdin/stdout can contain only method request and response payloads.
type CallMetadata struct {
	Service  string
	Method   string
	Encoding rpcv1.PayloadEncoding
}

// Env returns the reserved environment variables that identify one stdio RPC
// call and its payload encoding.
func (metadata CallMetadata) Env() ([]string, error) {
	encoding, err := EncodingEnvValue(metadata.Encoding)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(metadata.Service) == "" {
		return nil, errors.New("stdio RPC metadata service is required")
	}
	if strings.TrimSpace(metadata.Method) == "" {
		return nil, errors.New("stdio RPC metadata method is required")
	}
	return []string{
		EnvService + "=" + metadata.Service,
		EnvMethod + "=" + metadata.Method,
		EnvEncoding + "=" + encoding,
	}, nil
}

// EncodingEnvValue maps the protobuf control enum to the stable metadata value
// used for stdio RPC calls.
func EncodingEnvValue(encoding rpcv1.PayloadEncoding) (string, error) {
	switch encoding {
	case rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY:
		return EncodingProtobufBinary, nil
	case rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO:
		return EncodingProtobufTextproto, nil
	default:
		return "", fmt.Errorf("unsupported stdio RPC payload encoding %s", encoding.String())
	}
}

// ParseEncodingEnvValue maps a stdio RPC metadata value back to the protobuf
// control enum used by capability negotiation.
func ParseEncodingEnvValue(value string) (rpcv1.PayloadEncoding, error) {
	switch value {
	case EncodingProtobufBinary:
		return rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY, nil
	case EncodingProtobufTextproto:
		return rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO, nil
	default:
		return rpcv1.PayloadEncoding_PAYLOAD_ENCODING_UNSPECIFIED, fmt.Errorf("unsupported stdio RPC payload encoding %q", value)
	}
}

// CapabilityClient discovers and caches stdio provider capabilities for the
// lifetime of a recall process.
type CapabilityClient struct {
	mu    sync.Mutex
	cache map[string]*rpcv1.StdioRpcCapabilitiesResponse
}

// NewCapabilityClient creates an empty per-process capability cache.
func NewCapabilityClient() *CapabilityClient {
	return &CapabilityClient{cache: make(map[string]*rpcv1.StdioRpcCapabilitiesResponse)}
}

// Get returns cached capabilities for providerID or discovers them with the
// mandatory binary protobuf bootstrap control call.
func (client *CapabilityClient) Get(ctx context.Context, providerID string, transport *configv1.StdioTransport, timeout time.Duration) (*rpcv1.StdioRpcCapabilitiesResponse, error) {
	if strings.TrimSpace(providerID) == "" {
		return nil, errors.New("provider id is required for stdio capability cache")
	}

	client.mu.Lock()
	if cached := client.cache[providerID]; cached != nil {
		client.mu.Unlock()
		return cloneCapabilities(cached), nil
	}
	client.mu.Unlock()

	capabilities, err := DiscoverCapabilities(ctx, providerID, transport, timeout)
	if err != nil {
		return nil, err
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if cached := client.cache[providerID]; cached != nil {
		return cloneCapabilities(cached), nil
	}
	client.cache[providerID] = cloneCapabilities(capabilities)
	return cloneCapabilities(capabilities), nil
}

// DiscoverCapabilities asks a one-shot stdio RPC server which method payload
// encodings it supports.
func DiscoverCapabilities(ctx context.Context, providerID string, transport *configv1.StdioTransport, timeout time.Duration) (*rpcv1.StdioRpcCapabilitiesResponse, error) {
	if transport == nil {
		return nil, fmt.Errorf("discover capabilities for provider %q: stdio transport is nil", providerID)
	}
	if strings.TrimSpace(transport.GetCommand()) == "" {
		return nil, fmt.Errorf("discover capabilities for provider %q: stdio command is required", providerID)
	}

	request := &rpcv1.StdioRpcCapabilitiesRequest{}
	response := &rpcv1.StdioRpcCapabilitiesResponse{}
	metadata := CallMetadata{
		Service:  ControlService,
		Method:   ControlGetCapabilities,
		Encoding: rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
	}
	if err := CallUnary(ctx, transport, timeout, metadata, request, response); err != nil {
		return nil, fmt.Errorf("discover capabilities for provider %q: %w", providerID, err)
	}
	if err := ValidateCapabilities(response); err != nil {
		return nil, fmt.Errorf("discover capabilities for provider %q: %w", providerID, err)
	}
	return cloneCapabilities(response), nil
}

// SelectPayloadEncoding chooses the recall-side encoding for a provider based
// on advertised capabilities and the requested preference.
func SelectPayloadEncoding(capabilities *rpcv1.StdioRpcCapabilitiesResponse, preference EncodingPreference) (rpcv1.PayloadEncoding, error) {
	if capabilities == nil {
		return rpcv1.PayloadEncoding_PAYLOAD_ENCODING_UNSPECIFIED, errors.New("stdio capabilities are nil")
	}
	if err := ValidateCapabilities(capabilities); err != nil {
		return rpcv1.PayloadEncoding_PAYLOAD_ENCODING_UNSPECIFIED, err
	}

	supported := supportedEncodingSet(capabilities.GetSupportedEncodings())
	preferredOrder := []rpcv1.PayloadEncoding{
		rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
		capabilities.GetPreferredEncoding(),
		rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO,
	}
	if preference == PreferTextproto {
		preferredOrder = []rpcv1.PayloadEncoding{
			rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO,
			rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
			capabilities.GetPreferredEncoding(),
		}
	}

	for _, candidate := range preferredOrder {
		if isKnownPayloadEncoding(candidate) && supported[candidate] {
			return candidate, nil
		}
	}
	return rpcv1.PayloadEncoding_PAYLOAD_ENCODING_UNSPECIFIED, errors.New("provider did not advertise a mutually supported payload encoding")
}

// ValidateCapabilities enforces capability semantics before recall selects an
// encoding or caches the provider response.
func ValidateCapabilities(capabilities *rpcv1.StdioRpcCapabilitiesResponse) error {
	if capabilities == nil {
		return errors.New("stdio capabilities are nil")
	}

	var problems []error
	supported := supportedEncodingSet(capabilities.GetSupportedEncodings())
	for index, encoding := range capabilities.GetSupportedEncodings() {
		if !isKnownPayloadEncoding(encoding) {
			problems = append(problems, fmt.Errorf("supported_encodings[%d] has unsupported value %s", index, encoding.String()))
		}
	}
	if len(supported) == 0 {
		problems = append(problems, errors.New("supported_encodings must contain at least one supported payload encoding"))
	}
	preferred := capabilities.GetPreferredEncoding()
	if preferred != rpcv1.PayloadEncoding_PAYLOAD_ENCODING_UNSPECIFIED && !supported[preferred] {
		problems = append(problems, fmt.Errorf("preferred_encoding %s is not listed in supported_encodings", preferred.String()))
	}
	return errors.Join(problems...)
}

// CallUnary executes one unary protobuf RPC over a one-shot stdio provider
// process. Metadata identifies the service, method, and payload encoding while
// stdin/stdout carry only the encoded request and response messages.
func CallUnary(ctx context.Context, transport *configv1.StdioTransport, timeout time.Duration, metadata CallMetadata, request proto.Message, response proto.Message) error {
	if transport == nil {
		return errors.New("stdio transport is nil")
	}
	if strings.TrimSpace(transport.GetCommand()) == "" {
		return errors.New("stdio command is required")
	}
	requestBytes, err := marshalPayload(metadata.Encoding, request)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	metadataEnv, err := metadata.Env()
	if err != nil {
		return err
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(callCtx, transport.GetCommand(), transport.GetArgs()...)
	cmd.Env = appendProviderAndMetadataEnv(os.Environ(), transport.GetEnv(), metadataEnv)
	cmd.Stdin = bytes.NewReader(requestBytes)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if callCtx.Err() != nil {
			return fmt.Errorf("provider command timed out: %w", callCtx.Err())
		}
		return fmt.Errorf("provider command failed: %w%s", err, stderrSuffix(stderr.String()))
	}
	if stdout.Len() == 0 {
		return errors.New("provider command wrote no response to stdout")
	}
	if err := unmarshalPayload(metadata.Encoding, stdout.Bytes(), response); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func marshalPayload(encoding rpcv1.PayloadEncoding, message proto.Message) ([]byte, error) {
	switch encoding {
	case rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY:
		return proto.Marshal(message)
	case rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO:
		return prototext.MarshalOptions{}.Marshal(message)
	default:
		return nil, fmt.Errorf("unsupported payload encoding %s", encoding.String())
	}
}

func unmarshalPayload(encoding rpcv1.PayloadEncoding, data []byte, message proto.Message) error {
	switch encoding {
	case rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY:
		return proto.Unmarshal(data, message)
	case rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO:
		return prototext.UnmarshalOptions{DiscardUnknown: false}.Unmarshal(data, message)
	default:
		return fmt.Errorf("unsupported payload encoding %s", encoding.String())
	}
}

func appendProviderAndMetadataEnv(base []string, providerEnv map[string]string, metadataEnv []string) []string {
	env := append([]string{}, base...)
	keys := make([]string, 0, len(providerEnv))
	for key := range providerEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+providerEnv[key])
	}
	return append(env, metadataEnv...)
}

func stderrSuffix(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	if len(stderr) > 4096 {
		stderr = stderr[:4096] + "…"
	}
	return "; stderr: " + stderr
}

func supportedEncodingSet(encodings []rpcv1.PayloadEncoding) map[rpcv1.PayloadEncoding]bool {
	supported := make(map[rpcv1.PayloadEncoding]bool, len(encodings))
	for _, encoding := range encodings {
		if isKnownPayloadEncoding(encoding) {
			supported[encoding] = true
		}
	}
	return supported
}

func isKnownPayloadEncoding(encoding rpcv1.PayloadEncoding) bool {
	switch encoding {
	case rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_BINARY,
		rpcv1.PayloadEncoding_PAYLOAD_ENCODING_PROTOBUF_TEXTPROTO:
		return true
	default:
		return false
	}
}

func cloneCapabilities(capabilities *rpcv1.StdioRpcCapabilitiesResponse) *rpcv1.StdioRpcCapabilitiesResponse {
	if capabilities == nil {
		return nil
	}
	return proto.Clone(capabilities).(*rpcv1.StdioRpcCapabilitiesResponse)
}
