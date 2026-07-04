package parser

import (
	"encoding/json"
	"strings"
	"time"
)

// concurrentTaskDurationThreshold is the maximum plausible duration for a
// non-Task tool (Bash, Read, Edit, etc.) before we suspect it's inflated by
// concurrent background Task agents. When the same AI turn contains both
// Task calls and non-Task calls, Claude Code delays writing tool_result
// entries until all background agents complete, inflating wall-clock
// durations for tools that actually finished in seconds.
const concurrentTaskDurationThreshold int64 = 60_000 // 60 seconds

// DisplayItemType discriminates the display item categories.
type DisplayItemType int

const (
	ItemThinking DisplayItemType = iota
	ItemOutput
	ItemToolCall
	ItemSubagent        // Task tool spawned subagent
	ItemTeammateMessage // message from a teammate agent
	ItemMemoryLoad      // nested memory file loaded into context ("Loaded X")
)

// DisplayItem is a structured element within an AI chunk's detail view.
type DisplayItem struct {
	Type DisplayItemType

	// Timestamp is the source message's timestamp. Consecutive AI messages
	// merge into one chunk (whose Timestamp is the FIRST message's), so
	// items within a chunk carry their own times. Zero for items built
	// outside mergeAIBuffer.
	Timestamp time.Time

	Text        string
	ToolName    string
	ToolID      string
	ToolInput   json.RawMessage
	ToolSummary string // "main.go" for Read, "go test" for Bash
	ToolResult  string
	ToolError   bool
	DurationMs  int64 // tool_use -> tool_result timestamp delta
	TokenCount  int   // estimated tokens: len(text)/4

	// Tool categorization
	ToolCategory ToolCategory // broad functional group (Read, Edit, Bash, etc.)

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

// InferenceCycle is one LLM call plus the tool calls it dispatched. Tool
// results arrive as meta entries within the cycle's item range; the next
// non-meta assistant entry starts the next cycle.
//
// Cycles index into Chunk.Items via StartItem (inclusive) and EndItem
// (exclusive). The items themselves keep their existing flat ordering --
// this is a derived view, not a replacement structure.
type InferenceCycle struct {
	Index       int    // 0-based, per chunk
	StartItem   int    // inclusive index into Chunk.Items
	EndItem     int    // exclusive
	Model       string // model that produced this response
	Usage       Usage  // context-window snapshot for this call
	StopReason  string
	HasThinking bool
	ToolCount   int   // ItemToolCall + ItemSubagent in range
	DurationMs  int64 // wall time from this assistant entry to the next, or to chunk end
}

// Chunk is the output of the pipeline. Each chunk represents one visible unit
// in the conversation timeline.
type Chunk struct {
	Type      ChunkType
	Timestamp time.Time

	// User chunk fields.
	UserText       string
	ExpandedPrompt string // expanded skill/command prompt (from isMeta=true entry after /command)

	// AI chunk fields.
	Model         string
	Text          string
	ThinkingCount int
	ToolCalls     []ToolCall
	Items         []DisplayItem    // structured detail, nil until populated
	Cycles        []InferenceCycle // one per non-meta assistant entry; nil for non-AI chunks
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

	for i := 0; i < len(msgs); i++ {
		switch m := msgs[i].(type) {
		case UserMsg:
			flush()
			c := Chunk{
				Type:      UserChunk,
				Timestamp: m.Timestamp,
				UserText:  m.Text,
			}
			// Slash commands: the next entry may be the expanded skill prompt
			// (isMeta=true with text content, no tool_result blocks). Attach
			// it to this user chunk instead of letting it fall into the AI buffer.
			if strings.HasPrefix(m.Text, "/") && i+1 < len(msgs) {
				if expanded := extractExpandedPrompt(msgs[i+1]); expanded != "" {
					c.ExpandedPrompt = expanded
					i++ // consume the expanded prompt entry
				}
			}
			chunks = append(chunks, c)
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
		case MemoryLoadMsg:
			// Same fold pattern as TeammateMsg. Memory loads happen mid-turn
			// (after the user submits, before the assistant replies) and
			// belong with the surrounding AI turn, not as a standalone chunk.
			aiBuf = append(aiBuf, AIMsg{
				Timestamp: m.Timestamp,
				IsMeta:    true,
				Blocks: []ContentBlock{{
					Type:        "memory_load",
					DisplayPath: m.DisplayPath,
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

// extractExpandedPrompt checks whether a classified message is an expanded
// skill/command prompt — an isMeta=true AI message with only text blocks
// (no tool_result). Returns the text content, or empty string if not a match.
func extractExpandedPrompt(msg ClassifiedMsg) string {
	ai, ok := msg.(AIMsg)
	if !ok || !ai.IsMeta || ai.Text == "" {
		return ""
	}
	for _, b := range ai.Blocks {
		if b.Type == "tool_result" {
			return ""
		}
	}
	return ai.Text
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

	// Per-message item-start positions, recorded BEFORE the message's blocks
	// are appended to items. Used to derive InferenceCycle ranges below.
	itemStarts := make([]int, len(buf))

	for i, m := range buf {
		itemStarts[i] = len(items)
		// --- Flat field accumulation ---
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
					if b.ToolName == "Task" || b.ToolName == "Agent" || b.ToolName == "Skill" {
						info := extractSubagentInfo(parseInputFields(b.ToolInput))
						items = append(items, DisplayItem{
							Type:           ItemSubagent,
							ToolName:       b.ToolName,
							ToolID:         b.ToolID,
							ToolInput:      b.ToolInput,
							ToolSummary:    ToolSummary(b.ToolName, b.ToolInput),
							ToolCategory:   CategorizeToolName(b.ToolName),
							SubagentType:   info.Type,
							SubagentDesc:   info.Description,
							TeamMemberName: info.MemberName,
							TokenCount:     inputLen / 4,
						})
					} else {
						items = append(items, DisplayItem{
							Type:         ItemToolCall,
							ToolName:     b.ToolName,
							ToolID:       b.ToolID,
							ToolInput:    b.ToolInput,
							ToolSummary:  ToolSummary(b.ToolName, b.ToolInput),
							ToolCategory: CategorizeToolName(b.ToolName),
							TokenCount:   inputLen / 4,
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
				case "memory_load":
					items = append(items, DisplayItem{
						Type: ItemMemoryLoad,
						Text: b.DisplayPath,
					})
				}
			}
		}

		// Stamp items created by this message with its own timestamp.
		// Matched tool_result blocks mutate earlier items (no append), so
		// the [itemStarts[i], len(items)) range is exactly this message's
		// new items.
		for k := itemStarts[i]; k < len(items); k++ {
			items[k].Timestamp = m.Timestamp
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
		suppressInflatedDurations(items)
		finalItems = items
	}

	cycles := buildCycles(buf, itemStarts, items)

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
		Cycles:        cycles,
		Usage:         usage,
		StopReason:    stop,
		DurationMs:    dur,
	}
}

// buildCycles derives one InferenceCycle per non-meta AIMsg. Each cycle's
// item range starts where its source message began appending and ends where
// the next non-meta message began (or at len(items) for the last cycle).
// Duration is the wall-clock gap to the next non-meta message, or to the
// final buffer timestamp for the last cycle.
//
// Returns nil when buf has no non-meta messages (rare: meta-only chunks).
func buildCycles(buf []AIMsg, itemStarts []int, items []DisplayItem) []InferenceCycle {
	// Indices of non-meta messages, in order.
	nonMeta := make([]int, 0, len(buf))
	for i, m := range buf {
		if !m.IsMeta {
			nonMeta = append(nonMeta, i)
		}
	}
	if len(nonMeta) == 0 {
		return nil
	}

	cycles := make([]InferenceCycle, len(nonMeta))
	lastTS := buf[len(buf)-1].Timestamp

	for i, msgIdx := range nonMeta {
		msg := buf[msgIdx]
		startItem := itemStarts[msgIdx]

		var endItem int
		var endTS time.Time
		if i+1 < len(nonMeta) {
			next := nonMeta[i+1]
			endItem = itemStarts[next]
			endTS = buf[next].Timestamp
		} else {
			endItem = len(items)
			endTS = lastTS
		}

		var dur int64
		if !msg.Timestamp.IsZero() && !endTS.IsZero() {
			dur = endTS.Sub(msg.Timestamp).Milliseconds()
		}

		toolCount := 0
		for j := startItem; j < endItem; j++ {
			if items[j].Type == ItemToolCall || items[j].Type == ItemSubagent {
				toolCount++
			}
		}

		cycles[i] = InferenceCycle{
			Index:       i,
			StartItem:   startItem,
			EndItem:     endItem,
			Model:       msg.Model,
			Usage:       msg.Usage,
			StopReason:  msg.StopReason,
			HasThinking: msg.ThinkingCount > 0,
			ToolCount:   toolCount,
			DurationMs:  dur,
		}
	}
	return cycles
}

// suppressInflatedDurations zeroes out non-Task tool durations that are
// inflated by concurrent background Task agents in the same AI turn.
//
// When Claude Code runs Bash/Read/Edit alongside background Task calls,
// the tool_result entry timestamps reflect wall-clock time (including the
// wait for agents to complete), not the tool's actual execution time.
// A git push that takes 3 seconds can show as 11 minutes.
//
// Heuristic: if the turn contains at least one Task (ItemSubagent) AND a
// non-Task tool exceeds concurrentTaskDurationThreshold, the non-Task
// duration is unreliable. Zero it to suppress display.
func suppressInflatedDurations(items []DisplayItem) {
	// Find the maximum subagent duration in this turn. Only suppress when
	// a subagent itself ran long enough to plausibly inflate sibling durations.
	// A short-lived subagent (e.g. a non-fork Skill completing in 200ms)
	// can't cause inflation.
	var maxTaskDur int64
	for i := range items {
		if items[i].Type == ItemSubagent && items[i].DurationMs > maxTaskDur {
			maxTaskDur = items[i].DurationMs
		}
	}
	if maxTaskDur < concurrentTaskDurationThreshold {
		return
	}

	// Zero out non-subagent tools whose duration exceeds the threshold,
	// suggesting they waited for the same background work.
	for i := range items {
		if items[i].Type == ItemSubagent || items[i].Type == ItemTeammateMessage {
			continue
		}
		if items[i].DurationMs > concurrentTaskDurationThreshold {
			items[i].DurationMs = 0
		}
	}
}

// subagentInfo holds metadata extracted from a Task tool_use input.
type subagentInfo struct {
	Type        string // "Explore", "Plan", "general-purpose", etc.
	Description string // Task description or truncated prompt
	MemberName  string // team member name (only for team Task calls)
}

// extractSubagentInfo extracts metadata from Task/Agent/Skill tool input
// fields. Single decoder for the type and description fallback chains —
// summaryTask builds its display string from this result, so the summary
// prefix and DisplayItem.SubagentType can never disagree.
func extractSubagentInfo(fields map[string]json.RawMessage) subagentInfo {
	var info subagentInfo

	info.Type = getString(fields, "subagent_type")
	if info.Type == "" {
		// Some sessions carry the camelCase variant.
		info.Type = getString(fields, "subagentType")
	}
	if info.Type == "" {
		// Skill tool uses "skill" for type and "args" for description.
		info.Type = getString(fields, "skill")
	}

	// Try "description" first, then "prompt", then "args" (Skill tool).
	info.Description = getString(fields, "description")
	if info.Description == "" {
		info.Description = Truncate(getString(fields, "prompt"), 80)
	}
	if info.Description == "" {
		info.Description = Truncate(getString(fields, "args"), 80)
	}

	// Team member name (present when team_name + name are both set).
	info.MemberName = getString(fields, "name")
	return info
}
