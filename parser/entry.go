package parser

import "encoding/json"

// Entry represents a raw JSONL line from a Claude Code session file.
// Fields map directly to the on-disk format at ~/.claude/projects/{project}/{session}.jsonl.
type Entry struct {
	Type        string  `json:"type"`
	UUID        string  `json:"uuid"`
	ParentUUID  *string `json:"parentUuid"`
	Timestamp   string  `json:"timestamp"`
	IsSidechain bool    `json:"isSidechain"`
	IsMeta      bool    `json:"isMeta"`
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
		// Content blocks when content is an array (assistant messages).
		// We also parse these in extractors; the raw Content field covers both string and array forms.
		ID string `json:"id"`
	} `json:"message"`
}

// ParseEntry parses a single JSONL line into an Entry.
// Returns false if the JSON is invalid or the entry has no UUID.
func ParseEntry(line []byte) (Entry, bool) {
	var e Entry
	if err := json.Unmarshal(line, &e); err != nil {
		return Entry{}, false
	}
	if e.UUID == "" {
		return Entry{}, false
	}
	return e, true
}
