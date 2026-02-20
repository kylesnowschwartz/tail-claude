package parser

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// ClassifiedMsg is a sealed interface representing the three message categories
// that survive noise filtering. Noise entries are dropped, not classified.
type ClassifiedMsg interface {
	classifiedMsg()
	timestamp() time.Time
}

// UserMsg represents genuine user input that starts a new request cycle.
type UserMsg struct {
	Timestamp time.Time
	Text      string // sanitized display text
	IsSlash   bool   // was a /command
	SlashName string // e.g. "model" if IsSlash
}

func (UserMsg) classifiedMsg()         {}
func (m UserMsg) timestamp() time.Time { return m.Timestamp }

// AIMsg represents assistant responses and internal flow messages (tool results).
type AIMsg struct {
	Timestamp  time.Time
	Model      string
	Text       string // sanitized text content
	Thinking   int    // count of thinking blocks
	ToolCalls  []ToolCall
	Usage      Usage
	StopReason string
	IsMeta     bool // internal user message (tool results)
}

func (AIMsg) classifiedMsg()         {}
func (m AIMsg) timestamp() time.Time { return m.Timestamp }

// ToolCall is a tool invocation extracted from an assistant message.
type ToolCall struct {
	ID   string
	Name string
}

// Usage holds token counts for a single API response.
type Usage struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
}

// TotalTokens returns the sum of all token fields.
func (u Usage) TotalTokens() int {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens
}

// SystemMsg represents command output (slash command results).
type SystemMsg struct {
	Timestamp time.Time
	Output    string // extracted from <local-command-stdout>/<local-command-stderr>
}

func (SystemMsg) classifiedMsg()         {}
func (m SystemMsg) timestamp() time.Time { return m.Timestamp }

// --- Hard noise detection ---

// noiseEntryTypes are entry types that never produce visible messages.
var noiseEntryTypes = map[string]bool{
	"system":                true,
	"summary":               true,
	"file-history-snapshot": true,
	"queue-operation":       true,
	"progress":              true,
}

// hardNoiseTags are XML tags whose sole presence means the entire message is noise.
var hardNoiseTags = []string{
	"<local-command-caveat>",
	"<system-reminder>",
}

// systemOutputTags exclude a user message from being a "user chunk" starter.
var systemOutputTags = []string{
	localCommandStderrTag,
	localCommandStdoutTag,
	"<local-command-caveat>",
	"<system-reminder>",
}

var emptyStdout = "<local-command-stdout></local-command-stdout>"
var emptyStderr = "<local-command-stderr></local-command-stderr>"

var teammateMessageRe = regexp.MustCompile(`^<teammate-message\s+teammate_id="[^"]+"`)

