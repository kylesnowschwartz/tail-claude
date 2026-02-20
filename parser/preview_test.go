package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

// Helper to write a JSONL file from lines.
func writeJSONL(t *testing.T, dir, name string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Shorthand entry builders to keep tests readable.
func userEntry(uuid, ts, content string) string {
	return fmt.Sprintf(
		`{"uuid":%q,"type":"user","timestamp":%q,"isSidechain":false,"isMeta":false,"message":{"role":"user","content":%q}}`,
		uuid, ts, content,
	)
}

func metaUserEntry(uuid, ts, content string) string {
	return fmt.Sprintf(
		`{"uuid":%q,"type":"user","timestamp":%q,"isSidechain":false,"isMeta":true,"message":{"role":"user","content":%q}}`,
		uuid, ts, content,
	)
}

func assistantEntry(uuid, ts, text string) string {
	return fmt.Sprintf(
		`{"uuid":%q,"type":"assistant","timestamp":%q,"isSidechain":false,"isMeta":false,"message":{"role":"assistant","content":[{"type":"text","text":%q}],"model":"claude-opus-4-6","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}}`,
		uuid, ts, text,
	)
}

func snapshotEntry(uuid string) string {
	return fmt.Sprintf(
		`{"type":"file-history-snapshot","messageId":%q,"snapshot":{"messageId":%q,"trackedFileBackups":{},"timestamp":"2026-02-20T06:42:33.954Z"},"isSnapshotUpdate":false}`,
		uuid, uuid,
	)
}

// --- scanSessionPreview tests ---

func TestScanPreview_NormalUserText(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "Hello world"),
		assistantEntry("a1", "2025-01-15T10:00:01Z", "Hi there"),
	)

	preview, count := parser.ScanSessionPreview(path)
	if preview != "Hello world" {
		t.Errorf("preview = %q, want %q", preview, "Hello world")
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestScanPreview_CommandFallback(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "<command-name>/model</command-name><command-args>sonnet</command-args>"),
		assistantEntry("a1", "2025-01-15T10:00:01Z", "Switched model"),
	)

	preview, _ := parser.ScanSessionPreview(path)
	if preview != "/model" {
		t.Errorf("preview = %q, want %q", preview, "/model")
	}
}

func TestScanPreview_CommandFallbackOverriddenByText(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "<command-name>/model</command-name>"),
		userEntry("u2", "2025-01-15T10:00:01Z", "Now help me with this"),
	)

	preview, _ := parser.ScanSessionPreview(path)
	if preview != "Now help me with this" {
		t.Errorf("preview = %q, want %q", preview, "Now help me with this")
	}
}

func TestScanPreview_SkipsCommandOutput(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "<local-command-stdout>file1.go file2.go</local-command-stdout>"),
		userEntry("u2", "2025-01-15T10:00:01Z", "What are these files?"),
	)

	preview, _ := parser.ScanSessionPreview(path)
	if preview != "What are these files?" {
		t.Errorf("preview = %q, want %q", preview, "What are these files?")
	}
}

func TestScanPreview_SkipsInterruptions(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "[Request interrupted by user at 2025-01-15T10:00:00Z]"),
		userEntry("u2", "2025-01-15T10:00:01Z", "Try again please"),
	)

	preview, _ := parser.ScanSessionPreview(path)
	if preview != "Try again please" {
		t.Errorf("preview = %q, want %q", preview, "Try again please")
	}
}

func TestScanPreview_DoesNotFilterIsMeta(t *testing.T) {
	// claude-devtools processes isMeta entries for preview. If the first
	// type=user entry is isMeta with real text, it should be used.
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		metaUserEntry("m1", "2025-01-15T10:00:00Z", "Tool result: some data here"),
		assistantEntry("a1", "2025-01-15T10:00:01Z", "Got it"),
	)

	preview, _ := parser.ScanSessionPreview(path)
	if preview != "Tool result: some data here" {
		t.Errorf("preview = %q, want %q", preview, "Tool result: some data here")
	}
	// Turn count is 0 here because isMeta user messages aren't counted as
	// conversation turns. That's correct -- the test is about preview extraction.
}

