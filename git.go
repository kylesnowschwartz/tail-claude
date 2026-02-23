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
