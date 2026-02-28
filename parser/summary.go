package parser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

// ellipsis is the Unicode horizontal ellipsis used for text truncation.
const ellipsis = "\u2026"

// ToolSummary generates a human-readable summary for a tool call.
// Returns the tool name as fallback when input is nil or unparseable.
// Ported from claude-devtools toolSummaryHelpers.ts.
func ToolSummary(name string, input json.RawMessage) string {
	if len(input) == 0 {
		return name
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return name
	}

	switch name {
	case "Read":
		return summaryRead(fields)
	case "Write":
		return summaryWrite(fields)
	case "Edit":
		return summaryEdit(fields)
	case "Bash":
		return summaryBash(fields)
	case "Grep":
		return summaryGrep(fields)
	case "Glob":
		return summaryGlob(fields)
	case "Task", "Agent":
		return summaryTask(fields)
	case "LSP":
		return summaryLSP(fields)
	case "WebFetch":
		return summaryWebFetch(fields)
	case "WebSearch":
		return summaryWebSearch(fields)
	case "TodoWrite":
		return summaryTodoWrite(fields)
	case "NotebookEdit":
		return summaryNotebookEdit(fields)
	case "TaskCreate":
		return summaryTaskCreate(fields)
	case "TaskUpdate":
		return summaryTaskUpdate(fields)
	case "SendMessage":
		return summarySendMessage(fields)
	default:
		return summaryDefault(name, fields)
	}
}

// --- Per-tool summary implementations ---

func summaryRead(f map[string]json.RawMessage) string {
	fp := getString(f, "file_path")
	if fp == "" {
		return "Read"
	}
	short := ShortPath(fp, 2)

	limit := getNumber(f, "limit")
	if limit > 0 {
		offset := getNumber(f, "offset")
		if offset == 0 {
			offset = 1
		}
		return fmt.Sprintf("%s - lines %d-%d", short, offset, offset+limit-1)
	}
	return short
}

func summaryWrite(f map[string]json.RawMessage) string {
	fp := getString(f, "file_path")
	if fp == "" {
		return "Write"
	}
	short := ShortPath(fp, 2)

	content := getString(f, "content")
	if content != "" {
		lines := len(strings.Split(content, "\n"))
		return fmt.Sprintf("%s - %d lines", short, lines)
	}
	return short
}

func summaryEdit(f map[string]json.RawMessage) string {
	fp := getString(f, "file_path")
	if fp == "" {
		return "Edit"
	}
	short := ShortPath(fp, 2)

	oldStr := getString(f, "old_string")
	newStr := getString(f, "new_string")
	if oldStr != "" && newStr != "" {
		oldLines := len(strings.Split(oldStr, "\n"))
		newLines := len(strings.Split(newStr, "\n"))
		if oldLines == newLines {
			s := ""
			if oldLines > 1 {
				s = "s"
			}
			return fmt.Sprintf("%s - %d line%s", short, oldLines, s)
		}
		return fmt.Sprintf("%s - %d -> %d lines", short, oldLines, newLines)
	}
	return short
}

func summaryBash(f map[string]json.RawMessage) string {
	desc := getString(f, "description")
	cmd := getString(f, "command")

	if desc != "" && cmd != "" {
		return Truncate(desc+": "+cmd, 60)
	}
	if desc != "" {
		return Truncate(desc, 60)
	}
	if cmd != "" {
		return Truncate(cmd, 60)
	}
	return "Bash"
}

func summaryGrep(f map[string]json.RawMessage) string {
	pattern := getString(f, "pattern")
	if pattern == "" {
		return "Grep"
	}
	patStr := `"` + Truncate(pattern, 30) + `"`

	if glob := getString(f, "glob"); glob != "" {
		return patStr + " in " + glob
	}
	if p := getString(f, "path"); p != "" {
		return patStr + " in " + filepath.Base(p)
	}
	return patStr
}

func summaryGlob(f map[string]json.RawMessage) string {
	pattern := getString(f, "pattern")
	if pattern == "" {
		return "Glob"
	}
	patStr := `"` + Truncate(pattern, 30) + `"`

	if p := getString(f, "path"); p != "" {
		return patStr + " in " + filepath.Base(p)
	}
	return patStr
}

func summaryTask(f map[string]json.RawMessage) string {
	desc := getString(f, "description")
	if desc == "" {
		desc = getString(f, "prompt")
	}
	subType := getString(f, "subagentType")

	typePrefix := ""
	if subType != "" {
		typePrefix = subType + " - "
	}
	if desc != "" {
		return typePrefix + Truncate(desc, 40)
	}
	if subType != "" {
		return subType
	}
	return "Task"
}

func summaryLSP(f map[string]json.RawMessage) string {
	op := getString(f, "operation")
	if op == "" {
		return "LSP"
	}
	if fp := getString(f, "filePath"); fp != "" {
		return op + " - " + filepath.Base(fp)
	}
	return op
}

