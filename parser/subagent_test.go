package parser_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// writeParentSession creates a temp JSONL file with tool result entries
// containing structured toolUseResult and sourceToolUseID fields.
func writeParentSession(t *testing.T, entries []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "parent.jsonl")
	content := strings.Join(entries, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// makeTaskChunk builds an AI chunk with a Task tool call DisplayItem.
func makeTaskChunk(toolID, subagentType, desc string) parser.Chunk {
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
				SubagentType: subagentType,
				SubagentDesc: desc,
			},
		},
	}
}

// makeTeamTaskChunk builds an AI chunk with a team Task call (has team_name + name).
func makeTeamTaskChunk(toolID, subagentType, desc, teamName, memberName string) parser.Chunk {
	input, _ := json.Marshal(map[string]string{
		"subagent_type": subagentType,
		"description":   desc,
		"team_name":     teamName,
		"name":          memberName,
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
				SubagentType: subagentType,
				SubagentDesc: desc,
			},
		},
	}
}

// makeTeamSubagentWithSummary builds a SubagentProcess whose first UserChunk
// contains a <teammate-message summary="..."> tag, matching real JSONL format.
// Phase 2 matches by comparing this summary to the Task call's SubagentDesc.
func makeTeamSubagentWithSummary(id string, startTime time.Time, summary string) parser.SubagentProcess {
	return parser.SubagentProcess{
		ID:          id,
		StartTime:   startTime,
		TeamSummary: summary,
		Chunks: []parser.Chunk{
			{Type: parser.AIChunk, Timestamp: startTime, Model: "claude-haiku-4-5"},
			{Type: parser.AIChunk, Timestamp: startTime.Add(time.Second), Model: "claude-haiku-4-5"},
		},
	}
}

