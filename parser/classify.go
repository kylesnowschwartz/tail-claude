package parser

import (
	"encoding/json"
	"strings"
	"time"
)

// ClassifiedMsg is a sealed interface representing the message categories
// that survive noise filtering. Noise entries are dropped, not classified.
type ClassifiedMsg interface {
	classifiedMsg()
}

// UserMsg represents genuine user input that starts a new request cycle.
type UserMsg struct {
	Timestamp      time.Time
	Text           string // sanitized display text
	PermissionMode string // "default", "acceptEdits", "bypassPermissions", "plan"; empty if not present
}

func (UserMsg) classifiedMsg() {}

// ContentBlock represents a single content block from an assistant or tool result message.
type ContentBlock struct {
	Type       string          // "thinking", "text", "tool_use", "tool_result", "teammate"
	Text       string          // thinking or text content
	ToolID     string          // tool_use: call ID; tool_result: tool_use_id
	ToolName   string          // tool_use only
	ToolInput  json.RawMessage // tool_use only
	Content    string          // tool_result content (stringified)
	IsError    bool            // tool_result only
	TeammateID string          // teammate only
}

// AIMsg represents assistant responses and internal flow messages (tool results).
type AIMsg struct {
	Timestamp     time.Time
	Model         string
	Text          string // sanitized text content
	ThinkingCount int    // count of thinking blocks
	ToolCalls     []ToolCall
	Blocks        []ContentBlock // ordered content blocks, nil until populated
	Usage         Usage
	StopReason    string
	IsMeta        bool // internal user message (tool results)
}

func (AIMsg) classifiedMsg() {}

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

// SystemMsg represents command output (slash command results, bash mode, task notifications).
type SystemMsg struct {
	Timestamp time.Time
	Output    string // extracted from stdout/stderr/notification tags
	IsError   bool   // true when stderr is non-empty or task was killed
}

func (SystemMsg) classifiedMsg() {}

// TeammateMsg represents a message from a teammate agent.
// Folded into the AI turn during chunk building rather than starting a new user chunk.
type TeammateMsg struct {
	Timestamp  time.Time
	Text       string // sanitized inner content
	TeammateID string
}

func (TeammateMsg) classifiedMsg() {}

// CompactMsg represents a context compression boundary (summary entries).
// Displayed as a visual divider in the conversation timeline.
type CompactMsg struct {
	Timestamp time.Time
	Text      string
}

func (CompactMsg) classifiedMsg() {}

// --- Hard noise detection ---

// noiseEntryTypes are entry types that never produce visible messages.
// Note: "summary" is handled separately as CompactMsg, not noise.
var noiseEntryTypes = map[string]bool{
	"system":                true,
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
	bashStdoutTag,
	bashStderrTag,
	taskNotificationTag,
}

var emptyStdout = "<local-command-stdout></local-command-stdout>"
var emptyStderr = "<local-command-stderr></local-command-stderr>"

