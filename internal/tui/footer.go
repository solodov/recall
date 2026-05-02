package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const footerHeight = 2

type footerModel struct {
	latestActivity string
	activePrefix   string
	whichKey       []whichKeyEntry
}

func (m footerModel) reservedHeight() int { return footerHeight }

func (m *footerModel) setActivity(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	m.latestActivity = message
}

func (m *footerModel) showPrefix(sequence keySequence) {
	m.activePrefix = formatKeySequence(sequence)
	m.whichKey = nil
}

func (m *footerModel) showWhichKey(sequence keySequence, entries []whichKeyEntry) {
	m.activePrefix = formatKeySequence(sequence)
	m.whichKey = append([]whichKeyEntry(nil), entries...)
}

func (m *footerModel) clearPrefix() {
	m.activePrefix = ""
	m.whichKey = nil
}

func (m footerModel) render(width int) string {
	contentWidth := max(1, width)
	divider := renderPlainDividerLine(contentWidth)
	content := lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(m.renderFooterLine(contentWidth))
	return lipgloss.JoinVertical(lipgloss.Left, divider, content)
}

func (m footerModel) overlayView(width int) string {
	if len(m.whichKey) == 0 {
		return ""
	}
	contentWidth := max(1, width)
	lines := m.renderWhichKeyLines(contentWidth)
	lines = append(lines, m.renderFooterLine(contentWidth))
	block := lipgloss.JoinVertical(lipgloss.Left, append([]string{renderPlainDividerLine(contentWidth)}, lines...)...)
	return lipgloss.NewStyle().Width(contentWidth).MaxWidth(contentWidth).Render(block)
}

func (m footerModel) renderFooterLine(width int) string {
	line := m.latestActivity
	if m.activePrefix != "" {
		line = m.activePrefix
	}
	line = fitLine(line, max(1, width))
	if m.activePrefix == "" {
		return subtleStyle.Render(line)
	}
	return footerActivePrefixStyle().Render(line)
}

func (m footerModel) renderWhichKeyLines(width int) []string {
	entries := make([]whichKeyRenderedEntry, 0, len(m.whichKey))
	for _, entry := range m.whichKey {
		entries = append(entries, newWhichKeyRenderedEntry(entry, width))
	}
	if len(entries) == 0 {
		return nil
	}
	for cols := len(entries); cols >= 1; cols-- {
		lines, ok := renderWhichKeyColumns(entries, width, cols)
		if ok {
			return lines
		}
	}
	return []string{entries[0].rendered}
}

type whichKeyRenderedEntry struct {
	plain    string
	rendered string
}

func newWhichKeyRenderedEntry(entry whichKeyEntry, width int) whichKeyRenderedEntry {
	plainKey := formatKeyPress(entry.key)
	plainLabel := entry.label
	plain := fitLine(fmt.Sprintf("%s : %s", plainKey, plainLabel), width)
	rendered := footerWhichKeyKeyStyle().Render(plainKey) + footerWhichKeySeparatorStyle().Render(" : ") + footerWhichKeyCommandStyle().Render(plainLabel)
	rendered = fitLine(rendered, width)
	return whichKeyRenderedEntry{plain: plain, rendered: rendered}
}

func renderWhichKeyColumns(entries []whichKeyRenderedEntry, width int, columns int) ([]string, bool) {
	if len(entries) == 0 || columns < 1 || width < 1 {
		return nil, false
	}
	rows := int(math.Ceil(float64(len(entries)) / float64(columns)))
	columnWidths := make([]int, columns)
	grid := make([][]string, rows)
	for row := range rows {
		grid[row] = make([]string, columns)
	}
	for index, entry := range entries {
		row := index / columns
		column := index % columns
		grid[row][column] = entry.rendered
		columnWidths[column] = max(columnWidths[column], lipgloss.Width(entry.plain))
	}
	totalWidth := 0
	for column, columnWidth := range columnWidths {
		if column > 0 {
			totalWidth += 2
		}
		totalWidth += columnWidth
	}
	if totalWidth > width {
		return nil, false
	}
	lines := make([]string, 0, rows)
	for _, row := range grid {
		parts := make([]string, 0, columns)
		for column, entry := range row {
			if entry == "" {
				continue
			}
			parts = append(parts, lipgloss.NewStyle().Width(columnWidths[column]).Render(entry))
		}
		lines = append(lines, strings.Join(parts, "  "))
	}
	return lines, true
}

func footerActivePrefixStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#F59E0B"})
}

func footerWhichKeyKeyStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#F59E0B"})
}

func footerWhichKeySeparatorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#7F7568", Dark: "#5F7384"})
}

func footerWhichKeyCommandStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#0F5D7A", Dark: "#8BD5FF"})
}
