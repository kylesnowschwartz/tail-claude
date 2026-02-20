package parser

import (
	"encoding/json"
	"regexp"
	"strings"
)

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

// activity tracks an event type and its position in the activity stream.
type activity struct {
	typ   activityType
	index int
}

// isEndingEvent returns true if this activity type terminates an ongoing session.
func (a activity) isEndingEvent() bool {
	return a.typ == actTextOutput || a.typ == actInterruption || a.typ == actExitPlanMode
}

// isAIActivity returns true if this activity type represents AI work in progress.
func (a activity) isAIActivity() bool {
	return a.typ == actThinking || a.typ == actToolUse || a.typ == actToolResult
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
// A session is ongoing if there's AI activity (thinking, tool_use, tool_result)
// after the last "ending event."
//
// Ending events:
//   - Text output with non-empty content
//   - User interruption messages
//   - ExitPlanMode tool calls
//   - SendMessage shutdown_response with approve: true
//
// If no ending event exists, it's ongoing if there's any AI activity at all.
//
// For chunks without structured items (old-style), falls back to checking
// whether the last chunk is an AI chunk without a stop_reason of "end_turn".
func IsOngoing(chunks []Chunk) bool {
	if len(chunks) == 0 {
		return false
	}

	// Collect activities from structured items across all chunks.
	var activities []activity
	actIdx := 0
	hasItems := false

	// Track tool_use IDs that are shutdown approvals so their tool_results
	// are also treated as ending events.
	shutdownToolIDs := make(map[string]bool)

	for _, chunk := range chunks {
		if chunk.Type != AIChunk {
			continue
		}

		if len(chunk.Items) == 0 {
			continue
		}
		hasItems = true

		for _, item := range chunk.Items {
			switch item.Type {
			case ItemThinking:
				activities = append(activities, activity{typ: actThinking, index: actIdx})
				actIdx++

			case ItemOutput:
				if strings.TrimSpace(item.Text) != "" {
					activities = append(activities, activity{typ: actTextOutput, index: actIdx})
					actIdx++
				}

			case ItemToolCall:
				if item.ToolName == "ExitPlanMode" {
					activities = append(activities, activity{typ: actExitPlanMode, index: actIdx})
					actIdx++
				} else if isShutdownApproval(item.ToolName, item.ToolInput) {
					shutdownToolIDs[item.ToolID] = true
					activities = append(activities, activity{typ: actInterruption, index: actIdx})
					actIdx++
				} else {
					activities = append(activities, activity{typ: actToolUse, index: actIdx})
					actIdx++
				}

				// If this tool call has a result, track it too.
				if item.ToolResult != "" {
					if shutdownToolIDs[item.ToolID] {
						activities = append(activities, activity{typ: actInterruption, index: actIdx})
					} else {
						activities = append(activities, activity{typ: actToolResult, index: actIdx})
					}
					actIdx++
				}

			case ItemSubagent:
				// Subagent spawns are AI activity (like tool_use).
				activities = append(activities, activity{typ: actToolUse, index: actIdx})
				actIdx++
				if item.ToolResult != "" {
					activities = append(activities, activity{typ: actToolResult, index: actIdx})
					actIdx++
				}
			}
		}
	}

	// If we had items, use the activity-based detection.
	if hasItems {
		return isOngoingFromActivities(activities)
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

// isOngoingFromActivities determines ongoing state from collected activities.
// Ported from claude-devtools sessionStateDetection.ts.
func isOngoingFromActivities(activities []activity) bool {
	if len(activities) == 0 {
		return false
	}

	// Find the index of the last ending event.
	lastEndingIdx := -1
	for i := len(activities) - 1; i >= 0; i-- {
		if activities[i].isEndingEvent() {
			lastEndingIdx = activities[i].index
			break
		}
	}

	// No ending event: ongoing if there's any AI activity at all.
	if lastEndingIdx == -1 {
		for _, a := range activities {
			if a.isAIActivity() {
				return true
			}
		}
		return false
	}

	// Check for AI activity AFTER the last ending event.
	for _, a := range activities {
		if a.index > lastEndingIdx && a.isAIActivity() {
			return true
		}
	}

	return false
}
