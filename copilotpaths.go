package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kylesnowschwartz/agent-ouija/claude/discover"
	"github.com/kylesnowschwartz/agent-ouija/gitroot"

	"github.com/kylesnowschwartz/tail-claude/copilot"
)

// copilotRoot is the Copilot CLI session-state directory
// (~/.copilot/session-state), resolved once at startup. Empty when the home
// directory cannot be determined; the helpers below degrade to "no Copilot
// sessions" the same way claudepaths.go degrades.
var copilotRoot, copilotRootErr = copilot.DefaultRoot()

// copilotCache memoizes per-file session scans across picker refreshes,
// mirroring the package-level use of discover.SessionCache for Claude.
var copilotCache = copilot.NewCache()

// sessionSource identifies which CLI produced a session file, deciding which
// parsing pipeline (agent-ouija vs the local copilot package) owns it.
type sessionSource int

const (
	sourceClaude sessionSource = iota
	sourceCopilot
)

// sourceForPath classifies a session path by origin. Copilot sessions live
// under ~/.copilot/session-state (dir-per-session events.jsonl or flat
// {uuid}.jsonl); everything else is Claude. The events.jsonl basename check
// covers fixtures and explicit CLI paths outside the default root, but the
// basename alone is too weak a discriminator for arbitrary CLI paths (a
// Claude-format file happening to be named events.jsonl must load through
// the Claude pipeline), so it is confirmed by a first-line content sniff.
func sourceForPath(path string) sessionSource {
	if path == "" {
		return sourceClaude
	}
	if copilotRootErr == nil && strings.HasPrefix(path, copilotRoot+string(filepath.Separator)) {
		return sourceCopilot
	}
	if strings.Contains(filepath.ToSlash(path), "/.copilot/session-state/") {
		return sourceCopilot
	}
	if filepath.Base(path) == "events.jsonl" && looksLikeCopilotEvents(path) {
		return sourceCopilot
	}
	return sourceClaude
}

// copilotSniffCache memoizes first-line sniffs (sourceForPath is called on
// render paths; the sniff does file IO). A session file never changes
// format mid-life, so the verdict is stable per path.
var copilotSniffCache sync.Map // path -> bool

// looksLikeCopilotEvents reports whether the file's first non-blank line is
// a Copilot event envelope rather than a Claude entry. Copilot event types
// are dotted (session.start, user.message, tool.execution_start, ... —
// "abort" is the lone exception) and ride next to a data payload; Claude
// entry types (user, assistant, system, summary, file-history-snapshot,
// metadata records) never contain a dot. Unreadable or empty files keep the
// basename verdict (true): both pipelines fail the load identically anyway.
func looksLikeCopilotEvents(path string) bool {
	if v, ok := copilotSniffCache.Load(path); ok {
		return v.(bool)
	}
	res := sniffCopilotEvents(path)
	copilotSniffCache.Store(path, res)
	return res
}

func sniffCopilotEvents(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return false
		}
		return (strings.Contains(probe.Type, ".") || probe.Type == "abort") && len(probe.Data) > 0
	}
	return true
}

// copilotWorktreePaths memoizes the repo's worktree paths (discovered once,
// matching worktreeProjectDirs' set-once-at-startup semantics) so Copilot
// session scoping can tell worktree sessions from main-repo ones.
var copilotWorktreePaths = sync.OnceValue(func() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return discoverWorktreeDirs(cwd)
})

