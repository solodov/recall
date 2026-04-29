// Package runtime carries command-scoped context, logging, and tracing state.
package runtime

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/solodov/recall/internal/logging"
	"github.com/solodov/recall/internal/perf"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// Context carries per-call runtime state shared by recall orchestration layers.
type Context struct {
	Context context.Context
	Logger  *slog.Logger
	Perf    *slog.Logger
}

// LogPaths identifies main and performance log destinations for one command.
type LogPaths struct {
	Main string
	Perf string
}

// DefaultLogPaths returns recall's XDG state log locations.
func DefaultLogPaths() (LogPaths, error) {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return LogPaths{}, fmt.Errorf("resolve home directory for log path: %w", err)
		}
		if home == "" {
			return LogPaths{}, fmt.Errorf("resolve home directory for log path: HOME is unset")
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	logDir := filepath.Join(stateHome, "recall")
	return LogPaths{Main: filepath.Join(logDir, "recall.log"), Perf: filepath.Join(logDir, "perf.log")}, nil
}

// New constructs a runtime context from a stdlib context and initial logger.
func New(ctx context.Context, logger *slog.Logger) Context {
	return Context{Context: ctx, Logger: logger}
}

// NewWithLogPaths constructs a runtime context with rotated main and perf logs.
func NewWithLogPaths(ctx context.Context, logPaths LogPaths, stderrLevel string) (Context, error) {
	logger, err := logging.New(logPaths.Main, stderrLevel)
	if err != nil {
		return Context{}, err
	}
	perfLogger, err := logging.New(logPaths.Perf, "off")
	if err != nil {
		return Context{}, err
	}
	return New(ctx, logger).WithPerfLogger(perfLogger), nil
}

// Std returns the underlying context, defaulting to Background when unset.
func (run Context) Std() context.Context {
	if run.Context == nil {
		return context.Background()
	}
	return run.Context
}

// Log returns the scoped logger for this call, defaulting to a quiet logger.
func (run Context) Log() *slog.Logger {
	if run.Logger == nil {
		return discardLogger
	}
	return run.Logger
}

// PerfLog returns the dedicated performance logger for this call.
func (run Context) PerfLog() *slog.Logger {
	if run.Perf == nil {
		return discardLogger
	}
	return run.Perf
}

// WithLogger returns a copy of this runtime context with the provided logger.
func (run Context) WithLogger(logger *slog.Logger) Context {
	run.Logger = logger
	return run
}

// WithPerfLogger returns a copy of this runtime context with the provided perf logger.
func (run Context) WithPerfLogger(logger *slog.Logger) Context {
	run.Perf = logger
	return run
}

// WithLogMeta returns a copy of this runtime context with structured logger
// fields attached to both main and perf loggers.
func (run Context) WithLogMeta(args ...any) Context {
	run = run.WithLogger(run.Log().With(args...))
	if run.Perf != nil {
		run = run.WithPerfLogger(run.Perf.With(args...))
	}
	return run
}

// StartOperation creates a child tracing span and stores it in the runtime context.
func (run Context) StartOperation(name string, attrs ...any) (Context, *perf.Span) {
	ctx, span := perf.Start(run.Std(), run.PerfLog(), name, attrs...)
	run.Context = ctx
	return run, span
}

// Span returns the active span when one is attached to the runtime context.
func (run Context) Span() *perf.Span {
	if span := perf.CurrentSpan(run.Std()); span != nil {
		return span
	}
	return perf.Noop()
}
