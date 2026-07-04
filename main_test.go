package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

// Session fixture lines for buildSessionState tests. The pending assistant
// line has a tool_use with no matching result, which IsOngoing treats as an
// in-progress session; the done line ends with text output (an ending event).
const (
	bssUserLine = `{"parentUuid":null,"isSidechain":false,"type":"user","uuid":"u1","timestamp":"2026-07-04T00:00:00Z","message":{"role":"user","content":"do the thing"}}`
	bssDoneLine = `{"parentUuid":"u1","isSidechain":false,"type":"assistant","uuid":"a1","timestamp":"2026-07-04T00:00:01Z","message":{"role":"assistant","model":"claude-fable-5","stop_reason":"end_turn","content":[{"type":"text","text":"all done"}]}}`
	bssPendLine = `{"parentUuid":"u1","isSidechain":false,"type":"assistant","uuid":"a2","timestamp":"2026-07-04T00:00:01Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"sleep 100"}}]}}`

	bssSubUserLine = `{"parentUuid":null,"isSidechain":true,"agentId":"ab12cd34","type":"user","uuid":"s1","timestamp":"2026-07-04T00:00:02Z","message":{"role":"user","content":"investigate the thing"}}`
	bssSubPendLine = `{"parentUuid":"s1","isSidechain":true,"agentId":"ab12cd34","type":"assistant","uuid":"s2","timestamp":"2026-07-04T00:00:03Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"sleep 100"}}]}}`
)

// writeSessionFixture writes a parent session file (and optionally a pending
// subagent file) into a temp dir and returns the parent path.
func writeSessionFixture(t *testing.T, parentLines string, withSubagent bool) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(parentLines), 0o644); err != nil {
		t.Fatal(err)
	}
	if withSubagent {
		subDir := filepath.Join(dir, "session", "subagents")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatal(err)
		}
		subPath := filepath.Join(subDir, "agent-ab12cd34.jsonl")
		if err := os.WriteFile(subPath, []byte(bssSubUserLine+"\n"+bssSubPendLine+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// makeStale pushes a file's mtime past OngoingStalenessThreshold.
func makeStale(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-parser.OngoingStalenessThreshold - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func buildStateFromPath(t *testing.T, path string) sessionState {
	t.Helper()
	classified, _, err := parser.ReadSessionIncremental(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	return buildSessionState(path, parser.BuildChunks(classified))
}

func TestBuildSessionState_Ongoing(t *testing.T) {
	t.Run("finished session is not ongoing", func(t *testing.T) {
		path := writeSessionFixture(t, bssUserLine+"\n"+bssDoneLine+"\n", false)
		state := buildStateFromPath(t, path)
		if state.ongoing {
			t.Error("ongoing = true, want false for finished session")
		}
		if len(state.messages) == 0 {
			t.Error("messages empty, want converted chunks")
		}
	})

	t.Run("pending tool call with fresh file is ongoing", func(t *testing.T) {
		path := writeSessionFixture(t, bssUserLine+"\n"+bssPendLine+"\n", false)
		state := buildStateFromPath(t, path)
		if !state.ongoing {
			t.Error("ongoing = false, want true for pending tool call")
		}
	})

	t.Run("stale parent file clears parent ongoing", func(t *testing.T) {
		path := writeSessionFixture(t, bssUserLine+"\n"+bssPendLine+"\n", false)
		makeStale(t, path)
		state := buildStateFromPath(t, path)
		if state.ongoing {
			t.Error("ongoing = true, want false when parent file is stale")
		}
	})

	t.Run("fresh subagent keeps stale parent ongoing", func(t *testing.T) {
		// Parent-file staleness must gate only the parent predicate: a
		// subagent still writing to its own file means work is in progress.
		path := writeSessionFixture(t, bssUserLine+"\n"+bssPendLine+"\n", true)
		makeStale(t, path)
		state := buildStateFromPath(t, path)
		if !state.ongoing {
			t.Error("ongoing = false, want true when a fresh subagent is still working")
		}
		if len(state.allProcs) != 1 {
			t.Errorf("allProcs = %d, want 1 discovered subagent", len(state.allProcs))
		}
	})

	t.Run("stale subagent does not resurrect finished session", func(t *testing.T) {
		path := writeSessionFixture(t, bssUserLine+"\n"+bssDoneLine+"\n", true)
		subPath := filepath.Join(filepath.Dir(path), "session", "subagents", "agent-ab12cd34.jsonl")
		makeStale(t, subPath)
		state := buildStateFromPath(t, path)
		if state.ongoing {
			t.Error("ongoing = true, want false when the only pending subagent is stale")
		}
	})
}

func TestIsDevBuild(t *testing.T) {
	orig := version
	defer func() { version = orig }()

	cases := []struct {
		version string
		want    bool
	}{
		{"v0.12.0", false},
		{"v0.12.0-dev-a17b50a", true},
		{"dev", true},
	}
	for _, c := range cases {
		version = c.version
		if got := isDevBuild(); got != c.want {
			t.Errorf("isDevBuild() with version %q = %v, want %v", c.version, got, c.want)
		}
	}
}
