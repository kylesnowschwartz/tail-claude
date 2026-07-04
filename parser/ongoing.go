package parser

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// OngoingStalenessThreshold is the maximum time since last file modification
// before a session is considered dead regardless of content. Claude Code writes
// on every API response and tool call, so 2 minutes of silence means the
// process is gone.
const OngoingStalenessThreshold = 2 * time.Minute

// activityType classifies events for ongoing detection.
type activityType int

const (
	actTextOutput   activityType = iota // text response (ending event)
	actThinking                         // extended thinking (AI activity)
	actToolUse                          // tool invocation (AI activity)
	actToolResult                       // tool result (AI activity)
	actInterruption                     // user interruption (ending event)
	actExitPlanMode                     // ExitPlanMode tool call (ending event)
)

// isEndingEvent returns true if this activity type terminates an ongoing session.
func (t activityType) isEndingEvent() bool {
	return t == actTextOutput || t == actInterruption || t == actExitPlanMode
}

// isAIActivity returns true if this activity type represents AI work in progress.
func (t activityType) isAIActivity() bool {
	return t == actThinking || t == actToolUse || t == actToolResult
}

// approvePattern matches approve: true in SendMessage shutdown_response input.
var approvePattern = regexp.MustCompile(`"approve"\s*:\s*true`)

// isShutdownApproval checks if a tool_use block is a SendMessage shutdown_response
// with approve: true.
func isShutdownApproval(toolName string, toolInput json.RawMessage) bool {
	if toolName != "SendMessage" {
		return false
	}
	// Quick structural check: parse and inspect the fields.
	var fields struct {
		Type    string `json:"type"`
		Approve *bool  `json:"approve"`
	}
	if err := json.Unmarshal(toolInput, &fields); err != nil {
		// Fallback to regex for malformed JSON.
		return approvePattern.Match(toolInput)
	}
	return fields.Type == "shutdown_response" && fields.Approve != nil && *fields.Approve
}

// IsOngoing reports whether the session appears to still be in progress.
// A session is ongoing if either:
//
//  1. There's AI activity (thinking, tool_use, tool_result) after the last
//     "ending event" (text output, interruption, ExitPlanMode, shutdown approval).
//  2. Any tool call is still awaiting a result (pending tool calls).
//
// Condition 2 catches team sessions where the parent writes text output after
// receiving partial agent results. The activity-based check (1) only looks
// forward from the last ending event, so it misses still-running agents whose
// tool_use appeared earlier in the sequence.
//
// If no ending event exists, it's ongoing if there's any AI activity at all.
//
// For chunks without structured items (old-style), falls back to checking
// whether the last chunk is an AI chunk without a stop_reason of "end_turn".
func IsOngoing(chunks []Chunk) bool {
	if len(chunks) == 0 {
		return false
	}

	// A trailing user prompt means Claude is processing the request.
	// Callers apply staleness thresholds to handle dead sessions where
	// the user typed but Claude never responded.
	if chunks[len(chunks)-1].Type == UserChunk {
		return true
	}

	// Collect activities from structured items across all chunks.
	var activities []activityType
	hasItems := false

	// Track tool_use IDs that are shutdown approvals so their tool_results
	// are also treated as ending events.
	shutdownToolIDs := make(map[string]bool)

	for _, chunk := range chunks {
		if chunk.Type != AIChunk {
			continue
		}

		if len(chunk.Items) == 0 {
			// Thinking-only turns: Opus 4.7+/Claude 5 sessions persist thinking
			// as empty blocks, which Classify counts (ThinkingCount) but does
			// not emit as items. A chunk with no items but a thinking count is
			// Claude mid-thought — AI activity, not silence.
			if chunk.ThinkingCount > 0 {
				hasItems = true
				activities = append(activities, actThinking)
			}
			continue
		}
		hasItems = true

		for _, item := range chunk.Items {
			switch item.Type {
			case ItemThinking:
				activities = append(activities, actThinking)

			case ItemOutput:
				if strings.TrimSpace(item.Text) != "" {
					activities = append(activities, actTextOutput)
				}

			case ItemToolCall:
				if item.ToolName == "ExitPlanMode" {
					activities = append(activities, actExitPlanMode)
				} else if isShutdownApproval(item.ToolName, item.ToolInput) {
					shutdownToolIDs[item.ToolID] = true
					activities = append(activities, actInterruption)
				} else {
					activities = append(activities, actToolUse)
				}

				// If this tool call has a result, track it too.
				if item.ToolResult != "" {
					if shutdownToolIDs[item.ToolID] {
						activities = append(activities, actInterruption)
					} else {
						activities = append(activities, actToolResult)
					}
				}

			case ItemSubagent:
				// Subagent spawns are AI activity (like tool_use).
				activities = append(activities, actToolUse)
				if item.ToolResult != "" {
					activities = append(activities, actToolResult)
				}
			}
		}
	}

	// If we had items, use the activity-based detection.
	if hasItems {
		if isOngoingFromActivities(activities) {
			return true
		}
		// Activity sequence says complete, but check for pending agents.
		// This catches team sessions where the parent writes text output after
		// receiving some agent results, masking still-running agents earlier
		// in the activity sequence. Only agent/task calls are checked — regular
		// tools (Read, Bash, Write) can legitimately lack results after
		// interruptions or context compaction without meaning the session is ongoing.
		return hasPendingAgents(chunks)
	}

	// Fallback for old-style chunks without structured items:
	// ongoing if the last AI chunk has no end_turn stop reason.
	for i := len(chunks) - 1; i >= 0; i-- {
		if chunks[i].Type == AIChunk {
			return chunks[i].StopReason != "end_turn"
		}
	}

	return false
}

// hasPendingAgents checks whether any agent/task tool call is still awaiting a
// result. Checks ItemSubagent items (Task, Agent, Skill) and ItemToolCall
// items where ToolName is "Task" or "Agent" — regular tools (Read, Bash,
// Write, etc.) execute and return within seconds, so a missing result means
// the session was interrupted or the JSONL is incomplete, not evidence of
// ongoing work.
func hasPendingAgents(chunks []Chunk) bool {
	for _, chunk := range chunks {
		if chunk.Type != AIChunk {
			continue
		}
		for _, item := range chunk.Items {
			switch item.Type {
			case ItemSubagent:
				if item.ToolResult == "" {
					return true
				}
			case ItemToolCall:
				if (item.ToolName == "Task" || item.ToolName == "Agent") && item.ToolResult == "" {
					return true
				}
			}
		}
	}
	return false
}

// isOngoingFromActivities determines ongoing state from collected activities.
// Ported from claude-devtools sessionStateDetection.ts.
func isOngoingFromActivities(activities []activityType) bool {
	if len(activities) == 0 {
		return false
	}

	// Find the index of the last ending event.
	lastEndingIdx := -1
	for i := len(activities) - 1; i >= 0; i-- {
		if activities[i].isEndingEvent() {
			lastEndingIdx = i
			break
		}
	}

	// No ending event: ongoing if there's any AI activity at all.
	// Otherwise: ongoing if there's AI activity AFTER the last ending event.
	for i, a := range activities {
		if i > lastEndingIdx && a.isAIActivity() {
			return true
		}
	}

	return false
}
