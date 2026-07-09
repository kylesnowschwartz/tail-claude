package copilot

import (
	"encoding/json"
	"strings"

	"github.com/kylesnowschwartz/agent-ouija/claude/discover"
	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
)

// Reader carries classification state across incremental reads: the current
// model (session.start selectedModel, updated by session.model_change and
// assistant.message model fields), the session metadata for the info bar,
// and the last event type for ongoing detection. All other classification
// is stateless.
//
// inFlight/deferred handle mid-turn auto-compaction: Copilot can emit
// session.compaction_complete BETWEEN a top-level tool.execution_start and
// its tool.execution_complete. Emitting the CompactMsg there would make
// transcript.BuildChunks flush the AI buffer mid-pair, orphaning the
// tool_use (no result, no duration) and rendering the later tool_result as
// bare assistant prose in a model-less chunk. Instead the CompactMsg is
// deferred until no top-level tool call is in flight; callers drain it via
// TakeDeferred after each event.
type Reader struct {
	Model         string
	Meta          discover.SessionMeta // Cwd/GitBranch; PermissionMode always ""
	SessionID     string
	LastEventType string

	inFlight map[string]struct{}        // top-level toolCallIDs started but not yet completed
	deferred []transcript.ClassifiedMsg // CompactMsgs held while a tool call is in flight
}

// NewReader returns a Reader with empty state. A watcher must construct a
// fresh Reader whenever it resets to offset 0 (file truncation) so
// classification state restarts with the file.
func NewReader() *Reader {
	return &Reader{inFlight: make(map[string]struct{})}
}

// TakeDeferred returns compaction messages that were deferred while a
// top-level tool call was in flight, once no call remains in flight, and
// clears them from the Reader. Returns nil while a call is still pending.
// ReadIncremental calls this after every event; direct ClassifyEvent
// callers must do the same or deferred compaction messages are lost.
func (r *Reader) TakeDeferred() []transcript.ClassifiedMsg {
	if len(r.inFlight) > 0 || len(r.deferred) == 0 {
		return nil
	}
	out := r.deferred
	r.deferred = nil
	return out
}

