package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	"github.com/charmbracelet/lipgloss"
)

// shortModel turns "claude-opus-4-6" into "opus4.6".
func shortModel(m string) string {
	m = strings.TrimPrefix(m, "claude-")
	parts := strings.SplitN(m, "-", 2)
	if len(parts) == 2 {
		modelFamily := parts[0]
		// Keep major-minor only, drop patch/build metadata (e.g. "4-6-20250101" -> "4-6").
		vParts := strings.SplitN(parts[1], "-", 3)
		modelVersion := vParts[0]
		if len(vParts) >= 2 {
			modelVersion = vParts[0] + "-" + vParts[1]
		}
		return modelFamily + strings.ReplaceAll(modelVersion, "-", ".")
	}
	return m
}

// modelColor returns a color based on the Claude model family.
func modelColor(model string) lipgloss.AdaptiveColor {
	switch {
	case strings.Contains(model, "opus"):
		return ColorModelOpus
	case strings.Contains(model, "sonnet"):
		return ColorModelSonnet
	case strings.Contains(model, "haiku"):
		return ColorModelHaiku
	default:
		return ColorTextSecondary
	}
}

// isTeamTaskItem checks whether a DisplayItem is a team Task call by looking
// for team_name and name in ToolInput. Thin wrapper matching parser.isTeamTask
// but takes a pointer to avoid allocation.
func isTeamTaskItem(it *parser.DisplayItem) bool {
	if len(it.ToolInput) == 0 {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(it.ToolInput, &fields); err != nil {
		return false
	}
	_, hasTeamName := fields["team_name"]
	_, hasName := fields["name"]
	return hasTeamName && hasName
}

// countOutputItems counts text output items in a display items slice.
func countOutputItems(items []parser.DisplayItem) int {
	n := 0
	for _, it := range items {
		if it.Type == parser.ItemOutput {
			n++
		}
	}
	return n
}

// formatTime renders a timestamp for the message header.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("3:04:05 PM")
}

// formatTokens formats a token count for display: 1234 -> "1.2k", 123456 -> "123.5k", 1234567 -> "1.2M"
func formatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// formatDuration formats milliseconds into human-readable duration: 71000 -> "1m 11s", 3500 -> "3.5s"
func formatDuration(ms int64) string {
	secs := float64(ms) / 1000
	switch {
	case secs >= 60:
		mins := int(secs) / 60
		rem := int(secs) % 60
		return fmt.Sprintf("%dm %ds", mins, rem)
	case secs >= 10:
		return fmt.Sprintf("%.0fs", secs)
	default:
		return fmt.Sprintf("%.1fs", secs)
	}
}


// hasTeamTaskItems checks if any chunk contains team Task items (Task calls
// with team_name + name in input). Used to decide whether directory events
// should trigger team session re-discovery.
func hasTeamTaskItems(chunks []parser.Chunk) bool {
	for i := range chunks {
		for j := range chunks[i].Items {
			it := &chunks[i].Items[j]
			if it.Type == parser.ItemSubagent && isTeamTaskItem(it) {
				return true
			}
		}
	}
	return false
}
