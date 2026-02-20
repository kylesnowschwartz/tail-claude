package parser_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestBuildChunks_SingleUser(t *testing.T) {
	msgs := []parser.ClassifiedMsg{
		parser.UserMsg{
			Timestamp: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			Text:      "Hello",
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].Type != parser.UserChunk {
		t.Errorf("Type = %d, want UserChunk", chunks[0].Type)
	}
	if chunks[0].UserText != "Hello" {
		t.Errorf("UserText = %q, want %q", chunks[0].UserText, "Hello")
	}
}

func TestBuildChunks_UserAIUser(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.UserMsg{Timestamp: t0, Text: "Question"},
		parser.AIMsg{Timestamp: t0.Add(1 * time.Second), Text: "Answer", Model: "claude-opus-4-6"},
		parser.UserMsg{Timestamp: t0.Add(5 * time.Second), Text: "Follow-up"},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(chunks))
	}
	if chunks[0].Type != parser.UserChunk {
		t.Errorf("chunks[0].Type = %d, want UserChunk", chunks[0].Type)
	}
	if chunks[1].Type != parser.AIChunk {
		t.Errorf("chunks[1].Type = %d, want AIChunk", chunks[1].Type)
	}
	if chunks[2].Type != parser.UserChunk {
		t.Errorf("chunks[2].Type = %d, want UserChunk", chunks[2].Type)
	}
}

func TestBuildChunks_ConsecutiveAIMerged(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp:     t0,
			Text:          "First response",
			Model:         "claude-opus-4-6",
			ThinkingCount: 1,
			ToolCalls:     []parser.ToolCall{{ID: "t1", Name: "Bash"}},
			Usage:         parser.Usage{InputTokens: 100, OutputTokens: 50},
		},
		parser.AIMsg{
			Timestamp:     t0.Add(3 * time.Second),
			Text:          "Continued response",
			IsMeta:        true,
			ThinkingCount: 0,
			ToolCalls:     []parser.ToolCall{{ID: "t2", Name: "Read"}},
			Usage:         parser.Usage{InputTokens: 200, OutputTokens: 75},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1 (merged AI)", len(chunks))
	}

	c := chunks[0]
	if c.Type != parser.AIChunk {
		t.Errorf("Type = %d, want AIChunk", c.Type)
	}
	if c.Text != "First response\nContinued response" {
		t.Errorf("Text = %q, want merged text", c.Text)
	}
	if c.ThinkingCount != 1 {
		t.Errorf("Thinking = %d, want 1", c.ThinkingCount)
	}
	if len(c.ToolCalls) != 2 {
		t.Errorf("len(ToolCalls) = %d, want 2", len(c.ToolCalls))
	}
	// Tokens summed.
	if c.Usage.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", c.Usage.InputTokens)
	}
	if c.Usage.OutputTokens != 125 {
		t.Errorf("OutputTokens = %d, want 125", c.Usage.OutputTokens)
	}
}

func TestBuildChunks_AIDuration(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{Timestamp: t0, Text: "start", Model: "claude-opus-4-6"},
		parser.AIMsg{Timestamp: t0.Add(5 * time.Second), Text: "end"},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want 5000", chunks[0].DurationMs)
	}
}

func TestBuildChunks_AIModelFromFirstNonMeta(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{Timestamp: t0, Text: "meta result", IsMeta: true},
		parser.AIMsg{Timestamp: t0.Add(1 * time.Second), Text: "real response", Model: "claude-opus-4-6"},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].Model != "claude-opus-4-6" {
		t.Errorf("Model = %q, want %q", chunks[0].Model, "claude-opus-4-6")
	}
}

func TestBuildChunks_Empty(t *testing.T) {
	chunks := parser.BuildChunks(nil)
	if len(chunks) != 0 {
		t.Errorf("len(chunks) = %d, want 0", len(chunks))
	}
}

// --- DisplayItem tests ---

