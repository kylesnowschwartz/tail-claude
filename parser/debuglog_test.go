package parser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDebugLine_AllLevels(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantOk   bool
		level    DebugLevel
		category string
		message  string
	}{
		{
			name:    "debug no category",
			line:    "2026-02-25T02:03:45.579Z [DEBUG] MDM settings load completed in 11ms",
			wantOk:  true,
			level:   LevelDebug,
			message: "MDM settings load completed in 11ms",
		},
		{
			name:     "debug with category",
			line:     "2026-02-25T02:03:45.661Z [DEBUG] [init] configureGlobalMTLS starting",
			wantOk:   true,
			level:    LevelDebug,
			category: "init",
			message:  "configureGlobalMTLS starting",
		},
		{
			name:    "warn",
			line:    "2026-02-25T02:03:45.731Z [WARN] Failed to parse YAML frontmatter",
			wantOk:  true,
			level:   LevelWarn,
			message: "Failed to parse YAML frontmatter",
		},
		{
			name:    "error",
			line:    "2026-02-25T02:03:45.712Z [ERROR] Error: Lock acquisition failed",
			wantOk:  true,
			level:   LevelError,
			message: "Error: Lock acquisition failed",
		},
		{
			name:     "compound category",
			line:     "2026-02-25T02:03:46.100Z [DEBUG] [API:auth] Token refresh completed",
			wantOk:   true,
			level:    LevelDebug,
			category: "API:auth",
			message:  "Token refresh completed",
		},
		{
			name:     "category with spaces",
			line:     "2026-02-25T02:03:45.680Z [DEBUG] [Claude in Chrome] Found chrome profiles",
			wantOk:   true,
			level:    LevelDebug,
			category: "Claude in Chrome",
			message:  "Found chrome profiles",
		},
		{
			name:   "continuation line (stack trace)",
			line:   "    at lPR (/$bunfs/root/claude:3631:2100)",
			wantOk: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOk: false,
		},
		{
			name:   "json continuation",
			line:   `  "expected": {"type": "object"}`,
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := ParseDebugLine(tt.line)
			if ok != tt.wantOk {
				t.Fatalf("ParseDebugLine ok = %v, want %v", ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if entry.Level != tt.level {
				t.Errorf("Level = %v, want %v", entry.Level, tt.level)
			}
			if entry.Category != tt.category {
				t.Errorf("Category = %q, want %q", entry.Category, tt.category)
			}
			if entry.Message != tt.message {
				t.Errorf("Message = %q, want %q", entry.Message, tt.message)
			}
			if entry.Count != 1 {
				t.Errorf("Count = %d, want 1", entry.Count)
			}
		})
	}
}

func TestParseDebugLine_Timestamp(t *testing.T) {
	entry, ok := ParseDebugLine("2026-02-25T02:03:45.579Z [DEBUG] test")
	if !ok {
		t.Fatal("expected ok")
	}
	want := time.Date(2026, 2, 25, 2, 3, 45, 579_000_000, time.UTC)
	if !entry.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want %v", entry.Timestamp, want)
	}
}

func TestReadDebugLog_MultiLineJoining(t *testing.T) {
	path := filepath.Join("testdata", "debug-sample.txt")
	entries, offset, err := ReadDebugLog(path)
	if err != nil {
		t.Fatalf("ReadDebugLog: %v", err)
	}
	if offset <= 0 {
		t.Error("expected positive offset")
	}

	// Find the ERROR entry with stack trace continuation
	var errorEntry *DebugEntry
	for i := range entries {
		if entries[i].Level == LevelError && entries[i].Message == "Error: NON-FATAL: Lock acquisition failed" {
			errorEntry = &entries[i]
			break
		}
	}
	if errorEntry == nil {
		t.Fatal("expected to find ERROR entry with stack trace")
	}
	if !errorEntry.HasExtra() {
		t.Error("ERROR entry should have Extra (stack trace)")
	}
	if errorEntry.ExtraLineCount() != 3 {
		t.Errorf("ERROR Extra lines = %d, want 3", errorEntry.ExtraLineCount())
	}

	// Find the hooks entry with JSON continuation
	var hooksEntry *DebugEntry
	for i := range entries {
		if entries[i].Category == "hooks" {
			hooksEntry = &entries[i]
			break
		}
	}
	if hooksEntry == nil {
		t.Fatal("expected to find hooks entry")
	}
	if !hooksEntry.HasExtra() {
		t.Error("hooks entry should have Extra (JSON block)")
	}
	// JSON block is { + 2 inner lines + } = 4 lines
	if hooksEntry.ExtraLineCount() != 4 {
		t.Errorf("hooks Extra lines = %d, want 4", hooksEntry.ExtraLineCount())
	}
}

