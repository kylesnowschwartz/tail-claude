package parser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

// writePersistedFixture builds a minimal session in a temp project dir whose
// tool_result content is a persisted-output placeholder pointing at
// {projectDir}/{session}/tool-results/{id}.txt. Returns the session path.
func writePersistedFixture(t *testing.T, projectDir, outputPath string) string {
	t.Helper()

	placeholder := "<persisted-output>\\nOutput too large (820.5KB). Full output saved to: " + outputPath

	jsonl := `{"type":"user","uuid":"u1","timestamp":"2026-07-01T10:00:00.000Z","message":{"role":"user","content":"run the thing"}}
{"type":"assistant","uuid":"a1","timestamp":"2026-07-01T10:00:01.000Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"big"}}]}}
{"type":"user","uuid":"m1","timestamp":"2026-07-01T10:00:02.000Z","isMeta":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"` + placeholder + `"}]}}
`
	sessionPath := filepath.Join(projectDir, "sess-1.jsonl")
	if err := os.WriteFile(sessionPath, []byte(jsonl), 0o644); err != nil {
		t.Fatal(err)
	}
	return sessionPath
}

// firstToolResult returns the first tool-call item's result across chunks.
func firstToolResult(t *testing.T, chunks []parser.Chunk) string {
	t.Helper()
	for _, c := range chunks {
		for _, it := range c.Items {
			if it.Type == parser.ItemToolCall {
				return it.ToolResult
			}
		}
	}
	t.Fatal("no tool-call item found")
	return ""
}

func TestPersistedOutputResolved(t *testing.T) {
	projectDir := t.TempDir()
	resultsDir := filepath.Join(projectDir, "sess-1", "tool-results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(resultsDir, "abc123.txt")
	if err := os.WriteFile(outputPath, []byte("the real tool output"), 0o644); err != nil {
		t.Fatal(err)
	}

	sessionPath := writePersistedFixture(t, projectDir, outputPath)
	chunks, err := parser.ReadSession(sessionPath)
	if err != nil {
		t.Fatal(err)
	}

	if got := firstToolResult(t, chunks); got != "the real tool output" {
		t.Errorf("ToolResult = %q, want companion file contents", got)
	}
}

func TestPersistedOutputOutsideProjectDirNotRead(t *testing.T) {
	projectDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escape.txt") // different temp root
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	sessionPath := writePersistedFixture(t, projectDir, outside)
	chunks, err := parser.ReadSession(sessionPath)
	if err != nil {
		t.Fatal(err)
	}

	got := firstToolResult(t, chunks)
	if strings.Contains(got, "secret") {
		t.Error("path outside the project dir must not be read")
	}
	if !strings.Contains(got, "<persisted-output>") {
		t.Errorf("placeholder should be preserved when unresolvable, got %q", got)
	}
}

func TestPersistedOutputMissingFileKeepsPlaceholder(t *testing.T) {
	projectDir := t.TempDir()
	missing := filepath.Join(projectDir, "sess-1", "tool-results", "gone.txt")

	sessionPath := writePersistedFixture(t, projectDir, missing)
	chunks, err := parser.ReadSession(sessionPath)
	if err != nil {
		t.Fatal(err)
	}

	if got := firstToolResult(t, chunks); !strings.Contains(got, "<persisted-output>") {
		t.Errorf("placeholder should survive a missing companion file, got %q", got)
	}
}

func TestPersistedOutputOversizeTruncated(t *testing.T) {
	projectDir := t.TempDir()
	resultsDir := filepath.Join(projectDir, "sess-1", "tool-results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(resultsDir, "big.txt")
	big := strings.Repeat("x", 300*1024)
	if err := os.WriteFile(outputPath, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	sessionPath := writePersistedFixture(t, projectDir, outputPath)
	chunks, err := parser.ReadSession(sessionPath)
	if err != nil {
		t.Fatal(err)
	}

	got := firstToolResult(t, chunks)
	if len(got) > 300*1024 {
		t.Errorf("oversize output not capped: len = %d", len(got))
	}
	if !strings.Contains(got, "truncated") || !strings.Contains(got, outputPath) {
		t.Error("truncation notice with original path expected")
	}
}
