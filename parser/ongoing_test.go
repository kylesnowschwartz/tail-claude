package parser_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestIsOngoing_EmptyChunks(t *testing.T) {
	if parser.IsOngoing(nil) {
		t.Error("empty chunks should not be ongoing")
	}
}

func TestIsOngoing_LastItemIsTextOutput(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "thinking", Text: "Let me think..."},
				{Type: "text", Text: "Here is the answer."},
			},
		},
	})
	if parser.IsOngoing(chunks) {
		t.Error("session ending with text output should not be ongoing")
	}
}

func TestIsOngoing_LastItemIsToolUseNoResult(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "c1", Name: "Read"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "c1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"x.go"}`)},
			},
		},
	})
	if !parser.IsOngoing(chunks) {
		t.Error("tool_use with no result should be ongoing")
	}
}

func TestIsOngoing_LastItemIsThinking(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "thinking", Text: "Hmm, let me consider..."},
			},
		},
	})
	if !parser.IsOngoing(chunks) {
		t.Error("thinking with no following output should be ongoing")
	}
}

func TestIsOngoing_ToolCallAfterTextOutput(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "c1", Name: "Bash"}},
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Let me check that."},
				{Type: "tool_use", ToolID: "c1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`)},
			},
		},
	})
	if !parser.IsOngoing(chunks) {
		t.Error("tool_use after text output should be ongoing (activity after ending event)")
	}
}

func TestIsOngoing_ExitPlanModeIsEndingEvent(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "c1", Name: "ExitPlanMode"}},
			Blocks: []parser.ContentBlock{
				{Type: "thinking", Text: "Planning..."},
				{Type: "tool_use", ToolID: "c1", ToolName: "ExitPlanMode", ToolInput: json.RawMessage(`{}`)},
			},
		},
	})
	if parser.IsOngoing(chunks) {
		t.Error("ExitPlanMode should be an ending event")
	}
}

func TestIsOngoing_ToolUseWithResultThenTextIsComplete(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(1 * time.Second)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "c1", Name: "Read"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "c1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"x.go"}`)},
			},
		},
		parser.AIMsg{
			Timestamp: t1,
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "c1", Content: "file contents"},
			},
		},
		parser.AIMsg{
			Timestamp: t1.Add(1 * time.Second),
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "The file looks good."},
			},
		},
	})
	if parser.IsOngoing(chunks) {
		t.Error("tool_use with result followed by text output should not be ongoing")
	}
}

func TestIsOngoing_ShutdownResponseIsEndingEvent(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	shutdownInput := json.RawMessage(`{"type":"shutdown_response","approve":true,"request_id":"abc"}`)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "c1", Name: "SendMessage"}},
			Blocks: []parser.ContentBlock{
				{Type: "thinking", Text: "Shutting down..."},
				{Type: "tool_use", ToolID: "c1", ToolName: "SendMessage", ToolInput: shutdownInput},
			},
		},
	})
	if parser.IsOngoing(chunks) {
		t.Error("SendMessage shutdown_response with approve:true should be an ending event")
	}
}

func TestIsOngoing_Fallback_LastAIChunkEndTurn(t *testing.T) {
	// Old-style chunks without structured items.
	chunks := []parser.Chunk{
		{
			Type:       parser.AIChunk,
			Model:      "claude-opus-4-6",
			Text:       "Done.",
			StopReason: "end_turn",
		},
	}
	if parser.IsOngoing(chunks) {
		t.Error("AI chunk with stop_reason end_turn should not be ongoing (fallback)")
	}
}

func TestIsOngoing_Fallback_LastAIChunkNoStopReason(t *testing.T) {
	// Old-style chunk without stop reason = still streaming.
	chunks := []parser.Chunk{
		{
			Type:  parser.AIChunk,
			Model: "claude-opus-4-6",
			Text:  "Working...",
		},
	}
	if !parser.IsOngoing(chunks) {
		t.Error("AI chunk with no stop_reason should be ongoing (fallback)")
	}
}

func TestIsOngoing_UserChunksOnly(t *testing.T) {
	chunks := []parser.Chunk{
		{Type: parser.UserChunk, UserText: "Hello"},
	}
	if parser.IsOngoing(chunks) {
		t.Error("user-only chunks should not be ongoing")
	}
}

func TestIsOngoing_MultipleChunks_OngoingInLast(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	// First turn: complete (text output)
	// Second turn: ongoing (tool_use, no result)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.UserMsg{Timestamp: t0, Text: "First question"},
		parser.AIMsg{
			Timestamp: t0.Add(1 * time.Second),
			Model:     "claude-opus-4-6",
			Blocks:    []parser.ContentBlock{{Type: "text", Text: "First answer."}},
		},
		parser.UserMsg{Timestamp: t0.Add(5 * time.Second), Text: "Second question"},
		parser.AIMsg{
			Timestamp: t0.Add(6 * time.Second),
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "c1", Name: "Bash"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "c1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make"}`)},
			},
		},
	})
	if !parser.IsOngoing(chunks) {
		t.Error("should be ongoing when last AI chunk has pending tool_use")
	}
}

func TestIsOngoing_SubagentSpawnIsActivity(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	taskInput := json.RawMessage(`{"subagent_type":"Explore","description":"Find stuff"}`)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Let me search."},
				{Type: "tool_use", ToolID: "c1", ToolName: "Task", ToolInput: taskInput},
			},
			ToolCalls: []parser.ToolCall{{ID: "c1", Name: "Task"}},
		},
	})
	if !parser.IsOngoing(chunks) {
		t.Error("subagent spawn (Task tool) after text should be ongoing")
	}
}
