package main

import (
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

// --- buildVisibleRows -------------------------------------------------------

func TestBuildVisibleRows(t *testing.T) {
	t.Run("no items returns empty", func(t *testing.T) {
		rows := buildVisibleRows(nil, nil)
		if len(rows) != 0 {
			t.Errorf("len = %d, want 0", len(rows))
		}
	})

	t.Run("no expansions returns parent rows only", func(t *testing.T) {
		items := []displayItem{
			{itemType: parser.ItemThinking, text: "thinking"},
			{itemType: parser.ItemToolCall, toolName: "Read"},
			{itemType: parser.ItemOutput, text: "done"},
		}
		rows := buildVisibleRows(items, make(map[int]bool))
		if len(rows) != 3 {
			t.Fatalf("len = %d, want 3", len(rows))
		}
		for i, row := range rows {
			if row.parentIndex != i {
				t.Errorf("rows[%d].parentIndex = %d, want %d", i, row.parentIndex, i)
			}
			if row.childIndex != -1 {
				t.Errorf("rows[%d].childIndex = %d, want -1", i, row.childIndex)
			}
		}
	})

	t.Run("expanded subagent with process inserts children", func(t *testing.T) {
		proc := &parser.SubagentProcess{
			Chunks: []parser.Chunk{
				{Type: parser.UserChunk, UserText: "hello"},
				{Type: parser.AIChunk, Items: []parser.DisplayItem{
					{Type: parser.ItemToolCall, ToolName: "Read"},
					{Type: parser.ItemOutput, Text: "result"},
				}},
			},
		}
		items := []displayItem{
			{itemType: parser.ItemThinking, text: "thinking"},
			{itemType: parser.ItemSubagent, subagentType: "Explore", subagentProcess: proc},
			{itemType: parser.ItemOutput, text: "done"},
		}
		expanded := map[int]bool{1: true}
		rows := buildVisibleRows(items, expanded)

		// 3 parents + 3 children (Input, Read, Output from trace)
		if len(rows) != 6 {
			t.Fatalf("len = %d, want 6", len(rows))
		}

		// Row 0: parent 0 (thinking)
		if rows[0].parentIndex != 0 || rows[0].childIndex != -1 {
			t.Errorf("row 0: parent=%d child=%d, want parent=0 child=-1",
				rows[0].parentIndex, rows[0].childIndex)
		}

		// Row 1: parent 1 (subagent)
		if rows[1].parentIndex != 1 || rows[1].childIndex != -1 {
			t.Errorf("row 1: parent=%d child=%d, want parent=1 child=-1",
				rows[1].parentIndex, rows[1].childIndex)
		}

		// Rows 2-4: children of parent 1
		for ci := 0; ci < 3; ci++ {
			ri := 2 + ci
			if rows[ri].parentIndex != 1 || rows[ri].childIndex != ci {
				t.Errorf("row %d: parent=%d child=%d, want parent=1 child=%d",
					ri, rows[ri].parentIndex, rows[ri].childIndex, ci)
			}
		}

		// Row 5: parent 2 (output)
		if rows[5].parentIndex != 2 || rows[5].childIndex != -1 {
			t.Errorf("row 5: parent=%d child=%d, want parent=2 child=-1",
				rows[5].parentIndex, rows[5].childIndex)
		}
	})

	t.Run("expanded subagent without process does not insert children", func(t *testing.T) {
		items := []displayItem{
			{itemType: parser.ItemSubagent, subagentType: "Explore"},
		}
		expanded := map[int]bool{0: true}
		rows := buildVisibleRows(items, expanded)
		if len(rows) != 1 {
			t.Errorf("len = %d, want 1 (no children without process)", len(rows))
		}
	})

	t.Run("collapsed subagent with process does not insert children", func(t *testing.T) {
		proc := &parser.SubagentProcess{
			Chunks: []parser.Chunk{
				{Type: parser.AIChunk, Items: []parser.DisplayItem{
					{Type: parser.ItemToolCall, ToolName: "Read"},
				}},
			},
		}
		items := []displayItem{
			{itemType: parser.ItemSubagent, subagentProcess: proc},
		}
		rows := buildVisibleRows(items, make(map[int]bool))
		if len(rows) != 1 {
			t.Errorf("len = %d, want 1 (collapsed)", len(rows))
		}
	})
}

// --- buildTraceItems --------------------------------------------------------

func TestBuildTraceItems(t *testing.T) {
	t.Run("nil process returns nil", func(t *testing.T) {
		items := buildTraceItems(displayItem{})
		if items != nil {
			t.Errorf("expected nil, got %d items", len(items))
		}
	})

	t.Run("maps user chunks to Input items", func(t *testing.T) {
		proc := &parser.SubagentProcess{
			Chunks: []parser.Chunk{
				{Type: parser.UserChunk, UserText: "do the thing"},
			},
		}
		items := buildTraceItems(displayItem{subagentProcess: proc})
		if len(items) != 1 {
			t.Fatalf("len = %d, want 1", len(items))
		}
		if items[0].itemType != parser.ItemOutput {
			t.Errorf("type = %d, want ItemOutput", items[0].itemType)
		}
		if items[0].toolName != "Input" {
			t.Errorf("toolName = %q, want Input", items[0].toolName)
		}
	})

	t.Run("maps AI chunk items via displayItemFromParser", func(t *testing.T) {
		proc := &parser.SubagentProcess{
			Chunks: []parser.Chunk{
				{Type: parser.AIChunk, Items: []parser.DisplayItem{
					{Type: parser.ItemToolCall, ToolName: "Bash", ToolSummary: "run test"},
					{Type: parser.ItemOutput, Text: "all passed"},
				}},
			},
		}
		items := buildTraceItems(displayItem{subagentProcess: proc})
		if len(items) != 2 {
			t.Fatalf("len = %d, want 2", len(items))
		}
		if items[0].toolName != "Bash" {
			t.Errorf("items[0].toolName = %q, want Bash", items[0].toolName)
		}
		if items[1].text != "all passed" {
			t.Errorf("items[1].text = %q, want 'all passed'", items[1].text)
		}
	})
}

// --- traceItemStats ---------------------------------------------------------

func TestTraceItemStats(t *testing.T) {
	items := []displayItem{
		{itemType: parser.ItemOutput, toolName: "Input"},
		{itemType: parser.ItemToolCall, toolName: "Read"},
		{itemType: parser.ItemToolCall, toolName: "Bash"},
		{itemType: parser.ItemSubagent, subagentType: "Explore"},
		{itemType: parser.ItemOutput, text: "result"},
		{itemType: parser.ItemThinking, text: "hmm"},
	}
	tools, msgs := traceItemStats(items)
	if tools != 3 {
		t.Errorf("toolCount = %d, want 3", tools)
	}
	if msgs != 2 {
		t.Errorf("msgCount = %d, want 2", msgs)
	}
}
