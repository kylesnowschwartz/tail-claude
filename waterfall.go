package main

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

// niceIntervals is the ordered set of round time boundaries (in ms) that the
// smart tick algorithm snaps to. Derived from agentviz TimeAxis.jsx.
var niceIntervals = []int64{
	100, 250, 500,
	1_000, 2_000, 5_000, 10_000, 15_000, 30_000,
	60_000, 120_000, 300_000, 600_000,
}

// pickNiceInterval returns the nice interval closest to rawInterval (in ms).
// When rawInterval falls exactly between two candidates the larger one wins,
// which keeps labels from overlapping on narrow bars.
func pickNiceInterval(rawInterval int64) int64 {
	if rawInterval <= 0 {
		return niceIntervals[0]
	}
	best := niceIntervals[0]
	bestDiff := abs64(rawInterval - best)
	for _, v := range niceIntervals[1:] {
		d := abs64(rawInterval - v)
		if d < bestDiff {
			best = v
			bestDiff = d
		}
	}
	return best
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// renderTimeAxisHeader renders a one-line time axis with tick marks at smart
// round-number intervals. The number of ticks targets ~8 on wide terminals and
// ~4 on narrow ones (barWidth < 60). Tick positions are placed using compressed
// display coordinates from timeMap, but labels show real (uncompressed)
// wall-clock offsets so the user knows actual elapsed time.
//
// When totalMs is zero no ticks are rendered.
func renderTimeAxisHeader(gutterWidth, barWidth int, totalMs int64, timeMap parser.TimeMap) string {
	dimStyle := lipgloss.NewStyle().Faint(true)

	gutter := strings.Repeat(" ", gutterWidth)

	if totalMs <= 0 {
		return dimStyle.Render(gutter)
	}

	// Extra capacity so the rightmost tick label isn't clipped by the buffer
	// boundary. The last tick label (e.g. "|10.0s") can be up to ~8 chars,
	// so 16 bytes of headroom is plenty.
	bufLen := barWidth + 16
	buf := make([]byte, bufLen)
	for i := range buf {
		buf[i] = ' '
	}

	compressedTotal := timeMap.CompressedTotalMs
	if compressedTotal <= 0 {
		compressedTotal = totalMs
	}

	// Target fewer ticks on narrow terminals to avoid label collisions.
	targetTicks := int64(8)
	if barWidth < 60 {
		targetTicks = 4
	}

	interval := pickNiceInterval(totalMs / targetTicks)

	// Generate ticks at 0, interval, 2*interval, ... up to totalMs (inclusive).
	for rawMs := int64(0); rawMs <= totalMs; rawMs += interval {
		displayMs := timeMap.MapToDisplay(rawMs)
		col := parser.ColOffset(displayMs, compressedTotal, barWidth)
		label := "|" + formatRelativeMs(rawMs)
		for j, ch := range []byte(label) {
			if col+j < bufLen {
				buf[col+j] = ch
			}
		}
	}

	// Trim trailing spaces while keeping the full last label.
	result := strings.TrimRight(string(buf), " ")
	return dimStyle.Render(gutter + result)
}

// fractionalBlock returns the left-block element rune for a fractional cell
// width (0.0 to 1.0). Uses 1/8th character precision via Unicode block elements.
// Ported from NimbleMarkets/ntcharts canvas/runes/runes.go.
func fractionalBlock(f float64) rune {
	if f >= 1.0 {
		return '\u2588' // full block
	}
	if f <= 0.0 {
		return 0
	}
	// 8 levels: ▏▎▍▌▋▊▉█
	leftBlocks := [9]rune{
		0,        // 0/8
		'\u258F', // 1/8 ▏
		'\u258E', // 2/8 ▎
		'\u258D', // 3/8 ▍
		'\u258C', // 4/8 ▌
		'\u258B', // 5/8 ▋
		'\u258A', // 6/8 ▊
		'\u2589', // 7/8 ▉
		'\u2588', // 8/8 █
	}
	idx := int(f / 0.125)
	if remainder := f - float64(idx)*0.125; remainder >= 0.0625 {
		idx++ // round up at 1/16th boundary
	}
	if idx > 8 {
		idx = 8
	}
	return leftBlocks[idx]
}

// renderBarString builds a proportional horizontal bar with sub-character
// trailing edge precision. widthCells is in float64 character cells.
func renderBarString(widthCells float64, barColor color.Color) string {
	if widthCells <= 0 {
		return ""
	}

	fullCount := int(math.Floor(widthCells))
	frac := widthCells - float64(fullCount)
	trail := fractionalBlock(frac)

	var buf strings.Builder

	// Full blocks with fg+bg matching for solid fill.
	if fullCount > 0 {
		solidStyle := lipgloss.NewStyle().Foreground(barColor).Background(barColor)
		buf.WriteString(solidStyle.Render(strings.Repeat("\u2588", fullCount)))
	}

	// Trailing fractional character with fg=bar color, bg=default.
	if trail != 0 {
		trailStyle := lipgloss.NewStyle().Foreground(barColor)
		buf.WriteString(trailStyle.Render(string(trail)))
	}

	return buf.String()
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
	b.WriteString(renderTimeAxisHeader(gutterWidth, barWidth, m.wfTimeAxis.TotalMs, m.wfTimeMap))
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
			// Float64 scaling for sub-character precision using compressed display ms.
			// MapToDisplay converts raw StartMs/EndMs to compressed coordinates so
			// long idle gaps shrink and active tool work spreads proportionally.
			compressedTotal := m.wfTimeMap.CompressedTotalMs
			if compressedTotal <= 0 {
				compressedTotal = m.wfTimeAxis.TotalMs
			}
			scaleFactor := float64(barWidth) / float64(compressedTotal)
			if compressedTotal == 0 {
				scaleFactor = 0
			}
			displayStartMs := m.wfTimeMap.MapToDisplay(row.StartMs)
			displayEndMs := m.wfTimeMap.MapToDisplay(row.StartMs + row.DurationMs)
			startColF := float64(displayStartMs) * scaleFactor
			barWidthF := float64(displayEndMs-displayStartMs) * scaleFactor
			if barWidthF < 0.125 {
				barWidthF = 0.125 // minimum visible: 1/8th block
			}

			// Round start to nearest whole column.
			startCol := int(math.Round(startColF))
			if startCol >= barWidth {
				startCol = barWidth - 1
			}

			// Choose color from primary tool category.
			var cat parser.ToolCategory
			if len(row.Tools) > 0 {
				cat = row.Tools[0].Category
			}
			barCol := categoryColor(cat)
			coloredBar := renderBarString(barWidthF, barCol)

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
			// Truncate to barWidth so the bar never bleeds into the inspector panel.
			prefix := strings.Repeat(" ", startCol)
			barArea = ansi.Truncate(prefix+coloredBar+" "+label, barWidth, "")
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


// renderWaterfallInspector renders the right-side inspector panel with
// timing-focused detail for the currently selected row.
func (m model) renderWaterfallInspector(width int) string {
	containerStyle := lipgloss.NewStyle().
		Width(width).
		PaddingLeft(1)

	dimStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	secondaryStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)

	// No row selected: show session statistics instead of an empty hint.
	if m.wfCursor >= len(m.wfRows) {
		return containerStyle.Render(m.renderWaterfallStats(width, dimStyle, secondaryStyle))
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
					nameStyle := lipgloss.NewStyle().Foreground(categoryColor(t.Category))
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
			nameStyle := lipgloss.NewStyle().Foreground(categoryColor(t.Category))
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

// renderWaterfallStats formats the aggregate session statistics for the inspector
// panel when no row is selected.
func (m model) renderWaterfallStats(width int, dimStyle, secondaryStyle lipgloss.Style) string {
	divider := dimStyle.Render(strings.Repeat("\u2500", min(width-2, 20)))
	boldStyle := lipgloss.NewStyle().Bold(true)

	if m.wfStats.TotalTools == 0 {
		return dimStyle.Render("No tool calls")
	}

	var b strings.Builder

	b.WriteString(boldStyle.Render("Session Statistics"))
	b.WriteByte('\n')
	b.WriteString(divider)
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("%s  %s\n",
		dimStyle.Render("Duration:   "),
		secondaryStyle.Render(formatDuration(m.wfStats.SessionMs))))
	b.WriteString(fmt.Sprintf("%s  %s\n",
		dimStyle.Render("Tools:      "),
		secondaryStyle.Render(fmt.Sprintf("%d", m.wfStats.TotalTools))))
	b.WriteString(fmt.Sprintf("%s  %s\n",
		dimStyle.Render("Concurrency:"),
		secondaryStyle.Render(fmt.Sprintf("%d max", m.wfStats.MaxConcurrency))))
	if m.wfStats.LongestTool != "" {
		longest := fmt.Sprintf("%s (%s)", m.wfStats.LongestTool, formatDuration(m.wfStats.LongestToolMs))
		b.WriteString(fmt.Sprintf("%s  %s\n",
			dimStyle.Render("Longest:    "),
			secondaryStyle.Render(longest)))
	}

	if len(m.wfStats.TopTools) > 0 {
		b.WriteByte('\n')
		b.WriteString(boldStyle.Render("Top Tools"))
		b.WriteByte('\n')
		b.WriteString(divider)
		b.WriteByte('\n')
		for _, tf := range m.wfStats.TopTools {
			b.WriteString(fmt.Sprintf("%-12s  %s\n",
				secondaryStyle.Render(tf.Name),
				dimStyle.Render(fmt.Sprintf("%d", tf.Count))))
		}
	}

	return b.String()
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
