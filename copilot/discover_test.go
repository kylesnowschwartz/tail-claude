package copilot

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kylesnowschwartz/agent-ouija/claude/discover"
)

const fixtureRoot = "testdata/sessions"

func discoverFixtures(t *testing.T) []discover.SessionInfo {
	t.Helper()
	sessions, err := DiscoverSessions(fixtureRoot, NewCache())
	if err != nil {
		t.Fatal(err)
	}
	return sessions
}

func sessionByID(t *testing.T, sessions []discover.SessionInfo, id string) discover.SessionInfo {
	t.Helper()
	for _, s := range sessions {
		if s.SessionID == id {
			return s
		}
	}
	t.Fatalf("session %s not found in %d results", id, len(sessions))
	return discover.SessionInfo{}
}

func TestDiscoverSessions(t *testing.T) {
	sessions := discoverFixtures(t)

	// Fixtures 1-5 plus the flat file; the ghost session (6, no user
	// messages) is skipped.
	if len(sessions) != 6 {
		t.Fatalf("got %d sessions: %+v", len(sessions), sessions)
	}
	for _, s := range sessions {
		if s.SessionID == "66666666-ffff-4666-8666-666666666666" {
			t.Error("ghost session (TurnCount 0) not skipped")
		}
		if s.TurnCount == 0 {
			t.Errorf("session %s has TurnCount 0", s.SessionID)
		}
		if s.ContextTokens != 0 {
			t.Errorf("session %s ContextTokens = %d, want 0", s.SessionID, s.ContextTokens)
		}
		if s.PermissionMode != "" {
			t.Errorf("session %s PermissionMode = %q, want empty", s.SessionID, s.PermissionMode)
		}
	}

	s1 := sessionByID(t, sessions, "11111111-aaaa-4111-8111-111111111111")
	if s1.Title != "greeting feature" {
		t.Errorf("Title = %q (workspace.yaml name overlay)", s1.Title)
	}
	if s1.FirstMessage != "add a greeting function" {
		t.Errorf("FirstMessage = %q", s1.FirstMessage)
	}
	if s1.LastPrompt != "add a greeting function" {
		t.Errorf("LastPrompt = %q", s1.LastPrompt)
	}
	if s1.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", s1.TurnCount)
	}
	if s1.Model != "claude-sonnet-4.6" {
		t.Errorf("Model = %q", s1.Model)
	}
	if s1.Cwd != "/home/dev/example-project" || s1.GitBranch != "main" {
		t.Errorf("Cwd/Branch = %q / %q", s1.Cwd, s1.GitBranch)
	}
	if s1.DurationMs != 16000 {
		t.Errorf("DurationMs = %d, want 16000", s1.DurationMs)
	}
	if s1.IsOngoing {
		t.Error("IsOngoing = true for a shut-down fixture")
	}
	if filepath.Base(s1.Path) != "events.jsonl" {
		t.Errorf("Path = %q", s1.Path)
	}

	// Resumed session: model change and context change win.
	s4 := sessionByID(t, sessions, "44444444-dddd-4444-8444-444444444444")
	if s4.Model != "claude-opus-4.6" {
		t.Errorf("s4 Model = %q", s4.Model)
	}
	if s4.GitBranch != "feature/next" {
		t.Errorf("s4 GitBranch = %q", s4.GitBranch)
	}
	if s4.TurnCount != 2 {
		t.Errorf("s4 TurnCount = %d, want 2", s4.TurnCount)
	}

	// Subagent session: nested user messages never counted; last prompt is
	// the real second prompt.
	s5 := sessionByID(t, sessions, "55555555-eeee-4555-8555-555555555555")
	if s5.TurnCount != 2 || s5.LastPrompt != "stop" {
		t.Errorf("s5 TurnCount=%d LastPrompt=%q", s5.TurnCount, s5.LastPrompt)
	}

	// Flat root-level session file: discovered, preview newline-collapsed,
	// no workspace overlay.
	s7 := sessionByID(t, sessions, "77777777-9999-4777-8777-777777777777")
	if filepath.Base(s7.Path) != "77777777-9999-4777-8777-777777777777.jsonl" {
		t.Errorf("s7 Path = %q", s7.Path)
	}
	if s7.FirstMessage != "hello from a flat session second line of the prompt" {
		t.Errorf("s7 FirstMessage = %q", s7.FirstMessage)
	}
	if s7.Title != "" {
		t.Errorf("s7 Title = %q, want empty", s7.Title)
	}
}

func TestDiscoverSessionsSorted(t *testing.T) {
	sessions := discoverFixtures(t)
	for i := 1; i < len(sessions); i++ {
		prev, cur := sessions[i-1], sessions[i]
		if cur.ModTime.After(prev.ModTime) {
			t.Fatalf("not sorted newest-first at %d: %v then %v", i, prev.ModTime, cur.ModTime)
		}
		if cur.ModTime.Equal(prev.ModTime) && cur.Path < prev.Path {
			t.Fatalf("tiebreak not deterministic at %d: %q then %q", i, prev.Path, cur.Path)
		}
	}
}

