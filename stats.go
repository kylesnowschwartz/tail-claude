package main

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/kylesnowschwartz/agent-ouija/claude/tools"
	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
)

// aggregateMessageStats walks the loaded TUI messages and returns
// per-tool usage stats, sorted by call count descending.
//
// Consumes the TUI's []message rather than []transcript.Chunk -- the model
// doesn't keep the underlying chunks, and the per-key-press cost of a
// 1000-item walk is negligible.
//
// Duration sums only include results where durationMs > 0. The concurrent-
// task suppression already zeros inflated durations, so this is the right
// floor for "trustworthy" totals.
func aggregateMessageStats(msgs []message) []tools.ToolStats {
	byName := make(map[string]*tools.ToolStats)
	for _, m := range msgs {
		if m.role != RoleClaude {
			continue
		}
		for _, it := range m.items {
			name := statsName(it)
			if name == "" {
				continue
			}
			s, ok := byName[name]
			if !ok {
				s = &tools.ToolStats{Name: name}
				byName[name] = s
			}
			s.CallCount++
			if it.durationMs > 0 {
				s.TotalDurationMs += it.durationMs
			}
			if it.toolError {
				s.ErrorCount++
			}
		}
	}
	out := make([]tools.ToolStats, 0, len(byName))
	for _, s := range byName {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CallCount != out[j].CallCount {
			return out[i].CallCount > out[j].CallCount
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// statsName returns the name to attribute to a displayItem in the stats
// aggregation: subagents fold into "Task" (they share the dispatch tool name
// even when the underlying type is "Skill" or "Agent"), non-tool items
// return "" to skip.
func statsName(it displayItem) string {
	switch it.itemType {
	case transcript.ItemToolCall:
		return it.toolName
	case transcript.ItemSubagent:
		return "Task"
	default:
		return ""
	}
}

// updateStats handles input in the stats view. Any key returns to the
// previous view (list). Esc/q/backspace explicit; everything else also
// closes it -- the view is read-only and not meant to be dwelt in.
func (m model) updateStats(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	}
	m.enterList()
	return m, nil
}

// viewStats renders the tool-usage summary for the loaded session.
//
// Layout:
//
//	Tool Usage  total: 65 calls  errors: 3
//
//	  Tool             Count   Total   Errors
//	  ────             ─────   ─────   ──────
//	  Read                47    2.1s        -
//	  Bash                12   18.3s        2
//	  Edit                 3    0.4s        -
//
// Sorted by CallCount descending; errors column dashed when zero.
func (m model) viewStats() string {
	width := m.clampWidth()
	stats := aggregateMessageStats(m.messages)

	header := StylePrimaryBold.Render("Tool Usage") + "  " + statsHeaderTotals(stats)

	if len(stats) == 0 {
		empty := StyleDim.Render("No tool calls in this session.")
		return centerBlock(header+"\n\n"+empty, lipgloss.Width(header), width)
	}

	rows := []string{
		fmt.Sprintf("  %-16s %5s   %5s   %6s", "Tool", "Count", "Total", "Errors"),
		fmt.Sprintf("  %-16s %5s   %5s   %6s", "────", "─────", "─────", "──────"),
	}
	for _, s := range stats {
		errCell := StyleDim.Render("       -")
		if s.ErrorCount > 0 {
			errCell = lipgloss.NewStyle().Foreground(ColorError).Render(fmt.Sprintf("%6d", s.ErrorCount)) + "  "
		}
		dur := formatDuration(s.TotalDurationMs)
		if s.TotalDurationMs == 0 {
			dur = "-"
		}
		rows = append(rows, fmt.Sprintf("  %-16s %5d   %5s   %s", s.Name, s.CallCount, dur, errCell))
	}

	footer := StyleDim.Render("Press any key to return")
	body := strings.Join(rows, "\n")
	return header + "\n\n" + body + "\n\n" + footer
}

// statsHeaderTotals returns a one-line summary of total calls and total errors.
func statsHeaderTotals(stats []tools.ToolStats) string {
	if len(stats) == 0 {
		return StyleDim.Render("(no tool calls)")
	}
	var totalCalls, totalErrors int
	for _, s := range stats {
		totalCalls += s.CallCount
		totalErrors += s.ErrorCount
	}
	parts := []string{
		StyleSecondary.Render(fmt.Sprintf("%d calls", totalCalls)),
	}
	if totalErrors > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(ColorError).Render(fmt.Sprintf("%d errors", totalErrors)))
	}
	dot := " " + Icon.Dot.Render() + " "
	return strings.Join(parts, dot)
}
