package parser_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

// TestCycles_SimpleSingleResponse: one non-meta AIMsg with text + tool, no
// follow-up tool result. The whole thing is a single cycle.
func TestCycles_SimpleSingleResponse(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp:     t0,
			Model:         "claude-opus-4-7",
			Text:          "Reading the file.",
			ThinkingCount: 1,
			Usage:         parser.Usage{InputTokens: 1000, OutputTokens: 50},
			StopReason:    "tool_use",
			Blocks: []parser.ContentBlock{
				{Type: "thinking", Text: "..."},
				{Type: "text", Text: "Reading the file."},
				{Type: "tool_use", ToolID: "c1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"a.go"}`)},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	c := chunks[0]
	if len(c.Cycles) != 1 {
		t.Fatalf("len(Cycles) = %d, want 1", len(c.Cycles))
	}

	cyc := c.Cycles[0]
	if cyc.Index != 0 {
		t.Errorf("Index = %d, want 0", cyc.Index)
	}
	if cyc.StartItem != 0 || cyc.EndItem != 3 {
		t.Errorf("range = [%d,%d), want [0,3)", cyc.StartItem, cyc.EndItem)
	}
	if cyc.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q", cyc.Model)
	}
	if cyc.Usage.InputTokens != 1000 {
		t.Errorf("Usage.InputTokens = %d, want 1000", cyc.Usage.InputTokens)
	}
	if cyc.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want tool_use", cyc.StopReason)
	}
	if !cyc.HasThinking {
		t.Error("HasThinking should be true")
	}
	if cyc.ToolCount != 1 {
		t.Errorf("ToolCount = %d, want 1", cyc.ToolCount)
	}
	// Single-cycle chunk with no follow-up: duration is 0 (start == end).
	if cyc.DurationMs != 0 {
		t.Errorf("DurationMs = %d, want 0", cyc.DurationMs)
	}
}

// TestCycles_MultipleCycles: three non-meta messages with a meta tool-result
// between each. Should produce three cycles. Meta entries belong to the
// preceding cycle's item range.
func TestCycles_MultipleCycles(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		// Cycle 0: text + tool_use
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-7",
			Usage:     parser.Usage{InputTokens: 1000, OutputTokens: 30},
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "checking"},
				{Type: "tool_use", ToolID: "c1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"a.go"}`)},
			},
		},
		// Meta tool_result -- fills cycle 0 (no new items appended)
		parser.AIMsg{
			Timestamp: t0.Add(1 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "c1", Content: "result"},
			},
		},
		// Cycle 1: another tool_use
		parser.AIMsg{
			Timestamp: t0.Add(2 * time.Second),
			Model:     "claude-opus-4-7",
			Usage:     parser.Usage{InputTokens: 1500, OutputTokens: 40},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "c2", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`)},
			},
		},
		parser.AIMsg{
			Timestamp: t0.Add(3 * time.Second),
			IsMeta:    true,
			Blocks: []parser.ContentBlock{
				{Type: "tool_result", ToolID: "c2", Content: "files"},
			},
		},
		// Cycle 2: final text answer
		parser.AIMsg{
			Timestamp:  t0.Add(5 * time.Second),
			Model:      "claude-opus-4-7",
			Usage:      parser.Usage{InputTokens: 2000, OutputTokens: 80},
			StopReason: "end_turn",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "done"},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	c := chunks[0]

	if len(c.Cycles) != 3 {
		t.Fatalf("len(Cycles) = %d, want 3", len(c.Cycles))
	}

	// Cycle 0: items [0, 2) -- the text + the tool_use (which gets ToolResult filled in place)
	cyc0 := c.Cycles[0]
	if cyc0.StartItem != 0 || cyc0.EndItem != 2 {
		t.Errorf("cycle 0 range = [%d,%d), want [0,2)", cyc0.StartItem, cyc0.EndItem)
	}
	if cyc0.ToolCount != 1 {
		t.Errorf("cycle 0 ToolCount = %d, want 1", cyc0.ToolCount)
	}
	if cyc0.DurationMs != 2000 {
		t.Errorf("cycle 0 DurationMs = %d, want 2000 (start->next non-meta)", cyc0.DurationMs)
	}

	// Cycle 1: items [2, 3) -- just the Bash tool_use
	cyc1 := c.Cycles[1]
	if cyc1.StartItem != 2 || cyc1.EndItem != 3 {
		t.Errorf("cycle 1 range = [%d,%d), want [2,3)", cyc1.StartItem, cyc1.EndItem)
	}
	if cyc1.ToolCount != 1 {
		t.Errorf("cycle 1 ToolCount = %d, want 1", cyc1.ToolCount)
	}
	if cyc1.DurationMs != 3000 {
		t.Errorf("cycle 1 DurationMs = %d, want 3000", cyc1.DurationMs)
	}
	if cyc1.Usage.InputTokens != 1500 {
		t.Errorf("cycle 1 Usage.InputTokens = %d, want 1500", cyc1.Usage.InputTokens)
	}

	// Cycle 2: items [3, 4) -- final text. DurationMs = lastTs - startTs = 0
	cyc2 := c.Cycles[2]
	if cyc2.StartItem != 3 || cyc2.EndItem != 4 {
		t.Errorf("cycle 2 range = [%d,%d), want [3,4)", cyc2.StartItem, cyc2.EndItem)
	}
	if cyc2.ToolCount != 0 {
		t.Errorf("cycle 2 ToolCount = %d, want 0", cyc2.ToolCount)
	}
	if cyc2.StopReason != "end_turn" {
		t.Errorf("cycle 2 StopReason = %q, want end_turn", cyc2.StopReason)
	}
	if cyc2.DurationMs != 0 {
		t.Errorf("cycle 2 DurationMs = %d, want 0 (last cycle, no follow-up)", cyc2.DurationMs)
	}
}

// TestCycles_MetaOnlyProducesNilCycles: a chunk with only meta entries
// (rare, but seen in TestBuildChunks_UsageOnlyMetaMessages) has no cycles.
func TestCycles_MetaOnlyProducesNilCycles(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
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
	if chunks[0].Cycles != nil {
		t.Errorf("Cycles should be nil for meta-only chunk, got %d", len(chunks[0].Cycles))
	}
}

// TestCycles_TruncatedChunk: chunk ends mid-cycle (tool_result hasn't
// arrived). Last cycle's EndItem == len(Items), DurationMs == 0.
func TestCycles_TruncatedChunk(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-7",
			Usage:     parser.Usage{InputTokens: 1000},
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "c1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"a.go"}`)},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	c := chunks[0]
	if len(c.Cycles) != 1 {
		t.Fatalf("len(Cycles) = %d, want 1", len(c.Cycles))
	}
	cyc := c.Cycles[0]
	if cyc.EndItem != len(c.Items) {
		t.Errorf("EndItem = %d, want %d (len(Items))", cyc.EndItem, len(c.Items))
	}
	if cyc.DurationMs != 0 {
		t.Errorf("DurationMs = %d, want 0 (truncated)", cyc.DurationMs)
	}
}

