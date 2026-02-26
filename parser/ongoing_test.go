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

func TestIsOngoing_PendingTaskMaskedByTextOutput(t *testing.T) {
	// Simulates a team session where Agent A completed and Agent B is still
	// running. The parent wrote text output after processing Agent A's result,
	// which the activity-based check sees as the last ending event with no
	// AI activity after it. But Agent B's Task tool call has no result yet.
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	taskInputA := json.RawMessage(`{"subagent_type":"general-purpose","description":"Agent A"}`)
	taskInputB := json.RawMessage(`{"subagent_type":"general-purpose","description":"Agent B"}`)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		// Initial AI turn: spawn both agents.
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Spawning the team."},
				{Type: "tool_use", ToolID: "taskA", ToolName: "Task", ToolInput: taskInputA},
				{Type: "tool_use", ToolID: "taskB", ToolName: "Task", ToolInput: taskInputB},
			},
			ToolCalls: []parser.ToolCall{
				{ID: "taskA", Name: "Task"},
				{ID: "taskB", Name: "Task"},
			},
		},
		// Agent A completes — tool_result for taskA.
		parser.AIMsg{
			Timestamp: t0.Add(30 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "taskA", Content: "Agent A finished."},
			},
		},
		// Parent processes the result and writes text output.
		parser.AIMsg{
			Timestamp: t0.Add(31 * time.Second),
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Agent A completed. Waiting for Agent B."},
			},
		},
		// Agent B is still running — no tool_result for taskB.
	})
	if !parser.IsOngoing(chunks) {
		t.Error("should be ongoing: taskB has no result, Agent B is still running")
	}
}

func TestIsOngoing_AllTasksCompleted(t *testing.T) {
	// Same structure as above but both agents have completed.
	// Should NOT be ongoing.
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	taskInputA := json.RawMessage(`{"subagent_type":"general-purpose","description":"Agent A"}`)
	taskInputB := json.RawMessage(`{"subagent_type":"general-purpose","description":"Agent B"}`)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Spawning the team."},
				{Type: "tool_use", ToolID: "taskA", ToolName: "Task", ToolInput: taskInputA},
				{Type: "tool_use", ToolID: "taskB", ToolName: "Task", ToolInput: taskInputB},
			},
			ToolCalls: []parser.ToolCall{
				{ID: "taskA", Name: "Task"},
				{ID: "taskB", Name: "Task"},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(30 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "taskA", Content: "Agent A finished."},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(60 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "taskB", Content: "Agent B finished."},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(61 * time.Second),
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Both agents completed successfully."},
			},
		},
	})
	if parser.IsOngoing(chunks) {
		t.Error("should not be ongoing: all tasks have results and session ends with text")
	}
}

func TestIsOngoing_PendingRegularToolCall(t *testing.T) {
	// A regular tool call (not Task) without a result should also be ongoing.
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	chunks := parser.BuildChunks([]parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "c1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make test"}`)},
				{Type: "tool_use", ToolID: "c2", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"x.go"}`)},
			},
			ToolCalls: []parser.ToolCall{
				{ID: "c1", Name: "Bash"},
				{ID: "c2", Name: "Read"},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(1 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "c1", Content: "ok"},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(2 * time.Second),
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Bash finished."},
			},
		},
		// c2 (Read) still has no result.
	})
	if !parser.IsOngoing(chunks) {
		t.Error("should be ongoing: Read tool call c2 has no result")
	}
}
