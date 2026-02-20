package parser

import (
	"encoding/json"
	"strings"
	"time"
)

// DisplayItemType discriminates the three display item categories.
type DisplayItemType int

const (
	ItemThinking DisplayItemType = iota
	ItemOutput
	ItemToolCall
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
	Timestamp   time.Time
}

// ChunkType discriminates the three chunk categories.
type ChunkType int

const (
	UserChunk ChunkType = iota
	AIChunk
	SystemChunk
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
	Output string
}

// BuildChunks folds classified messages into display chunks.
// The algorithm buffers consecutive AI messages and flushes them into a single
// AI chunk whenever a User or System message appears (or at end of input).
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
			})
		case AIMsg:
			aiBuf = append(aiBuf, m)
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
		usage     Usage
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
		usage.InputTokens += m.Usage.InputTokens
		usage.OutputTokens += m.Usage.OutputTokens
		usage.CacheReadTokens += m.Usage.CacheReadTokens
		usage.CacheCreationTokens += m.Usage.CacheCreationTokens

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
						Timestamp:  m.Timestamp,
						TokenCount: len(b.Text) / 4,
					})
				case "text":
					items = append(items, DisplayItem{
						Type:       ItemOutput,
						Text:       b.Text,
						Timestamp:  m.Timestamp,
						TokenCount: len(b.Text) / 4,
					})
				case "tool_use":
					inputLen := len(b.ToolInput)
					item := DisplayItem{
						Type:        ItemToolCall,
						ToolName:    b.ToolName,
						ToolID:      b.ToolID,
						ToolInput:   b.ToolInput,
						ToolSummary: ToolSummary(b.ToolName, b.ToolInput),
						Timestamp:   m.Timestamp,
						TokenCount:  inputLen / 4,
					}
					items = append(items, item)
					pending[b.ToolID] = pendingTool{
						index:     len(items) - 1,
						timestamp: m.Timestamp,
					}
				}
			}
		} else {
			// Meta messages: match tool_result blocks to pending tool_use items.
			for _, b := range m.Blocks {
				if b.Type != "tool_result" {
					continue
				}
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
						Timestamp:  m.Timestamp,
						TokenCount: len(b.Content) / 4,
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
