// Package logging builds shared structured loggers with file rotation.
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

const (
	defaultMaxLogFileBytes = int64(10 * 1024 * 1024)
	defaultMaxLogBackups   = 5
)

// New configures a logger that always writes info-and-above records to an
// auto-rotated file. When stderrLevel is empty or "off", stderr remains quiet;
// otherwise it must be debug, info, warn, or error.
func New(logPath string, stderrLevel string) (*slog.Logger, error) {
	writer, err := NewRotatingWriter(logPath, defaultMaxLogFileBytes, defaultMaxLogBackups)
	if err != nil {
		return nil, fmt.Errorf("create rotating log writer: %w", err)
	}

	fileHandler := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: slog.LevelInfo})
	if strings.TrimSpace(stderrLevel) == "" || strings.EqualFold(strings.TrimSpace(stderrLevel), "off") {
		return slog.New(fileHandler), nil
	}

	level, err := parseLevel(stderrLevel)
	if err != nil {
		return nil, err
	}
	stderrHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return slog.New(newTeeHandler(fileHandler, stderrHandler)), nil
}

func parseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid --log-level %q (expected debug|info|warn|error|off)", value)
	}
}

type teeHandler struct {
	handlers []slog.Handler
}

func newTeeHandler(handlers ...slog.Handler) slog.Handler {
	return &teeHandler{handlers: handlers}
}

func (handler *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, child := range handler.handlers {
		if child.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (handler *teeHandler) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for _, child := range handler.handlers {
		if !child.Enabled(ctx, record.Level) {
			continue
		}
		if err := child.Handle(ctx, record.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (handler *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, 0, len(handler.handlers))
	for _, child := range handler.handlers {
		next = append(next, child.WithAttrs(attrs))
	}
	return &teeHandler{handlers: next}
}

func (handler *teeHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, 0, len(handler.handlers))
	for _, child := range handler.handlers {
		next = append(next, child.WithGroup(name))
	}
	return &teeHandler{handlers: next}
}
