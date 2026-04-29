// Package stdiorpc implements recall's one-shot path-based stdio RPC transport.
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
	"time"
	"unicode/utf8"

	configv1 "github.com/solodov/recall/proto/recall/config/v1"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

// PayloadFormat identifies the protobuf encoding used for stdin/stdout bytes.
type PayloadFormat string

const (
	// PayloadFormatBinary is protobuf's compact binary wire format.
	PayloadFormatBinary PayloadFormat = "protobuf_binary"

	// PayloadFormatTextproto is protobuf text format for inspectable CLI use.
	PayloadFormatTextproto PayloadFormat = "protobuf_textproto"
)

// Path returns the canonical stdio RPC path for this method.
func (key MethodKey) Path() (string, error) {
	service := strings.TrimSpace(key.Service)
	method := strings.TrimSpace(key.Method)
	if service == "" {
		return "", errors.New("stdio RPC service is required")
	}
	if method == "" {
		return "", errors.New("stdio RPC method is required")
	}
	if strings.Contains(service, "/") || strings.Contains(method, "/") {
		return "", fmt.Errorf("stdio RPC path components must not contain '/': %q/%q", service, method)
	}
	return "/" + service + "/" + method, nil
}

// ParsePath parses a canonical stdio RPC path such as
// /recall.search.v1.SearchProvider/Search.
func ParsePath(path string) (MethodKey, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return MethodKey{}, errors.New("stdio RPC path is required")
	}
	if !strings.HasPrefix(path, "/") {
		return MethodKey{}, fmt.Errorf("stdio RPC path %q must start with '/'", path)
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return MethodKey{}, fmt.Errorf("stdio RPC path %q must have form /<service>/<method>", path)
	}
	return MethodKey{Service: parts[0], Method: parts[1]}, nil
}

// CallUnary executes one unary protobuf RPC over a one-shot stdio provider
// process. The RPC path is appended after the configured provider args; stdin
// carries a binary protobuf request and stdout carries the provider response.
func CallUnary(ctx context.Context, transport *configv1.StdioTransport, timeout time.Duration, method MethodKey, request proto.Message, response proto.Message) error {
	if transport == nil {
		return errors.New("stdio transport is nil")
	}
	if strings.TrimSpace(transport.GetCommand()) == "" {
		return errors.New("stdio command is required")
	}
	rpcPath, err := method.Path()
	if err != nil {
		return err
	}
	requestBytes, err := MarshalPayload(PayloadFormatBinary, request)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	args := append([]string{}, transport.GetArgs()...)
	args = append(args, rpcPath)
	cmd := exec.CommandContext(callCtx, transport.GetCommand(), args...)
	cmd.Env = appendProviderEnv(os.Environ(), transport.GetEnv())
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
	if _, err := UnmarshalPayloadAuto(stdout.Bytes(), response); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// MarshalPayload encodes one protobuf message for stdio RPC stdin/stdout.
func MarshalPayload(format PayloadFormat, message proto.Message) ([]byte, error) {
	if message == nil {
		return nil, errors.New("protobuf message is nil")
	}
	switch format {
	case PayloadFormatBinary:
		return proto.Marshal(message)
	case PayloadFormatTextproto:
		return prototext.MarshalOptions{}.Marshal(message)
	default:
		return nil, fmt.Errorf("unsupported stdio RPC payload format %q", format)
	}
}

// UnmarshalPayload decodes one protobuf message from a known stdio RPC format.
func UnmarshalPayload(format PayloadFormat, data []byte, message proto.Message) error {
	if message == nil {
		return errors.New("protobuf message is nil")
	}
	switch format {
	case PayloadFormatBinary:
		return proto.Unmarshal(data, message)
	case PayloadFormatTextproto:
		return prototext.UnmarshalOptions{DiscardUnknown: false}.Unmarshal(data, message)
	default:
		return fmt.Errorf("unsupported stdio RPC payload format %q", format)
	}
}

// UnmarshalPayloadAuto decodes binary protobuf or textproto input and returns
// the detected format so servers can mirror it for the response.
func UnmarshalPayloadAuto(data []byte, message proto.Message) (PayloadFormat, error) {
	if message == nil {
		return "", errors.New("protobuf message is nil")
	}

	var problems []error
	for _, format := range candidateFormats(data) {
		candidate := message.ProtoReflect().New().Interface()
		if err := UnmarshalPayload(format, data, candidate); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", format, err))
			continue
		}
		proto.Reset(message)
		proto.Merge(message, candidate)
		return format, nil
	}
	return "", errors.Join(problems...)
}

func candidateFormats(data []byte) []PayloadFormat {
	if looksLikeTextproto(data) {
		return []PayloadFormat{PayloadFormatTextproto, PayloadFormatBinary}
	}
	return []PayloadFormat{PayloadFormatBinary, PayloadFormatTextproto}
}

func looksLikeTextproto(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return true
	}
	if !utf8.Valid(trimmed) {
		return false
	}
	for _, r := range string(trimmed) {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return bytes.ContainsAny(trimmed, ":{}")
}

func appendProviderEnv(base []string, providerEnv map[string]string) []string {
	env := append([]string{}, base...)
	keys := make([]string, 0, len(providerEnv))
	for key := range providerEnv {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+providerEnv[key])
	}
	return env
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
