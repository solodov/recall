package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type tabID string

type tabSpec struct {
	ID    tabID
	Title string
}

const (
	tabSearch  tabID = "search"
	tabConsole tabID = "console"
)

func defaultTabs() []tabSpec {
	return []tabSpec{{ID: tabSearch, Title: "Search"}, {ID: tabConsole, Title: "Console"}}
}

func (m *model) setActiveTab(tab tabID) {
	if !m.hasTab(tab) {
		return
	}
	m.activeTab = tab
	if tab == tabSearch {
		m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.footer.clearPrefix()
}

func (m *model) switchTab(delta int) {
	if len(m.tabs) <= 1 || delta == 0 {
		return
	}
	current := m.activeTabIndex()
	for range len(m.tabs) {
		current = (current + delta + len(m.tabs)) % len(m.tabs)
		m.setActiveTab(m.tabs[current].ID)
		return
	}
}

func (m model) hasTab(tab tabID) bool {
	for _, candidate := range m.tabs {
		if candidate.ID == tab {
			return true
		}
	}
	return false
}

func (m model) activeTabIndex() int {
	for index, candidate := range m.tabs {
		if candidate.ID == m.activeTab {
			return index
		}
	}
	return 0
}

func (m model) keyContext() uiContext {
	if m.activeTab == tabConsole {
		return uiContextConsole
	}
	return uiContextSearch
}

func (m model) renderTabs(width int) string {
	lineBackground := lipgloss.AdaptiveColor{Light: "#EEF4F7", Dark: "#152630"}
	lineStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#8AA2B1", Dark: "#4F6777"})
	fillStyle := lipgloss.NewStyle().Background(lineBackground)
	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#16354A", Dark: "#D9EEF9"}).
		Background(lipgloss.AdaptiveColor{Light: "#B8D8E8", Dark: "#23485D"}).
		Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#526371", Dark: "#8FA4B3"}).
		Background(lipgloss.AdaptiveColor{Light: "#DCE8EE", Dark: "#1C3441"}).
		Padding(0, 1)
	statusStyle := fillStyle.Foreground(lipgloss.AdaptiveColor{Light: "#3D5565", Dark: "#A8C1D0"})
	gap := fillStyle.Render(" ")
	pieces := make([]string, 0, len(m.tabs))
	for _, tab := range m.tabs {
		label := tab.Title
		if tab.ID == m.activeTab {
			pieces = append(pieces, activeStyle.Render(label))
		} else {
			pieces = append(pieces, inactiveStyle.Render(label))
		}
	}
	line := gap + strings.Join(pieces, gap)
	if status := m.tabStatus(); status != "" {
		if aligned, ok := renderRightAlignedTabStatus(line, statusStyle.Render(status), width, fillStyle); ok {
			line = aligned
		}
	}
	visibleWidth := ansi.StringWidth(line)
	if visibleWidth < width {
		line += fillStyle.Render(strings.Repeat(" ", width-visibleWidth))
	}
	return lineStyle.Width(width).Render(line)
}

func (m model) tabStatus() string {
	parts := []string{}
	if m.searching {
		parts = append(parts, "searching")
	}
	if m.previewVisible {
		parts = append(parts, "preview")
	}
	if m.previewing {
		parts = append(parts, "preview loading")
	}
	return strings.Join(parts, " · ")
}

func renderRightAlignedTabStatus(left string, right string, width int, fillStyle lipgloss.Style) (string, bool) {
	leftWidth := ansi.StringWidth(left)
	rightWidth := ansi.StringWidth(right)
	paddingWidth := width - leftWidth - rightWidth
	if paddingWidth < 1 {
		return "", false
	}
	return left + fillStyle.Render(strings.Repeat(" ", paddingWidth)) + right, true
}
