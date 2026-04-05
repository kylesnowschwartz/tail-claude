package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// updateWaterfall handles key events in the waterfall timeline view.
func (m model) updateWaterfall(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "escape":
		m.view = viewList
		return m, nil
	case "j":
		if len(m.wfRows) > 0 && m.wfCursor < len(m.wfRows)-1 {
			m.wfCursor++
			m.ensureWfCursorVisible()
		}
	case "k":
		if m.wfCursor > 0 {
			m.wfCursor--
			m.ensureWfCursorVisible()
		}
	case "G":
		if len(m.wfRows) > 0 {
			m.wfCursor = len(m.wfRows) - 1
			m.ensureWfCursorVisible()
		}
	case "g":
		m.wfCursor = 0
		m.wfScroll = 0
	case "J", "ctrl+d":
		m.wfScroll += m.height / 2
		m.clampWfScroll()
	case "K", "ctrl+u":
		m.wfScroll -= m.height / 2
		if m.wfScroll < 0 {
			m.wfScroll = 0
		}
	case "tab":
		// Toggle expansion for subagent rows (placeholder for card bl-6y8y).
		if m.wfCursor < len(m.wfRows) && m.wfRows[m.wfCursor].IsSubagent {
			m.wfExpanded[m.wfCursor] = !m.wfExpanded[m.wfCursor]
		}
	case "?":
		m.showKeybinds = !m.showKeybinds
	}
	return m, nil
}

// viewWaterfall renders the waterfall timeline view with a horizontal split:
// left panel (70%) for the timeline, right panel (30%) for the inspector.
func (m model) viewWaterfall() string {
	if len(m.wfRows) == 0 {
		return "No tool calls to display in waterfall view.\n\nPress q to go back."
	}

	leftWidth := m.width * 70 / 100
	rightWidth := m.width - leftWidth

	left := m.renderWaterfallTimeline(leftWidth)
	right := m.renderWaterfallInspector(rightWidth)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	footer := m.renderFooter("j/k", "nav", "tab", "expand", "q/esc", "back", "?", "keys")

	// Fill to terminal height
	bodyLines := strings.Count(body, "\n") + 1
	footerLines := strings.Count(footer, "\n") + 1
	available := m.height - footerLines
	if bodyLines < available {
		body += strings.Repeat("\n", available-bodyLines)
	}

	return body + "\n" + footer
}

// renderWaterfallTimeline renders the left panel with row labels.
// Bars are added by card bl-qx3j; this card renders labels only.
func (m model) renderWaterfallTimeline(width int) string {
	var b strings.Builder

	// Header
	title := lipgloss.NewStyle().Bold(true).Render("Waterfall Timeline")
	b.WriteString(title)
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("\u2500", min(width, 40)))
	b.WriteByte('\n')

	// Visible rows based on scroll
	visibleRows := m.height - 4 // header + footer room
	if visibleRows < 1 {
		visibleRows = 1
	}
	start := m.wfScroll
	end := start + visibleRows
	if end > len(m.wfRows) {
		end = len(m.wfRows)
	}

	dimStyle := lipgloss.NewStyle().Faint(true)
	selectedStyle := lipgloss.NewStyle().Reverse(true)

	for i := start; i < end; i++ {
		row := m.wfRows[i]
		var line string

		if row.IsUserSeparator {
			line = dimStyle.Render(fmt.Sprintf("  \u2500\u2500 user message @ +%s \u2500\u2500", formatRelativeMs(row.StartMs)))
		} else {
			// Primary tool name and count
			primary := "unknown"
			if len(row.Tools) > 0 {
				primary = row.Tools[0].Name
			}
			toolCount := len(row.Tools)
			label := primary
			if toolCount > 1 {
				label = fmt.Sprintf("%s +%d", primary, toolCount-1)
			}
			if row.IsSubagent {
				chevron := "\u25b6" // right-pointing triangle
				if m.wfExpanded[i] {
					chevron = "\u25bc" // down-pointing triangle
				}
				label = chevron + " " + label
			}
			line = fmt.Sprintf("  +%-8s %s", formatRelativeMs(row.StartMs), label)
		}

		// Truncate to width
		if len(line) > width {
			line = line[:width]
		}

		if i == m.wfCursor {
			line = selectedStyle.Render(line)
		}

		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
}

// renderWaterfallInspector renders the right-side inspector panel.
// Full content is added by card bl-qkkj; this card shows a placeholder.
func (m model) renderWaterfallInspector(width int) string {
	style := lipgloss.NewStyle().
		Faint(true).
		Width(width).
		PaddingLeft(1)

	if m.wfCursor >= len(m.wfRows) {
		return style.Render("Select a row to inspect")
	}

	row := m.wfRows[m.wfCursor]
	if row.IsUserSeparator {
		return style.Render("User message")
	}

	var b strings.Builder
	b.WriteString("Inspector\n")
	b.WriteString(strings.Repeat("\u2500", min(width-2, 20)))
	b.WriteByte('\n')
	if row.Model != "" {
		b.WriteString(fmt.Sprintf("Model: %s\n", shortModel(row.Model)))
	}
	b.WriteString(fmt.Sprintf("Duration: %s\n", formatDuration(row.DurationMs)))
	b.WriteString(fmt.Sprintf("Tools: %d\n", len(row.Tools)))

	return style.Render(b.String())
}

// ensureWfCursorVisible scrolls the waterfall viewport to keep the cursor visible.
func (m *model) ensureWfCursorVisible() {
	visibleRows := m.height - 4
	if visibleRows < 1 {
		visibleRows = 1
	}
	if m.wfCursor < m.wfScroll {
		m.wfScroll = m.wfCursor
	}
	if m.wfCursor >= m.wfScroll+visibleRows {
		m.wfScroll = m.wfCursor - visibleRows + 1
	}
}

// clampWfScroll limits waterfall scroll to valid range.
func (m *model) clampWfScroll() {
	visibleRows := m.height - 4
	if visibleRows < 1 {
		visibleRows = 1
	}
	maxScroll := len(m.wfRows) - visibleRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.wfScroll > maxScroll {
		m.wfScroll = maxScroll
	}
	if m.wfScroll < 0 {
		m.wfScroll = 0
	}
}

// formatRelativeMs formats a millisecond offset as a human-readable relative timestamp.
func formatRelativeMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := float64(ms) / 1000
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs) / 60
	remainSecs := secs - float64(mins*60)
	return fmt.Sprintf("%dm%02.0fs", mins, remainSecs)
}

