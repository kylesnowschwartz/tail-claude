package parser

import "time"

// WaterfallRow represents one horizontal bar in the waterfall timeline view.
// Each AI chunk with tool calls becomes a row; user messages become separators.
type WaterfallRow struct {
	ChunkIndex      int
	StartMs         int64 // offset from session start (first chunk timestamp)
	DurationMs      int64
	Tools           []WaterfallTool
	IsSubagent      bool // true if any item in the chunk is ItemSubagent
	IsUserSeparator bool // true for UserChunk rows (no tools, no duration)
	Model           string
}

// WaterfallTool holds per-tool timing data extracted from a DisplayItem.
type WaterfallTool struct {
	Name       string
	Category   ToolCategory
	DurationMs int64
	Error      bool
	Summary    string
}

// TimeAxis carries the session-level time boundaries for the waterfall view.
type TimeAxis struct {
	StartTime time.Time
	EndTime   time.Time
	TotalMs   int64
}

// BuildWaterfallRows converts a slice of Chunks into waterfall rows and a
// TimeAxis. Pure function -- no side effects.
//
// Only AIChunks that contain at least one ItemToolCall or ItemSubagent produce
// a data row. UserChunks produce separator rows. Other chunk types are skipped.
func BuildWaterfallRows(chunks []Chunk) ([]WaterfallRow, TimeAxis) {
	if len(chunks) == 0 {
		return nil, TimeAxis{}
	}

	firstTS := chunks[0].Timestamp
	// Compute the true latest end time across all chunks (not just the last one).
	// A long-running chunk starting earlier can end after the chronologically last chunk.
	var endTime time.Time
	for _, c := range chunks {
		chunkEnd := c.Timestamp.Add(time.Duration(c.DurationMs) * time.Millisecond)
		if chunkEnd.After(endTime) {
			endTime = chunkEnd
		}
	}
	totalMs := endTime.Sub(firstTS).Milliseconds()

	var rows []WaterfallRow

	for i, c := range chunks {
		switch c.Type {
		case UserChunk:
			rows = append(rows, WaterfallRow{
				ChunkIndex:      i,
				StartMs:         c.Timestamp.Sub(firstTS).Milliseconds(),
				IsUserSeparator: true,
			})

		case AIChunk:
			if !hasToolItems(c.Items) {
				continue
			}
			tools := extractTools(c.Items)
			rows = append(rows, WaterfallRow{
				ChunkIndex: i,
				StartMs:    c.Timestamp.Sub(firstTS).Milliseconds(),
				DurationMs: c.DurationMs,
				Tools:      tools,
				IsSubagent: hasSubagentItem(c.Items),
				Model:      c.Model,
			})
		}
	}

	axis := TimeAxis{
		StartTime: firstTS,
		EndTime:   endTime,
		TotalMs:   totalMs,
	}
	return rows, axis
}

// hasToolItems reports whether items contains at least one ItemToolCall or ItemSubagent.
func hasToolItems(items []DisplayItem) bool {
	for _, it := range items {
		if it.Type == ItemToolCall || it.Type == ItemSubagent {
			return true
		}
	}
	return false
}

// hasSubagentItem reports whether any item is an ItemSubagent.
func hasSubagentItem(items []DisplayItem) bool {
	for _, it := range items {
		if it.Type == ItemSubagent {
			return true
		}
	}
	return false
}

// extractTools converts DisplayItems to WaterfallTool entries. Only
// ItemToolCall and ItemSubagent items are included.
func extractTools(items []DisplayItem) []WaterfallTool {
	var tools []WaterfallTool
	for _, it := range items {
		if it.Type != ItemToolCall && it.Type != ItemSubagent {
			continue
		}
		tools = append(tools, WaterfallTool{
			Name:       it.ToolName,
			Category:   it.ToolCategory,
			DurationMs: it.DurationMs,
			Error:      it.ToolError,
			Summary:    it.ToolSummary,
		})
	}
	return tools
}

// ColOffset maps a millisecond offset to a column position within a width-wide
// display area. Returns 0 when totalMs is 0. Result is clamped to [0, width-1].
func ColOffset(ms, totalMs int64, width int) int {
	if totalMs == 0 || width <= 0 {
		return 0
	}
	col := int(float64(ms) / float64(totalMs) * float64(width))
	if col < 0 {
		return 0
	}
	if col >= width {
		return width - 1
	}
	return col
}
