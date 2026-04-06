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

// TestBuildTimeMap_NoCompression verifies that a session with all small gaps
// returns a linear (uncompressed) mapping where CompressedTotalMs == totalMs.
func TestBuildTimeMap_NoCompression(t *testing.T) {
	// Gaps of 1s, 1s, 2s -- median 1s, threshold max(60s, 10s) = 60s.
	// All gaps are well below 60s, so no compression.
	rows := []WaterfallRow{
		{StartMs: 0, IsUserSeparator: false},
		{StartMs: 1000, IsUserSeparator: false},
		{StartMs: 2000, IsUserSeparator: false},
		{StartMs: 4000, IsUserSeparator: false},
	}
	const totalMs = int64(5000)

	tm := BuildTimeMap(rows, totalMs)

	if tm.CompressedTotalMs != totalMs {
		t.Errorf("no compression expected: CompressedTotalMs=%d, want %d", tm.CompressedTotalMs, totalMs)
	}
	// MapToDisplay should be identity.
	for _, ms := range []int64{0, 1000, 2000, 4000, 5000} {
		if got := tm.MapToDisplay(ms); got != ms {
			t.Errorf("MapToDisplay(%d) = %d, want identity %d", ms, got, ms)
		}
	}
}

// TestBuildTimeMap_Compression verifies the spec case:
// gaps of 1s, 1s, 300s, 2s => the 300s gap gets compressed.
//
// Timeline:
//   t=0, t=1s, t=2s, t=302s, t=304s => totalMs = 304000
//
// Gaps: 1000, 1000, 300000, 2000 => sorted: 1000, 1000, 2000, 300000
// median = gaps[2] = 2000ms (index len/2 = 2)
// threshold = max(60000, 2000*10) = max(60000, 20000) = 60000ms
// compressedGapDisplay = min(30, max(5, 2000*3)) = min(30, 6000) = 30ms
// The 300000ms gap exceeds 60000ms threshold => compressed to 30ms.
func TestBuildTimeMap_Compression(t *testing.T) {
	rows := []WaterfallRow{
		{StartMs: 0, IsUserSeparator: false},
		{StartMs: 1000, IsUserSeparator: false},
		{StartMs: 2000, IsUserSeparator: false},
		{StartMs: 302000, IsUserSeparator: false},
		{StartMs: 304000, IsUserSeparator: false},
	}
	const totalMs = int64(304000)

	tm := BuildTimeMap(rows, totalMs)

	// CompressedTotalMs should be less than totalMs.
	if tm.CompressedTotalMs >= totalMs {
		t.Errorf("compression expected: CompressedTotalMs=%d should be < totalMs=%d", tm.CompressedTotalMs, totalMs)
	}

	// The large gap [2000, 302000] should be compressed to 30ms display units.
	// Before the gap: display matches raw (1:1).
	if got := tm.MapToDisplay(0); got != 0 {
		t.Errorf("MapToDisplay(0) = %d, want 0", got)
	}
	if got := tm.MapToDisplay(1000); got != 1000 {
		t.Errorf("MapToDisplay(1000) = %d, want 1000", got)
	}
	if got := tm.MapToDisplay(2000); got != 2000 {
		t.Errorf("MapToDisplay(2000) = %d, want 2000", got)
	}

	// After the compressed gap [2000..302000]:
	// displayStart=2000, displayEnd=2000+30=2030
	// So raw 302000 maps to display 2030.
	wantAt302000 := int64(2030)
	if got := tm.MapToDisplay(302000); got != wantAt302000 {
		t.Errorf("MapToDisplay(302000) = %d, want %d", got, wantAt302000)
	}

	// The next gap [302000..304000] is 2000ms < threshold, linear.
	// displayStart=2030, displayEnd=2030+2000=4030.
	// raw 304000 => display 4030.
	wantAt304000 := int64(4030)
	if got := tm.MapToDisplay(304000); got != wantAt304000 {
		t.Errorf("MapToDisplay(304000) = %d, want %d", got, wantAt304000)
	}
}

// TestBuildTimeMap_UserSeparatorsIgnored verifies that IsUserSeparator rows
// are excluded from gap analysis (they don't represent AI activity intervals).
func TestBuildTimeMap_UserSeparatorsIgnored(t *testing.T) {
	rows := []WaterfallRow{
		{StartMs: 0, IsUserSeparator: false},
		{StartMs: 500, IsUserSeparator: true},  // separator: excluded
		{StartMs: 1000, IsUserSeparator: false},
	}
	const totalMs = int64(2000)

	tm := BuildTimeMap(rows, totalMs)

	// Only two non-separator times: 0 and 1000. Gap = 1000ms.
	// With only one gap, median = 1000ms, threshold = max(60000, 10000) = 60000.
	// Gap 1000ms < 60000ms => no compression.
	if tm.CompressedTotalMs != totalMs {
		t.Errorf("no compression expected: CompressedTotalMs=%d, want %d", tm.CompressedTotalMs, totalMs)
	}
}

