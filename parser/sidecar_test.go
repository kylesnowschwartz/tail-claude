package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

// TestSidecarMetaLinksSubagent verifies Phase 0 linking: a subagent whose
// agent-{id}.meta.json names the spawning toolUseId links to the parent Task
// item even when no toolUseResult exists yet (async agents link at spawn
// time), and the sidecar's description survives enrichment.
func TestSidecarMetaLinksSubagent(t *testing.T) {
	projectDir := t.TempDir()

	parentJSONL := `{"type":"user","uuid":"u1","timestamp":"2026-07-01T10:00:00.000Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","timestamp":"2026-07-01T10:00:01.000Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"tool_use","id":"toolu_task1","name":"Agent","input":{"subagent_type":"Explore","description":"scan the repo","prompt":"look around"}}]}}
`
	parentPath := filepath.Join(projectDir, "parent.jsonl")
	if err := os.WriteFile(parentPath, []byte(parentJSONL), 0o644); err != nil {
		t.Fatal(err)
	}

	subagentsDir := filepath.Join(projectDir, "parent", "subagents")
	if err := os.MkdirAll(subagentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentJSONL := `{"type":"user","uuid":"su1","timestamp":"2026-07-01T10:00:02.000Z","isSidechain":true,"message":{"role":"user","content":"look around"}}
{"type":"assistant","uuid":"sa1","timestamp":"2026-07-01T10:00:03.000Z","isSidechain":true,"message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"text","text":"done"}]}}
`
	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-abc123.jsonl"), []byte(agentJSONL), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := `{"agentType":"Explore","description":"scan the repo (sidecar)","toolUseId":"toolu_task1","spawnDepth":1}`
	if err := os.WriteFile(filepath.Join(subagentsDir, "agent-abc123.meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	procs, err := parser.DiscoverSubagents(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 {
		t.Fatalf("len(procs) = %d, want 1", len(procs))
	}
	if procs[0].ParentTaskID != "toolu_task1" {
		t.Errorf("ParentTaskID = %q, want toolu_task1 (from sidecar)", procs[0].ParentTaskID)
	}
	if procs[0].SubagentType != "Explore" {
		t.Errorf("SubagentType = %q, want Explore", procs[0].SubagentType)
	}

	parentChunks, err := parser.ReadSession(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	parser.LinkSubagents(procs, parentChunks, parentPath)

	if procs[0].ParentTaskID != "toolu_task1" {
		t.Errorf("ParentTaskID after linking = %q, want toolu_task1", procs[0].ParentTaskID)
	}
	if procs[0].Description != "scan the repo (sidecar)" {
		t.Errorf("Description = %q, want sidecar value preserved", procs[0].Description)
	}
}

// TestReadTeamSessionMeta_LeadingMetadataEntries: Claude Code 2.1.19x
// re-appends uuid-less metadata records at the head of session files, so the
// team fields are no longer guaranteed to be on line 1.
func TestReadTeamSessionMeta_LeadingMetadataEntries(t *testing.T) {
	dir := t.TempDir()

	teamFile := filepath.Join(dir, "team.jsonl")
	teamJSONL := `{"type":"last-prompt","lastPrompt":"hi","leafUuid":"lf1","sessionId":"s1"}
{"type":"mode","mode":"normal","sessionId":"s1"}
{"type":"custom-title","customTitle":"my session","sessionId":"s1"}
{"type":"user","uuid":"u1","timestamp":"2026-07-01T10:00:00.000Z","teamName":"analysis","agentName":"planner","message":{"role":"user","content":"go"}}
`
	if err := os.WriteFile(teamFile, []byte(teamJSONL), 0o644); err != nil {
		t.Fatal(err)
	}
	teamName, agentName := parser.ReadTeamSessionMeta(teamFile)
	if teamName != "analysis" || agentName != "planner" {
		t.Errorf("got (%q, %q), want (analysis, planner)", teamName, agentName)
	}

	// Non-team session: first conversation entry stops the scan.
	plainFile := filepath.Join(dir, "plain.jsonl")
	plainJSONL := `{"type":"last-prompt","lastPrompt":"hi","leafUuid":"lf1","sessionId":"s1"}
{"type":"user","uuid":"u1","timestamp":"2026-07-01T10:00:00.000Z","message":{"role":"user","content":"go"}}
{"type":"user","uuid":"u2","timestamp":"2026-07-01T10:00:01.000Z","teamName":"late","agentName":"late","message":{"role":"user","content":"never scanned"}}
`
	if err := os.WriteFile(plainFile, []byte(plainJSONL), 0o644); err != nil {
		t.Fatal(err)
	}
	teamName, agentName = parser.ReadTeamSessionMeta(plainFile)
	if teamName != "" || agentName != "" {
		t.Errorf("got (%q, %q), want empty for non-team session", teamName, agentName)
	}
}
