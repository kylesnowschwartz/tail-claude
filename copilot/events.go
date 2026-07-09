// Package copilot parses GitHub Copilot CLI session-state files
// (~/.copilot/session-state) into agent-ouija transcript types so the
// existing tail-claude pipeline (BuildChunks -> chunksToMessages -> TUI)
// renders them unchanged.
//
// This package is the sole exception to "parsing lives in agent-ouija":
// Copilot is a non-Claude source that maps onto ouija's exported types
// (transcript.ClassifiedMsg, transcript.Chunk, discover.SessionInfo)
// without forking the library. Everything here is pure transformation;
// file IO lives only in the Read* and Discover* functions.
package copilot

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
)

// Event is the envelope shared by every line in a Copilot events.jsonl file.
// Data stays raw; typed payloads are decoded lazily per event type.
type Event struct {
	Type      string
	Data      json.RawMessage
	ID        string
	Timestamp time.Time
	ParentID  *string
	AgentID   string // set only on events emitted inside a subagent run
}

// ParseEvent deserializes one JSONL line into an Event.
// Returns false for blank or unparseable lines.
func ParseEvent(line []byte) (Event, bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return Event{}, false
	}
	var raw struct {
		Type      string          `json:"type"`
		Data      json.RawMessage `json:"data"`
		ID        string          `json:"id"`
		Timestamp string          `json:"timestamp"`
		ParentID  *string         `json:"parentId"`
		AgentID   string          `json:"agentId"`
	}
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return Event{}, false
	}
	if raw.Type == "" {
		return Event{}, false
	}
	return Event{
		Type:      raw.Type,
		Data:      raw.Data,
		ID:        raw.ID,
		Timestamp: transcript.ParseTimestamp(raw.Timestamp),
		ParentID:  raw.ParentID,
		AgentID:   raw.AgentID,
	}, true
}

// --- Typed payloads, decoded per event type ---

// ContextData is the working-directory snapshot carried by session.start,
// session.resume (nested under "context") and session.context_changed
// (the data object itself).
type ContextData struct {
	Cwd     string `json:"cwd"`
	GitRoot string `json:"gitRoot"`
	Branch  string `json:"branch"`
}

// SessionStartData covers both session.start and session.resume
// (resume lacks sessionId/startTime; unset fields stay empty).
type SessionStartData struct {
	SessionID      string      `json:"sessionId"`
	StartTime      string      `json:"startTime"`
	SelectedModel  string      `json:"selectedModel"`
	CopilotVersion string      `json:"copilotVersion"`
	Context        ContextData `json:"context"`
}

// UserMessageData is the payload of user.message events.
// transformedContent (the expanded prompt sent to the model) is deliberately
// not modeled: only the raw content the user typed is ever displayed.
type UserMessageData struct {
	Content  string `json:"content"`
	Source   string `json:"source"`   // non-empty on skill/instruction-injected synthetic messages
	Delivery string `json:"delivery"` // "idle" for messages queued while the agent was idle
}

// ToolRequest is a tool call embedded in an assistant.message. Decoded but
// never used for block emission -- tool_use blocks come from
// tool.execution_start, which is present for every executed call.
type ToolRequest struct {
	ToolCallID string          `json:"toolCallId"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
}

// AssistantMessageData is the payload of assistant.message events.
type AssistantMessageData struct {
	Content          string        `json:"content"`
	Model            string        `json:"model"`
	ReasoningText    string        `json:"reasoningText"`
	ReasoningOpaque  string        `json:"reasoningOpaque"`
	Phase            string        `json:"phase"`
	ParentToolCallID string        `json:"parentToolCallId"`
	OutputTokens     int           `json:"outputTokens"`
	ToolRequests     []ToolRequest `json:"toolRequests"`
}

// ReasoningData is the payload of standalone assistant.reasoning events.
type ReasoningData struct {
	Content string `json:"content"`
}

// ToolStartData is the payload of tool.execution_start events.
type ToolStartData struct {
	ToolCallID       string          `json:"toolCallId"`
	ToolName         string          `json:"toolName"`
	ParentToolCallID string          `json:"parentToolCallId"`
	Arguments        json.RawMessage `json:"arguments"`
}

// ToolResult is the result object of a tool.execution_complete event.
// Always object-or-absent in the wire format, never a bare string.
type ToolResult struct {
	Content         string `json:"content"`
	DetailedContent string `json:"detailedContent"`
	DisplayContent  string `json:"displayContent"`
}

// ToolError is present on tool.execution_complete exactly when result is absent.
type ToolError struct {
	Code    string `json:"code"` // "failure", "denied", "rejected"
	Message string `json:"message"`
}

// ToolCompleteData is the payload of tool.execution_complete events.
type ToolCompleteData struct {
	ToolCallID       string      `json:"toolCallId"`
	ParentToolCallID string      `json:"parentToolCallId"`
	Success          bool        `json:"success"`
	Result           *ToolResult `json:"result"`
	Error            *ToolError  `json:"error"`
}

// CompactionCompleteData is the payload of session.compaction_complete events.
type CompactionCompleteData struct {
	Success        bool   `json:"success"`
	SummaryContent string `json:"summaryContent"`
	Error          string `json:"error"`
}

// ErrorData is the payload of session.error events.
type ErrorData struct {
	ErrorType string `json:"errorType"`
	Message   string `json:"message"`
}

// InfoData is the payload of session.info events.
type InfoData struct {
	Message string `json:"message"`
}

// ModelChangeData is the payload of session.model_change events.
type ModelChangeData struct {
	NewModel string `json:"newModel"`
}

// AbortData is the payload of abort events.
type AbortData struct {
	Reason string `json:"reason"` // "user initiated" or "user_initiated"
}
