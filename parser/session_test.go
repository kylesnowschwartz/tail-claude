package parser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestProjectDirForPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	prefix := filepath.Join(home, ".claude", "projects") + "/"

	tests := []struct {
		name    string
		path    string
		wantDir string // just the encoded part after ~/.claude/projects/
	}{
		{"plain path", "/Users/kyle/Code/proj", "-Users-kyle-Code-proj"},
		{"dotfile path", "/Users/kyle/.config/nvim", "-Users-kyle--config-nvim"},
		{"worktree with .claude", "/Users/kyle/Code/proj/.claude/worktrees/wt", "-Users-kyle-Code-proj--claude-worktrees-wt"},
		{"underscore in path", "/private/var/folders/s0/abc_def/T/proj", "-private-var-folders-s0-abc-def-T-proj"},
		{"dots in project name", "/Users/kyle/Code/my.project.name", "-Users-kyle-Code-my-project-name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, err := parser.ProjectDirForPath(tt.path)
			if err != nil {
				t.Fatalf("ProjectDirForPath error: %v", err)
			}
			want := prefix + tt.wantDir
			if dir != want {
				t.Errorf("got  %q\nwant %q", dir, want)
			}
		})
	}
}

func TestReadSession_ValidFile(t *testing.T) {
	path := filepath.Join("testdata", "minimal.jsonl")
	chunks, err := parser.ReadSession(path)
	if err != nil {
		t.Fatalf("ReadSession(%q) error: %v", path, err)
	}
	// minimal.jsonl has: 1 user, 1 assistant, 1 system output
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(chunks))
	}
	if chunks[0].Type != parser.UserChunk {
		t.Errorf("chunks[0].Type = %d, want UserChunk", chunks[0].Type)
	}
	if chunks[1].Type != parser.AIChunk {
		t.Errorf("chunks[1].Type = %d, want AIChunk", chunks[1].Type)
	}
	if chunks[2].Type != parser.SystemChunk {
		t.Errorf("chunks[2].Type = %d, want SystemChunk", chunks[2].Type)
	}
}

