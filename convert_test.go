package main

import (
	"strings"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestChunksToMessages(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	chunks := []parser.Chunk{
		{
			Type:      parser.UserChunk,
			Timestamp: ts,
			UserText:  "Hello",
		},
		{
			Type:          parser.AIChunk,
			Timestamp:     ts.Add(1 * time.Second),
			Model:         "claude-opus-4-6",
			Text:          "Response here",
			ThinkingCount: 2,
			ToolCalls:     []parser.ToolCall{{ID: "t1", Name: "Bash"}, {ID: "t2", Name: "Read"}},
			Usage:         parser.Usage{InputTokens: 100, OutputTokens: 50},
			DurationMs:    3500,
		},
		{
			Type:      parser.SystemChunk,
			Timestamp: ts.Add(2 * time.Second),
			Output:    "Command output",
		},
	}

	msgs := chunksToMessages(chunks, nil)
	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %d, want 3", len(msgs))
	}

	// User message
	if msgs[0].role != RoleUser {
		t.Errorf("msgs[0].role = %q, want %q", msgs[0].role, RoleUser)
	}
	if msgs[0].content != "Hello" {
		t.Errorf("msgs[0].content = %q, want %q", msgs[0].content, "Hello")
	}

	// AI message
	if msgs[1].role != RoleClaude {
		t.Errorf("msgs[1].role = %q, want %q", msgs[1].role, RoleClaude)
	}
	if msgs[1].model != "opus4.6" {
		t.Errorf("msgs[1].model = %q, want %q", msgs[1].model, "opus4.6")
	}
	if msgs[1].thinkingCount != 2 {
		t.Errorf("msgs[1].thinkingCount = %d, want 2", msgs[1].thinkingCount)
	}
	if msgs[1].toolCallCount != 2 {
		t.Errorf("msgs[1].toolCallCount = %d, want 2", msgs[1].toolCallCount)
	}
	if msgs[1].tokensRaw != 150 {
		t.Errorf("msgs[1].tokensRaw = %d, want 150", msgs[1].tokensRaw)
	}
	if msgs[1].durationMs != 3500 {
		t.Errorf("msgs[1].durationMs = %d, want 3500", msgs[1].durationMs)
	}

	// System message
	if msgs[2].role != RoleSystem {
		t.Errorf("msgs[2].role = %q, want %q", msgs[2].role, RoleSystem)
	}
	if msgs[2].content != "Command output" {
		t.Errorf("msgs[2].content = %q, want %q", msgs[2].content, "Command output")
	}
}

func TestChunksToMessages_EmptyToolCalls(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	chunks := []parser.Chunk{
		{
			Type:      parser.AIChunk,
			Timestamp: ts,
			Model:     "claude-opus-4-6",
			Text:      "No tools used",
			// ToolCalls deliberately nil
		},
	}
	msgs := chunksToMessages(chunks, nil)
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].toolCallCount != 0 {
		t.Errorf("toolCalls = %d, want 0 for nil ToolCalls slice", msgs[0].toolCallCount)
	}
}

func TestChunksToMessages_CompactChunk(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	chunks := []parser.Chunk{
		{
			Type:      parser.CompactChunk,
			Timestamp: ts,
			Output:    "Context compressed here",
		},
	}
	msgs := chunksToMessages(chunks, nil)
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].role != RoleCompact {
		t.Errorf("role = %q, want %q", msgs[0].role, RoleCompact)
	}
	if msgs[0].content != "Context compressed here" {
		t.Errorf("content = %q, want %q", msgs[0].content, "Context compressed here")
	}
}

func TestDisplayItemFromParser(t *testing.T) {
	t.Run("tool call with JSON input is pretty-printed", func(t *testing.T) {
		it := parser.DisplayItem{
			Type:        parser.ItemToolCall,
			ToolName:    "Read",
			ToolInput:   []byte(`{"file_path":"/foo/bar.go"}`),
			ToolSummary: "/foo/bar.go",
			ToolResult:  "file contents",
		}
		got := displayItemFromParser(it)
		if got.toolName != "Read" {
			t.Errorf("toolName = %q, want %q", got.toolName, "Read")
		}
		if !strings.Contains(got.toolInput, "\n") {
			t.Errorf("toolInput should be pretty-printed (have newlines), got: %q", got.toolInput)
		}
		if !strings.Contains(got.toolInput, "file_path") {
			t.Errorf("toolInput should contain the key, got: %q", got.toolInput)
		}
		if got.toolResult != "file contents" {
			t.Errorf("toolResult = %q, want %q", got.toolResult, "file contents")
		}
	})

	t.Run("thinking block — text only, no tool fields", func(t *testing.T) {
		it := parser.DisplayItem{
			Type: parser.ItemThinking,
			Text: "Let me think about this...",
		}
		got := displayItemFromParser(it)
		if got.text != "Let me think about this..." {
			t.Errorf("text = %q, want %q", got.text, "Let me think about this...")
		}
		if got.toolInput != "" {
			t.Errorf("toolInput should be empty for thinking block, got %q", got.toolInput)
		}
	})

	t.Run("malformed JSON falls back to raw bytes", func(t *testing.T) {
		it := parser.DisplayItem{
			Type:      parser.ItemToolCall,
			ToolInput: []byte(`{not valid json`),
		}
		got := displayItemFromParser(it)
		// Should fall back to raw string, not empty
		if got.toolInput == "" {
			t.Error("toolInput should not be empty when JSON indentation fails")
		}
		if got.toolInput != "{not valid json" {
			t.Errorf("toolInput = %q, want raw fallback %q", got.toolInput, "{not valid json")
		}
	})
}