func TestScanPreview_TeammateMessageNotFiltered(t *testing.T) {
	// Teammate messages should pass through sanitization, not be skipped.
	teammateMsg := `<teammate-message teammate_id="lead">You are working on task #1</teammate-message>`
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", teammateMsg),
		assistantEntry("a1", "2025-01-15T10:00:01Z", "On it"),
	)

	preview, _ := parser.ScanSessionPreview(path)
	// SanitizeContent strips noise tags but not teammate-message tags.
	// The preview should contain the teammate message text.
	if preview == "" {
		t.Error("preview is empty, teammate message should produce a preview")
	}
	if !strings.Contains(preview, "task #1") {
		t.Errorf("preview = %q, should contain teammate message content", preview)
	}
}

func TestScanPreview_SanitizesNoiseFromPreview(t *testing.T) {
	// system-reminder tags inside user content should be stripped.
	content := "<system-reminder>some noise</system-reminder>Real question here"
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", content),
	)

	preview, _ := parser.ScanSessionPreview(path)
	if preview != "Real question here" {
		t.Errorf("preview = %q, want %q", preview, "Real question here")
	}
}

func TestScanPreview_GhostSession(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		snapshotEntry("snap1"),
		snapshotEntry("snap2"),
	)

	preview, count := parser.ScanSessionPreview(path)
	if preview != "" {
		t.Errorf("preview = %q, want empty for ghost session", preview)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 for ghost session", count)
	}
}

func TestScanPreview_NewlinesCollapsed(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "line one\nline two\nline three"),
	)

	preview, _ := parser.ScanSessionPreview(path)
	if strings.Contains(preview, "\n") {
		t.Errorf("preview contains newlines: %q", preview)
	}
	if preview != "line one line two line three" {
		t.Errorf("preview = %q, want %q", preview, "line one line two line three")
	}
}

func TestScanPreview_TruncatesLongPreview(t *testing.T) {
	longText := strings.Repeat("x", 200)
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", longText),
	)

	preview, _ := parser.ScanSessionPreview(path)
	// Truncation: 119 ASCII chars + "…" (3 bytes) = 122 bytes total.
	// The ellipsis is a multi-byte rune so len() exceeds 120.
	runeCount := len([]rune(preview))
	if runeCount > 120 {
		t.Errorf("preview rune count = %d, should be <= 120", runeCount)
	}
	if !strings.HasSuffix(preview, "…") {
		t.Errorf("preview should end with ellipsis, got %q", preview)
	}
}

func TestScanPreview_CountsEntireFile(t *testing.T) {
	// Even though preview stops at 200 lines, turn counting covers entire file.
	// Build a file with alternating user + assistant messages past line 200.
	var lines []string
	for i := 0; i < 125; i++ {
		lines = append(lines,
			userEntry(
				fmt.Sprintf("u%d", i),
				fmt.Sprintf("2025-01-15T10:%02d:%02dZ", (i*2)/60, (i*2)%60),
				fmt.Sprintf("question %d", i),
			),
			assistantEntry(
				fmt.Sprintf("a%d", i),
				fmt.Sprintf("2025-01-15T10:%02d:%02dZ", (i*2+1)/60, (i*2+1)%60),
				fmt.Sprintf("response %d", i),
			),
		)
	}

	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl", lines...)

	preview, count := parser.ScanSessionPreview(path)
	if preview != "question 0" {
		t.Errorf("preview = %q, want %q", preview, "question 0")
	}
	// 125 user turns + 125 AI turns = 250 conversation turns
	if count != 250 {
		t.Errorf("count = %d, want 250", count)
	}
}

