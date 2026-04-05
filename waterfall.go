package main

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/kylesnowschwartz/tail-claude/parser"
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

// categoryColor returns the theme color for a tool category.
func categoryColor(cat parser.ToolCategory) color.Color {
	switch cat {
	case parser.CategoryRead:
		return ColorToolRead
	case parser.CategoryEdit:
		return ColorToolEdit
	case parser.CategoryWrite:
		return ColorToolWrite
	case parser.CategoryBash:
		return ColorToolBash
	case parser.CategoryGrep:
		return ColorToolGrep
	case parser.CategoryGlob:
		return ColorToolGlob
	case parser.CategoryTask:
		return ColorToolTask
	case parser.CategoryTool:
		return ColorToolSkill
	case parser.CategoryWeb:
		return ColorToolWeb
	default:
		return ColorToolOther
	}
}

// renderTimeAxisHeader renders a one-line time axis with tick marks at 0%, 25%,
// 50%, 75%, and 100% of totalMs. The gutter prefix is blank to align with bars.
func renderTimeAxisHeader(gutterWidth, barWidth int, totalMs int64) string {
	dimStyle := lipgloss.NewStyle().Faint(true)

	// Build the bar-area portion: place tick labels at each quarter position.
	buf := make([]byte, barWidth)
	for i := range buf {
		buf[i] = ' '
	}

	// The five tick positions (0, 25, 50, 75, 100 percent).
	ticks := [5]int{0, 1, 2, 3, 4}
	for _, t := range ticks {
		ms := int64(t) * totalMs / 4
		col := parser.ColOffset(ms, totalMs, barWidth)
		label := "|" + formatRelativeMs(ms)
		for j, ch := range []byte(label) {
			if col+j < barWidth {
				buf[col+j] = ch
			}
		}
	}

	gutter := strings.Repeat(" ", gutterWidth)
	return dimStyle.Render(gutter + string(buf))
}

// renderWaterfallTimeline renders the left panel with a time axis and horizontal
// bars colored by tool category.
func (m model) renderWaterfallTimeline(width int) string {
	const gutterWidth = 12 // "+XXX.Xs   " prefix

	barWidth := width - gutterWidth
	if barWidth < 1 {
		barWidth = 1
	}

	var b strings.Builder

	// Time axis header
	b.WriteString(renderTimeAxisHeader(gutterWidth, barWidth, m.wfTimeAxis.TotalMs))
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
	selectedBg := lipgloss.NewStyle().Background(ColorPickerSelectedBg)

	for i := start; i < end; i++ {
		row := m.wfRows[i]

		// Gutter: left-aligned relative timestamp padded to gutterWidth.
		gutter := fmt.Sprintf("+%-8s  ", formatRelativeMs(row.StartMs))

		var barArea string
		if row.IsUserSeparator {
			// Thin dimmed horizontal line across the bar area.
			barArea = dimStyle.Render(strings.Repeat("\u2500", barWidth))
		} else {
			// Compute bar start and width in columns.
			startCol := parser.ColOffset(row.StartMs, m.wfTimeAxis.TotalMs, barWidth)
			endCol := parser.ColOffset(row.StartMs+row.DurationMs, m.wfTimeAxis.TotalMs, barWidth)
			barCols := endCol - startCol
			if barCols < 1 {
				barCols = 1
			}

			// Choose color from primary tool category.
			var cat parser.ToolCategory
			if len(row.Tools) > 0 {
				cat = row.Tools[0].Category
			}
			col := categoryColor(cat)
			barStr := strings.Repeat("\u2588", barCols)
			coloredBar := lipgloss.NewStyle().Foreground(col).Render(barStr)

			// Label: primary tool name + extra count.
			primary := "unknown"
			if len(row.Tools) > 0 {
				primary = row.Tools[0].Name
			}
			label := primary
			if len(row.Tools) > 1 {
				label = fmt.Sprintf("%s +%d", primary, len(row.Tools)-1)
			}
			if row.IsSubagent {
				chevron := "\u25b6"
				if m.wfExpanded[i] {
					chevron = "\u25bc"
				}
				label = chevron + " " + label
			}

			// Build the bar area: leading spaces + colored bar + space + label.
			prefix := strings.Repeat(" ", startCol)
			barArea = prefix + coloredBar + " " + label
		}

		line := gutter + barArea

		if i == m.wfCursor {
			line = selectedBg.Render(line)
		}

		b.WriteString(line)
		b.WriteByte('\n')
	}

	return b.String()
}

