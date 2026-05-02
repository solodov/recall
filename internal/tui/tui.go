// Package tui implements recall's interactive search frontend.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/solodov/recall/internal/openers"
	"github.com/solodov/recall/internal/orchestrator"
	"github.com/solodov/recall/internal/render"
	"github.com/solodov/recall/internal/runtime"
	"github.com/solodov/recall/internal/searchargs"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
)

// SearchFunc executes one provider fan-out for an interactive prompt.
type SearchFunc func(runtime.Context, *configv1.RecallConfig, string, orchestrator.Options) (*orchestrator.Result, error)

// PreviewFunc loads preview text for the selected summarized result.
type PreviewFunc func(context.Context, render.ResultSummary, PreviewOptions) (Preview, error)

// OpenCommandFunc returns the Bubble Tea command that opens a recall:// target.
type OpenCommandFunc func(context.Context, *configv1.RecallConfig, string) tea.Cmd

// Options contains runtime dependencies for the interactive TUI.
type Options struct {
	Config         *configv1.RecallConfig
	Runtime        runtime.Context
	Search         SearchFunc
	Preview        PreviewFunc
	OpenCommand    OpenCommandFunc
	PreviewOptions PreviewOptions
	Input          io.Reader
	Output         io.Writer
}

// Run starts the interactive recall TUI and blocks until it exits.
func Run(ctx context.Context, options Options) error {
	if options.Config == nil {
		return errors.New("recall TUI requires a config")
	}
	programOptions := []tea.ProgramOption{tea.WithAltScreen()}
	if options.Input != nil {
		programOptions = append(programOptions, tea.WithInput(options.Input))
	}
	if options.Output != nil {
		programOptions = append(programOptions, tea.WithOutput(options.Output))
	}
	_, err := tea.NewProgram(newModel(ctx, options), programOptions...).Run()
	return err
}

type model struct {
	ctx            context.Context
	config         *configv1.RecallConfig
	runtime        runtime.Context
	search         SearchFunc
	preview        PreviewFunc
	openCommand    OpenCommandFunc
	previewOptions PreviewOptions

	input   textinput.Model
	spinner spinner.Model
	width   int
	height  int

	tabs      []tabSpec
	activeTab tabID
	console   consoleModel
	footer    footerModel
	prefix    keyPrefixState
	commands  commandRegistry

	searchRequestID int
	searchCancel    context.CancelFunc
	searching       bool
	lastSubmitted   string
	searchErr       error

	results  []render.ResultSummary
	selected int
	scroll   int

	previewVisible   bool
	previewRequestID int
	previewCancel    context.CancelFunc
	previewing       bool
	previewText      Preview
	previewErr       error
	previewCache     map[string]Preview

	opening bool
	status  string
}

var (
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	subtleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62"))
)

func newModel(ctx context.Context, options Options) model {
	if ctx == nil {
		ctx = context.Background()
	}
	search := options.Search
	if search == nil {
		search = orchestrator.Search
	}
	preview := options.Preview
	if preview == nil {
		preview = DefaultPreview
	}
	openCommand := options.OpenCommand
	if openCommand == nil {
		openCommand = defaultOpenCommand
	}
	console := newConsoleModel()
	run := options.Runtime
	if run.Context == nil {
		run.Context = ctx
	}
	run = withConsoleLogSink(run, console)

	input := textinput.New()
	input.Placeholder = "-s code:file:content query"
	input.Prompt = "search> "
	input.Focus()

	spin := spinner.New()
	spin.Spinner = spinner.MiniDot

	footer := footerModel{}
	footer.setActivity("Type a query and press Enter. Use C-c p for preview, C-c c for console, C-x C-c to quit.")

	return model{
		ctx:            ctx,
		config:         options.Config,
		runtime:        run.WithLogMeta("command", "tui"),
		search:         search,
		preview:        preview,
		openCommand:    openCommand,
		previewOptions: options.PreviewOptions,
		input:          input,
		spinner:        spin,
		width:          80,
		height:         24,
		tabs:           defaultTabs(),
		activeTab:      tabSearch,
		console:        console,
		footer:         footer,
		commands:       defaultCommandRegistry(),
		previewCache:   map[string]Preview{},
	}
}

