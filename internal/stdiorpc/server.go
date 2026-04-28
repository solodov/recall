package stdiorpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"
)

// MethodKey identifies one unary RPC method served by a stdio RPC server.
type MethodKey struct {
	Service string
	Method  string
}

// UnaryHandler decodes one request type and returns one response message for a
// stdio RPC method.
type UnaryHandler struct {
	NewRequest func() proto.Message
	Handle     func(context.Context, proto.Message) (proto.Message, error)
}

// ServeOptions supplies the process-local streams, environment, and handlers
// needed to serve one stdio RPC invocation.
type ServeOptions struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Getenv   func(string) string
	Handlers map[MethodKey]UnaryHandler
}

// ServeOne handles exactly one unary stdio RPC call. The request and response
// use the call encoding from RECALL_RPC_ENCODING, while service and method are
// selected by RECALL_RPC_SERVICE and RECALL_RPC_METHOD.
func ServeOne(ctx context.Context, options ServeOptions) error {
	stdin := options.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := options.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	getenv := options.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}

	key := MethodKey{
		Service: strings.TrimSpace(getenv(EnvService)),
		Method:  strings.TrimSpace(getenv(EnvMethod)),
	}
	if key.Service == "" {
		return fmt.Errorf("%s is required", EnvService)
	}
	if key.Method == "" {
		return fmt.Errorf("%s is required", EnvMethod)
	}

	encoding, err := ParseEncodingEnvValue(getenv(EnvEncoding))
	if err != nil {
		return err
	}
	handler, exists := options.Handlers[key]
	if !exists {
		return fmt.Errorf("unsupported stdio RPC method %s/%s", key.Service, key.Method)
	}
	if handler.NewRequest == nil {
		return fmt.Errorf("stdio RPC method %s/%s has no request factory", key.Service, key.Method)
	}
	if handler.Handle == nil {
		return fmt.Errorf("stdio RPC method %s/%s has no handler", key.Service, key.Method)
	}

	requestBytes, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read request from stdin: %w", err)
	}
	request := handler.NewRequest()
	if request == nil {
		return fmt.Errorf("stdio RPC method %s/%s returned nil request", key.Service, key.Method)
	}
	if err := unmarshalPayload(encoding, requestBytes, request); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	response, err := handler.Handle(ctx, request)
	if err != nil {
		return err
	}
	if response == nil {
		return errors.New("stdio RPC handler returned nil response")
	}
	responseBytes, err := marshalPayload(encoding, response)
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	if _, err := stdout.Write(responseBytes); err != nil {
		return fmt.Errorf("write response to stdout: %w", err)
	}
	return nil
}
