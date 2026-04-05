package parser

import (
	"testing"
	"time"
)

// baseTime is a fixed anchor for all test timestamps.
var baseTime = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

// aiChunkWithTools builds a minimal AIChunk with the given tool items.
func aiChunkWithTools(ts time.Time, durationMs int64, model string, items []DisplayItem) Chunk {
	return Chunk{
		Type:       AIChunk,
		Timestamp:  ts,
		DurationMs: durationMs,
		Model:      model,
		Items:      items,
	}
}

// toolItem builds a DisplayItem of type ItemToolCall.
func toolItem(name string, cat ToolCategory, durMs int64, hasError bool, summary string) DisplayItem {
	return DisplayItem{
		Type:         ItemToolCall,
		ToolName:     name,
		ToolCategory: cat,
		DurationMs:   durMs,
		ToolError:    hasError,
		ToolSummary:  summary,
	}
}

// subagentItem builds a DisplayItem of type ItemSubagent.
func subagentItem(name string, durMs int64) DisplayItem {
	return DisplayItem{
		Type:         ItemSubagent,
		ToolName:     name,
		ToolCategory: CategoryTask,
		DurationMs:   durMs,
	}
}

// userChunk builds a minimal UserChunk.
func userChunk(ts time.Time) Chunk {
	return Chunk{
		Type:      UserChunk,
		Timestamp: ts,
		UserText:  "hello",
	}
}

// TestBuildWaterfallRows_Empty verifies the empty-input contract.
func TestBuildWaterfallRows_Empty(t *testing.T) {
	rows, axis := BuildWaterfallRows(nil)
	if rows != nil {
		t.Errorf("expected nil rows for empty input, got %v", rows)
	}
	if !axis.StartTime.IsZero() || !axis.EndTime.IsZero() || axis.TotalMs != 0 {
		t.Errorf("expected zero TimeAxis for empty input, got %+v", axis)
	}
}

// TestBuildWaterfallRows_EmptySlice verifies the empty slice contract.
func TestBuildWaterfallRows_EmptySlice(t *testing.T) {
	rows, axis := BuildWaterfallRows([]Chunk{})
	if rows != nil {
		t.Errorf("expected nil rows for empty slice, got %v", rows)
	}
	if !axis.StartTime.IsZero() {
		t.Errorf("expected zero TimeAxis for empty slice, got %+v", axis)
	}
}

// TestBuildWaterfallRows_SingleAIChunk verifies a single AI chunk with tools.
func TestBuildWaterfallRows_SingleAIChunk(t *testing.T) {
	chunk := aiChunkWithTools(baseTime, 500, "claude-3-5-sonnet", []DisplayItem{
		toolItem("Read", CategoryRead, 120, false, "main.go"),
	})
	rows, axis := BuildWaterfallRows([]Chunk{chunk})

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.StartMs != 0 {
		t.Errorf("first chunk StartMs should be 0, got %d", r.StartMs)
	}
	if r.DurationMs != 500 {
		t.Errorf("DurationMs should be 500, got %d", r.DurationMs)
	}
	if r.IsUserSeparator {
		t.Error("IsUserSeparator should be false for AIChunk")
	}
	if r.IsSubagent {
		t.Error("IsSubagent should be false when no ItemSubagent")
	}
	if r.Model != "claude-3-5-sonnet" {
		t.Errorf("Model should be claude-3-5-sonnet, got %q", r.Model)
	}
	if len(r.Tools) != 1 || r.Tools[0].Name != "Read" {
		t.Errorf("expected 1 tool Read, got %+v", r.Tools)
	}

	if !axis.StartTime.Equal(baseTime) {
		t.Errorf("StartTime should be baseTime, got %v", axis.StartTime)
	}
	expectedEnd := baseTime.Add(500 * time.Millisecond)
	if !axis.EndTime.Equal(expectedEnd) {
		t.Errorf("EndTime should be %v, got %v", expectedEnd, axis.EndTime)
	}
	if axis.TotalMs != 500 {
		t.Errorf("TotalMs should be 500, got %d", axis.TotalMs)
	}
}

// TestBuildWaterfallRows_AIChunkWithoutTools is skipped (not emitted).
func TestBuildWaterfallRows_AIChunkWithoutTools(t *testing.T) {
	chunk := Chunk{
		Type:      AIChunk,
		Timestamp: baseTime,
		Model:     "claude-3-5-sonnet",
		Items: []DisplayItem{
			{Type: ItemOutput, Text: "Some text output"},
		},
	}
	rows, _ := BuildWaterfallRows([]Chunk{chunk})
	if len(rows) != 0 {
		t.Errorf("AI chunk with no tool items should produce 0 rows, got %d", len(rows))
	}
}

