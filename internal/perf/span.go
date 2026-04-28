// Package perf records lightweight structured tracing spans for debugging.
package perf

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type contextKey struct{}

var nextID atomic.Uint64

var noopSpan = &Span{noop: true}

// Step captures one measured phase within a span.
type Step struct {
	Name     string
	Duration time.Duration
	Attrs    []slog.Attr
	Err      error
}

// Span tracks one traced operation and logs it on End.
type Span struct {
	mu           sync.Mutex
	logger       *slog.Logger
	traceID      string
	spanID       string
	parentSpanID string
	op           string
	startedAt    time.Time
	attrs        []slog.Attr
	steps        []Step
	err          error
	ended        bool
	noop         bool
}

// Start creates a new span, stores it in ctx, and returns the updated context.
func Start(ctx context.Context, logger *slog.Logger, name string, attrs ...any) (context.Context, *Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	parent := CurrentSpan(ctx)
	traceID := newID()
	parentSpanID := ""
	if parent != nil && !parent.noop {
		traceID = parent.traceID
		parentSpanID = parent.spanID
	}
	span := &Span{
		logger:       logger,
		traceID:      traceID,
		spanID:       newID(),
		parentSpanID: parentSpanID,
		op:           strings.TrimSpace(name),
		startedAt:    time.Now().UTC(),
		attrs:        attrsFromArgs(attrs...),
	}
	if span.op == "" {
		span.op = "unknown"
	}
	return context.WithValue(ctx, contextKey{}, span), span
}

// CurrentSpan returns the current span stored in ctx when one is active.
func CurrentSpan(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	span, _ := ctx.Value(contextKey{}).(*Span)
	return span
}

// Noop returns a span that safely ignores tracing operations.
func Noop() *Span {
	return noopSpan
}

// Set adds attributes to the span that will be logged on End.
func (span *Span) Set(attrs ...any) {
	if span == nil || span.noop {
		return
	}
	span.mu.Lock()
	defer span.mu.Unlock()
	span.attrs = append(span.attrs, attrsFromArgs(attrs...)...)
}

// RecordError marks the span as failed when err is non-nil.
func (span *Span) RecordError(err error) {
	if span == nil || span.noop || err == nil {
		return
	}
	span.mu.Lock()
	defer span.mu.Unlock()
	if span.err == nil {
		span.err = err
	}
}

// Measure runs fn, records one named step duration, and returns fn's error.
func (span *Span) Measure(step string, fn func() error, attrs ...any) error {
	if fn == nil {
		return nil
	}
	if span == nil || span.noop {
		return fn()
	}
	startedAt := time.Now()
	err := fn()
	span.mu.Lock()
	span.steps = append(span.steps, Step{Name: strings.TrimSpace(step), Duration: time.Since(startedAt), Attrs: attrsFromArgs(attrs...), Err: err})
	span.mu.Unlock()
	return err
}

// End logs one structured performance record for the completed span.
func (span *Span) End(attrs ...any) {
	if span == nil || span.noop {
		return
	}
	span.mu.Lock()
	if span.ended {
		span.mu.Unlock()
		return
	}
	span.ended = true
	span.attrs = append(span.attrs, attrsFromArgs(attrs...)...)
	duration := time.Since(span.startedAt)
	status := "ok"
	if span.err != nil {
		status = "error"
	}
	logger := span.logger
	traceID := span.traceID
	spanID := span.spanID
	parentSpanID := span.parentSpanID
	op := span.op
	startedAt := span.startedAt
	err := span.err
	attrsCopy := append([]slog.Attr(nil), span.attrs...)
	stepsCopy := append([]Step(nil), span.steps...)
	span.mu.Unlock()
	if logger == nil {
		return
	}
	args := []any{
		slog.String("event", "perf"),
		slog.String("trace_id", traceID),
		slog.String("span_id", spanID),
		slog.String("parent_span_id", parentSpanID),
		slog.String("op", op),
		slog.String("status", status),
		slog.Time("started_at", startedAt),
		slog.Int64("duration_us", duration.Microseconds()),
	}
	for _, attr := range attrsCopy {
		args = append(args, attr)
	}
	if len(stepsCopy) > 0 {
		args = append(args, slog.Group("steps", stepAttrs(stepsCopy)...))
	}
	if err != nil {
		args = append(args, slog.String("err", err.Error()))
	}
	logger.Info("perf", args...)
}

func attrsFromArgs(args ...any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(args)/2+1)
	for index := 0; index+1 < len(args); index += 2 {
		key, ok := args[index].(string)
		if !ok {
			key = fmt.Sprintf("arg_%d", index)
		}
		attrs = append(attrs, slog.Any(strings.TrimSpace(key), args[index+1]))
	}
	if len(args)%2 == 1 {
		attrs = append(attrs, slog.Any("arg_trailing", args[len(args)-1]))
	}
	return attrs
}

func stepAttrs(steps []Step) []any {
	attrs := make([]any, 0, len(steps))
	for index, step := range steps {
		name := strings.TrimSpace(step.Name)
		if name == "" {
			name = fmt.Sprintf("step_%d", index)
		}
		groupAttrs := []any{slog.Int64("duration_us", step.Duration.Microseconds())}
		for _, attr := range step.Attrs {
			groupAttrs = append(groupAttrs, attr)
		}
		if step.Err != nil {
			groupAttrs = append(groupAttrs, slog.String("err", step.Err.Error()))
		}
		attrs = append(attrs, slog.Group(name, groupAttrs...))
	}
	return attrs
}

func newID() string {
	return fmt.Sprintf("%016x", nextID.Add(1))
}