// ClassifyEvent maps a Copilot event to a transcript.ClassifiedMsg.
// Returns false for events that carry no conversation content (lifecycle,
// permissions, hooks, subagent-internal events, unknown types). Pure with
// respect to everything except the Reader's own fields.
func (r *Reader) ClassifyEvent(ev Event) (transcript.ClassifiedMsg, bool) {
	r.LastEventType = ev.Type

	// Subagent-internal events: the parent task tool call stays visible;
	// the nested transcript is a v1 non-goal.
	if ev.AgentID != "" {
		return nil, false
	}

	switch ev.Type {
	case "session.start", "session.resume":
		var d SessionStartData
		if json.Unmarshal(ev.Data, &d) == nil {
			if d.SessionID != "" {
				r.SessionID = d.SessionID
			}
			if d.SelectedModel != "" {
				r.Model = d.SelectedModel
			}
			r.updateContext(d.Context)
		}
		return nil, false

	case "session.context_changed":
		var d ContextData
		if json.Unmarshal(ev.Data, &d) == nil {
			r.updateContext(d)
		}
		return nil, false

	case "session.model_change":
		var d ModelChangeData
		if json.Unmarshal(ev.Data, &d) == nil && d.NewModel != "" {
			r.Model = d.NewModel
		}
		return nil, false

	case "user.message":
		var d UserMessageData
		if json.Unmarshal(ev.Data, &d) != nil {
			return nil, false
		}
		// Skill/instruction-injected synthetic messages are not user input.
		if d.Source != "" {
			return nil, false
		}
		if strings.TrimSpace(d.Content) == "" {
			return nil, false
		}
		// A new user turn means any still-open tool call from a previous
		// turn will never complete; stop holding deferred compactions on it.
		clear(r.inFlight)
		return transcript.UserMsg{
			Timestamp: ev.Timestamp,
			Text:      d.Content, // content, never transformedContent
		}, true

	case "assistant.message":
		var d AssistantMessageData
		if json.Unmarshal(ev.Data, &d) != nil {
			return nil, false
		}
		// Messages emitted inside a tool run (e.g. task) are not top-level turns.
		if d.ParentToolCallID != "" {
			return nil, false
		}
		model := d.Model
		if model == "" {
			model = r.Model
		} else {
			r.Model = d.Model
		}

		var blocks []transcript.ContentBlock
		thinking := 0
		if d.ReasoningText != "" {
			thinking = 1
			blocks = append(blocks, transcript.ContentBlock{
				Type: "thinking",
				Text: d.ReasoningText,
			})
		} else if d.ReasoningOpaque != "" {
			// Encrypted reasoning: count it (badge stays truthful) but emit
			// no block -- mirrors Claude's encrypted-thinking handling.
			thinking = 1
		}
		if d.Content != "" {
			blocks = append(blocks, transcript.ContentBlock{
				Type: "text",
				Text: d.Content,
			})
		}
		// toolRequests are ignored: tool_use blocks come from
		// tool.execution_start, avoiding double emission.
		return transcript.AIMsg{
			Timestamp:     ev.Timestamp,
			Model:         model,
			Text:          d.Content,
			ThinkingCount: thinking,
			Blocks:        blocks,
			Usage:         transcript.Usage{OutputTokens: d.OutputTokens},
		}, true

	case "assistant.reasoning":
		var d ReasoningData
		if json.Unmarshal(ev.Data, &d) != nil {
			return nil, false
		}
		msg := transcript.AIMsg{
			Timestamp:     ev.Timestamp,
			Model:         r.Model,
			ThinkingCount: 1,
		}
		if d.Content != "" {
			msg.Blocks = []transcript.ContentBlock{{
				Type: "thinking",
				Text: d.Content,
			}}
		}
		return msg, true

	case "tool.execution_start":
		var d ToolStartData
		if json.Unmarshal(ev.Data, &d) != nil {
			return nil, false
		}
		if d.ParentToolCallID != "" {
			return nil, false
		}
		if d.ToolCallID != "" {
			r.inFlight[d.ToolCallID] = struct{}{}
		}
		return transcript.AIMsg{
			Timestamp: ev.Timestamp,
			Model:     r.Model,
			ToolCalls: []transcript.ToolCall{{ID: d.ToolCallID, Name: d.ToolName}},
			Blocks: []transcript.ContentBlock{{
				Type:      "tool_use",
				ToolID:    d.ToolCallID,
				ToolName:  d.ToolName,
				ToolInput: d.Arguments,
			}},
		}, true

	case "tool.execution_complete":
		var d ToolCompleteData
		if json.Unmarshal(ev.Data, &d) != nil {
			return nil, false
		}
		if d.ParentToolCallID != "" {
			return nil, false
		}
		delete(r.inFlight, d.ToolCallID)
		return transcript.AIMsg{
			Timestamp: ev.Timestamp,
			IsMeta:    true,
			Blocks: []transcript.ContentBlock{{
				Type:    "tool_result",
				ToolID:  d.ToolCallID,
				Content: pickResultText(d),
				IsError: !d.Success,
			}},
		}, true

	case "session.compaction_complete":
		var d CompactionCompleteData
		if json.Unmarshal(ev.Data, &d) != nil {
			return nil, false
		}
		if !d.Success {
			out := "compaction failed"
			if d.Error != "" {
				out += ": " + d.Error
			}
			return transcript.SystemMsg{
				Timestamp: ev.Timestamp,
				Output:    out,
				IsError:   true,
			}, true
		}
		text := d.SummaryContent
		if text == "" {
			text = "context compacted"
		}
		msg := transcript.CompactMsg{Timestamp: ev.Timestamp, Text: text}
		// Copilot auto-compacts mid-turn, including between a top-level
		// tool.execution_start and its tool.execution_complete. Defer the
		// CompactMsg until the pair closes so BuildChunks never splits a
		// tool_use from its tool_result (see Reader doc).
		if len(r.inFlight) > 0 {
			r.deferred = append(r.deferred, msg)
			return nil, false
		}
		return msg, true

	case "session.error":
		var d ErrorData
		if json.Unmarshal(ev.Data, &d) != nil {
			return nil, false
		}
		out := d.Message
		if d.ErrorType != "" {
			out = "[" + d.ErrorType + "] " + d.Message
		}
		// A session error can abandon an open tool call; release any
		// deferred compaction rather than holding it forever.
		clear(r.inFlight)
		return transcript.SystemMsg{
			Timestamp: ev.Timestamp,
			Output:    out,
			IsError:   true,
		}, true

	case "abort":
		// An abort abandons any open tool call; release deferred compactions.
		clear(r.inFlight)
		return transcript.SystemMsg{
			Timestamp: ev.Timestamp,
			Output:    "aborted by user",
		}, true

	case "session.info":
		var d InfoData
		if json.Unmarshal(ev.Data, &d) != nil {
			return nil, false
		}
		return transcript.SystemMsg{
			Timestamp: ev.Timestamp,
			Output:    d.Message,
		}, true
	}

	// Everything else -- turn brackets, lifecycle/config, permissions, hooks,
	// system prompts, subagent markers, unknown future types -- drops.
	return nil, false
}

// updateContext applies a context snapshot to the session metadata,
// keeping prior values when the snapshot omits a field.
func (r *Reader) updateContext(ctx ContextData) {
	if ctx.Cwd != "" {
		r.Meta.Cwd = ctx.Cwd
	}
	if ctx.Branch != "" {
		r.Meta.GitBranch = ctx.Branch
	}
}

// pickResultText selects the display string for a tool result:
// result.content, falling back to detailedContent, then displayContent;
// error events (result absent) render as "[code] message".
func pickResultText(d ToolCompleteData) string {
	if d.Result != nil {
		if d.Result.Content != "" {
			return d.Result.Content
		}
		if d.Result.DetailedContent != "" {
			return d.Result.DetailedContent
		}
		if d.Result.DisplayContent != "" {
			return d.Result.DisplayContent
		}
	}
	if d.Error != nil {
		if d.Error.Code != "" {
			return "[" + d.Error.Code + "] " + d.Error.Message
		}
		return d.Error.Message
	}
	return ""
}