func summaryWebFetch(f map[string]json.RawMessage) string {
	rawURL := getString(f, "url")
	if rawURL == "" {
		return "WebFetch"
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return Truncate(rawURL, 50)
	}
	return Truncate(u.Hostname()+u.Path, 50)
}

func summaryWebSearch(f map[string]json.RawMessage) string {
	q := getString(f, "query")
	if q == "" {
		return "WebSearch"
	}
	return `"` + Truncate(q, 40) + `"`
}

func summaryTodoWrite(f map[string]json.RawMessage) string {
	raw, ok := f["todos"]
	if !ok {
		return "TodoWrite"
	}
	var todos []json.RawMessage
	if err := json.Unmarshal(raw, &todos); err != nil {
		return "TodoWrite"
	}
	s := "s"
	if len(todos) == 1 {
		s = ""
	}
	return fmt.Sprintf("%d item%s", len(todos), s)
}

func summaryNotebookEdit(f map[string]json.RawMessage) string {
	nbPath := getString(f, "notebook_path")
	if nbPath == "" {
		return "NotebookEdit"
	}
	base := filepath.Base(nbPath)
	if mode := getString(f, "edit_mode"); mode != "" {
		return mode + " - " + base
	}
	return base
}

func summaryTaskCreate(f map[string]json.RawMessage) string {
	if subj := getString(f, "subject"); subj != "" {
		return Truncate(subj, 50)
	}
	return "Create task"
}

func summaryTaskUpdate(f map[string]json.RawMessage) string {
	var parts []string
	if id := getString(f, "taskId"); id != "" {
		parts = append(parts, "#"+id)
	}
	if status := getString(f, "status"); status != "" {
		parts = append(parts, status)
	}
	if owner := getString(f, "owner"); owner != "" {
		parts = append(parts, "-> "+owner)
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}
	return "Update task"
}

func summarySendMessage(f map[string]json.RawMessage) string {
	msgType := getString(f, "type")
	recipient := getString(f, "recipient")
	summary := getString(f, "summary")

	if msgType == "shutdown_request" && recipient != "" {
		return "Shutdown " + recipient
	}
	if msgType == "shutdown_response" {
		return "Shutdown response"
	}
	if msgType == "broadcast" {
		return "Broadcast: " + Truncate(summary, 30)
	}
	if recipient != "" {
		return "To " + recipient + ": " + Truncate(summary, 30)
	}
	return "Send message"
}

func summaryDefault(name string, f map[string]json.RawMessage) string {
	if len(f) == 0 {
		return name
	}

	// Try common parameter names in order.
	for _, key := range []string{"name", "path", "file", "query", "command"} {
		if v := getString(f, key); v != "" {
			return Truncate(v, 50)
		}
	}

	// Fall back to first string value (sorted keys for deterministic output).
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var s string
		if err := json.Unmarshal(f[k], &s); err == nil && s != "" {
			return Truncate(s, 40)
		}
	}
	return name
}

// --- Helpers ---

// ShortPath returns the last n segments of a file path.
// Uses forward slashes for normalization. Returns the full path
// if it has fewer than n segments.
func ShortPath(fullPath string, n int) string {
	normalized := filepath.ToSlash(fullPath)
	parts := strings.Split(normalized, "/")
	// Filter out empty segments (leading slash produces one).
	var segments []string
	for _, p := range parts {
		if p != "" {
			segments = append(segments, p)
		}
	}
	if len(segments) <= n {
		return strings.Join(segments, "/")
	}
	return strings.Join(segments[len(segments)-n:], "/")
}

// getString extracts a string field from a raw JSON map. Returns "" if missing or wrong type.
func getString(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// getNumber extracts a numeric field from a raw JSON map. Returns 0 if missing or wrong type.
func getNumber(fields map[string]json.RawMessage, key string) int {
	raw, ok := fields[key]
	if !ok {
		return 0
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	return int(n)
}

// Truncate shortens a string to maxLen runes, appending an ellipsis if truncated.
// The result is exactly maxLen runes when truncation occurs.
// Collapses newlines to spaces since summaries are single-line display strings.
func Truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + ellipsis
}

// TruncateWord shortens a string to maxLen runes, breaking at the nearest
// preceding word boundary (space). Searches up to 20 characters back from
// the cut point. Falls back to hard truncation if no space is found.
func TruncateWord(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	cutoff := maxLen - 1
	searchStart := cutoff - 20
	if searchStart < 0 {
		searchStart = 0
	}
	for i := cutoff; i >= searchStart; i-- {
		if runes[i] == ' ' {
			return string(runes[:i]) + ellipsis
		}
	}
	return string(runes[:cutoff]) + ellipsis
}