func TestDiscoverSessionsMissingRoot(t *testing.T) {
	if _, err := DiscoverSessions(filepath.Join(t.TempDir(), "missing"), NewCache()); err == nil {
		t.Error("expected error for missing root")
	}
}

func TestDiscoverSessionsNilCache(t *testing.T) {
	sessions, err := DiscoverSessions(fixtureRoot, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 6 {
		t.Errorf("got %d sessions with nil cache, want 6", len(sessions))
	}
}

func TestSessionFiles(t *testing.T) {
	files := SessionFiles(fixtureRoot)
	if len(files) != 7 { // 6 dirs with events.jsonl + 1 flat file
		t.Fatalf("got %d files: %v", len(files), files)
	}
	if SessionFiles(filepath.Join(t.TempDir(), "missing")) != nil {
		t.Error("SessionFiles on missing root should return nil")
	}
}

func TestCacheHitAndInvalidation(t *testing.T) {
	// Copy one fixture into a temp root so the file can be mutated.
	root := t.TempDir()
	dir := filepath.Join(root, "11111111-aaaa-4111-8111-111111111111")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(fixturePath("11111111-aaaa-4111-8111-111111111111", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	eventsPath := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(eventsPath, src, 0o644); err != nil {
		t.Fatal(err)
	}

	cache := NewCache()
	first, err := DiscoverSessions(root, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].TurnCount != 1 {
		t.Fatalf("first discovery = %+v", first)
	}

	// Cache hit: replace the file with garbage of the SAME size, restore the
	// mtime -- the cached scan must be returned without re-reading the file.
	fi, err := os.Stat(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	garbage := make([]byte, len(src))
	for i := range garbage {
		garbage[i] = 'x'
	}
	if err := os.WriteFile(eventsPath, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(eventsPath, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	cached, err := DiscoverSessions(root, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 1 || cached[0].TurnCount != 1 {
		t.Fatalf("cache hit failed: %+v", cached)
	}

	// Invalidation: restore real content plus one more user message. The
	// size change forces a rescan even if mtime granularity hides the touch.
	extra := `{"type":"user.message","data":{"content":"one more thing"},"id":"z1","timestamp":"2026-05-01T10:05:00Z"}` + "\n"
	if err := os.WriteFile(eventsPath, append(src, []byte(extra)...), 0o644); err != nil {
		t.Fatal(err)
	}
	rescanned, err := DiscoverSessions(root, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(rescanned) != 1 || rescanned[0].TurnCount != 2 {
		t.Fatalf("rescan after change failed: %+v", rescanned)
	}
	if rescanned[0].LastPrompt != "one more thing" {
		t.Errorf("LastPrompt = %q", rescanned[0].LastPrompt)
	}
}

func TestDefaultRoot(t *testing.T) {
	root, err := DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(root) != "session-state" || filepath.Base(filepath.Dir(root)) != ".copilot" {
		t.Errorf("root = %q", root)
	}
}

// A growing file must be scanned incrementally (resume from the cached
// offset) and yield exactly the same metadata as a from-scratch scan —
// including DurationMs, which spans first to last event across passes.
func TestCacheIncrementalResumeMatchesFullScan(t *testing.T) {
	src, err := os.ReadFile(fixturePath("22222222-bbbb-4222-8222-222222222222", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(src), "\n")
	half := len(lines) / 2

	root := t.TempDir()
	dir := filepath.Join(root, "22222222-bbbb-4222-8222-222222222222")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	eventsPath := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(eventsPath, []byte(strings.Join(lines[:half], "")), 0o644); err != nil {
		t.Fatal(err)
	}

	cache := NewCache()
	if _, err := DiscoverSessions(root, cache); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(strings.Join(lines[half:], "")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	incremental, err := DiscoverSessions(root, cache)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := DiscoverSessions(root, NewCache())
	if err != nil {
		t.Fatal(err)
	}
	if len(incremental) != 1 || len(fresh) != 1 {
		t.Fatalf("got %d incremental / %d fresh sessions", len(incremental), len(fresh))
	}
	if !reflect.DeepEqual(incremental[0], fresh[0]) {
		t.Errorf("incremental scan != full scan\nincr:  %+v\nfresh: %+v", incremental[0], fresh[0])
	}

	// Truncation falls back to a full rescan (offset beyond the new size).
	if err := os.WriteFile(eventsPath, []byte(strings.Join(lines[:half], "")), 0o644); err != nil {
		t.Fatal(err)
	}
	truncated, err := DiscoverSessions(root, cache)
	if err != nil {
		t.Fatal(err)
	}
	freshHalf, err := DiscoverSessions(root, NewCache())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(truncated, freshHalf) {
		t.Errorf("post-truncation scan != full scan\ngot:  %+v\nwant: %+v", truncated, freshHalf)
	}
}