// TestBuildWaterfallRows_UserSeparator verifies IsUserSeparator rows.
func TestBuildWaterfallRows_UserSeparator(t *testing.T) {
	t1 := baseTime
	t2 := baseTime.Add(1 * time.Second)
	t3 := baseTime.Add(2 * time.Second)

	chunks := []Chunk{
		userChunk(t1),
		aiChunkWithTools(t2, 800, "claude-opus-4", []DisplayItem{
			toolItem("Bash", CategoryBash, 200, false, "go test"),
		}),
		userChunk(t3),
	}

	rows, axis := BuildWaterfallRows(chunks)

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// First row: user separator
	if !rows[0].IsUserSeparator {
		t.Error("rows[0] should be IsUserSeparator")
	}
	if rows[0].StartMs != 0 {
		t.Errorf("rows[0].StartMs should be 0, got %d", rows[0].StartMs)
	}

	// Second row: AI chunk
	if rows[1].IsUserSeparator {
		t.Error("rows[1] should not be IsUserSeparator")
	}
	if rows[1].StartMs != 1000 {
		t.Errorf("rows[1].StartMs should be 1000ms, got %d", rows[1].StartMs)
	}

	// Third row: user separator at 2000ms
	if !rows[2].IsUserSeparator {
		t.Error("rows[2] should be IsUserSeparator")
	}
	if rows[2].StartMs != 2000 {
		t.Errorf("rows[2].StartMs should be 2000ms, got %d", rows[2].StartMs)
	}

	// TimeAxis: EndTime is last chunk (t3 user, 0 duration) = t3
	if !axis.StartTime.Equal(t1) {
		t.Errorf("StartTime should be t1, got %v", axis.StartTime)
	}
	_ = axis
}

// TestBuildWaterfallRows_MultipleChunksWithGaps verifies StartMs offsets.
func TestBuildWaterfallRows_MultipleChunksWithGaps(t *testing.T) {
	t0 := baseTime
	t1 := baseTime.Add(5 * time.Second)
	t2 := baseTime.Add(15 * time.Second)

	chunks := []Chunk{
		aiChunkWithTools(t0, 3000, "sonnet", []DisplayItem{
			toolItem("Read", CategoryRead, 100, false, "a.go"),
		}),
		aiChunkWithTools(t1, 2000, "sonnet", []DisplayItem{
			toolItem("Edit", CategoryEdit, 50, false, "b.go"),
		}),
		aiChunkWithTools(t2, 1000, "sonnet", []DisplayItem{
			toolItem("Bash", CategoryBash, 200, true, "go build"),
		}),
	}

	rows, axis := BuildWaterfallRows(chunks)

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	if rows[0].StartMs != 0 {
		t.Errorf("rows[0].StartMs should be 0, got %d", rows[0].StartMs)
	}
	if rows[1].StartMs != 5000 {
		t.Errorf("rows[1].StartMs should be 5000, got %d", rows[1].StartMs)
	}
	if rows[2].StartMs != 15000 {
		t.Errorf("rows[2].StartMs should be 15000, got %d", rows[2].StartMs)
	}

	// EndTime = t2 + 1000ms = baseTime + 16s
	expectedEnd := t2.Add(1000 * time.Millisecond)
	if !axis.EndTime.Equal(expectedEnd) {
		t.Errorf("EndTime should be %v, got %v", expectedEnd, axis.EndTime)
	}
	if axis.TotalMs != 16000 {
		t.Errorf("TotalMs should be 16000, got %d", axis.TotalMs)
	}

	// Verify error flag propagated
	if !rows[2].Tools[0].Error {
		t.Error("rows[2] tool should have Error=true")
	}
}

// TestBuildWaterfallRows_ConcurrentChunks verifies same StartMs is valid.
func TestBuildWaterfallRows_ConcurrentChunks(t *testing.T) {
	// Two AI chunks starting at the same timestamp (concurrent tool calls).
	chunks := []Chunk{
		aiChunkWithTools(baseTime, 1000, "opus", []DisplayItem{
			toolItem("Bash", CategoryBash, 900, false, "task1"),
		}),
		aiChunkWithTools(baseTime, 1200, "opus", []DisplayItem{
			toolItem("Bash", CategoryBash, 1100, false, "task2"),
		}),
	}

	rows, _ := BuildWaterfallRows(chunks)

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].StartMs != 0 || rows[1].StartMs != 0 {
		t.Errorf("both concurrent rows should have StartMs=0, got %d and %d",
			rows[0].StartMs, rows[1].StartMs)
	}
}

