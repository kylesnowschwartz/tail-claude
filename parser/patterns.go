package parser

import (
	"encoding/json"
	"regexp"
)

// Tag constants matching the TypeScript messageTags.ts.
const (
	localCommandStdoutTag = "<local-command-stdout>"
	localCommandStderrTag = "<local-command-stderr>"
)

// Command extraction regexes -- used by sanitize.go and session.go.
var (
	reCommandName = regexp.MustCompile(`<command-name>/([^<]+)</command-name>`)
	reCommandArgs = regexp.MustCompile(`<command-args>([^<]*)</command-args>`)
	reStdout      = regexp.MustCompile(`(?is)<local-command-stdout>(.*?)</local-command-stdout>`)
	reStderr      = regexp.MustCompile(`(?is)<local-command-stderr>(.*?)</local-command-stderr>`)
)

// Teammate message regexes -- used by classify.go and session.go.
var (
	teammateMessageRe = regexp.MustCompile(`^<teammate-message\s+teammate_id="[^"]+"`)
	teammateIDRe      = regexp.MustCompile(`teammate_id="([^"]+)"`)
	teammateContentRe = regexp.MustCompile(`(?s)<teammate-message[^>]*>(.*)</teammate-message>`)
)

// contentBlockJSON is the common shape for partially unmarshaling JSONL content blocks.
// Different callers use different subsets of fields; unused fields unmarshal to zero values.
type contentBlockJSON struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// textBlockJSON is a minimal content block for extracting text content.
// Cheaper to unmarshal when only type and text are needed.
type textBlockJSON struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
