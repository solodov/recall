package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/solodov/recall/internal/normalize"
	"github.com/solodov/recall/internal/orchestrator"
	"github.com/solodov/recall/internal/render"
	"github.com/solodov/recall/internal/runtime"
	configv1 "github.com/solodov/recall/proto/recall/config/v1"
	searchv1 "github.com/solodov/recall/proto/recall/search/v1"
)

func TestModelEnterSearchesThenOpensSelectedResult(t *testing.T) {
	var openedURL string
	m := newModel(context.Background(), Options{
		Config:  &configv1.RecallConfig{},
		Runtime: runtime.New(context.Background(), nil),
		OpenCommand: func(_ context.Context, _ *configv1.RecallConfig, targetURL string) tea.Cmd {
			openedURL = targetURL
			return func() tea.Msg { return openFinishedMsg{} }
		},
	})
	m.input.SetValue("-s code:file:content parser")

	startedModel, _ := m.handleEnter()
	started := startedModel.(model)
	if !started.searching {
		t.Fatal("model did not enter searching state")
	}
	if started.lastSubmitted != "-s code:file:content parser" {
		t.Fatalf("lastSubmitted = %q", started.lastSubmitted)
	}

	result := tuiSearchResult()
	updatedModel, _ := started.Update(searchFinishedMsg{
		id:        started.searchRequestID,
		result:    result,
		summaries: render.SummarizeResults(result),
	})
	updated := updatedModel.(model)
	if len(updated.results) != 1 {
		t.Fatalf("results = %d, want 1", len(updated.results))
	}

	openedModel, _ := updated.handleEnter()
	opened := openedModel.(model)
	if !opened.opening {
		t.Fatal("model did not enter opening state")
	}
	if !strings.Contains(openedURL, "recall://open") || !strings.Contains(openedURL, "source=code") {
		t.Fatalf("opened URL = %q", openedURL)
	}
}

func TestSearchViewShowsDividerWithoutInlineResultStatus(t *testing.T) {
	m := newModel(context.Background(), Options{Config: &configv1.RecallConfig{}, Runtime: runtime.New(context.Background(), nil)})
	m.results = render.SummarizeResults(tuiSearchResult())
	m.status = "30 results"

	view := m.searchView(80, 10)
	if !strings.Contains(view, "─") {
		t.Fatalf("search view %q does not include a separator after the prompt", view)
	}
	if strings.Contains(view, "30 results") {
		t.Fatalf("search view %q includes inline result status", view)
	}
}

func TestPreviewIsHiddenUntilToggled(t *testing.T) {
	m := newModel(context.Background(), Options{Config: &configv1.RecallConfig{}, Runtime: runtime.New(context.Background(), nil)})
	result := tuiSearchResult()
	updatedModel, _ := m.Update(searchFinishedMsg{
		id:        m.searchRequestID,
		result:    result,
		summaries: render.SummarizeResults(result),
	})
	updated := updatedModel.(model)
	if updated.previewVisible || updated.previewing {
		t.Fatalf("preview visible=%t previewing=%t, want hidden and idle", updated.previewVisible, updated.previewing)
	}

	cmd := updated.togglePreview()
	if !updated.previewVisible || !updated.previewing || cmd == nil {
		t.Fatalf("toggle preview visible=%t previewing=%t cmd nil=%t", updated.previewVisible, updated.previewing, cmd == nil)
	}
}

func TestChordSwitchesToConsoleTab(t *testing.T) {
	m := newModel(context.Background(), Options{Config: &configv1.RecallConfig{}, Runtime: runtime.New(context.Background(), nil)})

	prefixedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	prefixed := prefixedModel.(model)
	if !prefixed.prefix.active() {
		t.Fatal("C-c did not start a key prefix")
	}

	consoleModel, _ := prefixed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	console := consoleModel.(model)
	if console.activeTab != tabConsole {
		t.Fatalf("active tab = %q, want console", console.activeTab)
	}
	if console.input.Focused() {
		t.Fatal("search input stayed focused on console tab")
	}
}

func TestRuntimeLogsAreDuplicatedToConsoleChannel(t *testing.T) {
	m := newModel(context.Background(), Options{Config: &configv1.RecallConfig{}, Runtime: runtime.New(context.Background(), nil)})

	m.runtime.Log().Info("hello console")
	select {
	case line := <-m.console.events:
		if !strings.Contains(line.Text, "hello console") {
			t.Fatalf("console line = %q", line.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for console log line")
	}
}

func tuiSearchResult() *orchestrator.Result {
	searchResult := &searchv1.SearchResponse_Result{
		Id:       "file:1",
		Selector: "file:content",
		Fields: []*searchv1.SearchResponse_Result_Field{
			{Key: "path", Value: &searchv1.SearchResponse_Result_Field_Text{Text: "main.go"}},
			{Key: "line", Value: &searchv1.SearchResponse_Result_Field_Integer{Integer: 12}},
			{Key: "snippet", Value: &searchv1.SearchResponse_Result_Field_Text{Text: "func main()"}},
		},
		Targets: []*searchv1.OpenTarget{{Target: &searchv1.OpenTarget_File{File: &searchv1.FileTarget{Path: "/tmp/main.go"}}}},
		Group:   &searchv1.SearchGroup{Key: "main.go", Title: "main.go"},
		Format:  &searchv1.SearchResponse_Result_Format{TitleFields: []string{"line", "snippet"}},
	}
	normalized := normalize.Result{ProviderID: "code", ProviderRank: 1, Result: searchResult}
	return &orchestrator.Result{Responses: []orchestrator.ProviderResponse{{ProviderID: "code", Results: []normalize.Result{normalized}}}}
}