func TestReadSession_EmptyLines(t *testing.T) {
	// Write a temp file with blank lines interspersed.
	content := "\n" +
		`{"uuid":"u1","type":"user","timestamp":"2025-01-15T10:00:00Z","isSidechain":false,"isMeta":false,"message":{"role":"user","content":"hello"}}` + "\n" +
		"\n" +
		`{"uuid":"a1","type":"assistant","timestamp":"2025-01-15T10:00:01Z","isSidechain":false,"isMeta":false,"message":{"role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-opus-4-6","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n\n"

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "empty_lines.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	chunks, err := parser.ReadSession(path)
	if err != nil {
		t.Fatalf("ReadSession error: %v", err)
	}
	// Should get 2 chunks (user + AI), blank lines skipped.
	if len(chunks) != 2 {
		t.Errorf("len(chunks) = %d, want 2", len(chunks))
	}
}

func TestReadSession_InvalidJSONLines(t *testing.T) {
	content := `{invalid json}` + "\n" +
		`{"uuid":"u1","type":"user","timestamp":"2025-01-15T10:00:00Z","isSidechain":false,"isMeta":false,"message":{"role":"user","content":"hello"}}` + "\n" +
		`also not json` + "\n"

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bad_lines.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	chunks, err := parser.ReadSession(path)
	if err != nil {
		t.Fatalf("ReadSession error: %v", err)
	}
	// Only the valid user line should produce a chunk.
	if len(chunks) != 1 {
		t.Errorf("len(chunks) = %d, want 1", len(chunks))
	}
}

func TestReadSession_NoiseFiltered(t *testing.T) {
	path := filepath.Join("testdata", "noise.jsonl")
	chunks, err := parser.ReadSession(path)
	if err != nil {
		t.Fatalf("ReadSession(%q) error: %v", path, err)
	}
	// noise.jsonl has 11 lines total. After filtering:
	// - u1 (user) -> UserChunk
	// - n1 (system type) -> filtered
	// - n2 (summary type) -> CompactChunk
	// - a1 (assistant with thinking/tools) -> starts AI buffer
	// - sc1 (sidechain) -> filtered
	// - n3 (synthetic) -> filtered
	// - m1 (meta user) -> AIMsg (merges into AI buffer with a1)
	// - n4 (system-reminder wrapped) -> filtered
	// - n5 (empty stdout) -> filtered
	// - n6 (interruption) -> filtered
	// - u2 (user) -> flushes AI buffer -> UserChunk
	// Result: UserChunk, CompactChunk, AIChunk, UserChunk = 4 chunks
	if len(chunks) != 4 {
		t.Fatalf("len(chunks) = %d, want 4", len(chunks))
	}
	if chunks[0].Type != parser.UserChunk {
		t.Errorf("chunks[0].Type = %d, want UserChunk", chunks[0].Type)
	}
	if chunks[1].Type != parser.CompactChunk {
		t.Errorf("chunks[1].Type = %d, want CompactChunk", chunks[1].Type)
	}
	if chunks[2].Type != parser.AIChunk {
		t.Errorf("chunks[2].Type = %d, want AIChunk", chunks[2].Type)
	}
	if chunks[3].Type != parser.UserChunk {
		t.Errorf("chunks[3].Type = %d, want UserChunk", chunks[3].Type)
	}

	// The AI chunk should have thinking and tool calls from the assistant message.
	ai := chunks[2]
	if ai.ThinkingCount != 1 {
		t.Errorf("AI Thinking = %d, want 1", ai.ThinkingCount)
	}
	if len(ai.ToolCalls) != 1 {
		t.Errorf("AI ToolCalls = %d, want 1", len(ai.ToolCalls))
	}
}

// writeTitledSession writes a minimal JSONL session with an optional
// custom-title and ai-title. Returns the written path.
func writeTitledSession(t *testing.T, dir, name, customTitle, aiTitle string) string {
	t.Helper()
	var lines []string
	if customTitle != "" {
		lines = append(lines, `{"type":"custom-title","customTitle":"`+customTitle+`","sessionId":"`+name+`"}`)
	}
	if aiTitle != "" {
		lines = append(lines, `{"type":"ai-title","aiTitle":"`+aiTitle+`","sessionId":"`+name+`"}`)
	}
	// A real conversation turn so turnCount > 0 (else the session is skipped as a ghost).
	lines = append(lines,
		`{"uuid":"u1","type":"user","timestamp":"2025-01-15T10:00:00Z","isSidechain":false,"isMeta":false,"message":{"role":"user","content":"hello"}}`,
		`{"uuid":"a1","type":"assistant","timestamp":"2025-01-15T10:00:01Z","isSidechain":false,"isMeta":false,"message":{"role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-opus-4-6","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}}`,
	)
	path := filepath.Join(dir, name+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindTitleMatches(t *testing.T) {
	root := t.TempDir()
	projA := filepath.Join(root, "-Users-x-proj-a")
	projB := filepath.Join(root, "-Users-x-proj-b")
	if err := os.MkdirAll(projA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projB, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTitledSession(t, projA, "11111111-1111-1111-1111-111111111111", "worklog-cron-config", "")
	writeTitledSession(t, projA, "22222222-2222-2222-2222-222222222222", "", "auto-named-thing")
	writeTitledSession(t, projB, "33333333-3333-3333-3333-333333333333", "plugin-stuff", "")
	writeTitledSession(t, projB, "44444444-4444-4444-4444-444444444444", "Worklog-Cron-Config", "") // case clash
	writeTitledSession(t, projB, "55555555-5555-5555-5555-555555555555", "", "")                    // no title

	dirs := []string{projA, projB}

	t.Run("exact match is case-insensitive and unique", func(t *testing.T) {
		got, err := parser.FindTitleMatches("WORKLOG-CRON-CONFIG", dirs)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2 (both case variants)", len(got))
		}
	})

	t.Run("substring match wins when no exact", func(t *testing.T) {
		got, err := parser.FindTitleMatches("plugin", dirs)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Title != "plugin-stuff" {
			t.Fatalf("got %+v, want single plugin-stuff match", got)
		}
	})

	t.Run("ai-title is searchable", func(t *testing.T) {
		got, err := parser.FindTitleMatches("auto-named-thing", dirs)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("len(got) = %d, want 1", len(got))
		}
	})

	t.Run("exact match preferred over substring", func(t *testing.T) {
		writeTitledSession(t, projA, "66666666-6666-6666-6666-666666666666", "plug", "")
		got, err := parser.FindTitleMatches("plug", dirs)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Title != "plug" {
			t.Fatalf("got %+v, want exact 'plug' only", got)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		got, err := parser.FindTitleMatches("no-such-thing", dirs)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("got %d, want 0", len(got))
		}
	})

	t.Run("scanner skips large content lines", func(t *testing.T) {
		// A real session has many KB-sized content lines. The title scanner
		// must not parse them — it should reject by length and substring.
		// This session has one huge bogus assistant line and the title at
		// the bottom, mimicking a late /rename.
		heavy := filepath.Join(root, "-heavy-proj")
		if err := os.MkdirAll(heavy, 0o755); err != nil {
			t.Fatal(err)
		}
		huge := strings.Repeat("x", 200_000)
		content := []string{
			`{"uuid":"u1","type":"user","timestamp":"2025-01-15T10:00:00Z","isSidechain":false,"isMeta":false,"message":{"role":"user","content":"` + huge + `"}}`,
			`{"type":"custom-title","customTitle":"late-rename","sessionId":"huge"}`,
		}
		if err := os.WriteFile(filepath.Join(heavy, "huge.jsonl"),
			[]byte(strings.Join(content, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := parser.FindTitleMatches("late-rename", []string{heavy})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Title != "late-rename" {
			t.Fatalf("got %+v, want one late-rename match", got)
		}
	})

	t.Run("empty query returns nil", func(t *testing.T) {
		got, err := parser.FindTitleMatches("  ", dirs)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Fatalf("got %+v, want nil for empty query", got)
		}
	})
}
