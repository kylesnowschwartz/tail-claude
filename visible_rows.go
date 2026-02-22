package main

import "github.com/kylesnowschwartz/tail-claude/parser"

// visibleRow represents one navigable row in the detail view's flat row list.
// Parent items have childIndex == -1. Child items belong to an expanded
// subagent and represent individual steps in the subagent's execution trace.
type visibleRow struct {
	parentIndex int         // index into msg.items
	childIndex  int         // index into trace items, or -1 for parent rows
	item        displayItem // the display item for this row
}

// visibleRowKey identifies a child row for expansion state tracking.
type visibleRowKey struct {
	parentIndex int
	childIndex  int
}

// buildVisibleRows computes the flat row list from items and parent expansion
// state. Expanded subagent items with a linked process insert their trace
// items as child rows. Non-subagent expansions don't add child rows -- their
// expanded content is rendered inline below the parent row.
func buildVisibleRows(items []displayItem, expanded map[int]bool) []visibleRow {
	var rows []visibleRow
	for i, item := range items {
		rows = append(rows, visibleRow{parentIndex: i, childIndex: -1, item: item})
		if expanded[i] && item.itemType == parser.ItemSubagent && item.subagentProcess != nil {
			for ci, child := range buildTraceItems(item) {
				rows = append(rows, visibleRow{parentIndex: i, childIndex: ci, item: child})
			}
		}
	}
	return rows
}

// buildTraceItems creates display items from a subagent's execution trace.
// UserChunks become "Input" items; AIChunk items pass through with full
// field mapping via displayItemFromParser.
func buildTraceItems(parent displayItem) []displayItem {
	if parent.subagentProcess == nil {
		return nil
	}
	proc := parent.subagentProcess
	var items []displayItem
	for _, c := range proc.Chunks {
		switch c.Type {
		case parser.UserChunk:
			items = append(items, displayItem{
				itemType: parser.ItemOutput,
				toolName: "Input",
				text:     c.UserText,
			})
		case parser.AIChunk:
			for _, it := range c.Items {
				items = append(items, displayItemFromParser(it))
			}
		}
	}
	return items
}

// traceItemStats counts tool calls and messages in a trace item list,
// used for the "Execution Trace" header summary.
func traceItemStats(items []displayItem) (toolCount, msgCount int) {
	for _, item := range items {
		switch item.itemType {
		case parser.ItemToolCall, parser.ItemSubagent:
			toolCount++
		case parser.ItemOutput:
			msgCount++
		}
	}
	return
}
