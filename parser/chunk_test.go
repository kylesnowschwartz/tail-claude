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
	// Usage = last non-meta assistant's snapshot (the first message; second is meta).
	if c.Usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100 (snapshot)", c.Usage.InputTokens)
	}
	if c.Usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50 (snapshot)", c.Usage.OutputTokens)
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
	if items[2].ToolSummary != "tmp/main.go" {
		t.Errorf("Items[2].ToolSummary = %q, want tmp/main.go", items[2].ToolSummary)
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
	if items[0].ToolSummary != "Run tests: go test ./..." {
		t.Errorf("ToolSummary = %q, want 'Run tests: go test ./...'", items[0].ToolSummary)
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

// --- ItemSubagent tests ---

func TestBuildChunks_Items_TaskToolCreatesSubagent(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	taskInput := json.RawMessage(`{"subagent_type":"Explore","description":"Find API endpoints","prompt":"Search the codebase for API endpoints"}`)

	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "call_1", Name: "Task"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "call_1", ToolName: "Task", ToolInput: taskInput},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}

	items := chunks[0].Items
	if len(items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(items))
	}

	item := items[0]
	if item.Type != parser.ItemSubagent {
		t.Errorf("Type = %d, want ItemSubagent", item.Type)
	}
	if item.SubagentType != "Explore" {
		t.Errorf("SubagentType = %q, want Explore", item.SubagentType)
	}
	if item.SubagentDesc != "Find API endpoints" {
		t.Errorf("SubagentDesc = %q, want 'Find API endpoints'", item.SubagentDesc)
	}
	if item.ToolName != "Task" {
		t.Errorf("ToolName = %q, want Task", item.ToolName)
	}
	if item.ToolID != "call_1" {
		t.Errorf("ToolID = %q, want call_1", item.ToolID)
	}
}

func TestBuildChunks_Items_TaskToolPromptFallback(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	// No "description" field -- should fall back to truncated prompt
	taskInput := json.RawMessage(`{"subagent_type":"general-purpose","prompt":"Implement the feature as described in the ticket above and make sure all tests pass"}`)

	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "call_1", Name: "Task"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "call_1", ToolName: "Task", ToolInput: taskInput},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	items := chunks[0].Items
	if len(items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(items))
	}

	item := items[0]
	if item.Type != parser.ItemSubagent {
		t.Errorf("Type = %d, want ItemSubagent", item.Type)
	}
	if item.SubagentType != "general-purpose" {
		t.Errorf("SubagentType = %q, want general-purpose", item.SubagentType)
	}
	// Should be truncated to 80 chars
	if len(item.SubagentDesc) > 83 { // 80 + "..."
		t.Errorf("SubagentDesc too long: %d chars", len(item.SubagentDesc))
	}
	if item.SubagentDesc == "" {
		t.Error("SubagentDesc should not be empty (prompt fallback)")
	}
}

func TestBuildChunks_Items_TaskToolWithResult(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(5 * time.Second)
	taskInput := json.RawMessage(`{"subagent_type":"Explore","description":"Find config"}`)

	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "call_1", Name: "Task"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "call_1", ToolName: "Task", ToolInput: taskInput},
			},
		},
		parser.AIMsg{
			Timestamp: t1,
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "call_1", Content: "Found config.yaml at /etc/app/config.yaml"},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	items := chunks[0].Items
	if len(items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(items))
	}

	item := items[0]
	if item.Type != parser.ItemSubagent {
		t.Errorf("Type = %d, want ItemSubagent", item.Type)
	}
	if item.ToolResult != "Found config.yaml at /etc/app/config.yaml" {
		t.Errorf("ToolResult = %q", item.ToolResult)
	}
	if item.DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want 5000", item.DurationMs)
	}
}

func TestBuildChunks_Items_NonTaskToolStillToolCall(t *testing.T) {
	// Verify that non-Task tool_use blocks still produce ItemToolCall
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "call_1", Name: "Read"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "call_1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"/tmp/foo.go"}`)},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	items := chunks[0].Items
	if len(items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(items))
	}
	if items[0].Type != parser.ItemToolCall {
		t.Errorf("Type = %d, want ItemToolCall (not ItemSubagent)", items[0].Type)
	}
}

// --- ItemTeammateMessage tests ---