// --- DiscoverProjectSessions tests ---

func TestDiscoverProjectSessions_FiltersGhosts(t *testing.T) {
	dir := t.TempDir()

	// Real session.
	writeJSONL(t, dir, "real-session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "Hello"),
		assistantEntry("a1", "2025-01-15T10:00:01Z", "Hi"),
	)
	// Ghost session (only snapshots).
	writeJSONL(t, dir, "ghost-session.jsonl",
		snapshotEntry("snap1"),
	)
	// Agent file (should be excluded).
	writeJSONL(t, dir, "agent_abc.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "agent msg"),
	)

	sessions, err := parser.DiscoverProjectSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 (ghost and agent should be filtered)", len(sessions))
	}
	if sessions[0].SessionID != "real-session" {
		t.Errorf("session ID = %q, want %q", sessions[0].SessionID, "real-session")
	}
}

func TestDiscoverProjectSessions_SortedByModTime(t *testing.T) {
	dir := t.TempDir()

	// Write older session first.
	p1 := writeJSONL(t, dir, "older.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "Older session"),
	)
	// Write newer session.
	p2 := writeJSONL(t, dir, "newer.jsonl",
		userEntry("u1", "2025-01-15T11:00:00Z", "Newer session"),
	)

	// Force the mod times to be in the right order.
	older := mustStat(t, p1).ModTime()
	os.Chtimes(p2, older.Add(1), older.Add(1))

	sessions, err := parser.DiscoverProjectSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	if sessions[0].SessionID != "newer" {
		t.Errorf("first session = %q, want newer (most recent first)", sessions[0].SessionID)
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi
}

// --- ReadSessionIncremental tests ---

func TestReadSessionIncremental_FullRead(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "Hello"),
		assistantEntry("a1", "2025-01-15T10:00:01Z", "Hi"),
	)

	msgs, offset, err := parser.ReadSessionIncremental(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Errorf("len(msgs) = %d, want 2", len(msgs))
	}
	if offset == 0 {
		t.Error("offset should be > 0 after reading")
	}
}

func TestReadSessionIncremental_IncrementalRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	// Write initial content.
	line1 := userEntry("u1", "2025-01-15T10:00:00Z", "Hello")
	os.WriteFile(path, []byte(line1+"\n"), 0644)

	// First read.
	msgs1, offset1, err := parser.ReadSessionIncremental(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs1) != 1 {
		t.Fatalf("first read: len(msgs) = %d, want 1", len(msgs1))
	}

	// Append more content.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	line2 := assistantEntry("a1", "2025-01-15T10:00:01Z", "Hi")
	f.WriteString(line2 + "\n")
	f.Close()

	// Incremental read from previous offset.
	msgs2, offset2, err := parser.ReadSessionIncremental(path, offset1)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs2) != 1 {
		t.Fatalf("incremental read: len(msgs) = %d, want 1", len(msgs2))
	}
	if offset2 <= offset1 {
		t.Errorf("offset didn't advance: %d <= %d", offset2, offset1)
	}
}

func TestReadSessionIncremental_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	os.WriteFile(path, []byte{}, 0644)

	msgs, offset, err := parser.ReadSessionIncremental(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d, want 0", len(msgs))
	}
	if offset != 0 {
		t.Errorf("offset = %d, want 0 for empty file", offset)
	}
}

func TestReadSessionIncremental_NoNewContent(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONL(t, dir, "session.jsonl",
		userEntry("u1", "2025-01-15T10:00:00Z", "Hello"),
	)

	// Read everything.
	_, offset1, _ := parser.ReadSessionIncremental(path, 0)

	// Read again from the end -- nothing new.
	msgs, offset2, err := parser.ReadSessionIncremental(path, offset1)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d, want 0 (no new content)", len(msgs))
	}
	if offset2 != offset1 {
		t.Errorf("offset changed: %d != %d", offset2, offset1)
	}
}