// TestBuildWaterfallRows_SubagentChunk verifies IsSubagent flag.
func TestBuildWaterfallRows_SubagentChunk(t *testing.T) {
	chunk := aiChunkWithTools(baseTime, 30000, "opus", []DisplayItem{
		toolItem("Read", CategoryRead, 100, false, "file.go"),
		subagentItem("Task", 29000),
	})

	rows, _ := BuildWaterfallRows([]Chunk{chunk})

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if !rows[0].IsSubagent {
		t.Error("row should have IsSubagent=true when chunk has ItemSubagent")
	}
	// Both the tool and the subagent item should be in Tools.
	if len(rows[0].Tools) != 2 {
		t.Errorf("expected 2 tools (Read + Task), got %d", len(rows[0].Tools))
	}
}

// TestBuildWaterfallRows_StartMsOffset verifies the spec formula explicitly.
func TestBuildWaterfallRows_StartMsOffset(t *testing.T) {
	first := baseTime
	second := baseTime.Add(7500 * time.Millisecond)

	chunks := []Chunk{
		aiChunkWithTools(first, 100, "m", []DisplayItem{toolItem("Read", CategoryRead, 50, false, "")}),
		aiChunkWithTools(second, 200, "m", []DisplayItem{toolItem("Edit", CategoryEdit, 50, false, "")}),
	}

	rows, _ := BuildWaterfallRows(chunks)

	if rows[1].StartMs != 7500 {
		t.Errorf("second row StartMs should be 7500 (Sub in ms), got %d", rows[1].StartMs)
	}
}

// TestColOffset_ZeroTotalMs verifies zero-division guard.
func TestColOffset_ZeroTotalMs(t *testing.T) {
	if got := ColOffset(500, 0, 100); got != 0 {
		t.Errorf("ColOffset with totalMs=0 should return 0, got %d", got)
	}
}

// TestColOffset_ZeroWidth verifies zero-width guard.
func TestColOffset_ZeroWidth(t *testing.T) {
	if got := ColOffset(500, 1000, 0); got != 0 {
		t.Errorf("ColOffset with width=0 should return 0, got %d", got)
	}
}

// TestColOffset_Clamp verifies the [0, width-1] clamp.
func TestColOffset_Clamp(t *testing.T) {
	// ms > totalMs: should clamp to width-1
	if got := ColOffset(2000, 1000, 80); got != 79 {
		t.Errorf("ColOffset should clamp to width-1=79, got %d", got)
	}
	// ms == totalMs: float64 * float64 = exactly width, clamp to width-1
	if got := ColOffset(1000, 1000, 80); got != 79 {
		t.Errorf("ColOffset(1000,1000,80) should clamp to 79, got %d", got)
	}
	// ms == 0: should return 0
	if got := ColOffset(0, 1000, 80); got != 0 {
		t.Errorf("ColOffset(0,1000,80) should return 0, got %d", got)
	}
}

// TestColOffset_Midpoint verifies basic mapping math.
func TestColOffset_Midpoint(t *testing.T) {
	// 500ms out of 1000ms on 100-wide = column 50.
	if got := ColOffset(500, 1000, 100); got != 50 {
		t.Errorf("ColOffset(500,1000,100) should be 50, got %d", got)
	}
	// 250ms out of 1000ms on 80-wide = 20
	if got := ColOffset(250, 1000, 80); got != 20 {
		t.Errorf("ColOffset(250,1000,80) should be 20, got %d", got)
	}
}

// TestBuildWaterfallRows_EndTimeUsesMaxChunkEnd verifies that EndTime is the
// maximum end timestamp across all chunks, not just the last chunk's end time.
func TestBuildWaterfallRows_EndTimeUsesMaxChunkEnd(t *testing.T) {
	// Long task starts at t=0, runs 10s. Short read at t=5s, runs 100ms.
	// Last chunk (index 1) ends at 5.1s, but chunk 0 ends at 10s.
	// EndTime must be 10s, not 5.1s.
	chunks := []Chunk{
		aiChunkWithTools(baseTime, 10000, "opus", []DisplayItem{
			subagentItem("Task", 9500),
		}),
		aiChunkWithTools(baseTime.Add(5*time.Second), 100, "sonnet", []DisplayItem{
			toolItem("Read", CategoryRead, 100, false, "file.go"),
		}),
	}
	rows, axis := BuildWaterfallRows(chunks)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	// TotalMs should be 10000 (from chunk 0), not 5100 (from chunk 1).
	if axis.TotalMs != 10000 {
		t.Errorf("TotalMs should be 10000 (max chunk end), got %d", axis.TotalMs)
	}
	expectedEnd := baseTime.Add(10 * time.Second)
	if !axis.EndTime.Equal(expectedEnd) {
		t.Errorf("EndTime should be %v, got %v", expectedEnd, axis.EndTime)
	}
}
