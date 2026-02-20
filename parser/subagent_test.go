package parser_test

import (
	"os"
	"path/filepath"
	"testing"

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
