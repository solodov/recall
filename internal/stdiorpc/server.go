package stdiorpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

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

// ServeOptions supplies the process-local streams, RPC path arguments, and
// handlers needed to serve one stdio RPC invocation.
type ServeOptions struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Args     []string
	Handlers map[MethodKey]UnaryHandler
}

// ServeOne handles exactly one unary stdio RPC call. The final CLI argument is
// the RPC path, stdin is auto-detected as binary protobuf or textproto, and the
// response mirrors the request format on stdout.
func ServeOne(ctx context.Context, options ServeOptions) error {
	stdin := options.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := options.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	args := options.Args
	if args == nil {
		args = os.Args[1:]
	}

	rpcPath, err := rpcPathArg(args)
	if err != nil {
		return err
	}
	key, err := ParsePath(rpcPath)
	if err != nil {
		return err
	}

	handler, exists := options.Handlers[key]
	if !exists {
		return fmt.Errorf("unsupported stdio RPC method %s", rpcPath)
	}
	if handler.NewRequest == nil {
		return fmt.Errorf("stdio RPC method %s has no request factory", rpcPath)
	}
	if handler.Handle == nil {
		return fmt.Errorf("stdio RPC method %s has no handler", rpcPath)
	}

	requestBytes, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read request from stdin: %w", err)
	}
	request := handler.NewRequest()
	if request == nil {
		return fmt.Errorf("stdio RPC method %s returned nil request", rpcPath)
	}
	format, err := UnmarshalPayloadAuto(requestBytes, request)
	if err != nil {
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
	responseBytes, err := MarshalPayload(format, response)
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	if _, err := stdout.Write(responseBytes); err != nil {
		return fmt.Errorf("write response to stdout: %w", err)
	}
	return nil
}

func rpcPathArg(args []string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("stdio RPC path argument is required")
	}
	for index := len(args) - 1; index >= 0; index-- {
		if args[index] == "--" {
			args = args[index+1:]
			break
		}
	}
	if len(args) != 1 {
		return "", fmt.Errorf("expected exactly one stdio RPC path argument, got %d", len(args))
	}
	return args[0], nil
}
