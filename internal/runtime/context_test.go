package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultLogPathsUseXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/state")

	paths, err := DefaultLogPaths()
	if err != nil {
		t.Fatalf("DefaultLogPaths returned error: %v", err)
	}
	if paths.Main != filepath.Join("/tmp/state", "recall", "recall.log") {
		t.Fatalf("main log path = %q", paths.Main)
	}
	if paths.Perf != filepath.Join("/tmp/state", "recall", "perf.log") {
		t.Fatalf("perf log path = %q", paths.Perf)
	}
}

func TestWithLogMetaScopesMainAndPerfLoggers(t *testing.T) {
	var mainLogs bytes.Buffer
	var perfLogs bytes.Buffer
	run := New(context.Background(), slog.New(slog.NewTextHandler(&mainLogs, nil))).WithPerfLogger(slog.New(slog.NewTextHandler(&perfLogs, nil)))
	run = run.WithLogMeta("provider_id", "example")

	run.Log().Info("main event")
	_, span := run.StartOperation("operation")
	span.End()

	if !strings.Contains(mainLogs.String(), "provider_id=example") {
		t.Fatalf("main logs = %q, want provider metadata", mainLogs.String())
	}
	if !strings.Contains(perfLogs.String(), "provider_id=example") || !strings.Contains(perfLogs.String(), "op=operation") {
		t.Fatalf("perf logs = %q, want provider metadata and operation", perfLogs.String())
	}
}