// wfCategoryColor maps a ToolCategory to its theme color for inspector display.
// Named wfCategoryColor to avoid conflict with the parallel card bl-qx3j which
// also adds a categoryColor function to waterfall.go.
func wfCategoryColor(cat parser.ToolCategory) color.Color {
	switch cat {
	case parser.CategoryRead:
		return ColorToolRead
	case parser.CategoryEdit:
		return ColorToolEdit
	case parser.CategoryWrite:
		return ColorToolWrite
	case parser.CategoryBash:
		return ColorToolBash
	case parser.CategoryGrep:
		return ColorToolGrep
	case parser.CategoryGlob:
		return ColorToolGlob
	case parser.CategoryTask:
		return ColorToolTask
	case parser.CategoryTool:
		return ColorToolSkill
	case parser.CategoryWeb:
		return ColorToolWeb
	default:
		return ColorToolOther
	}
}

// renderWaterfallInspector renders the right-side inspector panel with
// timing-focused detail for the currently selected row.
func (m model) renderWaterfallInspector(width int) string {
	containerStyle := lipgloss.NewStyle().
		Width(width).
		PaddingLeft(1)

	dimStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	secondaryStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)

	// No row selected
	if m.wfCursor >= len(m.wfRows) {
		return containerStyle.Render(dimStyle.Render("Select a row to inspect"))
	}

	row := m.wfRows[m.wfCursor]

	// User separator: show minimal info
	if row.IsUserSeparator {
		return containerStyle.Render(secondaryStyle.Render(
			fmt.Sprintf("User message\n@ +%s", formatRelativeMs(row.StartMs)),
		))
	}

	var b strings.Builder

	divider := dimStyle.Render(strings.Repeat("\u2500", min(width-2, 20)))

	// --- Top metadata ---
	if row.Model != "" {
		modelStyle := lipgloss.NewStyle().Foreground(modelColor(row.Model))
		b.WriteString(fmt.Sprintf("Model: %s\n", modelStyle.Render(shortModel(row.Model))))
	}
	b.WriteString(fmt.Sprintf("Duration: %s\n", formatDuration(row.DurationMs)))
	b.WriteString(fmt.Sprintf("Tools: %d\n", len(row.Tools)))

	// --- Subagent section ---
	if row.IsSubagent {
		b.WriteByte('\n')
		b.WriteString(divider)
		b.WriteByte('\n')
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("Subagent"))
		b.WriteByte('\n')

		// Find the Task tool (CategoryTask) for subagent metadata
		for _, t := range row.Tools {
			if t.Category == parser.CategoryTask {
				if t.Name != "" {
					nameStyle := lipgloss.NewStyle().Foreground(wfCategoryColor(t.Category))
					b.WriteString(fmt.Sprintf("Type: %s\n", nameStyle.Render(t.Name)))
				}
				if t.Summary != "" {
					b.WriteString(fmt.Sprintf("Desc: %s\n", dimStyle.Render(t.Summary)))
				}
				break
			}
		}

		// Child tool count: all tools excluding the Task tool itself
		childCount := 0
		for _, t := range row.Tools {
			if t.Category != parser.CategoryTask {
				childCount++
			}
		}
		b.WriteString(fmt.Sprintf("Child tools: %d\n", childCount))
	}

	// --- Tool details ---
	if len(row.Tools) > 0 {
		b.WriteByte('\n')
		b.WriteString(divider)
		b.WriteByte('\n')
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("Tool Details"))
		b.WriteByte('\n')

		for _, t := range row.Tools {
			nameStyle := lipgloss.NewStyle().Foreground(wfCategoryColor(t.Category))
			namePart := nameStyle.Render(t.Name)
			durPart := secondaryStyle.Render(formatDuration(t.DurationMs))

			if t.Error {
				errorStyle := lipgloss.NewStyle().Foreground(ColorError).Bold(true)
				b.WriteString(fmt.Sprintf("%s  %s  %s\n", namePart, durPart, errorStyle.Render("ERROR")))
			} else {
				b.WriteString(fmt.Sprintf("%s  %s\n", namePart, durPart))
			}

			if t.Summary != "" {
				b.WriteString(fmt.Sprintf("  %s\n", dimStyle.Render(t.Summary)))
			}
		}
	}

	return containerStyle.Render(b.String())
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