// TestBuildTimeMap_EmptyAndSingleRow verifies graceful handling of degenerate
// inputs that cannot produce a gap (0 or 1 non-separator rows).
func TestBuildTimeMap_EmptyAndSingleRow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rows    []WaterfallRow
		totalMs int64
	}{
		{"nil rows", nil, 5000},
		{"zero totalMs", []WaterfallRow{{StartMs: 0}}, 0},
		{"one non-separator", []WaterfallRow{{StartMs: 0, IsUserSeparator: false}}, 5000},
		{"only separators", []WaterfallRow{{StartMs: 0, IsUserSeparator: true}}, 5000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tm := BuildTimeMap(tc.rows, tc.totalMs)
			// Should not panic. CompressedTotalMs == totalMs (linear or zero).
			if tc.totalMs > 0 && tm.CompressedTotalMs != tc.totalMs {
				t.Errorf("expected CompressedTotalMs=%d, got %d", tc.totalMs, tm.CompressedTotalMs)
			}
		})
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

// TestComputeWaterfallStats_Empty verifies stats for a session with no rows.
func TestComputeWaterfallStats_Empty(t *testing.T) {
	stats := ComputeWaterfallStats(nil, TimeAxis{})
	if stats.TotalTools != 0 {
		t.Errorf("TotalTools should be 0, got %d", stats.TotalTools)
	}
	if stats.MaxConcurrency != 0 {
		t.Errorf("MaxConcurrency should be 0, got %d", stats.MaxConcurrency)
	}
	if len(stats.TopTools) != 0 {
		t.Errorf("TopTools should be empty, got %v", stats.TopTools)
	}
}

// TestComputeWaterfallStats_ZeroToolRows verifies stats when rows exist but have no tools.
func TestComputeWaterfallStats_ZeroToolRows(t *testing.T) {
	// Only a user separator row -- no tool calls.
	rows := []WaterfallRow{
		{IsUserSeparator: true, StartMs: 0},
	}
	axis := TimeAxis{TotalMs: 5000}
	stats := ComputeWaterfallStats(rows, axis)
	if stats.TotalTools != 0 {
		t.Errorf("TotalTools should be 0 for separator-only rows, got %d", stats.TotalTools)
	}
	if stats.SessionMs != 5000 {
		t.Errorf("SessionMs should be 5000, got %d", stats.SessionMs)
	}
}

// TestComputeWaterfallStats_ThreeRowsOneConcurrentPair verifies stats for a
// session with 3 tool rows where two overlap in time (spec requirement).
//
// Layout:
//
//	Row A: starts 0ms,    duration 3000ms  (Read)
//	Row B: starts 1000ms, duration 3000ms  (Bash, Grep)  -- overlaps with A
//	Row C: starts 5000ms, duration 1000ms  (Edit)         -- no overlap
//
// Expected:
//
//	TotalTools     = 4  (Read + Bash + Grep + Edit)
//	MaxConcurrency = 2  (rows A and B overlap at t=1000..3000ms)
//	LongestTool    = Bash (2500ms)
//	TopTools[0]    = Bash (1), Edit (1), Grep (1), Read (1)  -- tied, alpha sorted
func TestComputeWaterfallStats_ThreeRowsOneConcurrentPair(t *testing.T) {
	rows := []WaterfallRow{
		{
			StartMs:    0,
			DurationMs: 3000,
			Tools: []WaterfallTool{
				{Name: "Read", Category: CategoryRead, DurationMs: 500},
			},
		},
		{
			StartMs:    1000,
			DurationMs: 3000,
			Tools: []WaterfallTool{
				{Name: "Bash", Category: CategoryBash, DurationMs: 2500},
				{Name: "Grep", Category: CategoryGrep, DurationMs: 800},
			},
		},
		{
			StartMs:    5000,
			DurationMs: 1000,
			Tools: []WaterfallTool{
				{Name: "Edit", Category: CategoryEdit, DurationMs: 900},
			},
		},
	}
	axis := TimeAxis{TotalMs: 6000}

	stats := ComputeWaterfallStats(rows, axis)

	if stats.TotalTools != 4 {
		t.Errorf("TotalTools: want 4, got %d", stats.TotalTools)
	}
	if stats.MaxConcurrency != 2 {
		t.Errorf("MaxConcurrency: want 2, got %d", stats.MaxConcurrency)
	}
	if stats.LongestTool != "Bash" {
		t.Errorf("LongestTool: want Bash, got %q", stats.LongestTool)
	}
	if stats.LongestToolMs != 2500 {
		t.Errorf("LongestToolMs: want 2500, got %d", stats.LongestToolMs)
	}
	if stats.SessionMs != 6000 {
		t.Errorf("SessionMs: want 6000, got %d", stats.SessionMs)
	}
	// All four tools appear once -- tied -- so sorted alphabetically.
	if len(stats.TopTools) != 4 {
		t.Fatalf("TopTools: want 4 entries, got %d: %v", len(stats.TopTools), stats.TopTools)
	}
	if stats.TopTools[0].Name != "Bash" {
		t.Errorf("TopTools[0]: want Bash, got %q", stats.TopTools[0].Name)
	}
}

