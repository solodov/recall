package perf

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestStartCreatesChildSpanWithinSameTrace(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	ctx, root := Start(context.Background(), logger, "root")
	ctx, child := Start(ctx, logger, "child")
	if root.traceID != child.traceID {
		t.Fatalf("expected child span to inherit trace id, got root=%q child=%q", root.traceID, child.traceID)
	}
	if child.parentSpanID != root.spanID {
		t.Fatalf("expected child parent span id %q, got %q", root.spanID, child.parentSpanID)
	}
	if CurrentSpan(ctx) != child {
		t.Fatal("expected child span to be current span")
	}
	child.End()
	root.End()

	logged := logs.String()
	for _, expected := range []string{"msg=perf", "op=root", "op=child"} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("expected logs to contain %q, got %q", expected, logged)
		}
	}
}

func TestMeasureRecordsStepOnPerfLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	_, span := Start(context.Background(), logger, "provider.search", "provider_id", "example")
	if err := span.Measure("call", func() error { return nil }, "hit_count", 3); err != nil {
		t.Fatalf("measure step: %v", err)
	}
	span.End()

	logged := logs.String()
	for _, expected := range []string{"event=perf", "provider_id=example", "steps.call.duration_us=", "steps.call.hit_count=3"} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("expected logs to contain %q, got %q", expected, logged)
		}
	}
}