func TestBuildChunks_TeammateMessageFoldsIntoAITurn(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Text:      "Working on it",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Working on it"},
			},
		},
		parser.TeammateMsg{
			Timestamp:  t0.Add(1 * time.Second),
			Text:       "Task #1 is done",
			TeammateID: "researcher",
		},
		parser.AIMsg{
			Timestamp: t0.Add(2 * time.Second),
			Model:     "claude-opus-4-6",
			Text:      "Got the update",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Got the update"},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	// Should be a single AI chunk (teammate doesn't split the turn)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1 (teammate folds into AI turn)", len(chunks))
	}

	items := chunks[0].Items
	// text + teammate + text = 3 items
	if len(items) != 3 {
		t.Fatalf("len(Items) = %d, want 3", len(items))
	}

	if items[0].Type != parser.ItemOutput {
		t.Errorf("Items[0].Type = %d, want ItemOutput", items[0].Type)
	}
	if items[1].Type != parser.ItemTeammateMessage {
		t.Errorf("Items[1].Type = %d, want ItemTeammateMessage", items[1].Type)
	}
	if items[1].TeammateID != "researcher" {
		t.Errorf("Items[1].TeammateID = %q, want researcher", items[1].TeammateID)
	}
	if items[1].Text != "Task #1 is done" {
		t.Errorf("Items[1].Text = %q, want 'Task #1 is done'", items[1].Text)
	}
	if items[2].Type != parser.ItemOutput {
		t.Errorf("Items[2].Type = %d, want ItemOutput", items[2].Type)
	}
}

func TestBuildChunks_TeammateMessageBeforeAI(t *testing.T) {
	// Teammate message arrives before any AI response -- should still produce a chunk
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.UserMsg{Timestamp: t0, Text: "Go"},
		parser.TeammateMsg{
			Timestamp:  t0.Add(1 * time.Second),
			Text:       "Starting work",
			TeammateID: "worker-1",
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2 (user + AI from teammate)", len(chunks))
	}
	if chunks[0].Type != parser.UserChunk {
		t.Errorf("chunks[0].Type = %d, want UserChunk", chunks[0].Type)
	}
	if chunks[1].Type != parser.AIChunk {
		t.Errorf("chunks[1].Type = %d, want AIChunk", chunks[1].Type)
	}
	if len(chunks[1].Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(chunks[1].Items))
	}
	if chunks[1].Items[0].Type != parser.ItemTeammateMessage {
		t.Errorf("Items[0].Type = %d, want ItemTeammateMessage", chunks[1].Items[0].Type)
	}
}

// --- CompactChunk tests ---

func TestBuildChunks_CompactMsgProducesCompactChunk(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.CompactMsg{
			Timestamp: t0,
			Text:      "Context compressed",
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].Type != parser.CompactChunk {
		t.Errorf("Type = %d, want CompactChunk", chunks[0].Type)
	}
	if chunks[0].Output != "Context compressed" {
		t.Errorf("Output = %q, want 'Context compressed'", chunks[0].Output)
	}
}

func TestBuildChunks_CompactChunkFlushesAIBuffer(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{Timestamp: t0, Text: "First response", Model: "claude-opus-4-6"},
		parser.CompactMsg{Timestamp: t0.Add(1 * time.Second), Text: "Summarized"},
		parser.AIMsg{Timestamp: t0.Add(2 * time.Second), Text: "Second response", Model: "claude-opus-4-6"},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3 (AI + compact + AI)", len(chunks))
	}
	if chunks[0].Type != parser.AIChunk {
		t.Errorf("chunks[0].Type = %d, want AIChunk", chunks[0].Type)
	}
	if chunks[1].Type != parser.CompactChunk {
		t.Errorf("chunks[1].Type = %d, want CompactChunk", chunks[1].Type)
	}
	if chunks[2].Type != parser.AIChunk {
		t.Errorf("chunks[2].Type = %d, want AIChunk", chunks[2].Type)
	}
}

// --- Usage snapshot tests ---
// The Claude API reports input_tokens as the full context window per API call,
// not incremental. Chunk.Usage should reflect the last assistant message's
// usage (a context-window snapshot), not the sum of all messages.

