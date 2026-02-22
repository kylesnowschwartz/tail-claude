package main

import "os/exec"

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
