package parser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// copyFixture copies a testdata JSONL file into dir with the given name.
// Returns the destination path.
func copyFixture(t *testing.T, fixture, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dst
}

func TestSessionCache_DiscoverProjectSessions(t *testing.T) {
	fixture := filepath.Join("testdata", "minimal.jsonl")

	dir := t.TempDir()
	copyFixture(t, fixture, dir, "session-a.jsonl")

	cache := NewSessionCache()

	// First call: cache miss, scans file.
	sessions, err := cache.DiscoverProjectSessions(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "session-a" {
		t.Errorf("expected session-a, got %s", sessions[0].SessionID)
	}
	firstMsg := sessions[0].FirstMessage

	// Second call: file unchanged, should return cached result.
	sessions2, err := cache.DiscoverProjectSessions(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(sessions2) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions2))
	}
	if sessions2[0].FirstMessage != firstMsg {
		t.Errorf("cached FirstMessage changed: %q vs %q", firstMsg, sessions2[0].FirstMessage)
	}
}

func TestSessionCache_DetectsFileChanges(t *testing.T) {
	fixtureMinimal := filepath.Join("testdata", "minimal.jsonl")
	fixtureMultiTurn := filepath.Join("testdata", "multi_turn.jsonl")

	dir := t.TempDir()
	dst := copyFixture(t, fixtureMinimal, dir, "session-b.jsonl")

	cache := NewSessionCache()

	sessions, err := cache.DiscoverProjectSessions(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	origTurns := sessions[0].TurnCount

	// Overwrite with a different fixture that has more turns.
	// Advance modTime so the cache sees the change — some filesystems have
	// second-granularity timestamps, so a write within the same second may
	// not update modTime.
	data, err := os.ReadFile(fixtureMultiTurn)
	if err != nil {
		t.Fatalf("read multi_turn: %v", err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(dst, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	sessions2, err := cache.DiscoverProjectSessions(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(sessions2) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions2))
	}
	if sessions2[0].TurnCount == origTurns {
		t.Errorf("expected TurnCount to change after file modification, still %d", origTurns)
	}
}

func TestSessionCache_SkipsAgentFiles(t *testing.T) {
	fixture := filepath.Join("testdata", "minimal.jsonl")

	dir := t.TempDir()
	copyFixture(t, fixture, dir, "session-c.jsonl")
	copyFixture(t, fixture, dir, "agent_123.jsonl")

	cache := NewSessionCache()

	sessions, err := cache.DiscoverProjectSessions(dir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session (agent_ excluded), got %d", len(sessions))
	}
	if sessions[0].SessionID != "session-c" {
		t.Errorf("expected session-c, got %s", sessions[0].SessionID)
	}
}
