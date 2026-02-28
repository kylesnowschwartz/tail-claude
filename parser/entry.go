package parser

import "encoding/json"

// Entry represents a raw JSONL line from a Claude Code session file.
// Fields map directly to the on-disk format at ~/.claude/projects/{project}/{session}.jsonl.
type Entry struct {
	Type        string `json:"type"`
	UUID        string `json:"uuid"`
	Timestamp   string `json:"timestamp"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Message     struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		Model      string          `json:"model"`
		StopReason *string         `json:"stop_reason"`
		Usage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`

	// Session-level metadata. Present on most entry types.
	Cwd            string `json:"cwd"`
	GitBranch      string `json:"gitBranch"`
	PermissionMode string `json:"permissionMode"` // "default", "acceptEdits", "bypassPermissions", "plan"

	// Tool result metadata (present on isMeta user entries for tool results).
	// ToolUseResult holds structured output from the tool execution. For
	// regular tools this is a JSON object (agentId, status, usage, etc.);
	// for MCP tools it can be a JSON array (the raw tool output).
	// Stored as RawMessage to tolerate both shapes without breaking ParseEntry.
	// Use ToolUseResultMap() to access it as key-value pairs when needed.
	ToolUseResult   json.RawMessage `json:"toolUseResult"`
	SourceToolUseID string          `json:"sourceToolUseID"`

	// Summary entries (type=summary) use leafUuid instead of uuid and carry
	// the compression title in Summary rather than message.content.
	LeafUUID string `json:"leafUuid"`
	Summary  string `json:"summary"`
}

// ToolUseResultMap attempts to parse ToolUseResult as a JSON object.
// Returns nil if ToolUseResult is absent, empty, or a non-object type (e.g.
// the JSON array that MCP tools produce).
func (e Entry) ToolUseResultMap() map[string]json.RawMessage {
	if len(e.ToolUseResult) == 0 || e.ToolUseResult[0] != '{' {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(e.ToolUseResult, &m) != nil {
		return nil
	}
	return m
}

// ParseEntry parses a single JSONL line into an Entry.
// Returns false if the JSON is invalid or the entry has no UUID.
func ParseEntry(line []byte) (Entry, bool) {
	var e Entry
	if err := json.Unmarshal(line, &e); err != nil {
		return Entry{}, false
	}
	// Summary entries use leafUuid instead of uuid.
	if e.UUID == "" && e.LeafUUID == "" {
		return Entry{}, false
	}
	return e, true
}
