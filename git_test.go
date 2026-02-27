package main

import (
	"testing"
)

func TestParseWorktreePaths(t *testing.T) {
	// Typical `git worktree list --porcelain` output.
	output := `worktree /Users/kyle/Code/my-project
HEAD abc123def456
branch refs/heads/main

worktree /Users/kyle/Code/my-project/.claude/worktrees/feat-wt
HEAD 789abc012345
branch refs/heads/feat-wt

`
	got := parseWorktreePaths(output)
	want := []string{
		"/Users/kyle/Code/my-project",
		"/Users/kyle/Code/my-project/.claude/worktrees/feat-wt",
	}

	if len(got) != len(want) {
		t.Fatalf("parseWorktreePaths returned %d paths, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseWorktreePaths_Empty(t *testing.T) {
	got := parseWorktreePaths("")
	if len(got) != 0 {
		t.Errorf("parseWorktreePaths(\"\") returned %d paths, want 0", len(got))
	}
}