// copilotSessionsForProject returns Copilot sessions whose working directory
// resolves to the same git root as the current working directory — parity
// with the per-project Claude picker, including its worktree scoping:
// projectDirs is the picker's active Claude project-dir set (main only, or
// main + worktrees when the "b" toggle is on), and Copilot sessions whose
// Cwd lives inside a worktree that is NOT in that set are excluded, so the
// toggle affects Copilot rows exactly like Claude ones. Discovery errors
// (e.g. no ~/.copilot/session-state on disk) degrade to an empty list.
func copilotSessionsForProject(projectDirs []string) []discover.SessionInfo {
	if copilotRootErr != nil {
		return nil
	}
	sessions, err := copilot.DiscoverSessions(copilotRoot, copilotCache)
	if err != nil || len(sessions) == 0 {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return filterCopilotSessions(sessions, cwd, projectDirs, copilotWorktreePaths())
}

// excludedWorktreeRoots returns the worktree paths whose encoded Claude
// project dir is not in the picker's active projectDirs — the worktrees the
// Claude picker is currently hiding. Empty projectDirs means the Claude
// scoping is unknown (no ~/.claude); fail open and exclude nothing.
func excludedWorktreeRoots(projectDirs, worktreePaths []string) []string {
	if len(projectDirs) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(projectDirs))
	for _, d := range projectDirs {
		allowed[d] = true
	}
	var excluded []string
	for _, wt := range worktreePaths {
		dir, err := projectDirForPath(wt)
		if err != nil || allowed[dir] {
			continue
		}
		excluded = append(excluded, wt)
	}
	return excluded
}

// filterCopilotSessions keeps sessions that belong to cwd's repo (worktree
// cwds resolve to the main root) and are not inside a worktree the Claude
// picker currently hides. Pure; extracted from copilotSessionsForProject
// for testability.
func filterCopilotSessions(sessions []discover.SessionInfo, cwd string, projectDirs, worktreePaths []string) []discover.SessionInfo {
	projectRoot := gitroot.ResolveGitRoot(cwd)
	excluded := excludedWorktreeRoots(projectDirs, worktreePaths)
	var out []discover.SessionInfo
	for _, s := range sessions {
		if s.Cwd == "" {
			continue
		}
		if gitroot.ResolveGitRoot(s.Cwd) != projectRoot {
			continue
		}
		if pathUnderAny(s.Cwd, excluded) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// pathUnderAny reports whether path equals or lives under any of roots.
func pathUnderAny(path string, roots []string) bool {
	for _, r := range roots {
		if path == r || strings.HasPrefix(path, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// mergeByModTime merges two ModTime-descending session lists into one,
// preserving the newest-first order GroupSessionsByDate expects. Stable:
// on equal times, entries from a come first.
func mergeByModTime(a, b []discover.SessionInfo) []discover.SessionInfo {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	out := make([]discover.SessionInfo, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i].ModTime.Before(b[j].ModTime) {
			out = append(out, b[j])
			j++
		} else {
			out = append(out, a[i])
			i++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}

// resumeArgvFor returns the argv used to resume a session in the CLI that
// created it.
func resumeArgvFor(s *discover.SessionInfo) []string {
	if sourceForPath(s.Path) == sourceCopilot {
		return []string{"copilot", "--resume", s.SessionID}
	}
	return []string{"claude", "--resume", s.SessionID}
}

// resumeCommandLabel is the resume popup body: binary name plus the
// display-formatted session id (the exec uses the full id from resumeArgvFor).
func resumeCommandLabel(s *discover.SessionInfo) string {
	return resumeArgvFor(s)[0] + " --resume " + formatSessionName(s.SessionID)
}

// copilotSig is a cheap change signature over the Copilot session corpus:
// file count plus newest mtime. One stat per session file, no watches held.
type copilotSig struct {
	count  int
	maxMod time.Time
}

// copilotSignature computes the current signature, or the zero value when
// the Copilot root is unavailable. The picker watcher polls this instead of
// fsnotify-watching per-session dirs (kqueue opens one fd per file in a
// watched directory — the exact fd exhaustion CLAUDE.md warns about).
func copilotSignature() copilotSig {
	if copilotRootErr != nil {
		return copilotSig{}
	}
	var sig copilotSig
	for _, p := range copilot.SessionFiles(copilotRoot) {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		sig.count++
		if fi.ModTime().After(sig.maxMod) {
			sig.maxMod = fi.ModTime()
		}
	}
	return sig
}
