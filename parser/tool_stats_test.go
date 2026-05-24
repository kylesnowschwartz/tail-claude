package parser_test

import (
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestAggregateToolStats_BasicCounts(t *testing.T) {
	chunks := []parser.Chunk{
		{Type: parser.AIChunk, Items: []parser.DisplayItem{
			{Type: parser.ItemToolCall, ToolName: "Read"},
			{Type: parser.ItemToolCall, ToolName: "Read"},
			{Type: parser.ItemToolCall, ToolName: "Bash"},
			{Type: parser.ItemToolCall, ToolName: "Edit"},
		}},
		{Type: parser.AIChunk, Items: []parser.DisplayItem{
			{Type: parser.ItemToolCall, ToolName: "Read"},
			{Type: parser.ItemToolCall, ToolName: "Read"},
			{Type: parser.ItemToolCall, ToolName: "Read"},
		}},
	}
	stats := parser.AggregateToolStats(chunks)

	if len(stats) != 3 {
		t.Fatalf("len(stats) = %d, want 3 (Read, Bash, Edit)", len(stats))
	}
	// Sort order: by CallCount descending. Read(5) > Bash(1) ~ Edit(1) tied;
	// ties broken by Name ascending: Bash, Edit.
	if stats[0].Name != "Read" || stats[0].CallCount != 5 {
		t.Errorf("stats[0] = %+v, want Read x5", stats[0])
	}
	if stats[1].Name != "Bash" || stats[1].CallCount != 1 {
		t.Errorf("stats[1] = %+v, want Bash x1", stats[1])
	}
	if stats[2].Name != "Edit" || stats[2].CallCount != 1 {
		t.Errorf("stats[2] = %+v, want Edit x1", stats[2])
	}
}

func TestAggregateToolStats_SubagentCountsAsTask(t *testing.T) {
	chunks := []parser.Chunk{
		{Type: parser.AIChunk, Items: []parser.DisplayItem{
			{Type: parser.ItemSubagent, ToolName: "Skill"}, // counts as Task
			{Type: parser.ItemSubagent, ToolName: "Agent"}, // also counts as Task
			{Type: parser.ItemSubagent, ToolName: "Task"},
		}},
	}
	stats := parser.AggregateToolStats(chunks)
	if len(stats) != 1 {
		t.Fatalf("len(stats) = %d, want 1 (all collapse to Task)", len(stats))
	}
	if stats[0].Name != "Task" || stats[0].CallCount != 3 {
		t.Errorf("got %+v, want Task x3", stats[0])
	}
}

func TestAggregateToolStats_DurationAndErrors(t *testing.T) {
	chunks := []parser.Chunk{
		{Type: parser.AIChunk, Items: []parser.DisplayItem{
			{Type: parser.ItemToolCall, ToolName: "Bash", DurationMs: 1000, ToolError: false},
			{Type: parser.ItemToolCall, ToolName: "Bash", DurationMs: 500, ToolError: true},
			{Type: parser.ItemToolCall, ToolName: "Bash", DurationMs: 0, ToolError: true}, // 0 dur not summed
		}},
	}
	stats := parser.AggregateToolStats(chunks)
	if len(stats) != 1 {
		t.Fatalf("len(stats) = %d, want 1", len(stats))
	}
	s := stats[0]
	if s.CallCount != 3 {
		t.Errorf("CallCount = %d, want 3", s.CallCount)
	}
	if s.TotalDurationMs != 1500 {
		t.Errorf("TotalDurationMs = %d, want 1500 (zero-duration excluded)", s.TotalDurationMs)
	}
	if s.ErrorCount != 2 {
		t.Errorf("ErrorCount = %d, want 2", s.ErrorCount)
	}
}

func TestAggregateToolStats_SkipsNonAIChunks(t *testing.T) {
	chunks := []parser.Chunk{
		{Type: parser.UserChunk, UserText: "hi"},
		{Type: parser.SystemChunk, Output: "ok"},
		{Type: parser.CompactChunk, Output: "compressed"},
		{Type: parser.AIChunk, Items: []parser.DisplayItem{
			{Type: parser.ItemToolCall, ToolName: "Read"},
		}},
	}
	stats := parser.AggregateToolStats(chunks)
	if len(stats) != 1 || stats[0].Name != "Read" {
		t.Errorf("got %+v, want only Read", stats)
	}
}

func TestAggregateToolStats_SkipsNonToolItems(t *testing.T) {
	// Thinking, Output, TeammateMessage, MemoryLoad items should not count.
	chunks := []parser.Chunk{
		{Type: parser.AIChunk, Items: []parser.DisplayItem{
			{Type: parser.ItemThinking, Text: "hmm"},
			{Type: parser.ItemOutput, Text: "answer"},
			{Type: parser.ItemTeammateMessage, Text: "msg"},
			{Type: parser.ItemMemoryLoad, Text: "path"},
		}},
	}
	stats := parser.AggregateToolStats(chunks)
	if len(stats) != 0 {
		t.Errorf("len(stats) = %d, want 0 (no tool calls)", len(stats))
	}
}

func TestAggregateToolStats_EmptyReturnsEmptySlice(t *testing.T) {
	stats := parser.AggregateToolStats(nil)
	if stats == nil {
		t.Error("AggregateToolStats(nil) returned nil, want empty slice")
	}
	if len(stats) != 0 {
		t.Errorf("len = %d, want 0", len(stats))
	}
}

func TestToolAbbrev_KnownAndUnknown(t *testing.T) {
	cases := map[string]string{
		"Read":         "R",
		"Bash":         "B",
		"Glob":         "g", // lowercase to distinguish from Grep
		"WebSearch":    "S",
		"TodoWrite":    "t",
		"Unknown_Tool": "U", // fallback: first rune
		"":             "",  // empty stays empty
		"嗨":            "嗨", // non-ASCII first rune
	}
	for name, want := range cases {
		if got := parser.ToolAbbrev(name); got != want {
			t.Errorf("ToolAbbrev(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestTopTools_FormatsCompactString(t *testing.T) {
	stats := []parser.ToolStats{
		{Name: "Read", CallCount: 47},
		{Name: "Bash", CallCount: 12},
		{Name: "Edit", CallCount: 3},
		{Name: "Grep", CallCount: 1},
	}
	if got := parser.TopTools(stats, 3); got != "47R 12B 3E" {
		t.Errorf("TopTools(stats, 3) = %q, want %q", got, "47R 12B 3E")
	}
	if got := parser.TopTools(stats, 10); got != "47R 12B 3E 1G" {
		t.Errorf("TopTools(stats, 10) = %q, want %q (clamps to len)", got, "47R 12B 3E 1G")
	}
}

func TestTopTools_EdgeCases(t *testing.T) {
	if got := parser.TopTools(nil, 3); got != "" {
		t.Errorf("TopTools(nil, 3) = %q, want empty", got)
	}
	stats := []parser.ToolStats{{Name: "Read", CallCount: 1}}
	if got := parser.TopTools(stats, 0); got != "" {
		t.Errorf("TopTools(_, 0) = %q, want empty", got)
	}
	if got := parser.TopTools(stats, -1); got != "" {
		t.Errorf("TopTools(_, -1) = %q, want empty", got)
	}
}
