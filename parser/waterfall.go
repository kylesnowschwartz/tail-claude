package parser

import (
	"sort"
	"time"
)

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

// segment is one piece of the compressed timeline mapping.
// rawStart..rawEnd maps linearly to displayStart..displayEnd.
type segment struct {
	rawStart     int64
	rawEnd       int64
	displayStart int64
	displayEnd   int64
}

// TimeMap converts raw millisecond offsets to compressed display milliseconds.
// Gaps exceeding 10x the median inter-chunk gap are compressed to a bounded size,
// preventing long idle periods (lunch breaks, user thinking) from dominating the
// timeline and squishing all bars to the left edge.
//
// When no compression is needed (all gaps below threshold), MapToDisplay is a
// linear identity mapping and CompressedTotalMs equals the raw total.
type TimeMap struct {
	CompressedTotalMs int64
	segments          []segment
}

// MapToDisplay converts a raw millisecond offset to a compressed display offset.
// Returns the raw value unchanged when no segments exist (identity mapping).
func (tm TimeMap) MapToDisplay(rawMs int64) int64 {
	if len(tm.segments) == 0 {
		return rawMs
	}
	// Find the segment containing rawMs.
	for _, seg := range tm.segments {
		if rawMs >= seg.rawStart && rawMs <= seg.rawEnd {
			if seg.rawEnd == seg.rawStart {
				return seg.displayStart
			}
			// Linear interpolation within segment.
			ratio := float64(rawMs-seg.rawStart) / float64(seg.rawEnd-seg.rawStart)
			return seg.displayStart + int64(ratio*float64(seg.displayEnd-seg.displayStart))
		}
	}
	// Beyond the last segment: clamp to CompressedTotalMs.
	return tm.CompressedTotalMs
}

// BuildTimeMap constructs a TimeMap from waterfall rows. Gaps between consecutive
// non-separator rows are analysed; those exceeding 10x the median gap are
// compressed to max(5, min(30, median*3)) display milliseconds per millisecond.
//
// The algorithm is ported from agentviz session.ts TimeMap compression.
func BuildTimeMap(rows []WaterfallRow, totalMs int64) TimeMap {
	if totalMs <= 0 {
		return TimeMap{}
	}

	// Collect start times of non-separator rows to compute inter-chunk gaps.
	var times []int64
	for _, r := range rows {
		if !r.IsUserSeparator {
			times = append(times, r.StartMs)
		}
	}

	// Need at least two non-separator rows to have a gap.
	if len(times) < 2 {
		return buildLinearTimeMap(totalMs)
	}

	// Compute inter-chunk gaps (sorted for median).
	gaps := make([]int64, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		g := times[i] - times[i-1]
		if g > 0 {
			gaps = append(gaps, g)
		}
	}
	if len(gaps) == 0 {
		return buildLinearTimeMap(totalMs)
	}

	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	median := gaps[len(gaps)/2]
	if median == 0 {
		return buildLinearTimeMap(totalMs)
	}

	// Threshold: gaps longer than this get compressed.
	minThreshold := int64(60_000) // 60 seconds minimum threshold
	threshold := median * 10
	if threshold < minThreshold {
		threshold = minThreshold
	}

	// Compressed size for gaps exceeding threshold (in display ms per raw ms ratio,
	// but we store it as the absolute display duration for the compressed gap).
	compressedGapDisplay := median * 3
	if compressedGapDisplay < 5 {
		compressedGapDisplay = 5
	}
	if compressedGapDisplay > 30 {
		compressedGapDisplay = 30
	}

	// Check whether any gap actually exceeds the threshold.
	anyCompressed := false
	for _, g := range gaps {
		if g > threshold {
			anyCompressed = true
			break
		}
	}
	if !anyCompressed {
		return buildLinearTimeMap(totalMs)
	}

	// Build segments by walking through breakpoints (times[0]..times[n-1] and
	// totalMs). Each consecutive pair [prev, curr] becomes either a linear
	// segment (gap ≤ threshold) or a compressed segment (gap > threshold).
	var segs []segment
	displayCursor := int64(0)

	// Include 0 as the first breakpoint and totalMs as the last.
	breakpoints := make([]int64, 0, len(times)+1)
	breakpoints = append(breakpoints, times...)
	if len(times) == 0 || times[len(times)-1] < totalMs {
		breakpoints = append(breakpoints, totalMs)
	}

	prev := int64(0)
	for _, curr := range breakpoints {
		if curr <= prev {
			prev = curr
			continue
		}
		gap := curr - prev
		if gap > threshold {
			segs = append(segs, segment{
				rawStart:     prev,
				rawEnd:       curr,
				displayStart: displayCursor,
				displayEnd:   displayCursor + compressedGapDisplay,
			})
			displayCursor += compressedGapDisplay
		} else {
			segs = append(segs, segment{
				rawStart:     prev,
				rawEnd:       curr,
				displayStart: displayCursor,
				displayEnd:   displayCursor + gap,
			})
			displayCursor += gap
		}
		prev = curr
	}

	return TimeMap{
		CompressedTotalMs: displayCursor,
		segments:          segs,
	}
}

// buildLinearTimeMap returns a TimeMap that maps raw ms to display ms 1:1.
func buildLinearTimeMap(totalMs int64) TimeMap {
	return TimeMap{
		CompressedTotalMs: totalMs,
		segments: []segment{{
			rawStart:     0,
			rawEnd:       totalMs,
			displayStart: 0,
			displayEnd:   totalMs,
		}},
	}
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
