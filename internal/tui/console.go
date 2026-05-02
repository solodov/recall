package tui

import (
	"context"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const consoleHistoryLimit = 512

type consoleModel struct {
	events        chan consoleLine
	lines         []consoleLine
	scrollback    bool
	viewportStart int
}

type consoleLine struct {
	Text string
	At   time.Time
}

type consoleLineMsg struct {
	line consoleLine
}

func newConsoleModel() consoleModel {
	return consoleModel{events: make(chan consoleLine, consoleHistoryLimit)}
}

func (m consoleModel) waitCmd() tea.Cmd {
	if m.events == nil {
		return nil
	}
	return func() tea.Msg {
		return consoleLineMsg{line: <-m.events}
	}
}

func (m *consoleModel) appendLine(line consoleLine) {
	line.Text = canonicalConsoleLine(line.Text)
	if line.Text == "" {
		return
	}
	if line.At.IsZero() {
		line.At = time.Now()
	}
	m.lines = append(m.lines, line)
	if len(m.lines) <= consoleHistoryLimit {
		return
	}
	trimmed := len(m.lines) - consoleHistoryLimit
	m.lines = append([]consoleLine(nil), m.lines[trimmed:]...)
	if m.scrollback {
		m.viewportStart -= trimmed
		if m.viewportStart < 0 {
			m.viewportStart = 0
		}
	}
}

func (m consoleModel) render(width int, height int) string {
	contentWidth := max(1, width)
	contentHeight := max(1, height)
	lines := make([]string, 0, contentHeight)
	if len(m.lines) > 0 {
		start := m.historyViewportStart(contentHeight)
		end := start + contentHeight
		if end > len(m.lines) {
			end = len(m.lines)
		}
		for _, line := range m.lines[start:end] {
			lines = append(lines, fitLine(line.Text, contentWidth))
		}
	}
	if len(lines) == 0 {
		lines = append(lines, subtleStyle.Render("No console messages yet."))
	}
	return lipgloss.NewStyle().Width(contentWidth).Height(contentHeight).MaxWidth(contentWidth).MaxHeight(contentHeight).Render(padLines(lines, contentHeight, contentWidth))
}

func (m *consoleModel) pageDown(height int) {
	m.pageHistory(height, 1)
}

func (m *consoleModel) pageUp(height int) {
	m.pageHistory(height, -1)
}

func (m *consoleModel) pageHistory(height int, direction int) {
	if height < 1 || len(m.lines) <= height {
		m.scrollback = false
		m.viewportStart = 0
		return
	}
	start := m.historyViewportStart(height)
	step := pageStep(height)
	nextStart := clampViewportStart(start+(direction*step), len(m.lines), height)
	if nextStart == start && (!m.scrollback || direction < 0) {
		return
	}
	bottomStart := len(m.lines) - height
	if nextStart >= bottomStart {
		m.scrollback = false
		m.viewportStart = 0
		return
	}
	m.scrollback = true
	m.viewportStart = nextStart
}

func (m consoleModel) historyViewportStart(height int) int {
	if height < 1 || len(m.lines) <= height {
		return 0
	}
	if m.scrollback {
		return clampViewportStart(m.viewportStart, len(m.lines), height)
	}
	return len(m.lines) - height
}

func pageStep(height int) int {
	if height <= 1 {
		return 1
	}
	return height - 1
}

func clampViewportStart(start int, total int, height int) int {
	if start < 0 {
		return 0
	}
	maxStart := total - height
	if maxStart < 0 {
		return 0
	}
	if start > maxStart {
		return maxStart
	}
	return start
}

func canonicalConsoleLine(value string) string {
	return strings.TrimSpace(value)
}

func consoleLogHandler(channel chan<- consoleLine) slog.Handler {
	return slog.NewTextHandler(consoleLogWriter{channel: channel}, &slog.HandlerOptions{Level: slog.LevelInfo})
}

type consoleLogWriter struct {
	channel chan<- consoleLine
}

func (writer consoleLogWriter) Write(data []byte) (int, error) {
	text := strings.TrimRight(string(data), "\r\n")
	for _, line := range strings.Split(text, "\n") {
		line = canonicalConsoleLine(line)
		if line == "" {
			continue
		}
		select {
		case writer.channel <- consoleLine{Text: line, At: time.Now()}:
		default:
		}
	}
	return len(data), nil
}

type teeHandler struct {
	handlers []slog.Handler
}

func newTeeHandler(handlers ...slog.Handler) slog.Handler {
	return &teeHandler{handlers: handlers}
}

func (handler *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, child := range handler.handlers {
		if child != nil && child.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (handler *teeHandler) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for _, child := range handler.handlers {
		if child == nil || !child.Enabled(ctx, record.Level) {
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
		if child != nil {
			next = append(next, child.WithAttrs(attrs))
		}
	}
	return &teeHandler{handlers: next}
}

func (handler *teeHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, 0, len(handler.handlers))
	for _, child := range handler.handlers {
		if child != nil {
			next = append(next, child.WithGroup(name))
		}
	}
	return &teeHandler{handlers: next}
}