func TestReadDebugLog_Categories(t *testing.T) {
	path := filepath.Join("testdata", "debug-sample.txt")
	entries, _, err := ReadDebugLog(path)
	if err != nil {
		t.Fatalf("ReadDebugLog: %v", err)
	}

	// Verify we found entries with various category formats.
	categories := make(map[string]bool)
	for _, e := range entries {
		if e.Category != "" {
			categories[e.Category] = true
		}
	}

	for _, want := range []string{"init", "STARTUP", "hooks", "MCP", "API:auth", "3P telemetry"} {
		if !categories[want] {
			t.Errorf("missing category %q, found: %v", want, categories)
		}
	}
}

func TestReadDebugLogIncremental(t *testing.T) {
	path := filepath.Join("testdata", "debug-sample.txt")

	// Read the full file first to get partway offset.
	allEntries, fullOffset, err := ReadDebugLog(path)
	if err != nil {
		t.Fatalf("ReadDebugLog: %v", err)
	}

	// Read from the end: should return no new entries.
	newEntries, newOffset, err := ReadDebugLogIncremental(path, fullOffset)
	if err != nil {
		t.Fatalf("ReadDebugLogIncremental: %v", err)
	}
	if len(newEntries) != 0 {
		t.Errorf("expected 0 entries from end, got %d", len(newEntries))
	}
	if newOffset != fullOffset {
		t.Errorf("offset changed: %d -> %d", fullOffset, newOffset)
	}

	// Verify total entry count is reasonable.
	if len(allEntries) < 10 {
		t.Errorf("expected at least 10 entries, got %d", len(allEntries))
	}
}

func TestCollapseDuplicates(t *testing.T) {
	entries := []DebugEntry{
		{Message: "unique entry", Count: 1},
		{Message: "detectFileEncoding failed", Count: 1},
		{Message: "detectFileEncoding failed", Count: 1},
		{Message: "detectFileEncoding failed", Count: 1},
		{Message: "another entry", Count: 1},
		{Message: "detectFileEncoding failed", Count: 1},
	}

	collapsed := CollapseDuplicates(entries)

	if len(collapsed) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(collapsed))
	}
	if collapsed[0].Message != "unique entry" || collapsed[0].Count != 1 {
		t.Errorf("entry 0: %q x%d", collapsed[0].Message, collapsed[0].Count)
	}
	if collapsed[1].Message != "detectFileEncoding failed" || collapsed[1].Count != 3 {
		t.Errorf("entry 1: %q x%d, want x3", collapsed[1].Message, collapsed[1].Count)
	}
	if collapsed[2].Message != "another entry" || collapsed[2].Count != 1 {
		t.Errorf("entry 2: %q x%d", collapsed[2].Message, collapsed[2].Count)
	}
	// Non-consecutive duplicate: separate entry, count 1.
	if collapsed[3].Message != "detectFileEncoding failed" || collapsed[3].Count != 1 {
		t.Errorf("entry 3: %q x%d, want x1", collapsed[3].Message, collapsed[3].Count)
	}
}

func TestCollapseDuplicates_SkipsMultiLine(t *testing.T) {
	entries := []DebugEntry{
		{Message: "same message", Extra: "details", Count: 1},
		{Message: "same message", Count: 1},
	}

	collapsed := CollapseDuplicates(entries)
	// Multi-line entry shouldn't collapse with single-line entry.
	if len(collapsed) != 2 {
		t.Fatalf("expected 2 entries (no collapse for multi-line), got %d", len(collapsed))
	}
}

func TestCollapseDuplicates_Empty(t *testing.T) {
	collapsed := CollapseDuplicates(nil)
	if len(collapsed) != 0 {
		t.Errorf("expected 0, got %d", len(collapsed))
	}
}

func TestFilterByLevel(t *testing.T) {
	entries := []DebugEntry{
		{Level: LevelDebug, Message: "debug1"},
		{Level: LevelWarn, Message: "warn1"},
		{Level: LevelDebug, Message: "debug2"},
		{Level: LevelError, Message: "error1"},
		{Level: LevelWarn, Message: "warn2"},
	}

	// All (debug threshold)
	all := FilterByLevel(entries, LevelDebug)
	if len(all) != 5 {
		t.Errorf("LevelDebug filter: got %d, want 5", len(all))
	}

	// Warn+
	warns := FilterByLevel(entries, LevelWarn)
	if len(warns) != 3 {
		t.Errorf("LevelWarn filter: got %d, want 3", len(warns))
	}
	for _, e := range warns {
		if e.Level < LevelWarn {
			t.Errorf("got debug entry in warn filter: %q", e.Message)
		}
	}

	// Error only
	errors := FilterByLevel(entries, LevelError)
	if len(errors) != 1 {
		t.Errorf("LevelError filter: got %d, want 1", len(errors))
	}
	if errors[0].Message != "error1" {
		t.Errorf("expected error1, got %q", errors[0].Message)
	}
}

