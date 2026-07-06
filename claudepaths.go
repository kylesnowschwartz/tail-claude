package main

import (
	"os"

	"github.com/kylesnowschwartz/agent-ouija/claude/claudedir"
	"github.com/kylesnowschwartz/agent-ouija/claude/registry"
	"github.com/kylesnowschwartz/agent-ouija/gitroot"
)

// claudeRoot is the Claude Code state directory (~/.claude), resolved once
// at startup. Empty when the home directory cannot be determined; the
// helpers below degrade the same way the old implicit-home parser
// functions did.
var claudeRoot, claudeRootErr = claudedir.DefaultRoot()

// currentProjectDir returns the Claude projects directory for the current
// working directory. If the CWD is inside a git worktree, resolves to the
// main working tree root so we find sessions stored under the original
// project path.
func currentProjectDir() (string, error) {
	if claudeRootErr != nil {
		return "", claudeRootErr
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return claudeRoot.ProjectDirFor(gitroot.ResolveGitRoot(cwd)), nil
}

// projectDirForPath returns the Claude projects directory for an absolute
// path, resolving symlinks to match Claude Code's on-disk encoding.
func projectDirForPath(absPath string) (string, error) {
	if claudeRootErr != nil {
		return "", claudeRootErr
	}
	return claudeRoot.ProjectDirFor(absPath), nil
}

// listAllProjectDirs returns every Claude Code project directory.
func listAllProjectDirs() ([]string, error) {
	if claudeRootErr != nil {
		return nil, claudeRootErr
	}
	return claudeRoot.ListProjectDirs()
}

// readSessionRegistry returns raw live-session registry entries, or nil
// when the Claude root is unavailable. Feed the result to
// claude.NewNameResolver / claude.FindNameMatches, which own the
// per-session arbitration (including lingering entries of exited
// sessions -- do not liveness-filter here).
func readSessionRegistry() []registry.Live {
	if claudeRootErr != nil {
		return nil
	}
	return registry.Read(claudeRoot.SessionsDir())
}

// debugLogPathFor returns the debug log path for a session JSONL path, or
// "" when the debug file doesn't exist.
func debugLogPathFor(sessionPath string) string {
	if claudeRootErr != nil {
		return ""
	}
	return claudeRoot.DebugLogPath(sessionPath)
}
