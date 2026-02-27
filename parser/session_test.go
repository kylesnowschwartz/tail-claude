package parser_test

import (
	"os"
	"path/filepath"
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
