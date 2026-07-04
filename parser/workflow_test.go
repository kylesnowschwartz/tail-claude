package parser_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestScanWorkflowActivity(t *testing.T) {
	projectDir := t.TempDir()
	sessionPath := filepath.Join(projectDir, "sess.jsonl")

	// No companion dir at all: zero value.
	act := parser.ScanWorkflowActivity(sessionPath)
	if act.Runs != 0 || act.Agents != 0 || !act.LastWrite.IsZero() {
		t.Errorf("expected zero activity for missing dir, got %+v", act)
	}
	if act.Active(time.Hour) {
		t.Error("zero activity must not be Active")
	}

	// Two runs: one with two agents + journal, one with a single agent.
	run1 := filepath.Join(projectDir, "sess", "subagents", "workflows", "wf_abc123")
	run2 := filepath.Join(projectDir, "sess", "subagents", "workflows", "wf_def456")
	for _, d := range []string{run1, run2} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		filepath.Join(run1, "agent-a1.jsonl"),
		filepath.Join(run1, "agent-a2.jsonl"),
		filepath.Join(run1, "journal.jsonl"),
		filepath.Join(run2, "agent-b1.jsonl"),
	} {
		if err := os.WriteFile(f, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Non-workflow dirs are ignored.
	if err := os.MkdirAll(filepath.Join(projectDir, "sess", "subagents", "workflows", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}

	act = parser.ScanWorkflowActivity(sessionPath)
	if act.Runs != 2 {
		t.Errorf("Runs = %d, want 2", act.Runs)
	}
	if act.Agents != 3 {
		t.Errorf("Agents = %d, want 3 (journal.jsonl is not an agent)", act.Agents)
	}
	if !act.Active(time.Minute) {
		t.Error("freshly written files should be Active within a minute")
	}

	// Stale files are not Active.
	old := time.Now().Add(-time.Hour)
	for _, f := range []string{
		filepath.Join(run1, "agent-a1.jsonl"),
		filepath.Join(run1, "agent-a2.jsonl"),
		filepath.Join(run1, "journal.jsonl"),
		filepath.Join(run2, "agent-b1.jsonl"),
	} {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	act = parser.ScanWorkflowActivity(sessionPath)
	if act.Active(time.Minute) {
		t.Error("hour-old writes must not be Active within a minute")
	}
	if !act.Active(2 * time.Hour) {
		t.Error("hour-old writes should be Active within two hours")
	}
}