func TestBuildChunks_Items_ThinkingTextToolUse(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp:     t0,
			Model:         "claude-opus-4-6",
			Text:          "Here is my answer.",
			ThinkingCount: 1,
			ToolCalls:     []parser.ToolCall{{ID: "call_1", Name: "Read"}},
			Blocks: []parser.ContentBlock{
				{Type: "thinking", Text: "Let me think..."},
				{Type: "text", Text: "Here is my answer."},
				{Type: "tool_use", ToolID: "call_1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"/tmp/main.go"}`)},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}

	items := chunks[0].Items
	if len(items) != 3 {
		t.Fatalf("len(Items) = %d, want 3", len(items))
	}

	// Item 0: thinking
	if items[0].Type != parser.ItemThinking {
		t.Errorf("Items[0].Type = %d, want ItemThinking", items[0].Type)
	}
	if items[0].Text != "Let me think..." {
		t.Errorf("Items[0].Text = %q", items[0].Text)
	}
	if items[0].TokenCount != len("Let me think...")/4 {
		t.Errorf("Items[0].TokenCount = %d, want %d", items[0].TokenCount, len("Let me think...")/4)
	}

	// Item 1: text output
	if items[1].Type != parser.ItemOutput {
		t.Errorf("Items[1].Type = %d, want ItemOutput", items[1].Type)
	}
	if items[1].Text != "Here is my answer." {
		t.Errorf("Items[1].Text = %q", items[1].Text)
	}

	// Item 2: tool call
	if items[2].Type != parser.ItemToolCall {
		t.Errorf("Items[2].Type = %d, want ItemToolCall", items[2].Type)
	}
	if items[2].ToolName != "Read" {
		t.Errorf("Items[2].ToolName = %q, want Read", items[2].ToolName)
	}
	if items[2].ToolID != "call_1" {
		t.Errorf("Items[2].ToolID = %q, want call_1", items[2].ToolID)
	}
	if items[2].ToolSummary != "main.go" {
		t.Errorf("Items[2].ToolSummary = %q, want main.go", items[2].ToolSummary)
	}
}

func TestBuildChunks_Items_ToolUseLinkedToResult(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Second)

	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Text:      "",
			ToolCalls: []parser.ToolCall{{ID: "call_1", Name: "Bash"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "call_1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`)},
			},
		},
		parser.AIMsg{
			Timestamp: t1,
			IsMeta:    true,
			Text:      "file1.go\nfile2.go",
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "call_1", Content: "file1.go\nfile2.go", IsError: false},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}

	items := chunks[0].Items
	if len(items) != 1 {
		t.Fatalf("len(Items) = %d, want 1 (tool_use with linked result)", len(items))
	}

	item := items[0]
	if item.Type != parser.ItemToolCall {
		t.Errorf("Type = %d, want ItemToolCall", item.Type)
	}
	if item.ToolResult != "file1.go\nfile2.go" {
		t.Errorf("ToolResult = %q, want file listing", item.ToolResult)
	}
	if item.ToolError {
		t.Error("ToolError should be false")
	}
	if item.DurationMs != 2000 {
		t.Errorf("DurationMs = %d, want 2000", item.DurationMs)
	}
	// Token count should include result tokens
	resultTokens := len("file1.go\nfile2.go") / 4
	inputTokens := len(`{"command":"ls"}`) / 4
	if item.TokenCount != inputTokens+resultTokens {
		t.Errorf("TokenCount = %d, want %d", item.TokenCount, inputTokens+resultTokens)
	}
}

func TestBuildChunks_Items_ToolError(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "call_1", Name: "Bash"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "call_1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"bad"}`)},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(1 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "call_1", Content: "exit code 1", IsError: true},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	items := chunks[0].Items
	if len(items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(items))
	}
	if !items[0].ToolError {
		t.Error("ToolError should be true")
	}
}

func TestBuildChunks_Items_UnmatchedToolResult(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			IsMeta:    true,
			Text:      "orphan result text",
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "no_match", Content: "orphan result text"},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}

	items := chunks[0].Items
	if len(items) != 1 {
		t.Fatalf("len(Items) = %d, want 1 (unmatched becomes output)", len(items))
	}
	if items[0].Type != parser.ItemOutput {
		t.Errorf("Type = %d, want ItemOutput for unmatched result", items[0].Type)
	}
	if items[0].Text != "orphan result text" {
		t.Errorf("Text = %q, want orphan result text", items[0].Text)
	}
}

