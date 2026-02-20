package parser_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestDiscoverSubagents_FindsValidAgents(t *testing.T) {
	sessionPath := filepath.Join("testdata", "test-session.jsonl")

	procs, err := parser.DiscoverSubagents(sessionPath)
	if err != nil {
		t.Fatalf("DiscoverSubagents error: %v", err)
	}

	// Should find exactly 2: abc1234 and def5678.
	// Filtered: warmup99 (warmup), acompact-xyz (compact), empty000 (empty).
	if len(procs) != 2 {
		t.Fatalf("got %d subagents, want 2", len(procs))
	}

	// Sorted by StartTime, abc1234 (10:00:00) before def5678 (10:01:00).
	if procs[0].ID != "abc1234" {
		t.Errorf("procs[0].ID = %q, want %q", procs[0].ID, "abc1234")
	}
	if procs[1].ID != "def5678" {
		t.Errorf("procs[1].ID = %q, want %q", procs[1].ID, "def5678")
	}
}

func TestDiscoverSubagents_ParsesChunks(t *testing.T) {
	sessionPath := filepath.Join("testdata", "test-session.jsonl")

	procs, err := parser.DiscoverSubagents(sessionPath)
	if err != nil {
		t.Fatalf("DiscoverSubagents error: %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("expected at least 1 subagent")
	}

	// Each subagent fixture has 1 user + 1 assistant = 2 chunks (user + AI).
	// But subagent entries have isSidechain=true, which Classify filters.
	// So we actually need to check what we get.
	p := procs[0]
	if len(p.Chunks) == 0 {
		t.Errorf("procs[0] has 0 chunks, expected parsed content")
	}
	if p.FilePath == "" {
		t.Error("FilePath is empty")
	}
}

func TestDiscoverSubagents_ComputesTiming(t *testing.T) {
	sessionPath := filepath.Join("testdata", "test-session.jsonl")

	procs, err := parser.DiscoverSubagents(sessionPath)
	if err != nil {
		t.Fatalf("DiscoverSubagents error: %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("expected at least 1 subagent")
	}

	p := procs[0]
	if p.StartTime.IsZero() {
		t.Error("StartTime is zero")
	}
	if p.EndTime.IsZero() {
		t.Error("EndTime is zero")
	}
	if p.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >= 0", p.DurationMs)
	}
}

func TestDiscoverSubagents_AggregatesUsage(t *testing.T) {
	sessionPath := filepath.Join("testdata", "test-session.jsonl")

	procs, err := parser.DiscoverSubagents(sessionPath)
	if err != nil {
		t.Fatalf("DiscoverSubagents error: %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("expected at least 1 subagent")
	}

	// abc1234 fixture has: input=100, output=20, cache_read=50
	p := procs[0]
	if p.Usage.TotalTokens() == 0 {
		t.Error("expected non-zero token usage")
	}
}

func TestDiscoverSubagents_FiltersWarmup(t *testing.T) {
	sessionPath := filepath.Join("testdata", "test-session.jsonl")

	procs, err := parser.DiscoverSubagents(sessionPath)
	if err != nil {
		t.Fatalf("DiscoverSubagents error: %v", err)
	}

	for _, p := range procs {
		if p.ID == "warmup99" {
			t.Error("warmup agent should be filtered out")
		}
	}
}

func TestDiscoverSubagents_FiltersCompact(t *testing.T) {
	sessionPath := filepath.Join("testdata", "test-session.jsonl")

	procs, err := parser.DiscoverSubagents(sessionPath)
	if err != nil {
		t.Fatalf("DiscoverSubagents error: %v", err)
	}

	for _, p := range procs {
		if p.ID == "acompact-xyz" {
			t.Error("compact agent should be filtered out")
		}
	}
}

func TestDiscoverSubagents_FiltersEmpty(t *testing.T) {
	sessionPath := filepath.Join("testdata", "test-session.jsonl")

	procs, err := parser.DiscoverSubagents(sessionPath)
	if err != nil {
		t.Fatalf("DiscoverSubagents error: %v", err)
	}

	for _, p := range procs {
		if p.ID == "empty000" {
			t.Error("empty agent should be filtered out")
		}
	}
}

func TestDiscoverSubagents_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "nosession.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"uuid":"x","type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// No subagents directory at all.
	procs, err := parser.DiscoverSubagents(sessionPath)
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got: %v", err)
	}
	if len(procs) != 0 {
		t.Errorf("expected 0 subagents, got %d", len(procs))
	}
}

