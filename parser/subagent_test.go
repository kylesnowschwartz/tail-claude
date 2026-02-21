package parser_test

import (
	"encoding/json"
	"fmt"
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

// makeTeamSubagent builds a SubagentProcess with AgentName and TeamName
// set for team matching. Simulates a team member session file.
func makeTeamSubagent(id string, startTime time.Time, agentName, teamName string) parser.SubagentProcess {
	return parser.SubagentProcess{
		ID:        id,
		StartTime: startTime,
		AgentName: agentName,
		TeamName:  teamName,
		Chunks: []parser.Chunk{
			{Type: parser.UserChunk, Timestamp: startTime, UserText: `<teammate-message teammate_id="leader" color="#ff0000">Do the thing</teammate-message>`},
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
	// Team subagent matched by AgentName+TeamName to the Task call's name+team_name.
	procs := []parser.SubagentProcess{
		makeTeamSubagent("team-agent-1", time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), "implementer", "my-project"),
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
	// Two team subagents, each with different agentName. Must match to the
	// correct Task call by agentName+teamName, not by position.
	procs := []parser.SubagentProcess{
		makeTeamSubagent("agent-b", time.Date(2025, 6, 15, 10, 1, 0, 0, time.UTC), "tester", "proj"),
		makeTeamSubagent("agent-a", time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), "fixer", "proj"),
	}
	chunks := []parser.Chunk{
		makeTeamTaskChunk("tool-fix", "general-purpose", "Fix the bug", "proj", "fixer"),
		makeTeamTaskChunk("tool-test", "sc-test-runner", "Write tests", "proj", "tester"),
	}

	parentPath := writeParentSession(t, []string{
		`{"uuid":"x","type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`,
	})

	parser.LinkSubagents(procs, chunks, parentPath)

	// agent-a (agentName "fixer") -> tool-fix
	if procs[1].ParentTaskID != "tool-fix" {
		t.Errorf("agent-a.ParentTaskID = %q, want %q", procs[1].ParentTaskID, "tool-fix")
	}
	// agent-b (agentName "tester") -> tool-test
	if procs[0].ParentTaskID != "tool-test" {
		t.Errorf("agent-b.ParentTaskID = %q, want %q", procs[0].ParentTaskID, "tool-test")
	}
}

func TestLinkSubagents_TeamNoNameFallsThrough(t *testing.T) {
	// Subagent without AgentName/TeamName won't match via Phase 2.
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
	// Phase 2: team subagent matched by agentName+teamName.
	// Phase 3: positional fallback for remaining non-team task.
	procs := []parser.SubagentProcess{
		{ID: "regular-1", StartTime: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)},
		makeTeamSubagent("team-1", time.Date(2025, 6, 15, 10, 1, 0, 0, time.UTC), "researcher", "proj"),
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
	// Phase 2: team-1 -> tool-team (matched by agentName+teamName)
	if procs[1].ParentTaskID != "tool-team" {
		t.Errorf("team-1.ParentTaskID = %q, want %q", procs[1].ParentTaskID, "tool-team")
	}
	// Phase 3: orphan-1 -> tool-orphan (positional, team task excluded)
	if procs[2].ParentTaskID != "tool-orphan" {
		t.Errorf("orphan-1.ParentTaskID = %q, want %q", procs[2].ParentTaskID, "tool-orphan")
	}
}

func TestLinkSubagents_TeamEarliestWins(t *testing.T) {
	// Two subagents with the same agentName+teamName. Earliest by StartTime wins.
	procs := []parser.SubagentProcess{
		makeTeamSubagent("late", time.Date(2025, 6, 15, 10, 5, 0, 0, time.UTC), "worker", "proj"),
		makeTeamSubagent("early", time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC), "worker", "proj"),
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

// --- DiscoverTeamSessions tests ---

// writeTeamSession creates a JSONL file with agentName and teamName on entries.
func writeTeamSession(t *testing.T, dir, filename, agentName, teamName string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	// Two entries: user + assistant, both with agentName/teamName.
	lines := []string{
		fmt.Sprintf(`{"uuid":"u1","type":"user","timestamp":"2025-06-15T10:00:00Z","agentName":%q,"teamName":%q,"message":{"role":"user","content":"hello"}}`, agentName, teamName),
		fmt.Sprintf(`{"uuid":"a1","type":"assistant","timestamp":"2025-06-15T10:00:05Z","agentName":%q,"teamName":%q,"message":{"role":"assistant","model":"claude-haiku-4-5","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":10,"output_tokens":5}}}`, agentName, teamName),
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverTeamSessions_FindsMembers(t *testing.T) {
	dir := t.TempDir()

	// Parent session file.
	parentPath := filepath.Join(dir, "parent-session.jsonl")
	if err := os.WriteFile(parentPath, []byte(`{"uuid":"p1","type":"user","timestamp":"2025-06-15T09:00:00Z","message":{"role":"user","content":"start"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Team member session files.
	writeTeamSession(t, dir, "team-member-1.jsonl", "researcher", "my-team")
	writeTeamSession(t, dir, "team-member-2.jsonl", "implementer", "my-team")

	// Unrelated session (no agentName/teamName) — should be skipped.
	unrelated := filepath.Join(dir, "unrelated.jsonl")
	if err := os.WriteFile(unrelated, []byte(`{"uuid":"x","type":"user","timestamp":"2025-06-15T09:00:00Z","message":{"role":"user","content":"hi"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Parent chunks with team Task calls.
	chunks := []parser.Chunk{
		makeTeamTaskChunk("tool-r", "Explore", "Research stuff", "my-team", "researcher"),
		makeTeamTaskChunk("tool-i", "general-purpose", "Implement feature", "my-team", "implementer"),
	}

	procs, err := parser.DiscoverTeamSessions(parentPath, chunks)
	if err != nil {
		t.Fatalf("DiscoverTeamSessions error: %v", err)
	}

	if len(procs) != 2 {
		t.Fatalf("got %d team sessions, want 2", len(procs))
	}

	// Both should have AgentName and TeamName set.
	for _, p := range procs {
		if p.AgentName == "" || p.TeamName == "" {
			t.Errorf("proc %s: AgentName=%q, TeamName=%q — both should be set", p.ID, p.AgentName, p.TeamName)
		}
		if p.TeamName != "my-team" {
			t.Errorf("proc %s: TeamName=%q, want %q", p.ID, p.TeamName, "my-team")
		}
		if len(p.Chunks) == 0 {
			t.Errorf("proc %s: has 0 chunks", p.ID)
		}
	}
}

func TestDiscoverTeamSessions_SkipsParentAndAgentFiles(t *testing.T) {
	dir := t.TempDir()

	// Parent session.
	parentPath := filepath.Join(dir, "parent.jsonl")
	if err := os.WriteFile(parentPath, []byte(`{"uuid":"p1","type":"user","timestamp":"2025-06-15T09:00:00Z","agentName":"","teamName":"my-team","message":{"role":"user","content":"start"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// An agent-* file (regular subagent) — should be skipped.
	agentPath := filepath.Join(dir, "agent-abc123.jsonl")
	if err := os.WriteFile(agentPath, []byte(`{"uuid":"a1","type":"user","timestamp":"2025-06-15T10:00:00Z","agentName":"worker","teamName":"my-team","message":{"role":"user","content":"hi"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	chunks := []parser.Chunk{
		makeTeamTaskChunk("tool-1", "general-purpose", "Do work", "my-team", "worker"),
	}

	procs, err := parser.DiscoverTeamSessions(parentPath, chunks)
	if err != nil {
		t.Fatalf("DiscoverTeamSessions error: %v", err)
	}

	// Should find nothing — parent is skipped, agent-* is skipped.
	if len(procs) != 0 {
		t.Errorf("got %d team sessions, want 0", len(procs))
	}
}

func TestDiscoverTeamSessions_NoTeamTasks(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.jsonl")
	if err := os.WriteFile(parentPath, []byte(`{"uuid":"p1","type":"user","timestamp":"2025-06-15T09:00:00Z","message":{"role":"user","content":"start"}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// No team task chunks — should return nil immediately.
	chunks := []parser.Chunk{
		makeTaskChunk("tool-1", "Explore", "Search code"),
	}

	procs, err := parser.DiscoverTeamSessions(parentPath, chunks)
	if err != nil {
		t.Fatalf("DiscoverTeamSessions error: %v", err)
	}
	if len(procs) != 0 {
		t.Errorf("got %d team sessions, want 0", len(procs))
	}
}