func TestFilterByText(t *testing.T) {
	entries := []DebugEntry{
		{Level: LevelDebug, Category: "init", Message: "starting up"},
		{Level: LevelWarn, Category: "MCP", Message: "server timeout"},
		{Level: LevelDebug, Category: "API", Message: "request sent"},
		{Level: LevelError, Category: "", Message: "connection refused"},
		{Level: LevelDebug, Category: "init", Message: "loading config", Extra: "key=value\nfoo=bar"},
	}

	// Empty query returns all.
	all := FilterByText(entries, "")
	if len(all) != 5 {
		t.Errorf("empty query: got %d, want 5", len(all))
	}

	// Match message substring (case-insensitive).
	got := FilterByText(entries, "TIMEOUT")
	if len(got) != 1 || got[0].Message != "server timeout" {
		t.Errorf("'TIMEOUT' filter: got %v", got)
	}

	// Match category.
	got = FilterByText(entries, "init")
	if len(got) != 2 {
		t.Errorf("'init' category filter: got %d, want 2", len(got))
	}

	// Match within Extra content.
	got = FilterByText(entries, "foo=bar")
	if len(got) != 1 || got[0].Message != "loading config" {
		t.Errorf("'foo=bar' extra filter: got %v", got)
	}

	// No matches.
	got = FilterByText(entries, "zzzzz")
	if len(got) != 0 {
		t.Errorf("no-match filter: got %d, want 0", len(got))
	}
}

func TestDebugLogPath(t *testing.T) {
	// Create a temp debug file to test existence check.
	tmpDir := t.TempDir()

	// DebugLogPath uses os.UserHomeDir, so we can't easily test the full path.
	// Instead, test the UUID extraction logic indirectly.
	uuid := "abc12345-6789-0abc-def0-123456789abc"
	sessionPath := filepath.Join(tmpDir, uuid+".jsonl")

	// Without the debug file existing, should return empty.
	result := DebugLogPath(sessionPath)
	// We can't predict if ~/.claude/debug/{uuid}.txt exists, but at minimum
	// the function shouldn't crash.
	_ = result

	// Test with non-jsonl file.
	result = DebugLogPath(filepath.Join(tmpDir, "not-a-session.txt"))
	if result != "" {
		t.Errorf("expected empty for non-.jsonl file, got %q", result)
	}
}

func TestDebugLevelString(t *testing.T) {
	tests := []struct {
		level DebugLevel
		want  string
	}{
		{LevelDebug, "DEBUG"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("DebugLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestReadDebugLog_LineNumbers(t *testing.T) {
	path := filepath.Join("testdata", "debug-sample.txt")
	entries, _, err := ReadDebugLog(path)
	if err != nil {
		t.Fatalf("ReadDebugLog: %v", err)
	}

	// First entry should be line 1.
	if entries[0].LineNum != 1 {
		t.Errorf("first entry LineNum = %d, want 1", entries[0].LineNum)
	}

	// Line numbers should be monotonically increasing.
	for i := 1; i < len(entries); i++ {
		if entries[i].LineNum <= entries[i-1].LineNum {
			t.Errorf("entry %d LineNum (%d) not > entry %d LineNum (%d)",
				i, entries[i].LineNum, i-1, entries[i-1].LineNum)
		}
	}
}

func TestReadDebugLog_IntegrationWithCollapseAndFilter(t *testing.T) {
	path := filepath.Join("testdata", "debug-sample.txt")
	entries, _, err := ReadDebugLog(path)
	if err != nil {
		t.Fatalf("ReadDebugLog: %v", err)
	}

	// Collapse duplicates (detectFileEncoding spam).
	collapsed := CollapseDuplicates(entries)
	if len(collapsed) >= len(entries) {
		t.Error("CollapseDuplicates should reduce entry count (detectFileEncoding duplicates)")
	}

	// Filter to WARN+.
	filtered := FilterByLevel(collapsed, LevelWarn)
	for _, e := range filtered {
		if e.Level < LevelWarn {
			t.Errorf("found DEBUG entry after WARN filter: %q", e.Message)
		}
	}

	// Verify we still have the expected WARN and ERROR entries.
	hasWarn, hasError := false, false
	for _, e := range filtered {
		if e.Level == LevelWarn {
			hasWarn = true
		}
		if e.Level == LevelError {
			hasError = true
		}
	}
	if !hasWarn {
		t.Error("expected WARN entries in filtered output")
	}
	if !hasError {
		t.Error("expected ERROR entries in filtered output")
	}
}

func TestIsTimestampLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"2026-02-25T02:03:45.579Z [DEBUG] test", true},
		{"    at lPR (/$bunfs/root/claude:3631:2100)", false},
		{"", false},
		{`{"key": "value"}`, false},
		{"not a timestamp", false},
	}
	for _, tt := range tests {
		if got := isTimestampLine(tt.line); got != tt.want {
			t.Errorf("isTimestampLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestReadDebugLog_EmptyFile(t *testing.T) {
	// Create an empty temp file.
	f, err := os.CreateTemp(t.TempDir(), "debug-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	entries, offset, err := ReadDebugLog(f.Name())
	if err != nil {
		t.Fatalf("ReadDebugLog: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
	if offset != 0 {
		t.Errorf("expected offset 0, got %d", offset)
	}
}
