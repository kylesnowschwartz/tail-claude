package main

import (
	"strings"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestAggregateMessageStats_BasicAndOrder(t *testing.T) {
	msgs := []message{
		{
			role: RoleClaude,
			items: []displayItem{
				{itemType: parser.ItemToolCall, toolName: "Read", durationMs: 100},
				{itemType: parser.ItemToolCall, toolName: "Read", durationMs: 200},
				{itemType: parser.ItemToolCall, toolName: "Bash", durationMs: 1000, toolError: true},
				{itemType: parser.ItemSubagent, toolName: "Skill"}, // folds to Task
			},
		},
		{
			role: RoleClaude,
			items: []displayItem{
				{itemType: parser.ItemThinking, text: "ignored"},
				{itemType: parser.ItemToolCall, toolName: "Bash", durationMs: 0, toolError: true}, // 0 dur excluded
			},
		},
		{
			role:  RoleUser,
			items: []displayItem{{itemType: parser.ItemToolCall, toolName: "Read"}}, // user role excluded
		},
	}
	stats := aggregateMessageStats(msgs)

	if len(stats) != 3 {
		t.Fatalf("len = %d, want 3 (Read, Bash, Task)", len(stats))
	}
	// Sort: Bash(2) ~ Read(2) ~ Task(1). Bash/Read tied, alphabetical: Bash first.
	if stats[0].Name != "Bash" || stats[0].CallCount != 2 {
		t.Errorf("stats[0] = %+v, want Bash x2", stats[0])
	}
	if stats[0].TotalDurationMs != 1000 {
		t.Errorf("Bash duration = %d, want 1000 (0-dur entry excluded)", stats[0].TotalDurationMs)
	}
	if stats[0].ErrorCount != 2 {
		t.Errorf("Bash errors = %d, want 2", stats[0].ErrorCount)
	}
	if stats[1].Name != "Read" || stats[1].CallCount != 2 {
		t.Errorf("stats[1] = %+v, want Read x2", stats[1])
	}
	if stats[2].Name != "Task" || stats[2].CallCount != 1 {
		t.Errorf("stats[2] = %+v, want Task x1 (Skill folds)", stats[2])
	}
}

func TestAggregateMessageStats_NoToolsReturnsEmpty(t *testing.T) {
	msgs := []message{{role: RoleClaude, items: []displayItem{{itemType: parser.ItemOutput, text: "hi"}}}}
	if got := aggregateMessageStats(msgs); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestViewStats_RendersToolRowsAndHeader(t *testing.T) {
	m := model{
		width:  80,
		height: 24,
		messages: []message{
			{
				role: RoleClaude,
				items: []displayItem{
					{itemType: parser.ItemToolCall, toolName: "Read"},
					{itemType: parser.ItemToolCall, toolName: "Bash", toolError: true},
				},
			},
		},
	}
	got := m.viewStats()
	for _, want := range []string{"Tool Usage", "Read", "Bash", "Count", "Errors"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n---\n%s\n---", want, got)
		}
	}
}

func TestViewStats_EmptySessionShowsPlaceholder(t *testing.T) {
	m := model{width: 80, height: 24, messages: []message{
		{role: RoleClaude, items: []displayItem{{itemType: parser.ItemOutput, text: "no tools"}}},
	}}
	got := m.viewStats()
	if !strings.Contains(got, "No tool calls") {
		t.Errorf("empty session should show placeholder, got:\n%s", got)
	}
}
