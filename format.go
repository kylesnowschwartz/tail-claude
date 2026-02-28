package main

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
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
func modelColor(model string) color.Color {
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

// teamColor returns a color for a team member color name from the JSONL data.
// Falls back to ColorAccent (blue) for unknown or empty names.
func teamColor(name string) color.Color {
	switch strings.ToLower(name) {
	case "blue":
		return ColorTeamBlue
	case "green":
		return ColorTeamGreen
	case "red":
		return ColorTeamRed
	case "yellow":
		return ColorTeamYellow
	case "purple":
		return ColorTeamPurple
	case "cyan":
		return ColorTeamCyan
	case "orange":
		return ColorTeamOrange
	case "pink":
		return ColorTeamPink
	default:
		return ColorAccent
	}
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

// formatSessionName formats a session ID for compact picker display.
// Standard UUIDs (8-4-4-4-12 hex with dashes = 36 chars) show only the first
// group (8 chars) — enough to distinguish sessions without burning line width.
// Renamed sessions show up to 20 characters.
func formatSessionName(id string) string {
	if len(id) == 36 && id[8] == '-' && id[13] == '-' && id[18] == '-' && id[23] == '-' {
		return id[:8]
	}
	return parser.TruncateWord(id, 20)
}

// shortPath returns the project display name for the info bar.
// Uses git-root resolution for worktrees and submodules, with branch
// suffix trimming for worktree directory names.
func shortPath(cwd, gitBranch string) string {
	if cwd == "" {
		return ""
	}
	return parser.ProjectName(cwd, gitBranch)
}

// shortMode returns a human-readable label for a permission mode.
func shortMode(mode string) string {
	switch mode {
	case "default":
		return "default"
	case "acceptEdits":
		return "auto-edit"
	case "bypassPermissions":
		return "yolo"
	case "plan":
		return "plan"
	default:
		return mode
	}
}

// contextPercent returns the context window usage percentage (0-100) based on
// the last AI message's input tokens. Returns -1 if no usage data is available.
func contextPercent(msgs []message) int {
	// All current Claude models share a 200k context window.
	const contextWindowSize = 200_000

	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].role == RoleClaude && msgs[i].contextTokens > 0 {
			pct := msgs[i].contextTokens * 100 / contextWindowSize
			if pct > 100 {
				pct = 100
			}
			return pct
		}
	}
	return -1
}

// hasTeamTaskItems checks if any chunk contains team Task items (Task calls
// with team_name + name in input). Used to decide whether directory events
// should trigger team session re-discovery.
func hasTeamTaskItems(chunks []parser.Chunk) bool {
	for i := range chunks {
		for j := range chunks[i].Items {
			it := &chunks[i].Items[j]
			if it.Type == parser.ItemSubagent && parser.IsTeamTask(it) {
				return true
			}
		}
	}
	return false
}