func TestConvertDisplayItems(t *testing.T) {
	t.Run("empty slice returns nil", func(t *testing.T) {
		got := convertDisplayItems(nil, nil)
		if got != nil {
			t.Errorf("convertDisplayItems(nil, nil) = %v, want nil", got)
		}
	})

	t.Run("links SubagentProcess by ToolID match", func(t *testing.T) {
		toolID := "tool-abc-123"
		proc := parser.SubagentProcess{
			ParentTaskID: toolID,
			ID:           "agent-xyz",
		}
		items := []parser.DisplayItem{
			{Type: parser.ItemSubagent, ToolID: toolID, ToolName: "Task"},
		}
		got := convertDisplayItems(items, []parser.SubagentProcess{proc})
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if got[0].subagentProcess == nil {
			t.Fatal("subagentProcess should be linked, got nil")
		}
		if got[0].subagentProcess.ID != "agent-xyz" {
			t.Errorf("subagentProcess.ID = %q, want %q", got[0].subagentProcess.ID, "agent-xyz")
		}
	})

	t.Run("unmatched ItemSubagent gets nil process", func(t *testing.T) {
		items := []parser.DisplayItem{
			{Type: parser.ItemSubagent, ToolID: "no-match", ToolName: "Task"},
		}
		proc := parser.SubagentProcess{ParentTaskID: "other-id"}
		got := convertDisplayItems(items, []parser.SubagentProcess{proc})
		if got[0].subagentProcess != nil {
			t.Errorf("unmatched subagent should have nil process, got %v", got[0].subagentProcess)
		}
	})

	t.Run("non-subagent items get nil process regardless of match", func(t *testing.T) {
		proc := parser.SubagentProcess{ParentTaskID: "tool-1"}
		items := []parser.DisplayItem{
			{Type: parser.ItemToolCall, ToolID: "tool-1"},
		}
		got := convertDisplayItems(items, []parser.SubagentProcess{proc})
		if got[0].subagentProcess != nil {
			t.Errorf("non-subagent item should have nil process, got %v", got[0].subagentProcess)
		}
	})
}

func TestBuildSubagentMessage(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	proc := &parser.SubagentProcess{
		ID:        "agent-1",
		StartTime: ts,
		Usage:     parser.Usage{InputTokens: 200, OutputTokens: 100},
		DurationMs: 5000,
		Chunks: []parser.Chunk{
			{
				Type:     parser.UserChunk,
				UserText: "Explore the codebase",
			},
			{
				Type:  parser.AIChunk,
				Model: "claude-opus-4-6",
				Items: []parser.DisplayItem{
					{Type: parser.ItemThinking, Text: "thinking..."},
					{Type: parser.ItemToolCall, ToolName: "Read"},
					{Type: parser.ItemToolCall, ToolName: "Grep"},
					{Type: parser.ItemOutput, Text: "Found the thing"},
				},
			},
		},
	}

	got := buildSubagentMessage(proc, "Explore")

	if got.role != RoleClaude {
		t.Errorf("role = %q, want %q", got.role, RoleClaude)
	}
	if got.subagentLabel != "Explore" {
		t.Errorf("subagentLabel = %q, want %q", got.subagentLabel, "Explore")
	}
	if got.model != "opus4.6" {
		t.Errorf("model = %q, want %q (extracted from first AI chunk)", got.model, "opus4.6")
	}
	if got.thinkingCount != 1 {
		t.Errorf("thinkingCount = %d, want 1", got.thinkingCount)
	}
	if got.toolCallCount != 2 {
		t.Errorf("toolCallCount = %d, want 2", got.toolCallCount)
	}
	// UserChunk becomes an ItemOutput "Input" item; AIChunk ItemOutput also counted
	if got.messages != 2 {
		t.Errorf("messages = %d, want 2 (1 user input + 1 output item)", got.messages)
	}
	if got.tokensRaw != 300 {
		t.Errorf("tokensRaw = %d, want 300", got.tokensRaw)
	}
	if got.durationMs != 5000 {
		t.Errorf("durationMs = %d, want 5000", got.durationMs)
	}
}

func TestCurrentDetailMsg(t *testing.T) {
	t.Run("returns cursor message normally", func(t *testing.T) {
		m := testModel()
		m.cursor = 1
		got := m.currentDetailMsg()
		if got.role != RoleClaude {
			t.Errorf("role = %q, want %q", got.role, RoleClaude)
		}
	})

	t.Run("returns traceMsg when set", func(t *testing.T) {
		m := testModel()
		trace := &message{role: RoleClaude, model: "haiku4.5", subagentLabel: "Explore"}
		m.traceMsg = trace
		got := m.currentDetailMsg()
		if got.subagentLabel != "Explore" {
			t.Errorf("subagentLabel = %q, want %q", got.subagentLabel, "Explore")
		}
	})

	t.Run("returns empty message for out-of-bounds cursor", func(t *testing.T) {
		m := testModel()
		m.cursor = 999
		got := m.currentDetailMsg()
		if got.role != "" {
			t.Errorf("out-of-bounds cursor should return empty message, got role %q", got.role)
		}
	})
}