// Classify maps a raw Entry to one of the three ClassifiedMsg types.
// Returns false for noise entries (filtered out) and sidechain messages.
func Classify(e Entry) (ClassifiedMsg, bool) {
	// Filter sidechain messages - we only care about main thread.
	if e.IsSidechain {
		return nil, false
	}

	ts := parseTimestamp(e.Timestamp)

	// 1. Hard noise: structural metadata types.
	if noiseEntryTypes[e.Type] {
		return nil, false
	}

	// Hard noise: synthetic assistant messages.
	if e.Type == "assistant" && e.Message.Model == "<synthetic>" {
		return nil, false
	}

	// Get string content for user-type checks.
	contentStr := ExtractText(e.Message.Content)

	// Hard noise checks for user-type entries.
	if e.Type == "user" {
		trimmed := strings.TrimSpace(contentStr)

		// Wrapped entirely in a hard noise tag.
		for _, tag := range hardNoiseTags {
			closeTag := strings.Replace(tag, "<", "</", 1)
			if strings.HasPrefix(trimmed, tag) && strings.HasSuffix(trimmed, closeTag) {
				return nil, false
			}
		}

		// Empty command output.
		if trimmed == emptyStdout || trimmed == emptyStderr {
			return nil, false
		}

		// Interruption messages.
		if strings.HasPrefix(trimmed, "[Request interrupted by user") {
			return nil, false
		}

		// Array content with single interruption text block.
		if isArrayInterruption(e.Message.Content) {
			return nil, false
		}

		// Teammate messages are filtered (rendered separately in the TUI).
		if teammateMessageRe.MatchString(trimmed) {
			return nil, false
		}
	}

	// 2. System message: user entry starting with command output tag.
	if e.Type == "user" {
		trimmed := strings.TrimSpace(contentStr)
		if strings.HasPrefix(trimmed, localCommandStdoutTag) || strings.HasPrefix(trimmed, localCommandStderrTag) {
			return SystemMsg{
				Timestamp: ts,
				Output:    ExtractCommandOutput(contentStr),
			}, true
		}
	}

	// 3. User message: type=user, not isMeta, has real content, not system output.
	if e.Type == "user" && !e.IsMeta {
		trimmed := strings.TrimSpace(contentStr)

		// Exclude messages starting with system output tags.
		excluded := false
		for _, tag := range systemOutputTags {
			if strings.HasPrefix(trimmed, tag) {
				excluded = true
				break
			}
		}

		if !excluded && hasUserContent(e.Message.Content, contentStr) {
			text := SanitizeContent(contentStr)
			isSlash, slashName := detectSlash(contentStr)
			return UserMsg{
				Timestamp: ts,
				Text:      text,
				IsSlash:   isSlash,
				SlashName: slashName,
			}, true
		}
	}

	// 4. AI message: everything else (assistant messages, internal user messages with tool results).
	if e.Type == "assistant" {
		thinking, toolCalls := extractAssistantDetails(e.Message.Content)
		stopReason := ""
		if e.Message.StopReason != nil {
			stopReason = *e.Message.StopReason
		}
		return AIMsg{
			Timestamp: ts,
			Model:     e.Message.Model,
			Text:      SanitizeContent(ExtractText(e.Message.Content)),
			Thinking:  thinking,
			ToolCalls: toolCalls,
			Usage: Usage{
				InputTokens:         e.Message.Usage.InputTokens,
				OutputTokens:        e.Message.Usage.OutputTokens,
				CacheReadTokens:     e.Message.Usage.CacheReadInputTokens,
				CacheCreationTokens: e.Message.Usage.CacheCreationInputTokens,
			},
			StopReason: stopReason,
		}, true
	}

	// Internal user messages (isMeta=true, tool results) -> AI message.
	if e.Type == "user" && e.IsMeta {
		return AIMsg{
			Timestamp: ts,
			Text:      contentStr,
			IsMeta:    true,
		}, true
	}

	// Fallback: remaining user messages that weren't caught above -> AI message.
	return AIMsg{
		Timestamp: ts,
		Text:      contentStr,
		IsMeta:    true,
	}, true
}

// parseTimestamp parses an ISO 8601 timestamp, falling back to now on failure.
func parseTimestamp(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try the format without timezone that Claude sometimes emits.
	if t, err := time.Parse("2006-01-02T15:04:05.999999999", s); err == nil {
		return t
	}
	return time.Now()
}

// detectSlash checks for <command-name>/xxx</command-name> and returns (true, "xxx").
func detectSlash(content string) (bool, string) {
	m := reCommandName.FindStringSubmatch(content)
	if m == nil {
		return false, ""
	}
	return true, strings.TrimSpace(m[1])
}

// hasUserContent checks whether the raw content has real user text or images.
// String content is always considered real (already checked for system tags).
// Array content needs at least one text or image block.
func hasUserContent(raw json.RawMessage, strContent string) bool {
	// If ExtractText produced a non-empty string and raw is a JSON string, it's real.
	if len(raw) > 0 && raw[0] == '"' {
		return strings.TrimSpace(strContent) != ""
	}

	// Array content: check for text or image blocks.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return false
	}
	for _, b := range blocks {
		if b.Type == "text" || b.Type == "image" {
			return true
		}
	}
	return false
}

// isArrayInterruption checks if content is an array with a single text block
// starting with "[Request interrupted by user".
func isArrayInterruption(raw json.RawMessage) bool {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return false
	}
	if len(blocks) == 1 && blocks[0].Type == "text" && strings.HasPrefix(blocks[0].Text, "[Request interrupted by user") {
		return true
	}
	return false
}

// extractAssistantDetails pulls thinking count and tool calls from an assistant
// message's content blocks.
func extractAssistantDetails(raw json.RawMessage) (int, []ToolCall) {
	var blocks []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return 0, nil
	}

	thinking := 0
	var calls []ToolCall
	for _, b := range blocks {
		switch b.Type {
		case "thinking":
			thinking++
		case "tool_use":
			if b.ID != "" && b.Name != "" {
				calls = append(calls, ToolCall{ID: b.ID, Name: b.Name})
			}
		}
	}
	return thinking, calls
}