func TestBuildChunks_UsageLastAssistantSnapshot(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		// First assistant response (tool_use)
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Text:      "Let me check that file.",
			Usage:     parser.Usage{InputTokens: 1000, OutputTokens: 50},
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "Let me check that file."},
				{Type: "tool_use", ToolID: "c1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"a.go"}`)},
			},
		},
		// Tool result (meta, zero usage)
		parser.AIMsg{
			Timestamp: t0.Add(1 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "c1", Content: "package main"},
			},
		},
		// Second assistant response (final text)
		parser.AIMsg{
			Timestamp: t0.Add(2 * time.Second),
			Model:     "claude-opus-4-6",
			Text:      "The file looks good.",
			Usage:     parser.Usage{InputTokens: 2000, OutputTokens: 80},
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "The file looks good."},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	c := chunks[0]

	// Usage = last non-meta assistant's usage (context window snapshot).
	if c.Usage.InputTokens != 2000 {
		t.Errorf("Usage.InputTokens = %d, want 2000 (last assistant snapshot)", c.Usage.InputTokens)
	}
	if c.Usage.OutputTokens != 80 {
		t.Errorf("Usage.OutputTokens = %d, want 80 (last assistant snapshot)", c.Usage.OutputTokens)
	}
}

func TestBuildChunks_UsageThreeToolRoundTrips(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{Timestamp: t0, Model: "claude-opus-4-6",
			Usage: parser.Usage{InputTokens: 1000, OutputTokens: 50}},
		parser.AIMsg{Timestamp: t0.Add(1 * time.Second), IsMeta: true},
		parser.AIMsg{Timestamp: t0.Add(2 * time.Second), Model: "claude-opus-4-6",
			Usage: parser.Usage{InputTokens: 2000, OutputTokens: 60}},
		parser.AIMsg{Timestamp: t0.Add(3 * time.Second), IsMeta: true},
		parser.AIMsg{Timestamp: t0.Add(4 * time.Second), Model: "claude-opus-4-6",
			Text:  "Done.",
			Usage: parser.Usage{InputTokens: 3000, OutputTokens: 70, CacheReadTokens: 500}},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	c := chunks[0]

	// Last non-meta assistant: InputTokens=3000, OutputTokens=70, CacheReadTokens=500
	if c.Usage.InputTokens != 3000 {
		t.Errorf("InputTokens = %d, want 3000", c.Usage.InputTokens)
	}
	if c.Usage.OutputTokens != 70 {
		t.Errorf("OutputTokens = %d, want 70", c.Usage.OutputTokens)
	}
	if c.Usage.CacheReadTokens != 500 {
		t.Errorf("CacheReadTokens = %d, want 500", c.Usage.CacheReadTokens)
	}
	if c.Usage.TotalTokens() != 3570 {
		t.Errorf("TotalTokens = %d, want 3570", c.Usage.TotalTokens())
	}
}

func TestBuildChunks_UsageOnlyMetaMessages(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			IsMeta:    true,
			Text:      "tool result",
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].Usage.TotalTokens() != 0 {
		t.Errorf("Usage.TotalTokens = %d, want 0 (no non-meta assistant)", chunks[0].Usage.TotalTokens())
	}
}

func TestBuildChunks_UsageSingleMessage(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Text:      "Hello!",
			Usage:     parser.Usage{InputTokens: 500, OutputTokens: 30},
		},
	}
	chunks := parser.BuildChunks(msgs)
	c := chunks[0]
	if c.Usage.InputTokens != 500 {
		t.Errorf("InputTokens = %d, want 500", c.Usage.InputTokens)
	}
	if c.Usage.OutputTokens != 30 {
		t.Errorf("OutputTokens = %d, want 30", c.Usage.OutputTokens)
	}
}

