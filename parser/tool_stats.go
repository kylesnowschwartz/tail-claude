package parser

import (
	"sort"
	"strconv"
	"strings"
)

// ToolStats aggregates per-tool usage over a sequence of chunks. Sorted by
// CallCount descending when returned by AggregateToolStats.
type ToolStats struct {
	Name            string
	CallCount       int
	TotalDurationMs int64
	ErrorCount      int
}

// AggregateToolStats walks chunks and returns per-tool counts.
//
// Subagent dispatches (ItemSubagent) are counted under "Task" -- they share
// the dispatch tool name even when the underlying type is "Skill" or "Agent".
// The breakdown of subagent types is the stats view's job, not the
// aggregator's.
//
// Duration sums only include results where DurationMs > 0. The concurrent-
// task suppression already zeros inflated durations, so this is the right
// floor for "trustworthy" totals.
func AggregateToolStats(chunks []Chunk) []ToolStats {
	byName := make(map[string]*ToolStats)

	for _, c := range chunks {
		if c.Type != AIChunk {
			continue
		}
		for _, it := range c.Items {
			name := toolStatsName(it)
			if name == "" {
				continue
			}
			s, ok := byName[name]
			if !ok {
				s = &ToolStats{Name: name}
				byName[name] = s
			}
			s.CallCount++
			if it.DurationMs > 0 {
				s.TotalDurationMs += it.DurationMs
			}
			if it.ToolError {
				s.ErrorCount++
			}
		}
	}

	out := make([]ToolStats, 0, len(byName))
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

// toolStatsName returns the name to attribute to a DisplayItem in the stats
// aggregation, or "" to skip non-tool items.
func toolStatsName(it DisplayItem) string {
	switch it.Type {
	case ItemToolCall:
		return it.ToolName
	case ItemSubagent:
		return "Task"
	default:
		return ""
	}
}

// toolAbbrev maps full tool names to single-character display tokens for
// the compact picker suffix ("47R 12B 3E"). Collisions are accepted -- the
// suffix is informational, not authoritative. Lowercase is used to
// disambiguate visually similar tools (Glob vs Grep, TodoWrite vs Task).
var toolAbbrev = map[string]string{
	"Read":         "R",
	"Write":        "W",
	"Edit":         "E",
	"Bash":         "B",
	"Grep":         "G",
	"Glob":         "g",
	"Task":         "T",
	"WebFetch":     "F",
	"WebSearch":    "S",
	"TodoWrite":    "t",
	"NotebookEdit": "N",
	"LSP":          "L",
}

// ToolAbbrev returns the single-character abbreviation for a tool name,
// falling back to the first character of the name when unknown. Unknown
// tools return "" only when name is empty.
func ToolAbbrev(name string) string {
	if abbr, ok := toolAbbrev[name]; ok {
		return abbr
	}
	if name == "" {
		return ""
	}
	return string([]rune(name)[0])
}

// TopTools returns the n highest-call-count tools as a compact display
// string: "47R 12B 3E". Empty when stats is empty or n <= 0.
func TopTools(stats []ToolStats, n int) string {
	if n <= 0 || len(stats) == 0 {
		return ""
	}
	if n > len(stats) {
		n = len(stats)
	}
	var sb strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(strconv.Itoa(stats[i].CallCount))
		sb.WriteString(ToolAbbrev(stats[i].Name))
	}
	return sb.String()
}
