package parser

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DebugLevel represents the severity of a debug log entry.
type DebugLevel int

const (
	LevelDebug DebugLevel = iota
	LevelWarn
	LevelError
)

// String returns the human-readable label for a debug level.
func (l DebugLevel) String() string {
	switch l {
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "DEBUG"
	}
}

// DebugEntry represents a single parsed entry from a Claude Code debug log.
type DebugEntry struct {
	Timestamp time.Time
	Level     DebugLevel
	Category  string // "init", "API:auth", "MCP", "hooks", etc. Empty if none.
	Message   string // first line after level+category
	Extra     string // continuation lines (JSON, stack traces). Empty for single-line entries.
	LineNum   int    // 1-based line number in source file
	Count     int    // 1 normally; >1 when consecutive duplicates are collapsed
}

// HasExtra returns true when the entry has multi-line continuation content.
func (e DebugEntry) HasExtra() bool {
	return e.Extra != ""
}

// ExtraLineCount returns the number of continuation lines in Extra.
func (e DebugEntry) ExtraLineCount() int {
	if e.Extra == "" {
		return 0
	}
	return strings.Count(e.Extra, "\n") + 1
}

// debugLineRe matches the start of a timestamped debug line.
// Format: 2026-02-25T02:03:45.579Z [LEVEL] ...
var debugLineRe = regexp.MustCompile(
	`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z)\s+\[(DEBUG|WARN|ERROR)\]\s+(.*)$`)

// debugCategoryRe extracts an optional [category] prefix from the message body.
// Matches: [init], [MCP], [hooks], [API:auth], [Claude in Chrome], etc.
var debugCategoryRe = regexp.MustCompile(`^\[([^\]]+)\]\s*(.*)$`)

// isTimestampLine returns true if a line starts with a debug log timestamp.
// Used to distinguish new entries from continuation lines (stack traces, JSON).
func isTimestampLine(line string) bool {
	// Fast path: check length and first char before regex.
	if len(line) < 24 || line[0] < '0' || line[0] > '9' {
		return false
	}
	return debugLineRe.MatchString(line)
}

// ParseDebugLine parses a single timestamped debug line into a DebugEntry.
// Returns false for continuation lines (lines that don't start with a timestamp).
func ParseDebugLine(line string) (DebugEntry, bool) {
	m := debugLineRe.FindStringSubmatch(line)
	if m == nil {
		return DebugEntry{}, false
	}

	ts, err := time.Parse("2006-01-02T15:04:05.000Z", m[1])
	if err != nil {
		return DebugEntry{}, false
	}

	level := parseLevel(m[2])
	body := m[3]

	// Extract optional category from the message body.
	var category string
	if cm := debugCategoryRe.FindStringSubmatch(body); cm != nil {
		category = cm[1]
		body = cm[2]
	}

	return DebugEntry{
		Timestamp: ts,
		Level:     level,
		Category:  category,
		Message:   body,
		Count:     1,
	}, true
}

// ReadDebugLog reads a debug log file from the beginning and returns all entries.
// Continuation lines are joined into the previous entry's Extra field.
// Returns entries, final file offset, and any error.
func ReadDebugLog(path string) ([]DebugEntry, int64, error) {
	return ReadDebugLogIncremental(path, 0)
}

// ReadDebugLogIncremental reads new lines from a debug log starting at the
// given byte offset. Continuation lines (lines that don't start with a
// timestamp) are appended to the previous entry's Extra field.
func ReadDebugLogIncremental(path string, offset int64) ([]DebugEntry, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	if _, err := f.Seek(offset, 0); err != nil {
		return nil, offset, err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1*1024*1024)

	var entries []DebugEntry
	bytesRead := offset
	lineNum := countLinesBeforeOffset(path, offset)

	for scanner.Scan() {
		line := scanner.Text()
		bytesRead += int64(len(scanner.Bytes())) + 1 // +1 for \n
		lineNum++

		if entry, ok := ParseDebugLine(line); ok {
			entry.LineNum = lineNum
			entries = append(entries, entry)
		} else if len(entries) > 0 && line != "" {
			// Continuation line: append to previous entry's Extra.
			last := &entries[len(entries)-1]
			if last.Extra == "" {
				last.Extra = line
			} else {
				last.Extra += "\n" + line
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return entries, bytesRead, err
	}

	return entries, bytesRead, nil
}

// CollapseDuplicates merges consecutive entries with identical Message text
// into a single entry with Count reflecting the run length. Extra content
// prevents collapsing -- multi-line entries are always kept separate.
func CollapseDuplicates(entries []DebugEntry) []DebugEntry {
	if len(entries) == 0 {
		return entries
	}

	result := make([]DebugEntry, 0, len(entries))
	current := entries[0]

	for i := 1; i < len(entries); i++ {
		e := entries[i]
		if e.Message == current.Message && e.Extra == "" && current.Extra == "" {
			current.Count++
		} else {
			result = append(result, current)
			current = e
		}
	}
	result = append(result, current)

	return result
}

// FilterByLevel returns entries at or above the given minimum level.
func FilterByLevel(entries []DebugEntry, minLevel DebugLevel) []DebugEntry {
	if minLevel == LevelDebug {
		return entries // no filtering needed
	}

	result := make([]DebugEntry, 0, len(entries)/2)
	for _, e := range entries {
		if e.Level >= minLevel {
			result = append(result, e)
		}
	}
	return result
}

// FilterByText returns entries whose Message or Category contains the given
// substring (case-insensitive). Empty query returns all entries unchanged.
func FilterByText(entries []DebugEntry, query string) []DebugEntry {
	if query == "" {
		return entries
	}
	q := strings.ToLower(query)
	result := make([]DebugEntry, 0, len(entries)/2)
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Message), q) ||
			strings.Contains(strings.ToLower(e.Category), q) ||
			strings.Contains(strings.ToLower(e.Extra), q) {
			result = append(result, e)
		}
	}
	return result
}

// DebugLogPath returns the debug log file path for a given session JSONL path.
// Claude Code stores debug logs at ~/.claude/debug/{session-uuid}.txt.
// Returns empty string if the debug file doesn't exist.
func DebugLogPath(sessionPath string) string {
	// Extract the session UUID from the filename (strip .jsonl extension).
	base := filepath.Base(sessionPath)
	uuid := strings.TrimSuffix(base, ".jsonl")
	if uuid == "" || uuid == base {
		return "" // not a .jsonl file
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	debugPath := filepath.Join(home, ".claude", "debug", uuid+".txt")
	if _, err := os.Stat(debugPath); err != nil {
		return ""
	}

	return debugPath
}

// parseLevel converts a level string to DebugLevel.
func parseLevel(s string) DebugLevel {
	switch s {
	case "WARN":
		return LevelWarn
	case "ERROR":
		return LevelError
	default:
		return LevelDebug
	}
}

// countLinesBeforeOffset counts the number of newlines before the given byte
// offset in a file. Used to compute correct line numbers for incremental reads.
// Returns 0 on any error (conservative: line numbers will be off but not crash).
func countLinesBeforeOffset(path string, offset int64) int {
	if offset == 0 {
		return 0
	}

	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	buf := make([]byte, 32*1024)
	remaining := offset

	for remaining > 0 {
		toRead := int64(len(buf))
		if toRead > remaining {
			toRead = remaining
		}
		n, err := f.Read(buf[:toRead])
		if n > 0 {
			for i := 0; i < n; i++ {
				if buf[i] == '\n' {
					count++
				}
			}
			remaining -= int64(n)
		}
		if err != nil {
			break
		}
	}

	return count
}