func TestLinkSubagents_ResultBased(t *testing.T) {
	procs := []parser.SubagentProcess{
		{ID: "abc1234", StartTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)},
		{ID: "def5678", StartTime: time.Date(2025, 6, 15, 10, 1, 0, 0, time.UTC)},
	}
	chunks := []parser.Chunk{
		makeTaskChunk("tool-1", "Explore", "Search codebase"),
		makeTaskChunk("tool-2", "Plan", "Design architecture"),
	}

	// Parent session with structured toolUseResult entries.
	parentPath := writeParentSession(t, []string{
		`{"uuid":"r1","type":"user","timestamp":"2025-06-15T10:00:05Z","isMeta":true,"sourceToolUseID":"tool-1","toolUseResult":{"agentId":"abc1234","status":"completed"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"Done."}]}}`,
		`{"uuid":"r2","type":"user","timestamp":"2025-06-15T10:01:10Z","isMeta":true,"sourceToolUseID":"tool-2","toolUseResult":{"agentId":"def5678","status":"completed"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-2","content":"Done."}]}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

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

func TestLinkSubagents_SnakeCaseAgentID(t *testing.T) {
	// claude-devtools checks both agentId and agent_id (snake_case for team spawns).
	procs := []parser.SubagentProcess{
		{ID: "team-abc", StartTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)},
	}
	chunks := []parser.Chunk{
		makeTaskChunk("tool-team", "general-purpose", "Team work"),
	}

	parentPath := writeParentSession(t, []string{
		`{"uuid":"r1","type":"user","timestamp":"2025-06-15T10:00:05Z","isMeta":true,"sourceToolUseID":"tool-team","toolUseResult":{"agent_id":"team-abc","status":"teammate_spawned"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-team","content":"Spawned."}]}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

	if procs[0].ParentTaskID != "tool-team" {
		t.Errorf("procs[0].ParentTaskID = %q, want %q", procs[0].ParentTaskID, "tool-team")
	}
}

func TestLinkSubagents_PositionalFallback(t *testing.T) {
	procs := []parser.SubagentProcess{
		{ID: "nomatch1", StartTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)},
	}

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
					SubagentType: "general-purpose",
					SubagentDesc: "Fix the bug",
				},
			},
		},
	}

	// Empty parent session -- no structured links, falls back to positional.
	parentPath := writeParentSession(t, []string{
		`{"uuid":"x","type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

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
	parser.LinkSubagents(procs, nil, "")

	if procs[0].ParentTaskID != "" {
		t.Errorf("unmatched procs[0].ParentTaskID = %q, want empty", procs[0].ParentTaskID)
	}
	if procs[0].Description != "" {
		t.Errorf("unmatched procs[0].Description = %q, want empty", procs[0].Description)
	}
}

func TestLinkSubagents_NoProcesses(t *testing.T) {
	chunks := []parser.Chunk{
		makeTaskChunk("tool-1", "Explore", "Search"),
	}
	// Should not panic with empty processes.
	parser.LinkSubagents(nil, chunks, "")
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
					SubagentType: "Explore",
					SubagentDesc: "Find files",
				},
				{
					Type:         parser.ItemSubagent,
					ToolName:     "Task",
					ToolID:       "tool-B",
					ToolInput:    json.RawMessage(input2),
					SubagentType: "Plan",
					SubagentDesc: "Plan work",
				},
			},
		},
	}

	// Parent session only has a structured link for matched1, not unmatched1.
	parentPath := writeParentSession(t, []string{
		`{"uuid":"r1","type":"user","timestamp":"2025-06-15T10:00:05Z","isMeta":true,"sourceToolUseID":"tool-A","toolUseResult":{"agentId":"matched1","status":"completed"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-A","content":"Done."}]}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

	// matched1 linked by structured agentId.
	if procs[0].ParentTaskID != "tool-A" {
		t.Errorf("procs[0].ParentTaskID = %q, want %q", procs[0].ParentTaskID, "tool-A")
	}
	// unmatched1 linked positionally to the remaining task.
	if procs[1].ParentTaskID != "tool-B" {
		t.Errorf("procs[1].ParentTaskID = %q, want %q", procs[1].ParentTaskID, "tool-B")
	}
}

// --- Phase 2: Team member linking tests ---

func TestLinkSubagents_TeamMatching(t *testing.T) {
	// Team subagent matched by <teammate-message summary="..."> to Task call's description.
	procs := []parser.SubagentProcess{
		makeTeamSubagentWithSummary("team-agent-1", time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), "Implement feature"),
	}
	chunks := []parser.Chunk{
		makeTeamTaskChunk("tool-t1", "general-purpose", "Implement feature", "my-project", "implementer"),
	}

	parentPath := writeParentSession(t, []string{
		`{"uuid":"x","type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

	if procs[0].ParentTaskID != "tool-t1" {
		t.Errorf("ParentTaskID = %q, want %q", procs[0].ParentTaskID, "tool-t1")
	}
	if procs[0].Description != "Implement feature" {
		t.Errorf("Description = %q, want %q", procs[0].Description, "Implement feature")
	}
	if procs[0].SubagentType != "general-purpose" {
		t.Errorf("SubagentType = %q, want %q", procs[0].SubagentType, "general-purpose")
	}
}

func TestLinkSubagents_TeamMatchingMultiple(t *testing.T) {
	// Two team subagents with different summaries. Must match to the
	// correct Task call by summary content, not by position.
	procs := []parser.SubagentProcess{
		makeTeamSubagentWithSummary("agent-b", time.Date(2025, 6, 15, 10, 1, 0, 0, time.UTC), "Write tests"),
		makeTeamSubagentWithSummary("agent-a", time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), "Fix the bug"),
	}
	chunks := []parser.Chunk{
		makeTeamTaskChunk("tool-fix", "general-purpose", "Fix the bug", "proj", "fixer"),
		makeTeamTaskChunk("tool-test", "sc-test-runner", "Write tests", "proj", "tester"),
	}

	parentPath := writeParentSession(t, []string{
		`{"uuid":"x","type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

	// agent-a (summary "Fix the bug") -> tool-fix
	if procs[1].ParentTaskID != "tool-fix" {
		t.Errorf("agent-a.ParentTaskID = %q, want %q", procs[1].ParentTaskID, "tool-fix")
	}
	// agent-b (summary "Write tests") -> tool-test
	if procs[0].ParentTaskID != "tool-test" {
		t.Errorf("agent-b.ParentTaskID = %q, want %q", procs[0].ParentTaskID, "tool-test")
	}
}

func TestLinkSubagents_TeamNoSummaryFallsThrough(t *testing.T) {
	// Subagent without <teammate-message> won't match via Phase 2.
	// Since the only Task call is a team task, it won't match positionally either
	// (Phase 3 excludes team tasks).
	procs := []parser.SubagentProcess{
		{
			ID:        "no-team-info",
			StartTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
			Chunks: []parser.Chunk{
				{Type: parser.UserChunk, UserText: "Just a regular user message"},
			},
		},
	}
	chunks := []parser.Chunk{
		makeTeamTaskChunk("tool-t1", "general-purpose", "Do work", "proj", "worker"),
	}

	parentPath := writeParentSession(t, []string{
		`{"uuid":"x","type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

	// Should remain unmatched — team tasks excluded from positional fallback.
	if procs[0].ParentTaskID != "" {
		t.Errorf("ParentTaskID = %q, want empty (no match expected)", procs[0].ParentTaskID)
	}
}

func TestLinkSubagents_AllThreePhases(t *testing.T) {
	// Phase 1: regular subagent matched by agentId.
	// Phase 2: team subagent matched by summary -> description.
	// Phase 3: positional fallback for remaining non-team task.
	procs := []parser.SubagentProcess{
		{ID: "regular-1", StartTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)},
		makeTeamSubagentWithSummary("team-1", time.Date(2025, 6, 15, 10, 1, 0, 0, time.UTC), "Research topic"),
		{ID: "orphan-1", StartTime: time.Date(2025, 6, 15, 10, 2, 0, 0, time.UTC)},
	}

	chunks := []parser.Chunk{
		makeTaskChunk("tool-regular", "Explore", "Search code"),
		makeTeamTaskChunk("tool-team", "general-purpose", "Research topic", "proj", "researcher"),
		makeTaskChunk("tool-orphan", "Plan", "Design thing"),
	}

	// Parent session links regular-1 via agentId.
	parentPath := writeParentSession(t, []string{
		`{"uuid":"r1","type":"user","timestamp":"2025-06-15T10:00:05Z","isMeta":true,"sourceToolUseID":"tool-regular","toolUseResult":{"agentId":"regular-1","status":"completed"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-regular","content":"Done."}]}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

	// Phase 1: regular-1 -> tool-regular
	if procs[0].ParentTaskID != "tool-regular" {
		t.Errorf("regular-1.ParentTaskID = %q, want %q", procs[0].ParentTaskID, "tool-regular")
	}
	// Phase 2: team-1 -> tool-team (matched by summary)
	if procs[1].ParentTaskID != "tool-team" {
		t.Errorf("team-1.ParentTaskID = %q, want %q", procs[1].ParentTaskID, "tool-team")
	}
	// Phase 3: orphan-1 -> tool-orphan (positional, team task excluded)
	if procs[2].ParentTaskID != "tool-orphan" {
		t.Errorf("orphan-1.ParentTaskID = %q, want %q", procs[2].ParentTaskID, "tool-orphan")
	}
}

func TestLinkSubagents_TeamEarliestWins(t *testing.T) {
	// Two subagents with the same summary. Earliest by StartTime wins.
	procs := []parser.SubagentProcess{
		makeTeamSubagentWithSummary("late", time.Date(2025, 6, 15, 10, 5, 0, 0, time.UTC), "Do work"),
		makeTeamSubagentWithSummary("early", time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), "Do work"),
	}
	chunks := []parser.Chunk{
		makeTeamTaskChunk("tool-1", "general-purpose", "Do work", "proj", "worker"),
	}

	parentPath := writeParentSession(t, []string{
		`{"uuid":"x","type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

	// "early" (10:00) should match, "late" (10:05) should not.
	if procs[1].ParentTaskID != "tool-1" {
		t.Errorf("early.ParentTaskID = %q, want %q", procs[1].ParentTaskID, "tool-1")
	}
	if procs[0].ParentTaskID != "" {
		t.Errorf("late.ParentTaskID = %q, want empty", procs[0].ParentTaskID)
	}
}

func TestLinkSubagents_TeamContinuationFile(t *testing.T) {
	// Continuation files have no summary attribute in their teammate-message tag.
	// readSubagentSession sets TeamSummary="" for these. They should NOT match.
	procs := []parser.SubagentProcess{
		{
			ID:          "continuation-1",
			StartTime:   time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
			TeamSummary: "", // continuation — no summary attribute
			Chunks: []parser.Chunk{
				{Type: parser.AIChunk, Model: "claude-haiku-4-5"},
			},
		},
	}
	chunks := []parser.Chunk{
		makeTeamTaskChunk("tool-t1", "general-purpose", "Do work", "proj", "worker"),
	}

	parentPath := writeParentSession(t, []string{
		`{"uuid":"x","type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

	// Should remain unmatched -- no summary to compare.
	if procs[0].ParentTaskID != "" {
		t.Errorf("continuation.ParentTaskID = %q, want empty", procs[0].ParentTaskID)
	}
}

// --- Integration test: full pipeline from JSONL fixtures ---

func TestTeamLinkingIntegration(t *testing.T) {
	// Exercises the full pipeline from JSONL on disk through
	// DiscoverSubagents -> ReadSession -> BuildChunks -> LinkSubagents.
	//
	// The fixture has 3 team Task calls in the parent with team-style
	// agent_ids ("name@team") that can't match by UUID, forcing Phase 2
	// (summary matching). A 4th continuation file has no summary attribute
	// and must NOT match.
	//
	// This test would have failed with the original bug where
	// ExtractTeamMessageSummary operated on chunk text (post-Classify)
	// instead of raw entry content (pre-Classify).
	parentPath := filepath.Join("testdata", "team-parent.jsonl")

	// Step 1: Discover subagents — exercises readSubagentSession which
	// extracts TeamSummary from raw entry content before Classify strips it.
	procs, err := parser.DiscoverSubagents(parentPath)
	if err != nil {
		t.Fatalf("DiscoverSubagents: %v", err)
	}

	// 4 agent files, all valid (no warmup/compact/empty).
	if len(procs) != 4 {
		t.Fatalf("got %d subagents, want 4", len(procs))
	}

	// Verify team summaries were extracted from raw content.
	summaryByID := make(map[string]string, len(procs))
	for _, p := range procs {
		summaryByID[p.ID] = p.TeamSummary
	}
	wantSummaries := map[string]string{
		"team-impl-001":      "Implement auth module",
		"team-test-002":      "Write integration tests",
		"team-research-003":  "Research API docs",
		"team-impl-001-cont": "", // continuation — no summary attribute
	}
	for id, want := range wantSummaries {
		got := summaryByID[id]
		if got != want {
			t.Errorf("TeamSummary[%s] = %q, want %q", id, got, want)
		}
	}

	// Verify team colors were extracted from raw content.
	colorByID := make(map[string]string, len(procs))
	for _, p := range procs {
		colorByID[p.ID] = p.TeamColor
	}
	wantColors := map[string]string{
		"team-impl-001":      "green",
		"team-test-002":      "yellow",
		"team-research-003":  "purple",
		"team-impl-001-cont": "", // continuation — no color attribute
	}
	for id, want := range wantColors {
		got := colorByID[id]
		if got != want {
			t.Errorf("TeamColor[%s] = %q, want %q", id, got, want)
		}
	}

	// Step 2: Parse the parent session through the full pipeline.
	parentChunks, err := parser.ReadSession(parentPath)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}

	// Parent should have: 1 UserChunk + 3 AIChunks (each with a Task tool_use + tool_result).
	// Count Task items across chunks.
	var taskCount int
	for _, c := range parentChunks {
		for _, it := range c.Items {
			if it.Type == parser.ItemSubagent {
				taskCount++
			}
		}
	}
	if taskCount != 3 {
		t.Fatalf("got %d Task items in parent chunks, want 3", taskCount)
	}

	// Step 3: Link subagents to parent Task calls.
	parser.LinkSubagents(procs, parentChunks, parentPath)

	// Build lookup for assertions.
	procByID := make(map[string]parser.SubagentProcess, len(procs))
	for _, p := range procs {
		procByID[p.ID] = p
	}

	// Phase 1 should fail for all — agent_ids are "name@team", not file UUIDs.
	// Phase 2 should match the 3 agents with summaries.
	tests := []struct {
		id         string
		wantTaskID string
		wantDesc   string
		wantType   string
		wantLinked bool
	}{
		{"team-impl-001", "task-1", "Implement auth module", "general-purpose", true},
		{"team-test-002", "task-2", "Write integration tests", "sc-test-runner", true},
		{"team-research-003", "task-3", "Research API docs", "Explore", true},
		{"team-impl-001-cont", "", "", "", false}, // continuation: no summary, no match
	}

	for _, tt := range tests {
		p := procByID[tt.id]
		if tt.wantLinked {
			if p.ParentTaskID != tt.wantTaskID {
				t.Errorf("%s: ParentTaskID = %q, want %q", tt.id, p.ParentTaskID, tt.wantTaskID)
			}
			if p.Description != tt.wantDesc {
				t.Errorf("%s: Description = %q, want %q", tt.id, p.Description, tt.wantDesc)
			}
			if p.SubagentType != tt.wantType {
				t.Errorf("%s: SubagentType = %q, want %q", tt.id, p.SubagentType, tt.wantType)
			}
		} else {
			if p.ParentTaskID != "" {
				t.Errorf("%s: ParentTaskID = %q, want empty (should not match)", tt.id, p.ParentTaskID)
			}
		}
	}
}

func TestLinkSubagents_TeamAndRegularMixed(t *testing.T) {
	// One regular subagent (Phase 1) + one team agent (Phase 2) in same session.
	procs := []parser.SubagentProcess{
		{ID: "regular-1", StartTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)},
		makeTeamSubagentWithSummary("team-1", time.Date(2025, 6, 15, 10, 1, 0, 0, time.UTC), "Research docs"),
	}

	chunks := []parser.Chunk{
		makeTaskChunk("tool-regular", "Explore", "Search code"),
		makeTeamTaskChunk("tool-team", "general-purpose", "Research docs", "proj", "researcher"),
	}

	parentPath := writeParentSession(t, []string{
		`{"uuid":"r1","type":"user","timestamp":"2025-06-15T10:00:05Z","isMeta":true,"sourceToolUseID":"tool-regular","toolUseResult":{"agentId":"regular-1","status":"completed"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-regular","content":"Done."}]}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

	// Phase 1: regular subagent linked by agentId.
	if procs[0].ParentTaskID != "tool-regular" {
		t.Errorf("regular-1.ParentTaskID = %q, want %q", procs[0].ParentTaskID, "tool-regular")
	}
	// Phase 2: team subagent linked by summary match.
	if procs[1].ParentTaskID != "tool-team" {
		t.Errorf("team-1.ParentTaskID = %q, want %q", procs[1].ParentTaskID, "tool-team")
	}
}
