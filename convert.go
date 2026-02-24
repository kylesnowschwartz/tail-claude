package main

import (
	"bytes"
	"encoding/json"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

// teamColorPool is the ordered set of color names matching the teamColor()
// switch in format.go. Used to assign synthetic colors to subagents that
// don't carry color metadata from the JSONL data.
var teamColorPool = []string{
	"blue", "green", "red", "yellow", "purple", "cyan", "orange", "pink",
}

// chunksToMessages maps parser output into the TUI's message type.
// Discovered subagent processes are linked to their corresponding
// ItemSubagent display items by matching ParentTaskID to ToolID.
// colorByToolID provides fallback team colors for items without a linked process.
func chunksToMessages(chunks []parser.Chunk, subagents []parser.SubagentProcess, colorByToolID map[string]string) []message {
	msgs := make([]message, 0, len(chunks))
	for _, c := range chunks {
		switch c.Type {
		case parser.UserChunk:
			msgs = append(msgs, message{
				role:      RoleUser,
				content:   c.UserText,
				timestamp: formatTime(c.Timestamp),
			})
		case parser.AIChunk:
			// Count distinct team-spawned subagents and teammate message senders.
			var teamSpawns int
			teammateIDs := make(map[string]bool)
			for _, it := range c.Items {
				if it.Type == parser.ItemSubagent && parser.IsTeamTask(&it) {
					teamSpawns++
				}
				if it.Type == parser.ItemTeammateMessage && it.TeammateID != "" {
					teammateIDs[it.TeammateID] = true
				}
			}
			msgs = append(msgs, message{
				role:             RoleClaude,
				model:            shortModel(c.Model),
				content:          c.Text,
				thinkingCount:    c.ThinkingCount,
				toolCallCount:    len(c.ToolCalls),
				outputCount:      countOutputItems(c.Items),
				tokensRaw:        c.Usage.TotalTokens(),
				contextTokens:    c.Usage.InputTokens + c.Usage.CacheReadTokens + c.Usage.CacheCreationTokens,
				durationMs:       c.DurationMs,
				timestamp:        formatTime(c.Timestamp),
				items:            convertDisplayItems(c.Items, subagents, colorByToolID),
				lastOutput:       parser.FindLastOutput(c.Items),
				teammateSpawns:   teamSpawns,
				teammateMessages: len(teammateIDs),
			})
		case parser.SystemChunk:
			msgs = append(msgs, message{
				role:      RoleSystem,
				content:   c.Output,
				timestamp: formatTime(c.Timestamp),
				isError:   c.IsError,
			})
		case parser.CompactChunk:
			msgs = append(msgs, message{
				role:      RoleCompact,
				content:   c.Output,
				timestamp: formatTime(c.Timestamp),
			})
		}
	}
	return msgs
}

// displayItemFromParser maps a single parser.DisplayItem to the TUI's displayItem,
// including JSON pretty-printing of tool input.
func displayItemFromParser(it parser.DisplayItem) displayItem {
	input := ""
	if len(it.ToolInput) > 0 {
		var pretty bytes.Buffer
		if json.Indent(&pretty, it.ToolInput, "", "  ") == nil {
			input = pretty.String()
		} else {
			input = string(it.ToolInput)
		}
	}
	return displayItem{
		itemType:       it.Type,
		text:           it.Text,
		toolName:       it.ToolName,
		toolSummary:    it.ToolSummary,
		toolInput:      input,
		toolResult:     it.ToolResult,
		toolError:      it.ToolError,
		durationMs:     it.DurationMs,
		tokenCount:     it.TokenCount,
		subagentType:   it.SubagentType,
		subagentDesc:   it.SubagentDesc,
		teamMemberName: it.TeamMemberName,
		teammateID:     it.TeammateID,
		teamColor:      it.TeammateColor,
	}
}

// convertDisplayItems maps parser.DisplayItem to the TUI's displayItem type.
// Links ItemSubagent items to their discovered SubagentProcess by matching
// ToolID to ParentTaskID. colorByToolID provides fallback team colors for
// items without a linked process (e.g. team agents whose sessions live
// outside the subagents/ directory).
func convertDisplayItems(items []parser.DisplayItem, subagents []parser.SubagentProcess, colorByToolID map[string]string) []displayItem {
	if len(items) == 0 {
		return nil
	}

	// Build ParentTaskID -> SubagentProcess index for O(1) lookup.
	procByTaskID := make(map[string]*parser.SubagentProcess, len(subagents))
	for i := range subagents {
		if subagents[i].ParentTaskID != "" {
			procByTaskID[subagents[i].ParentTaskID] = &subagents[i]
		}
	}

	out := make([]displayItem, len(items))
	for i, it := range items {
		out[i] = displayItemFromParser(it)
		// Link subagent process if available.
		if it.Type == parser.ItemSubagent {
			if proc := procByTaskID[it.ToolID]; proc != nil {
				out[i].subagentProcess = proc
				out[i].subagentOngoing = parser.IsOngoing(proc.Chunks)
				if proc.TeammateColor != "" {
					out[i].teamColor = proc.TeammateColor
				}
			}
			// Fallback: apply team color from toolUseResult data when no
			// linked process exists (team agents run as separate sessions).
			if out[i].teamColor == "" {
				if color, ok := colorByToolID[it.ToolID]; ok {
					out[i].teamColor = color
				}
			}
		}
	}

	// Assign pool colors to subagents without a team color.
	// Collect claimed colors, then cycle through unclaimed palette entries.
	claimed := make(map[string]bool)
	for _, di := range out {
		if di.teamColor != "" {
			claimed[di.teamColor] = true
		}
	}
	var poolColors []string
	for _, name := range teamColorPool {
		if !claimed[name] {
			poolColors = append(poolColors, name)
		}
	}
	if len(poolColors) > 0 {
		poolIdx := 0
		for i := range out {
			if out[i].itemType == parser.ItemSubagent && out[i].teamColor == "" {
				out[i].teamColor = poolColors[poolIdx%len(poolColors)]
				poolIdx++
			}
		}
	}

	return out
}

// currentDetailMsg returns the message being viewed in detail view.
// Returns the trace message when drilled into a subagent, otherwise the
// selected message from the list.
func (m model) currentDetailMsg() message {
	if m.traceMsg != nil {
		return *m.traceMsg
	}
	if m.cursor >= 0 && m.cursor < len(m.messages) {
		return m.messages[m.cursor]
	}
	return message{}
}

// buildSubagentMessage creates a synthetic message from a subagent's execution
// trace. The message contains all items (Input, Output, Tool calls) from the
// subagent's chunks, suitable for rendering in the detail view.
func buildSubagentMessage(proc *parser.SubagentProcess, subagentType string) message {
	// Build a temporary parent displayItem to reuse buildTraceItems.
	parent := displayItem{subagentProcess: proc}
	items := buildTraceItems(parent)

	var toolCount, thinkCount, msgCount int
	for _, it := range items {
		switch it.itemType {
		case parser.ItemThinking:
			thinkCount++
		case parser.ItemToolCall, parser.ItemSubagent:
			toolCount++
		case parser.ItemOutput:
			msgCount++
		}
	}

	mdl := ""
	for _, c := range proc.Chunks {
		if c.Type == parser.AIChunk && c.Model != "" {
			mdl = shortModel(c.Model)
			break
		}
	}

	return message{
		role:          RoleClaude,
		model:         mdl,
		items:         items,
		thinkingCount: thinkCount,
		toolCallCount: toolCount,
		outputCount:   msgCount,
		tokensRaw:     proc.Usage.TotalTokens(),
		durationMs:    proc.DurationMs,
		timestamp:     formatTime(proc.StartTime),
		subagentLabel: subagentType,
	}
}