func withConsoleLogSink(run runtime.Context, console consoleModel) runtime.Context {
	if console.events == nil {
		return run
	}
	logger := slog.New(newTeeHandler(run.Log().Handler(), consoleLogHandler(console.events)))
	return run.WithLogger(logger)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.console.waitCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(1, msg.Width-lipgloss.Width(m.input.Prompt)-1)
		m.ensureSelectionVisible()
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	case searchFinishedMsg:
		return m.handleSearchFinished(msg)
	case previewFinishedMsg:
		return m.handlePreviewFinished(msg)
	case openFinishedMsg:
		m.opening = false
		if msg.err != nil {
			m.setStatus("open failed: " + msg.err.Error())
		} else {
			m.setStatus("opened selected result")
		}
		return m, nil
	case consoleLineMsg:
		m.console.appendLine(msg.line)
		m.footer.setActivity(msg.line.Text)
		return m, m.console.waitCmd()
	case whichKeyTimeoutMsg:
		return m.handleWhichKeyTimeout(msg)
	case spinner.TickMsg:
		if m.searching || m.previewing {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.activeTab == tabSearch {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.handleUniversalCancel(msg) {
		return m, nil
	}
	ctx := commandContext{context: m.keyContext(), model: &m}
	if result := m.commandRegistry().dispatchKey(ctx, &m.prefix, msg); result.handled {
		return m.applyDispatchResult(result)
	}
	if m.activeTab == tabSearch {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) commandRegistry() commandRegistry {
	if len(m.commands.commandIndex) == 0 {
		return defaultCommandRegistry()
	}
	return m.commands
}

func (m model) applyDispatchResult(result dispatchResult) (tea.Model, tea.Cmd) {
	if result.needsMore {
		m.footer.showPrefix(m.prefix.sequence)
		return m, tea.Batch(result.cmd, m.whichKeyTimeoutCmd())
	}
	m.footer.clearPrefix()
	return m, result.cmd
}

func (m model) whichKeyTimeoutCmd() tea.Cmd {
	if !m.prefix.active() {
		return nil
	}
	context := m.prefix.context
	generation := m.prefix.generation
	return tea.Tick(whichKeyTimeout, func(time.Time) tea.Msg {
		return whichKeyTimeoutMsg{context: context, generation: generation}
	})
}

func (m model) handleWhichKeyTimeout(msg whichKeyTimeoutMsg) (tea.Model, tea.Cmd) {
	if !m.prefix.active() || m.prefix.context != msg.context || m.prefix.generation != msg.generation {
		return m, nil
	}
	entries := m.commandRegistry().whichKeyEntries(commandContext{context: msg.context, model: &m}, m.prefix.sequence)
	if len(entries) == 0 {
		m.footer.showPrefix(m.prefix.sequence)
		return m, nil
	}
	m.footer.showWhichKey(m.prefix.sequence, entries)
	return m, nil
}

func (m *model) handleUniversalCancel(msg tea.KeyMsg) bool {
	if !isUniversalCancelKey(msg) {
		return false
	}
	m.prefix.clear()
	m.footer.clearPrefix()
	return true
}

func isUniversalCancelKey(msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyCtrlG || msg.String() == "ctrl+g" || msg.String() == "esc" {
		return true
	}
	return len(msg.Runes) == 1 && msg.Runes[0] == '\a'
}

func (m model) View() string {
	width := max(1, m.width)
	height := max(1, m.height)
	tabs := m.renderTabs(width)
	footer := m.footer.render(width)
	if overlay := m.footer.overlayView(width); overlay != "" {
		footer = overlay
	}
	bodyHeight := height - lipgloss.Height(tabs) - lipgloss.Height(footer)
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	body := m.bodyView(width, bodyHeight)
	content := lipgloss.JoinVertical(lipgloss.Left, tabs, body, footer)
	return lipgloss.NewStyle().Width(width).Height(height).MaxWidth(width).MaxHeight(height).Render(content)
}

func (m model) bodyView(width int, height int) string {
	switch m.activeTab {
	case tabConsole:
		return m.console.render(width, height)
	default:
		return m.searchView(width, height)
	}
}

func (m model) searchView(width int, height int) string {
	resultHeight, previewHeight := m.layoutHeightsForBody(height)
	sections := []string{
		fitLine(m.input.View(), width),
		renderPlainDividerLine(width),
		m.resultsView(resultHeight, width),
	}
	if m.previewVisible {
		sections = append(sections, fitLine(m.previewHeader(), width), m.previewView(previewHeight, width))
	}
	return lipgloss.NewStyle().Width(width).Height(height).MaxWidth(width).MaxHeight(height).Render(strings.Join(sections, "\n"))
}

func (m model) bodyHeight() int {
	tabsHeight := 2
	return max(1, m.height-tabsHeight-m.footer.reservedHeight())
}

func (m model) handleEnter() (tea.Model, tea.Cmd) {
	cmd := m.submitOrOpen()
	return m, cmd
}

func (m *model) submitOrOpen() tea.Cmd {
	if m.activeTab != tabSearch {
		m.setActiveTab(tabSearch)
		return nil
	}
	prompt := strings.TrimSpace(m.input.Value())
	if prompt == "" {
		m.searchErr = errors.New("missing query")
		m.status = ""
		m.footer.setActivity("missing query")
		return nil
	}
	if prompt != m.lastSubmitted || len(m.results) == 0 {
		return m.submitSearch(prompt)
	}
	return m.openSelected()
}

func (m *model) submitSearch(prompt string) tea.Cmd {
	parsed, err := searchargs.ParsePrompt(prompt)
	if err != nil {
		m.searchErr = err
		m.status = ""
		m.footer.setActivity(err.Error())
		return nil
	}
	m.cancelInFlight()
	searchCtx, cancel := context.WithCancel(m.runtime.Std())
	m.searchRequestID++
	id := m.searchRequestID
	m.searchCancel = cancel
	m.searching = true
	m.lastSubmitted = prompt
	m.searchErr = nil
	m.setStatus("searching " + prompt)
	m.results = nil
	m.selected = 0
	m.scroll = 0
	m.previewText = Preview{}
	m.previewErr = nil
	m.runtime.Log().InfoContext(searchCtx, "tui search submitted", "query", parsed.Query, "selector_count", len(parsed.Selectors), "limit", parsed.Limit)
	return tea.Batch(m.spinner.Tick, m.searchCmd(searchCtx, id, prompt, parsed))
}

func (m model) searchCmd(ctx context.Context, id int, prompt string, parsed searchargs.Search) tea.Cmd {
	search := m.search
	cfg := m.config
	run := m.runtime
	return func() tea.Msg {
		run.Context = ctx
		run, span := run.StartOperation("recall.tui.search", "query", parsed.Query)
		defer span.End()
		result, err := search(run, cfg, parsed.Query, orchestrator.Options{Selectors: parsed.Selectors, Limit: parsed.Limit})
		span.RecordError(err)
		return searchFinishedMsg{id: id, prompt: prompt, result: result, summaries: render.SummarizeResults(result), err: err}
	}
}

func (m model) handleSearchFinished(msg searchFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.id != m.searchRequestID {
		return m, nil
	}
	m.searching = false
	if m.searchCancel != nil {
		m.searchCancel()
		m.searchCancel = nil
	}
	if msg.err != nil && msg.result == nil {
		m.searchErr = msg.err
	} else {
		m.searchErr = nil
	}
	m.results = msg.summaries
	m.selected = 0
	m.scroll = 0
	m.setStatus(searchStatus(msg))
	if msg.err != nil {
		m.runtime.Log().WarnContext(m.runtime.Std(), "tui search completed with error", "err", msg.err, "result_count", len(msg.summaries))
	} else {
		m.runtime.Log().InfoContext(m.runtime.Std(), "tui search completed", "result_count", len(msg.summaries))
	}
	return m.startPreview()
}

func (m model) handlePreviewFinished(msg previewFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.id != m.previewRequestID {
		return m, nil
	}
	m.previewing = false
	if m.previewCancel != nil {
		m.previewCancel()
		m.previewCancel = nil
	}
	m.previewErr = msg.err
	if msg.err == nil {
		m.previewText = msg.preview
		m.previewCache[msg.key] = msg.preview
	}
	if msg.err != nil {
		m.runtime.Log().WarnContext(m.runtime.Std(), "tui preview failed", "err", msg.err)
	}
	return m, nil
}

func (m *model) openSelected() tea.Cmd {
	if len(m.results) == 0 || m.selected < 0 || m.selected >= len(m.results) {
		m.setStatus("no result selected")
		return nil
	}
	summary := m.results[m.selected]
	targetURL := render.OpenURL(summary.ProviderID, summary.Selector, summary.Target)
	if targetURL == "" {
		m.setStatus("selected result has no open target")
		return nil
	}
	m.opening = true
	m.setStatus("opening selected result")
	m.runtime.Log().InfoContext(m.runtime.Std(), "tui opening selected result", "provider_id", summary.ProviderID, "selector", summary.Selector)
	return m.openCommand(m.ctx, m.config, targetURL)
}

func (m model) moveSelection(delta int) (tea.Model, tea.Cmd) {
	cmd := m.moveSelectionInPlace(delta)
	return m, cmd
}

func (m *model) moveSelectionInPlace(delta int) tea.Cmd {
	if len(m.results) == 0 || delta == 0 {
		return nil
	}
	return m.selectResultInPlace(m.selected + delta)
}

func (m model) selectResult(index int) (tea.Model, tea.Cmd) {
	cmd := m.selectResultInPlace(index)
	return m, cmd
}

func (m *model) selectResultInPlace(index int) tea.Cmd {
	if len(m.results) == 0 {
		return nil
	}
	if index < 0 {
		index = 0
	}
	if index >= len(m.results) {
		index = len(m.results) - 1
	}
	if index == m.selected {
		return nil
	}
	m.selected = index
	m.ensureSelectionVisible()
	return m.startPreviewInPlace()
}

func (m *model) togglePreview() tea.Cmd {
	m.previewVisible = !m.previewVisible
	if !m.previewVisible {
		m.cancelPreview()
		m.previewText = Preview{}
		m.previewErr = nil
		m.setStatus("preview hidden")
		return nil
	}
	m.setStatus("preview shown")
	return m.startPreviewInPlace()
}

func (m model) startPreview() (model, tea.Cmd) {
	cmd := m.startPreviewInPlace()
	return m, cmd
}

func (m *model) startPreviewInPlace() tea.Cmd {
	m.cancelPreview()
	m.previewText = Preview{}
	m.previewErr = nil
	if !m.previewVisible || len(m.results) == 0 || m.selected < 0 || m.selected >= len(m.results) {
		return nil
	}
	summary := m.results[m.selected]
	key := previewCacheKey(summary)
	if cached, ok := m.previewCache[key]; ok {
		m.previewText = cached
		return nil
	}
	if summary.Target == nil {
		m.previewText = Preview{Text: "No preview available for this result.", Available: false}
		m.previewCache[key] = m.previewText
		return nil
	}
	previewCtx, cancel := context.WithCancel(m.ctx)
	m.previewRequestID++
	id := m.previewRequestID
	m.previewCancel = cancel
	m.previewing = true
	return tea.Batch(m.spinner.Tick, m.previewCmd(previewCtx, id, key, summary))
}

func (m model) previewCmd(ctx context.Context, id int, key string, summary render.ResultSummary) tea.Cmd {
	preview := m.preview
	options := m.previewOptions
	return func() tea.Msg {
		result, err := preview(ctx, summary, options)
		return previewFinishedMsg{id: id, key: key, preview: result, err: err}
	}
}

func defaultOpenCommand(ctx context.Context, cfg *configv1.RecallConfig, targetURL string) tea.Cmd {
	invocation, err := openers.Resolve(cfg, targetURL, openers.Options{})
	if err != nil {
		return func() tea.Msg { return openFinishedMsg{err: err} }
	}
	cmd := exec.CommandContext(ctx, invocation.Command, invocation.Args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return openFinishedMsg{err: err}
	})
}

func searchStatus(msg searchFinishedMsg) string {
	if msg.err != nil && msg.result == nil {
		return msg.err.Error()
	}
	if len(msg.summaries) == 0 {
		return "no results"
	}
	label := "result"
	if len(msg.summaries) != 1 {
		label = "results"
	}
	status := fmt.Sprintf("%d %s", len(msg.summaries), label)
	if msg.err != nil {
		status += "; " + msg.err.Error()
	}
	return status
}

func (m *model) setStatus(status string) {
	m.status = status
	m.footer.setActivity(status)
}

func (m model) statusLine() string {
	switch {
	case m.searching:
		return subtleStyle.Render(m.spinner.View() + " searching " + m.lastSubmitted)
	case m.searchErr != nil:
		return errorStyle.Render(m.searchErr.Error())
	case m.opening:
		return subtleStyle.Render(m.status)
	case m.status != "":
		return subtleStyle.Render(m.status)
	default:
		return subtleStyle.Render("Type a query and press Enter.")
	}
}

func (m model) previewHeader() string {
	header := "Preview"
	if m.previewing {
		header += " " + m.spinner.View() + " loading"
	}
	return subtleStyle.Render(header)
}

func (m model) resultsView(height int, width int) string {
	if height <= 0 {
		return ""
	}
	if len(m.results) == 0 {
		return padLines([]string{fitLine(subtleStyle.Render("No results yet."), width)}, height, width)
	}
	m.ensureSelectionVisible()
	end := m.scroll + height
	if end > len(m.results) {
		end = len(m.results)
	}
	lines := make([]string, 0, height)
	for index := m.scroll; index < end; index++ {
		line := fitLine(m.resultRow(m.results[index]), width)
		if index == m.selected {
			line = selectedStyle.Width(width).Render(line)
		}
		lines = append(lines, line)
	}
	return padLines(lines, height, width)
}

func (m model) previewView(height int, width int) string {
	if height <= 0 {
		return ""
	}
	text := "No preview."
	switch {
	case m.previewErr != nil:
		text = errorStyle.Render(m.previewErr.Error())
	case m.previewText.Text != "":
		text = m.previewText.Text
	case m.previewing:
		text = "Loading preview…"
	}
	lines := strings.Split(text, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for index := range lines {
		lines[index] = fitLine(lines[index], width)
	}
	return padLines(lines, height, width)
}

func (m model) resultRow(summary render.ResultSummary) string {
	source := summary.ProviderID
	if summary.Selector != "" {
		source += ":" + summary.Selector
	}
	title := summary.Title
	if title == "" && summary.Normalized.Result != nil {
		title = summary.Normalized.Result.GetId()
	}
	if summary.GroupTitle != "" {
		title = summary.GroupTitle + ": " + title
	}
	if len(summary.Details) > 0 {
		title += " — " + summary.Details[0].Value
	}
	return fmt.Sprintf("[%s] %s", source, title)
}

func (m model) layoutHeights() (int, int) {
	return m.layoutHeightsForBody(m.bodyHeight())
}

func (m model) layoutHeightsForBody(height int) (int, int) {
	if height <= 0 {
		height = 1
	}
	fixedLines := 2
	if !m.previewVisible {
		resultHeight := height - fixedLines
		if resultHeight < 0 {
			resultHeight = 0
		}
		return resultHeight, 0
	}
	fixedLines++
	minResults := 3
	previewHeight := height / 3
	if previewHeight < 3 {
		previewHeight = 3
	}
	maxPreview := height - fixedLines - minResults
	if maxPreview < 0 {
		previewHeight = 0
	} else if previewHeight > maxPreview {
		previewHeight = maxPreview
	}
	resultHeight := height - fixedLines - previewHeight
	if resultHeight < 0 {
		resultHeight = 0
	}
	return resultHeight, previewHeight
}

func (m model) resultPageSize() int {
	resultHeight, _ := m.layoutHeights()
	if resultHeight <= 1 {
		return 1
	}
	return resultHeight - 1
}

func (m *model) ensureSelectionVisible() {
	if len(m.results) == 0 {
		m.selected = 0
		m.scroll = 0
		return
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.results) {
		m.selected = len(m.results) - 1
	}
	resultHeight, _ := m.layoutHeights()
	if resultHeight <= 0 {
		m.scroll = 0
		return
	}
	if m.selected < m.scroll {
		m.scroll = m.selected
	}
	if m.selected >= m.scroll+resultHeight {
		m.scroll = m.selected - resultHeight + 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m *model) cancelInFlight() {
	if m.searchCancel != nil {
		m.searchCancel()
		m.searchCancel = nil
	}
	m.cancelPreview()
}

func (m *model) cancelPreview() {
	if m.previewCancel != nil {
		m.previewCancel()
		m.previewCancel = nil
	}
	m.previewing = false
}

func previewCacheKey(summary render.ResultSummary) string {
	parts := []string{summary.ProviderID, summary.Selector}
	if summary.Normalized.Result != nil {
		parts = append(parts, summary.Normalized.Result.GetId())
	}
	if target := summary.Target; target != nil {
		if file := target.GetFile(); file != nil {
			parts = append(parts, "file", file.GetPath(), strconv.FormatUint(uint64(file.GetLine()), 10), strconv.FormatUint(uint64(file.GetColumn()), 10))
		} else if uri := target.GetUri(); uri != nil {
			parts = append(parts, "uri", uri.GetUri())
		}
	}
	return strings.Join(parts, "\x00")
}

func padLines(lines []string, height int, width int) string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for index := range lines {
		lines[index] = fitLine(lines[index], width)
	}
	return strings.Join(lines, "\n")
}

func fitLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	if lipgloss.Width(line) <= width {
		return line
	}
	runes := []rune(line)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func renderPlainDividerLine(width int) string {
	return subtleStyle.Render(strings.Repeat("─", max(1, width)))
}

type searchFinishedMsg struct {
	id        int
	prompt    string
	result    *orchestrator.Result
	summaries []render.ResultSummary
	err       error
}

type previewFinishedMsg struct {
	id      int
	key     string
	preview Preview
	err     error
}

type openFinishedMsg struct {
	err error
}

type whichKeyTimeoutMsg struct {
	context    uiContext
	generation uint64
}
