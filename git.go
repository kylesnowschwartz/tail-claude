package main

import (
	"os/exec"
	"strings"
)

// checkGitBranch returns the current branch name for the repo rooted at cwd.
// Returns an empty string on any error (not a git repo, git not found, etc.).
func checkGitBranch(cwd string) string {
	if cwd == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// discoverWorktreeDirs returns absolute paths of all git worktrees for the
// repo at cwd. Returns nil on any error (not a git repo, git not found, etc.).
func discoverWorktreeDirs(cwd string) []string {
	if cwd == "" {
		return nil
	}
	out, err := exec.Command("git", "-C", cwd, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil
	}
	return parseWorktreePaths(string(out))
}

// parseWorktreePaths extracts worktree paths from `git worktree list --porcelain`
// output. Each worktree block starts with "worktree <path>".
func parseWorktreePaths(output string) []string {
	var paths []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, strings.TrimPrefix(line, "worktree "))
		}
	}
	return paths
}

// checkGitDirty runs `git -C cwd status --porcelain` and returns true when
// the working tree has uncommitted changes. Returns false on any error (no
// cwd, git not on PATH, not a git repo, etc.) — callers treat unknown as clean.
func checkGitDirty(cwd string) bool {
	if cwd == "" {
		return false
	}
	out, err := exec.Command("git", "-C", cwd, "status", "--porcelain").Output()
	if err != nil {
		return false
	}
	return len(out) > 0
}