func TestBuildChunks_Items_MultipleToolCallsInterleaved(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Text:      "Let me check two files.",
			ToolCalls: []parser.ToolCall{
				{ID: "c1", Name: "Read"},
				{ID: "c2", Name: "Read"},
			},
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Let me check two files."},
				{Type: "tool_use", ToolID: "c1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"/a.go"}`)},
				{Type: "tool_use", ToolID: "c2", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"/b.go"}`)},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(1 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "c1", Content: "contents of a"},
				{Type: "tool_result", ToolID: "c2", Content: "contents of b"},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(2 * time.Second),
			Model:     "claude-opus-4-6",
			Text:      "Both look good.",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Both look good."},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}

	items := chunks[0].Items
	// text + tool_use + tool_use + text = 4 items
	if len(items) != 4 {
		t.Fatalf("len(Items) = %d, want 4", len(items))
	}

	// Check ordering
	if items[0].Type != parser.ItemOutput {
		t.Errorf("Items[0].Type = %d, want ItemOutput", items[0].Type)
	}
	if items[1].Type != parser.ItemToolCall {
		t.Errorf("Items[1].Type = %d, want ItemToolCall", items[1].Type)
	}
	if items[1].ToolResult != "contents of a" {
		t.Errorf("Items[1].ToolResult = %q, want 'contents of a'", items[1].ToolResult)
	}
	if items[2].Type != parser.ItemToolCall {
		t.Errorf("Items[2].Type = %d, want ItemToolCall", items[2].Type)
	}
	if items[2].ToolResult != "contents of b" {
		t.Errorf("Items[2].ToolResult = %q, want 'contents of b'", items[2].ToolResult)
	}
	if items[3].Type != parser.ItemOutput {
		t.Errorf("Items[3].Type = %d, want ItemOutput", items[3].Type)
	}
}

func TestBuildChunks_Items_ToolSummaryPopulated(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "c1", Name: "Bash"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "c1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"go test ./...","description":"Run tests"}`)},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	items := chunks[0].Items
	if len(items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(items))
	}
	if items[0].ToolSummary != "Run tests" {
		t.Errorf("ToolSummary = %q, want 'Run tests'", items[0].ToolSummary)
	}
}

func TestBuildChunks_Items_FlatFieldsStillPopulated(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp:     t0,
			Model:         "claude-opus-4-6",
			Text:          "answer",
			ThinkingCount: 1,
			ToolCalls:     []parser.ToolCall{{ID: "c1", Name: "Read"}},
			StopReason:    "end_turn",
			Usage:         parser.Usage{InputTokens: 100, OutputTokens: 50},
			Blocks: []parser.ContentBlock{
				{Type: "thinking", Text: "hmm"},
				{Type: "text", Text: "answer"},
				{Type: "tool_use", ToolID: "c1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"x.go"}`)},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	c := chunks[0]

	// Flat fields
	if c.Text != "answer" {
		t.Errorf("Text = %q", c.Text)
	}
	if c.ThinkingCount != 1 {
		t.Errorf("Thinking = %d", c.ThinkingCount)
	}
	if len(c.ToolCalls) != 1 {
		t.Errorf("len(ToolCalls) = %d", len(c.ToolCalls))
	}
	if c.StopReason != "end_turn" {
		t.Errorf("StopReason = %q", c.StopReason)
	}
	if c.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d", c.Usage.InputTokens)
	}

	// Items also populated
	if len(c.Items) != 3 {
		t.Errorf("len(Items) = %d, want 3", len(c.Items))
	}
}

func TestBuildChunks_Items_EmptyBuffer(t *testing.T) {
	// Empty input should produce no chunks
	chunks := parser.BuildChunks(nil)
	if len(chunks) != 0 {
		t.Errorf("len(chunks) = %d, want 0", len(chunks))
	}
}

func TestBuildChunks_Items_NoBlocks(t *testing.T) {
	// AIMsg without Blocks (backward compat) should still produce a chunk
	// but Items should be nil
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Text:      "plain answer",
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].Items != nil {
		t.Errorf("Items should be nil when no Blocks present, got %d items", len(chunks[0].Items))
	}
	if chunks[0].Text != "plain answer" {
		t.Errorf("Text = %q, want 'plain answer'", chunks[0].Text)
	}
}