// TestComputeWaterfallStats_TopToolsLimit verifies that TopTools is capped at 5.
func TestComputeWaterfallStats_TopToolsLimit(t *testing.T) {
	tools := []WaterfallTool{
		{Name: "A", DurationMs: 100},
		{Name: "B", DurationMs: 100},
		{Name: "C", DurationMs: 100},
		{Name: "D", DurationMs: 100},
		{Name: "E", DurationMs: 100},
		{Name: "F", DurationMs: 100},
	}
	rows := []WaterfallRow{
		{StartMs: 0, DurationMs: 600, Tools: tools},
	}
	axis := TimeAxis{TotalMs: 600}
	stats := ComputeWaterfallStats(rows, axis)
	if len(stats.TopTools) != 5 {
		t.Errorf("TopTools should be capped at 5, got %d", len(stats.TopTools))
	}
}

// TestComputeWaterfallStats_NonOverlapping verifies concurrency=1 for sequential rows.
func TestComputeWaterfallStats_NonOverlapping(t *testing.T) {
	rows := []WaterfallRow{
		{StartMs: 0, DurationMs: 1000, Tools: []WaterfallTool{{Name: "Read", DurationMs: 500}}},
		{StartMs: 1000, DurationMs: 1000, Tools: []WaterfallTool{{Name: "Edit", DurationMs: 800}}},
	}
	axis := TimeAxis{TotalMs: 2000}
	stats := ComputeWaterfallStats(rows, axis)
	if stats.MaxConcurrency != 1 {
		t.Errorf("MaxConcurrency for non-overlapping rows: want 1, got %d", stats.MaxConcurrency)
	}
}

// TestBuildWaterfallRows_RowDurationUsesMaxToolDuration verifies spec bl-77es:
// when a chunk contains one long-running tool and one zeroed tool, the row
// DurationMs is the max of c.DurationMs and the longest individual tool DurationMs.
func TestBuildWaterfallRows_RowDurationUsesMaxToolDuration(t *testing.T) {
	// Simulate a chunk where c.DurationMs = 500ms but one tool ran for 800ms.
	// This can happen when the parser zeroes a fast tool's duration to avoid
	// inflation, but the long tool's recorded duration exceeds the chunk span.
	chunk := aiChunkWithTools(baseTime, 500, "claude-sonnet", []DisplayItem{
		toolItem("Task", CategoryTask, 800, false, "long running"),
		toolItem("Read", CategoryRead, 0, false, "zeroed"),
	})

	rows, _ := BuildWaterfallRows([]Chunk{chunk})

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// Row duration must be max(500, 800) = 800ms.
	if rows[0].DurationMs != 800 {
		t.Errorf("DurationMs: want 800 (max of chunk and tool), got %d", rows[0].DurationMs)
	}
}

// TestBuildWaterfallRows_RowDurationFallsBackToChunkWhenAllToolsZeroed verifies
// spec bl-77es: when all tool durations are zero the row falls back to c.DurationMs.
func TestBuildWaterfallRows_RowDurationFallsBackToChunkWhenAllToolsZeroed(t *testing.T) {
	chunk := aiChunkWithTools(baseTime, 1200, "claude-sonnet", []DisplayItem{
		toolItem("Bash", CategoryBash, 0, false, "zeroed"),
		toolItem("Read", CategoryRead, 0, false, "also zeroed"),
	})

	rows, _ := BuildWaterfallRows([]Chunk{chunk})

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// All tool durations are zero, so row falls back to chunk duration.
	if rows[0].DurationMs != 1200 {
		t.Errorf("DurationMs: want 1200 (chunk fallback), got %d", rows[0].DurationMs)
	}
}