// TestCycles_ThinkingFlag: HasThinking comes from the source AIMsg's
// ThinkingCount, not from items in range.
func TestCycles_ThinkingFlag(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		// Cycle 0: thinking present
		parser.AIMsg{
			Timestamp:     t0,
			Model:         "claude-opus-4-7",
			ThinkingCount: 1,
			Blocks: []parser.ContentBlock{
				{Type: "thinking", Text: "hmm"},
				{Type: "text", Text: "ok"},
			},
		},
		// Cycle 1: no thinking
		parser.AIMsg{
			Timestamp: t0.Add(1 * time.Second),
			Model:     "claude-opus-4-7",
			Blocks: []parser.ContentBlock{
				{Type: "text", Text: "done"},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	c := chunks[0]
	if len(c.Cycles) != 2 {
		t.Fatalf("len(Cycles) = %d, want 2", len(c.Cycles))
	}
	if !c.Cycles[0].HasThinking {
		t.Error("cycle 0 HasThinking should be true")
	}
	if c.Cycles[1].HasThinking {
		t.Error("cycle 1 HasThinking should be false")
	}
}

// TestCycles_SubagentCountsAsTool: ItemSubagent contributes to ToolCount.
func TestCycles_SubagentCountsAsTool(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Model:     "claude-opus-4-7",
			Blocks: []parser.ContentBlock{
				{Type: "tool_use", ToolID: "c1", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"a.go"}`)},
				{Type: "tool_use", ToolID: "c2", ToolName: "Task", ToolInput: json.RawMessage(`{"subagent_type":"Explore","description":"check"}`)},
			},
		},
	}
	chunks := parser.BuildChunks(msgs)
	c := chunks[0]
	if len(c.Cycles) != 1 {
		t.Fatalf("len(Cycles) = %d, want 1", len(c.Cycles))
	}
	if c.Cycles[0].ToolCount != 2 {
		t.Errorf("ToolCount = %d, want 2 (tool + subagent)", c.Cycles[0].ToolCount)
	}
}

// TestCycles_NonAIChunksHaveNilCycles: user / system / compact chunks
// must not carry Cycles.
func TestCycles_NonAIChunksHaveNilCycles(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.UserMsg{Timestamp: t0, Text: "hi"},
		parser.SystemMsg{Timestamp: t0.Add(1 * time.Second), Output: "ok"},
		parser.CompactMsg{Timestamp: t0.Add(2 * time.Second), Text: "compressed"},
	}
	chunks := parser.BuildChunks(msgs)
	for i, c := range chunks {
		if c.Cycles != nil {
			t.Errorf("chunks[%d] (type %d) has Cycles, want nil", i, c.Type)
		}
	}
}
