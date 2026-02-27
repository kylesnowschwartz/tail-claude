package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSessionMetadata_OngoingToolUse(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "ongoing_tooluse.jsonl"))
	if !meta.isOngoing {
		t.Error("expected isOngoing=true for session ending with tool_use (no result)")
	}
}

func TestScanSessionMetadata_OngoingToolResult(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "ongoing_toolresult.jsonl"))
	if !meta.isOngoing {
		t.Error("expected isOngoing=true for session ending with tool_result (no text output)")
	}
}

func TestScanSessionMetadata_NotOngoingText(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_text.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session ending with text output")
	}
}

func TestScanSessionMetadata_NotOngoingExitPlan(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_exitplan.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session ending with ExitPlanMode")
	}
}

func TestScanSessionMetadata_NotOngoingShutdown(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_shutdown.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session ending with shutdown_response")
	}
}

func TestScanSessionMetadata_NotOngoingRejected(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_rejected.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session with rejected tool use")
	}
}

func TestScanSessionMetadata_NotOngoingInterrupted(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_interrupted.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session ending with user interruption")
	}
}

func TestScanSessionMetadata_MultiTurn(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "multi_turn.jsonl"))

	// 3 user messages + 3 first-AI-after-user = 6 turns.
	// a3 is a continuation after tool_result, not a new turn (awaitingAIGroup already false after a2).
	if meta.turnCount != 6 {
		t.Errorf("turnCount = %d, want 6", meta.turnCount)
	}

	// Tokens: sum of all assistant usage fields (non-sidechain, non-synthetic).
	// a1: 100+50+10+5 = 165
	// a2: 200+80+0+0 = 280
	// a3: 300+100+0+0 = 400
	// a4: 400+120+20+0 = 540
	// Total: 1385
	wantTokens := 165 + 280 + 400 + 540
	if meta.totalTokens != wantTokens {
		t.Errorf("totalTokens = %d, want %d", meta.totalTokens, wantTokens)
	}

	// Duration: last timestamp - first timestamp.
	// u1: 10:00:00, a4: 10:02:30 -> 150 seconds = 150000 ms
	if meta.durationMs != 150000 {
		t.Errorf("durationMs = %d, want 150000", meta.durationMs)
	}

	// Model: from first real assistant entry.
	if meta.model != "claude-opus-4-6" {
		t.Errorf("model = %q, want %q", meta.model, "claude-opus-4-6")
	}

	// Preview: first user message.
	if meta.firstMsg != "First question" {
		t.Errorf("firstMsg = %q, want %q", meta.firstMsg, "First question")
	}

	// Ongoing: ends with text output -> not ongoing.
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session ending with text output")
	}
}

func TestScanSessionMetadata_ModelExtraction(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "ongoing_tooluse.jsonl"))
	if meta.model != "claude-sonnet-4-5-20250514" {
		t.Errorf("model = %q, want %q", meta.model, "claude-sonnet-4-5-20250514")
	}
}

func TestScanSessionMetadata_TokenAccumulation(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_text.jsonl"))
	// a1: 500+200+100+0 = 800
	if meta.totalTokens != 800 {
		t.Errorf("totalTokens = %d, want 800", meta.totalTokens)
	}
}

func TestScanSessionMetadata_Duration(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_text.jsonl"))
	// u1: 10:00:00, a1: 10:00:05 -> 5000 ms
	if meta.durationMs != 5000 {
		t.Errorf("durationMs = %d, want 5000", meta.durationMs)
	}
}

func TestScanSessionMetadata_OngoingPendingTask(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "ongoing_pending_task.jsonl"))
	if !meta.isOngoing {
		t.Error("expected isOngoing=true: taskB has no result, Agent B still running")
	}
}

func TestScanSessionMetadata_NotOngoingInterruptedPending(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_interrupted_pending.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false: user interrupted, pending task should be cleared")
	}
}

// --- ResolveGitRoot tests ---

func TestResolveGitRoot_NormalRepo(t *testing.T) {
	// Create a fake git repo with a .git directory.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	got := ResolveGitRoot(dir)
	if got != dir {
		t.Errorf("ResolveGitRoot(%q) = %q, want %q", dir, got, dir)
	}
}

func TestResolveGitRoot_Worktree(t *testing.T) {
	// Simulate a git worktree:
	// mainRepo/.git/ (directory)
	// mainRepo/.git/worktrees/my-wt/ (directory)
	// worktreeDir/.git (file) -> "gitdir: mainRepo/.git/worktrees/my-wt"
	mainRepo := t.TempDir()
	worktreeDir := t.TempDir()

	// Create main repo .git dir and worktrees subdir.
	gitDir := filepath.Join(mainRepo, ".git")
	os.Mkdir(gitDir, 0755)
	os.MkdirAll(filepath.Join(gitDir, "worktrees", "my-wt"), 0755)

	// Create worktree .git file.
	gitdirPath := filepath.Join(gitDir, "worktrees", "my-wt")
	os.WriteFile(
		filepath.Join(worktreeDir, ".git"),
		[]byte("gitdir: "+gitdirPath+"\n"),
		0644,
	)

	got := ResolveGitRoot(worktreeDir)
	if got != mainRepo {
		t.Errorf("ResolveGitRoot(%q) = %q, want %q (main repo)", worktreeDir, got, mainRepo)
	}
}

func TestResolveGitRoot_SubdirOfWorktree(t *testing.T) {
	// ResolveGitRoot should walk up from a subdirectory.
	mainRepo := t.TempDir()
	worktreeDir := t.TempDir()

	gitDir := filepath.Join(mainRepo, ".git")
	os.Mkdir(gitDir, 0755)
	os.MkdirAll(filepath.Join(gitDir, "worktrees", "wt"), 0755)

	os.WriteFile(
		filepath.Join(worktreeDir, ".git"),
		[]byte("gitdir: "+filepath.Join(gitDir, "worktrees", "wt")+"\n"),
		0644,
	)

	subdir := filepath.Join(worktreeDir, "src", "pkg")
	os.MkdirAll(subdir, 0755)

	got := ResolveGitRoot(subdir)
	if got != mainRepo {
		t.Errorf("ResolveGitRoot(%q) = %q, want %q", subdir, got, mainRepo)
	}
}

func TestResolveGitRoot_NoGit(t *testing.T) {
	// No .git anywhere -- should return the original path.
	dir := t.TempDir()
	subdir := filepath.Join(dir, "a", "b")
	os.MkdirAll(subdir, 0755)

	got := ResolveGitRoot(subdir)
	if got != subdir {
		t.Errorf("ResolveGitRoot(%q) = %q, want original path", subdir, got)
	}
}

func TestResolveGitRoot_RealWorktree(t *testing.T) {
	// Integration test: use the actual worktree we're running in.
	// This test only runs if we detect we're in the tail-claude worktree.
	wtGit := filepath.Join("..", "..", "..", ".git")
	data, err := os.ReadFile(wtGit)
	if err != nil {
		t.Skip("not running from a git worktree")
	}
	content := string(data)
	if len(content) == 0 || content[0] == ' ' {
		t.Skip("not a worktree .git file")
	}

	// If we get here, we're in a worktree. ResolveGitRoot should
	// resolve to the main repo, not the worktree dir.
	cwd, _ := os.Getwd()
	resolved := ResolveGitRoot(cwd)

	// The resolved path should NOT contain ".claude/worktrees".
	if filepath.Base(filepath.Dir(filepath.Dir(resolved))) == ".claude" {
		t.Errorf("ResolveGitRoot still points to worktree: %s", resolved)
	}
}
