package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

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
	// - n2 (summary type) -> filtered
	// - a1 (assistant with thinking/tools) -> starts AI buffer
	// - sc1 (sidechain) -> filtered
	// - n3 (synthetic) -> filtered
	// - m1 (meta user) -> AIMsg (merges into AI buffer with a1)
	// - n4 (system-reminder wrapped) -> filtered
	// - n5 (empty stdout) -> filtered
	// - n6 (interruption) -> filtered
	// - u2 (user) -> flushes AI buffer -> UserChunk
	// Result: UserChunk, AIChunk, UserChunk = 3 chunks
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(chunks))
	}
	if chunks[0].Type != parser.UserChunk {
		t.Errorf("chunks[0].Type = %d, want UserChunk", chunks[0].Type)
	}
	if chunks[1].Type != parser.AIChunk {
		t.Errorf("chunks[1].Type = %d, want AIChunk", chunks[1].Type)
	}
	if chunks[2].Type != parser.UserChunk {
		t.Errorf("chunks[2].Type = %d, want UserChunk", chunks[2].Type)
	}

	// The AI chunk should have thinking and tool calls from the assistant message.
	ai := chunks[1]
	if ai.ThinkingCount != 1 {
		t.Errorf("AI Thinking = %d, want 1", ai.ThinkingCount)
	}
	if len(ai.ToolCalls) != 1 {
		t.Errorf("AI ToolCalls = %d, want 1", len(ai.ToolCalls))
	}
}
