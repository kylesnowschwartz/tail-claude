package parser

import (
	"encoding/json"
	"strings"
	"time"
)

// DisplayItemType discriminates the display item categories.
type DisplayItemType int

const (
	ItemThinking DisplayItemType = iota
	ItemOutput
	ItemToolCall
	ItemSubagent        // Task tool spawned subagent
	ItemTeammateMessage // message from a teammate agent
)

// DisplayItem is a structured element within an AI chunk's detail view.
type DisplayItem struct {
	Type        DisplayItemType
	Text        string
	ToolName    string
	ToolID      string
	ToolInput   json.RawMessage
	ToolSummary string // "main.go" for Read, "go test" for Bash
	ToolResult  string
	ToolError   bool
	DurationMs  int64 // tool_use -> tool_result timestamp delta
	TokenCount  int   // estimated tokens: len(text)/4

	// Subagent fields (ItemSubagent only)
	SubagentType   string // "Explore", "Plan", "general-purpose", etc.
	SubagentDesc   string // Task description
	TeamMemberName string // team member name from Task input (e.g. "file-counter")

	// Teammate fields (ItemTeammateMessage only)
	TeammateID    string
	TeammateColor string // team color name (e.g. "blue", "green")
}

// ChunkType discriminates the chunk categories.
type ChunkType int

const (
	UserChunk ChunkType = iota
	AIChunk
	SystemChunk
	CompactChunk // context compression boundary
)

// Chunk is the output of the pipeline. Each chunk represents one visible unit
// in the conversation timeline.
type Chunk struct {
	Type      ChunkType
	Timestamp time.Time

	// User chunk fields.
	UserText string

	// AI chunk fields.
	Model         string
	Text          string
	ThinkingCount int
	ToolCalls     []ToolCall
	Items         []DisplayItem // structured detail, nil until populated
	Usage         Usage
	StopReason    string
	DurationMs    int64 // first to last message timestamp in chunk

	// System chunk fields.
	Output  string
	IsError bool // bash stderr present or task killed
}

// BuildChunks folds classified messages into display chunks.
// The algorithm buffers consecutive AI messages and flushes them into a single
// AI chunk whenever a User or System message appears (or at end of input).
// TeammateMsg entries fold into the current AI buffer rather than starting new chunks.
func BuildChunks(msgs []ClassifiedMsg) []Chunk {
	var chunks []Chunk
	var aiBuf []AIMsg

	flush := func() {
		if len(aiBuf) == 0 {
			return
		}
		chunks = append(chunks, mergeAIBuffer(aiBuf))
		aiBuf = aiBuf[:0]
	}

	for _, msg := range msgs {
		switch m := msg.(type) {
		case UserMsg:
			flush()
			chunks = append(chunks, Chunk{
				Type:      UserChunk,
				Timestamp: m.Timestamp,
				UserText:  m.Text,
			})
		case SystemMsg:
			flush()
			chunks = append(chunks, Chunk{
				Type:      SystemChunk,
				Timestamp: m.Timestamp,
				Output:    m.Output,
				IsError:   m.IsError,
			})
		case AIMsg:
			aiBuf = append(aiBuf, m)
		case TeammateMsg:
			// Fold teammate messages into the AI buffer as synthetic AIMsg
			// with a "teammate" content block. This keeps them within the
			// AI turn rather than splitting it.
			aiBuf = append(aiBuf, AIMsg{
				Timestamp: m.Timestamp,
				IsMeta:    true,
				Blocks: []ContentBlock{{
					Type:          "teammate",
					Text:          m.Text,
					TeammateID:    m.TeammateID,
					TeammateColor: m.Color,
				}},
			})
		case CompactMsg:
			flush()
			chunks = append(chunks, Chunk{
				Type:      CompactChunk,
				Timestamp: m.Timestamp,
				Output:    m.Text,
			})
		}
	}
	flush()

	return chunks
}

// pendingTool tracks a tool_use DisplayItem awaiting its result.
type pendingTool struct {
	index     int       // index into the items slice
	timestamp time.Time // tool_use message timestamp
}