// Classify maps a raw Entry to one of the classified message types.
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

	// Summary entries become CompactMsg (context compression boundary).
	// The title lives in e.Summary, not message.content.
	if e.Type == "summary" {
		return CompactMsg{
			Timestamp: ts,
			Text:      e.Summary,
		}, true
	}

	// Hard noise: synthetic assistant messages.
	if e.Type == "assistant" && e.Message.Model == "<synthetic>" {
		return nil, false
	}

	// Get string content for user-type checks.
	contentStr := ExtractText(e.Message.Content)

	// Filter user-type noise (hard noise tags, empty output, interruptions).
	if e.Type == "user" && isUserNoise(e.Message.Content, contentStr) {
		return nil, false
	}

	// Teammate messages: classify as TeammateMsg.
	if e.Type == "user" {
		trimmed := strings.TrimSpace(contentStr)
		if teammateMessageRe.MatchString(trimmed) {
			teammateID := extractTeammateID(trimmed)
			text := SanitizeContent(extractTeammateContent(trimmed))
			return TeammateMsg{
				Timestamp:  ts,
				Text:       text,
				TeammateID: teammateID,
			}, true
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

		// Bash mode output (!bash inline execution).
		if strings.HasPrefix(trimmed, bashStdoutTag) || strings.HasPrefix(trimmed, bashStderrTag) {
			stderrContent := ""
			if m := reBashStderr.FindStringSubmatch(contentStr); m != nil {
				stderrContent = strings.TrimSpace(m[1])
			}
			return SystemMsg{
				Timestamp: ts,
				Output:    extractBashOutput(contentStr),
				IsError:   stderrContent != "",
			}, true
		}

		// Background task notifications.
		if strings.HasPrefix(trimmed, taskNotificationTag) {
			status := ""
			if m := reTaskNotifyStatus.FindStringSubmatch(contentStr); m != nil {
				status = strings.TrimSpace(m[1])
			}
			return SystemMsg{
				Timestamp: ts,
				Output:    extractTaskNotification(contentStr),
				IsError:   status == "killed",
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
			return UserMsg{
				Timestamp:      ts,
				Text:           SanitizeContent(contentStr),
				PermissionMode: e.PermissionMode,
			}, true
		}
	}

	// 4. AI message: everything else (assistant messages, internal user messages with tool results).
	if e.Type == "assistant" {
		thinking, toolCalls, blocks := extractAssistantDetails(e.Message.Content)
		stopReason := ""
		if e.Message.StopReason != nil {
			stopReason = *e.Message.StopReason
		}
		return AIMsg{
			Timestamp:     ts,
			Model:         e.Message.Model,
			Text:          SanitizeContent(ExtractText(e.Message.Content)),
			ThinkingCount: thinking,
			ToolCalls:     toolCalls,
			Blocks:        blocks,
			Usage: Usage{
				InputTokens:         e.Message.Usage.InputTokens,
				OutputTokens:        e.Message.Usage.OutputTokens,
				CacheReadTokens:     e.Message.Usage.CacheReadInputTokens,
				CacheCreationTokens: e.Message.Usage.CacheCreationInputTokens,
			},
			StopReason: stopReason,
		}, true
	}

	// Fallback: remaining user messages -> AI message.
	// Covers both isMeta=true entries (slash commands etc.) and tool_result
	// entries where isMeta is null in the JSONL. extractMetaBlocks handles both:
	// if the content has tool_result blocks it extracts them; otherwise it returns
	// a text fallback that mergeAIBuffer silently ignores.
	blocks := extractMetaBlocks(e.Message.Content, contentStr)
	return AIMsg{
		Timestamp: ts,
		Text:      contentStr,
		IsMeta:    true,
		Blocks:    blocks,
	}, true
}

// extractTeammateID extracts the teammate_id attribute from a teammate-message XML tag.
func extractTeammateID(s string) string {
	m := teammateIDRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}

// extractTeammateContent extracts the inner text content from a teammate-message XML wrapper.
func extractTeammateContent(s string) string {
	m := teammateContentRe.FindStringSubmatch(s)
	if m == nil {
		return s // fallback to full string if no match
	}
	return strings.TrimSpace(m[1])
}

// parseTimestamp parses an ISO 8601 timestamp. Returns zero time on failure.
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
	return time.Time{}
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
	var blocks []textBlockJSON
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

// isUserNoise returns true if a user-type entry is noise that should be dropped.
// Checks: hard noise tag wrapping, empty command output, interruption messages.
func isUserNoise(raw json.RawMessage, contentStr string) bool {
	trimmed := strings.TrimSpace(contentStr)

	// Wrapped entirely in a hard noise tag.
	for _, tag := range hardNoiseTags {
		closeTag := strings.Replace(tag, "<", "</", 1)
		if strings.HasPrefix(trimmed, tag) && strings.HasSuffix(trimmed, closeTag) {
			return true
		}
	}

	// Empty command output.
	if trimmed == emptyStdout || trimmed == emptyStderr {
		return true
	}

	// Interruption messages (string content or array with single text block).
	if strings.HasPrefix(trimmed, "[Request interrupted by user") {
		return true
	}
	return isArrayInterruption(raw)
}

// isArrayInterruption checks if content is an array with a single text block
// starting with "[Request interrupted by user".
func isArrayInterruption(raw json.RawMessage) bool {
	var blocks []textBlockJSON
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return false
	}
	if len(blocks) == 1 && blocks[0].Type == "text" && strings.HasPrefix(blocks[0].Text, "[Request interrupted by user") {
		return true
	}
	return false
}

// extractAssistantDetails pulls thinking count, tool calls, and structured
// content blocks from an assistant message's content array.
func extractAssistantDetails(raw json.RawMessage) (int, []ToolCall, []ContentBlock) {
	var blocks []contentBlockJSON
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return 0, nil, nil
	}

	thinking := 0
	var calls []ToolCall
	var cblocks []ContentBlock
	for _, b := range blocks {
		switch b.Type {
		case "thinking":
			thinking++
			cblocks = append(cblocks, ContentBlock{
				Type: "thinking",
				Text: b.Thinking,
			})
		case "text":
			cblocks = append(cblocks, ContentBlock{
				Type: "text",
				Text: b.Text,
			})
		case "tool_use":
			if b.ID != "" && b.Name != "" {
				calls = append(calls, ToolCall{ID: b.ID, Name: b.Name})
			}
			cblocks = append(cblocks, ContentBlock{
				Type:      "tool_use",
				ToolID:    b.ID,
				ToolName:  b.Name,
				ToolInput: b.Input,
			})
		default:
			// Preserve unknown block types as-is.
			cblocks = append(cblocks, ContentBlock{
				Type: b.Type,
				Text: b.Text,
			})
		}
	}
	return thinking, calls, cblocks
}

// extractMetaBlocks parses isMeta user content (tool results) into ContentBlocks.
// Falls back to a single text block if content isn't a JSON array of tool_result blocks.
func extractMetaBlocks(raw json.RawMessage, textFallback string) []ContentBlock {
	var blocks []contentBlockJSON
	if err := json.Unmarshal(raw, &blocks); err != nil {
		// String content or unparseable -> single text block.
		return []ContentBlock{{Type: "text", Text: textFallback}}
	}

	// Verify we got actual tool_result blocks, not some other array.
	hasToolResult := false
	for _, b := range blocks {
		if b.Type == "tool_result" {
			hasToolResult = true
			break
		}
	}
	if !hasToolResult {
		return []ContentBlock{{Type: "text", Text: textFallback}}
	}

	var cblocks []ContentBlock
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		content := stringifyContent(b.Content)
		cblocks = append(cblocks, ContentBlock{
			Type:    "tool_result",
			ToolID:  b.ToolUseID,
			Content: content,
			IsError: b.IsError,
		})
	}
	return cblocks
}

// stringifyContent converts tool_result content (string or array of text blocks) to a string.
func stringifyContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	// Try string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try array of text blocks.
	var blocks []textBlockJSON
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	// Last resort: raw JSON string.
	return string(raw)
}
