package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kylesnowschwartz/agent-ouija/claude/discover"
)

func TestSourceForPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want sessionSource
	}{
		{"empty path", "", sourceClaude},
		{"claude project jsonl", "/home/dev/.claude/projects/-home-dev-foo/abc.jsonl", sourceClaude},
		{"copilot dir session", "/home/dev/.copilot/session-state/1111/events.jsonl", sourceCopilot},
		{"copilot flat session", "/home/dev/.copilot/session-state/2222.jsonl", sourceCopilot},
		{"events.jsonl outside root (fixture)", "copilot/testdata/sessions/1111/events.jsonl", sourceCopilot},
		{"plain jsonl elsewhere", "/tmp/session.jsonl", sourceClaude},
	}
	for _, tt := range tests {
		if got := sourceForPath(tt.path); got != tt.want {
			t.Errorf("%s: sourceForPath(%q) = %v, want %v", tt.name, tt.path, got, tt.want)
		}
	}

	// The events.jsonl basename alone must not hijack Claude-format files:
	// a Claude session copied/exported as events.jsonl (or an unrelated
	// analytics file) still routes through the Claude pipeline via the
	// first-line sniff.
	dir := t.TempDir()
	claudeStyle := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(claudeStyle, []byte(`{"type":"user","uuid":"u1","timestamp":"2026-05-01T10:00:00Z","message":{"role":"user","content":"hi"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := sourceForPath(claudeStyle); got != sourceClaude {
		t.Errorf("claude-format events.jsonl = %v, want sourceClaude", got)
	}

	copilotStyle := filepath.Join(dir, "sub")
	if err := os.MkdirAll(copilotStyle, 0o755); err != nil {
		t.Fatal(err)
	}
	copilotStyle = filepath.Join(copilotStyle, "events.jsonl")
	if err := os.WriteFile(copilotStyle, []byte(`{"type":"session.start","data":{"sessionId":"s1"},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := sourceForPath(copilotStyle); got != sourceCopilot {
		t.Errorf("copilot-format events.jsonl outside root = %v, want sourceCopilot", got)
	}

	// Missing file keeps the basename verdict (both pipelines fail the load).
	if got := sourceForPath(filepath.Join(dir, "missing", "events.jsonl")); got != sourceCopilot {
		t.Errorf("missing events.jsonl = %v, want sourceCopilot (basename verdict)", got)
	}
}

func TestMergeByModTime(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	mk := func(id string, offset time.Duration) discover.SessionInfo {
		return discover.SessionInfo{SessionID: id, ModTime: base.Add(offset)}
	}

	t.Run("interleaves newest first", func(t *testing.T) {
		a := []discover.SessionInfo{mk("a1", -1*time.Hour), mk("a2", -3*time.Hour)}
		b := []discover.SessionInfo{mk("b1", 0), mk("b2", -2*time.Hour)}
		got := mergeByModTime(a, b)
		want := []string{"b1", "a1", "b2", "a2"}
		if len(got) != len(want) {
			t.Fatalf("merged length = %d, want %d", len(got), len(want))
		}
		for i, id := range want {
			if got[i].SessionID != id {
				t.Errorf("merged[%d] = %s, want %s", i, got[i].SessionID, id)
			}
		}
		for i := 1; i < len(got); i++ {
			if got[i].ModTime.After(got[i-1].ModTime) {
				t.Errorf("merged not descending at index %d", i)
			}
		}
	})

	t.Run("empty sides", func(t *testing.T) {
		a := []discover.SessionInfo{mk("a1", 0)}
		if got := mergeByModTime(a, nil); len(got) != 1 || got[0].SessionID != "a1" {
			t.Errorf("merge(a, nil) = %v", got)
		}
		if got := mergeByModTime(nil, a); len(got) != 1 || got[0].SessionID != "a1" {
			t.Errorf("merge(nil, a) = %v", got)
		}
		if got := mergeByModTime(nil, nil); len(got) != 0 {
			t.Errorf("merge(nil, nil) = %v", got)
		}
	})

	t.Run("equal times keep first list first", func(t *testing.T) {
		a := []discover.SessionInfo{mk("a1", 0)}
		b := []discover.SessionInfo{mk("b1", 0)}
		got := mergeByModTime(a, b)
		if got[0].SessionID != "a1" || got[1].SessionID != "b1" {
			t.Errorf("merge stability violated: %s, %s", got[0].SessionID, got[1].SessionID)
		}
	})
}

func TestResumeArgvFor(t *testing.T) {
	claude := &discover.SessionInfo{
		Path:      "/home/dev/.claude/projects/-home-dev-foo/abc.jsonl",
		SessionID: "abc-123",
	}
	copilot := &discover.SessionInfo{
		Path:      "/home/dev/.copilot/session-state/1111/events.jsonl",
		SessionID: "1111-2222",
	}

	got := resumeArgvFor(claude)
	if got[0] != "claude" || got[1] != "--resume" || got[2] != "abc-123" {
		t.Errorf("claude argv = %v", got)
	}
	got = resumeArgvFor(copilot)
	if got[0] != "copilot" || got[1] != "--resume" || got[2] != "1111-2222" {
		t.Errorf("copilot argv = %v", got)
	}

	if label := resumeCommandLabel(copilot); label == "" || label[:7] != "copilot" {
		t.Errorf("copilot resume label = %q, want copilot prefix", label)
	}
	if label := resumeCommandLabel(claude); label == "" || label[:6] != "claude" {
		t.Errorf("claude resume label = %q, want claude prefix", label)
	}
}

// Copilot rows must honor the picker's worktree scoping: sessions whose Cwd
// lives inside a worktree whose encoded project dir is not in the active
// projectDirs are hidden, exactly like the Claude rows the "b" toggle scopes.
func TestFilterCopilotSessionsWorktreeScoping(t *testing.T) {
	// A plain directory tree (no git metadata): ResolveGitRoot falls back to
	// the input path, so each session's Cwd is its own "root". That means
	// only the session whose Cwd equals cwd matches the project-root check;
	// worktree exclusion is exercised via worktree Cwds equal to the
	// worktree path itself.
	repo := t.TempDir()
	wt := filepath.Join(repo+"-worktrees", "wt1")

	mainDir, err := projectDirForPath(repo)
	if err != nil {
		t.Skip("no claude root available:", err)
	}
	wtDir, err := projectDirForPath(wt)
	if err != nil {
		t.Fatal(err)
	}

	mainSess := discover.SessionInfo{SessionID: "main", Cwd: repo}
	wtSess := discover.SessionInfo{SessionID: "wt", Cwd: wt}
	sessions := []discover.SessionInfo{mainSess, wtSess}
	worktrees := []string{repo, wt}

	ids := func(ss []discover.SessionInfo) []string {
		var out []string
		for _, s := range ss {
			out = append(out, s.SessionID)
		}
		return out
	}

	// Worktree mode OFF: only the main project dir is active. The worktree
	// session must be hidden even though its git root resolves to the repo
	// (here: filter it from a candidate set rooted at wt's own path).
	got := excludedWorktreeRoots([]string{mainDir}, worktrees)
	if len(got) != 1 || got[0] != wt {
		t.Fatalf("excludedWorktreeRoots(off) = %v, want [%s]", got, wt)
	}

	// Worktree mode ON: both dirs active, nothing excluded.
	if got := excludedWorktreeRoots([]string{mainDir, wtDir}, worktrees); len(got) != 0 {
		t.Errorf("excludedWorktreeRoots(on) = %v, want empty", got)
	}

	// No Claude scoping info: fail open.
	if got := excludedWorktreeRoots(nil, worktrees); len(got) != 0 {
		t.Errorf("excludedWorktreeRoots(nil) = %v, want empty", got)
	}

	// End-to-end filter with cwd = wt (a user working inside the worktree,
	// mode off): the wt session is excluded.
	filtered := filterCopilotSessions(sessions, wt, []string{mainDir}, worktrees)
	if len(filtered) != 0 {
		t.Errorf("filter(cwd=wt, mode off) = %v, want empty (wt hidden, main is a different resolved root)", ids(filtered))
	}
	filtered = filterCopilotSessions(sessions, wt, []string{mainDir, wtDir}, worktrees)
	if len(filtered) != 1 || filtered[0].SessionID != "wt" {
		t.Errorf("filter(cwd=wt, mode on) = %v, want [wt]", ids(filtered))
	}

	// pathUnderAny covers nested paths and rejects sibling prefixes.
	if !pathUnderAny(filepath.Join(wt, "sub", "dir"), []string{wt}) {
		t.Error("nested path not detected under worktree root")
	}
	if pathUnderAny(wt+"2", []string{wt}) {
		t.Error("sibling with shared prefix wrongly under worktree root")
	}
}