func TestDiscoverSubagents_EmptySubagentsDir(t *testing.T) {
	tmpDir := t.TempDir()
	sessionPath := filepath.Join(tmpDir, "sess.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"uuid":"x","type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create empty subagents directory.
	subDir := filepath.Join(tmpDir, "sess", "subagents")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	procs, err := parser.DiscoverSubagents(sessionPath)
	if err != nil {
		t.Fatalf("DiscoverSubagents error: %v", err)
	}
	if len(procs) != 0 {
		t.Errorf("expected 0 subagents, got %d", len(procs))
	}
}

// --- LinkSubagents tests ---

// makeTaskChunk builds an AI chunk with a Task tool call and tool result.
func makeTaskChunk(toolID, agentID, subagentType, desc string) parser.Chunk {
	result := "Task completed successfully.\nagentId: " + agentID + "\ntotalTokens: 500"
	input, _ := json.Marshal(map[string]string{
		"subagent_type": subagentType,
		"description":   desc,
	})
	return parser.Chunk{
		Type:      parser.AIChunk,
		Timestamp: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		Items: []parser.DisplayItem{
			{
				Type:         parser.ItemSubagent,
				ToolName:     "Task",
				ToolID:       toolID,
				ToolInput:    json.RawMessage(input),
				ToolResult:   result,
				SubagentType: subagentType,
				SubagentDesc: desc,
			},
		},
	}
}

func TestLinkSubagents_ResultBased(t *testing.T) {
	procs := []parser.SubagentProcess{
		{ID: "abc1234", StartTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)},
		{ID: "def5678", StartTime: time.Date(2025, 6, 15, 10, 1, 0, 0, time.UTC)},
	}
	chunks := []parser.Chunk{
		makeTaskChunk("tool-1", "abc1234", "Explore", "Search codebase"),
		makeTaskChunk("tool-2", "def5678", "Plan", "Design architecture"),
	}

	parser.LinkSubagents(procs, chunks)

	if procs[0].ParentTaskID != "tool-1" {
		t.Errorf("procs[0].ParentTaskID = %q, want %q", procs[0].ParentTaskID, "tool-1")
	}
	if procs[0].SubagentType != "Explore" {
		t.Errorf("procs[0].SubagentType = %q, want %q", procs[0].SubagentType, "Explore")
	}
	if procs[0].Description != "Search codebase" {
		t.Errorf("procs[0].Description = %q, want %q", procs[0].Description, "Search codebase")
	}

	if procs[1].ParentTaskID != "tool-2" {
		t.Errorf("procs[1].ParentTaskID = %q, want %q", procs[1].ParentTaskID, "tool-2")
	}
	if procs[1].SubagentType != "Plan" {
		t.Errorf("procs[1].SubagentType = %q, want %q", procs[1].SubagentType, "Plan")
	}
}

func TestLinkSubagents_PositionalFallback(t *testing.T) {
	procs := []parser.SubagentProcess{
		{ID: "nomatch1", StartTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)},
	}

	// Task call with no agentId in result.
	input, _ := json.Marshal(map[string]string{
		"subagent_type": "general-purpose",
		"description":   "Fix the bug",
	})
	chunks := []parser.Chunk{
		{
			Type:      parser.AIChunk,
			Timestamp: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
			Items: []parser.DisplayItem{
				{
					Type:         parser.ItemSubagent,
					ToolName:     "Task",
					ToolID:       "tool-99",
					ToolInput:    json.RawMessage(input),
					ToolResult:   "Done, no agentId here.",
					SubagentType: "general-purpose",
					SubagentDesc: "Fix the bug",
				},
			},
		},
	}

	parser.LinkSubagents(procs, chunks)

	if procs[0].ParentTaskID != "tool-99" {
		t.Errorf("procs[0].ParentTaskID = %q, want %q (positional fallback)", procs[0].ParentTaskID, "tool-99")
	}
	if procs[0].Description != "Fix the bug" {
		t.Errorf("procs[0].Description = %q, want %q", procs[0].Description, "Fix the bug")
	}
}

func TestLinkSubagents_UnmatchedKeepsEmpty(t *testing.T) {
	procs := []parser.SubagentProcess{
		{ID: "orphan1"},
	}
	// No parent chunks at all.
	parser.LinkSubagents(procs, nil)

	if procs[0].ParentTaskID != "" {
		t.Errorf("unmatched procs[0].ParentTaskID = %q, want empty", procs[0].ParentTaskID)
	}
	if procs[0].Description != "" {
		t.Errorf("unmatched procs[0].Description = %q, want empty", procs[0].Description)
	}
}

func TestLinkSubagents_NoProcesses(t *testing.T) {
	chunks := []parser.Chunk{
		makeTaskChunk("tool-1", "abc", "Explore", "Search"),
	}
	// Should not panic with empty processes.
	parser.LinkSubagents(nil, chunks)
}

func TestLinkSubagents_MixedMatching(t *testing.T) {
	// Two processes: one matches by agentId, one falls back to positional.
	procs := []parser.SubagentProcess{
		{ID: "matched1", StartTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)},
		{ID: "unmatched1", StartTime: time.Date(2025, 6, 15, 10, 1, 0, 0, time.UTC)},
	}

	input1, _ := json.Marshal(map[string]string{"subagent_type": "Explore", "description": "Find files"})
	input2, _ := json.Marshal(map[string]string{"subagent_type": "Plan", "description": "Plan work"})

	chunks := []parser.Chunk{
		{
			Type: parser.AIChunk,
			Items: []parser.DisplayItem{
				{
					Type:         parser.ItemSubagent,
					ToolName:     "Task",
					ToolID:       "tool-A",
					ToolInput:    json.RawMessage(input1),
					ToolResult:   "agentId: matched1\nDone.",
					SubagentType: "Explore",
					SubagentDesc: "Find files",
				},
				{
					Type:         parser.ItemSubagent,
					ToolName:     "Task",
					ToolID:       "tool-B",
					ToolInput:    json.RawMessage(input2),
					ToolResult:   "Completed without agentId.",
					SubagentType: "Plan",
					SubagentDesc: "Plan work",
				},
			},
		},
	}

	parser.LinkSubagents(procs, chunks)

	// matched1 linked by agentId.
	if procs[0].ParentTaskID != "tool-A" {
		t.Errorf("procs[0].ParentTaskID = %q, want %q", procs[0].ParentTaskID, "tool-A")
	}
	// unmatched1 linked positionally to the remaining task.
	if procs[1].ParentTaskID != "tool-B" {
		t.Errorf("procs[1].ParentTaskID = %q, want %q", procs[1].ParentTaskID, "tool-B")
	}
}