// mergeAIBuffer collapses a buffer of consecutive AI messages into one AI chunk.
// Populates both flat fields (backward compat) and structured Items.
func mergeAIBuffer(buf []AIMsg) Chunk {
	var (
		texts     []string
		thinking  int
		toolCalls []ToolCall
		model     string
		stop      string
	)

	// Structured items built from ContentBlocks.
	var items []DisplayItem
	pending := make(map[string]pendingTool) // ToolID -> pending info
	hasBlocks := false

	for _, m := range buf {
		// --- Flat field accumulation (unchanged) ---
		if m.Text != "" {
			texts = append(texts, m.Text)
		}
		thinking += m.ThinkingCount
		toolCalls = append(toolCalls, m.ToolCalls...)

		if model == "" && !m.IsMeta && m.Model != "" {
			model = m.Model
		}
		if !m.IsMeta && m.StopReason != "" {
			stop = m.StopReason
		}

		// --- Structured item building ---
		if len(m.Blocks) == 0 {
			continue
		}
		hasBlocks = true

		if !m.IsMeta {
			// Non-meta messages: create display items from blocks.
			for _, b := range m.Blocks {
				switch b.Type {
				case "thinking":
					items = append(items, DisplayItem{
						Type:       ItemThinking,
						Text:       b.Text,
						TokenCount: len(b.Text) / 4,
					})
				case "text":
					items = append(items, DisplayItem{
						Type:       ItemOutput,
						Text:       b.Text,
						TokenCount: len(b.Text) / 4,
					})
				case "tool_use":
					inputLen := len(b.ToolInput)
					if b.ToolName == "Task" {
						info := extractSubagentInfo(b.ToolInput)
						items = append(items, DisplayItem{
							Type:           ItemSubagent,
							ToolName:       b.ToolName,
							ToolID:         b.ToolID,
							ToolInput:      b.ToolInput,
							ToolSummary:    ToolSummary(b.ToolName, b.ToolInput),
							SubagentType:   info.Type,
							SubagentDesc:   info.Description,
							TeamMemberName: info.MemberName,
							TokenCount:     inputLen / 4,
						})
					} else {
						items = append(items, DisplayItem{
							Type:        ItemToolCall,
							ToolName:    b.ToolName,
							ToolID:      b.ToolID,
							ToolInput:   b.ToolInput,
							ToolSummary: ToolSummary(b.ToolName, b.ToolInput),
							TokenCount:  inputLen / 4,
						})
					}
					pending[b.ToolID] = pendingTool{
						index:     len(items) - 1,
						timestamp: m.Timestamp,
					}
				}
			}
		} else {
			// Meta messages: match tool_result blocks and handle teammate blocks.
			for _, b := range m.Blocks {
				switch b.Type {
				case "tool_result":
					if p, ok := pending[b.ToolID]; ok {
						items[p.index].ToolResult = b.Content
						items[p.index].ToolError = b.IsError
						if !p.timestamp.IsZero() && !m.Timestamp.IsZero() {
							items[p.index].DurationMs = m.Timestamp.Sub(p.timestamp).Milliseconds()
						}
						items[p.index].TokenCount += len(b.Content) / 4
						delete(pending, b.ToolID)
					} else {
						// Unmatched tool_result -> output item.
						items = append(items, DisplayItem{
							Type:       ItemOutput,
							Text:       b.Content,
							TokenCount: len(b.Content) / 4,
						})
					}
				case "teammate":
					items = append(items, DisplayItem{
						Type:          ItemTeammateMessage,
						Text:          b.Text,
						TeammateID:    b.TeammateID,
						TeammateColor: b.TeammateColor,
						TokenCount:    len(b.Text) / 4,
					})
				}
			}
		}
	}

	first := buf[0].Timestamp
	last := buf[len(buf)-1].Timestamp

	var dur int64
	if !first.IsZero() && !last.IsZero() {
		dur = last.Sub(first).Milliseconds()
	}

	ts := first
	if ts.IsZero() {
		ts = last
	}

	// Only set Items if we had any blocks to process.
	var finalItems []DisplayItem
	if hasBlocks {
		finalItems = items
	}

	// Usage snapshot: last non-meta assistant message's usage. The Claude API
	// reports input_tokens as the full context window per call, so the last
	// call is the correct per-turn metric (not the sum across round trips).
	var usage Usage
	for i := len(buf) - 1; i >= 0; i-- {
		if !buf[i].IsMeta && buf[i].Usage.TotalTokens() > 0 {
			usage = buf[i].Usage
			break
		}
	}

	return Chunk{
		Type:          AIChunk,
		Timestamp:     ts,
		Model:         model,
		Text:          strings.Join(texts, "\n"),
		ThinkingCount: thinking,
		ToolCalls:     toolCalls,
		Items:         finalItems,
		Usage:         usage,
		StopReason:    stop,
		DurationMs:    dur,
	}
}

// subagentInfo holds metadata extracted from a Task tool_use input.
type subagentInfo struct {
	Type        string // "Explore", "Plan", "general-purpose", etc.
	Description string // Task description or truncated prompt
	MemberName  string // team member name (only for team Task calls)
}

// extractSubagentInfo extracts metadata from Task tool input JSON.
func extractSubagentInfo(input json.RawMessage) subagentInfo {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return subagentInfo{}
	}

	var info subagentInfo

	// Inner unmarshal errors are intentionally ignored — these are optional string
	// fields and "" is the correct default when absent or non-string.
	if raw, ok := fields["subagent_type"]; ok {
		json.Unmarshal(raw, &info.Type)
	}
	// Try "description" first, then "prompt" as fallback.
	if raw, ok := fields["description"]; ok {
		json.Unmarshal(raw, &info.Description)
	}
	if info.Description == "" {
		if raw, ok := fields["prompt"]; ok {
			var prompt string
			json.Unmarshal(raw, &prompt)
			info.Description = Truncate(prompt, 80)
		}
	}
	// Team member name (present when team_name + name are both set).
	if raw, ok := fields["name"]; ok {
		json.Unmarshal(raw, &info.MemberName)
	}
	return info
}