func TestBuildChunks_ItemTokenCountMultipleTools(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	bashInput := `{"command":"ls"}`
	readInput := `{"file_path":"main.go"}`
	bashResult := "file1.go\nfile2.go"
	readResult := "package main\n\nfunc main() {}"

	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "c1", ToolName: "Bash", ToolInput: json.RawMessage(bashInput)},
				{Type: "tool_use", ToolID: "c2", ToolName: "Read", ToolInput: json.RawMessage(readInput)},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(1 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "c1", Content: bashResult},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(2 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "c2", Content: readResult},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	items := chunks[0].Items
	if len(items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(items))
	}

	// Each tool's TokenCount = input estimate + result estimate
	wantBash := len(bashInput)/4 + len(bashResult)/4
	if items[0].TokenCount != wantBash {
		t.Errorf("Bash TokenCount = %d, want %d (input+result)", items[0].TokenCount, wantBash)
	}
	wantRead := len(readInput)/4 + len(readResult)/4
	if items[1].TokenCount != wantRead {
		t.Errorf("Read TokenCount = %d, want %d (input+result)", items[1].TokenCount, wantRead)
	}
}

// --- Concurrent Task duration suppression ---

func TestBuildChunks_ConcurrentTaskDuration(t *testing.T) {
	// When a Bash tool_use coexists with a background Task in the same AI
	// turn, the Bash tool_result timestamp is delayed by the Task's runtime.
	// The Bash DurationMs should be zeroed to suppress the misleading display.
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	bashResult := t0.Add(11 * time.Minute) // inflated: waited for Task agents
	taskResult := t0.Add(11 * time.Minute)

	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{
				{ID: "bash1", Name: "Bash"},
				{ID: "task1", Name: "Task"},
			},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "bash1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"git push"}`)},
				{Type: "tool_use", ToolID: "task1", ToolName: "Task", ToolInput: json.RawMessage(`{"subagent_type":"Explore","description":"Research something"}`)},
			},
		},
		// Bash result arrives after Task agents complete (inflated timestamp).
		parser.AIMsg{
			Timestamp: bashResult,
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "bash1", Content: "Everything up-to-date"},
			},
		},
		// Task result arrives around the same time.
		parser.AIMsg{
			Timestamp: taskResult,
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "task1", Content: "Agent completed research"},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}

	items := chunks[0].Items
	if len(items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(items))
	}

	// Bash duration should be suppressed (zeroed).
	if items[0].ToolName != "Bash" {
		t.Fatalf("Items[0].ToolName = %q, want Bash", items[0].ToolName)
	}
	if items[0].DurationMs != 0 {
		t.Errorf("Bash DurationMs = %d, want 0 (inflated by concurrent Task)", items[0].DurationMs)
	}

	// Task duration should be preserved.
	if items[1].ToolName != "Task" {
		t.Fatalf("Items[1].ToolName = %q, want Task", items[1].ToolName)
	}
	if items[1].DurationMs == 0 {
		t.Error("Task DurationMs should be preserved, got 0")
	}
}

func TestBuildChunks_NoConcurrentTask_DurationPreserved(t *testing.T) {
	// Without concurrent Task calls, Bash duration should be preserved even
	// if it exceeds the threshold (unlikely but tests the guard).
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(90 * time.Second) // 90s is above threshold but no Task

	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{{ID: "bash1", Name: "Bash"}},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "bash1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make build"}`)},
			},
		},
		parser.AIMsg{
			Timestamp: t1,
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "bash1", Content: "ok"},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	items := chunks[0].Items

	if items[0].DurationMs != 90000 {
		t.Errorf("Bash DurationMs = %d, want 90000 (no concurrent Task, should preserve)", items[0].DurationMs)
	}
}

func TestBuildChunks_ConcurrentTask_ShortDurationPreserved(t *testing.T) {
	// Non-Task tools under the threshold should keep their duration even
	// when a Task is present — only inflated durations are suspicious.
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-6",
			ToolCalls: []parser.ToolCall{
				{ID: "read1", Name: "Read"},
				{ID: "task1", Name: "Task"},
			},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "read1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"a.go"}`)},
				{Type: "tool_use", ToolID: "task1", ToolName: "Task", ToolInput: json.RawMessage(`{"subagent_type":"Explore","description":"check"}`)},
			},
		},
		// Read result comes back in 2 seconds — plausible, keep it.
		parser.AIMsg{
			Timestamp: t0.Add(2 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "read1", Content: "package main"},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(5 * time.Minute),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "task1", Content: "done"},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	items := chunks[0].Items

	// Read: 2s is under the 60s threshold, should be preserved.
	if items[0].DurationMs != 2000 {
		t.Errorf("Read DurationMs = %d, want 2000 (under threshold, preserved)", items[0].DurationMs)
	}
}
