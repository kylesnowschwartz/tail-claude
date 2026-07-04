package main

import (
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestWorkflowAdvanced(t *testing.T) {
	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	prev := parser.WorkflowActivity{Runs: 2, Agents: 10, LastWrite: base}

	tests := []struct {
		name string
		cur  parser.WorkflowActivity
		want bool
	}{
		{"unchanged", parser.WorkflowActivity{Runs: 2, Agents: 10, LastWrite: base}, false},
		{"no workflows at all", parser.WorkflowActivity{}, true}, // run dirs deleted — still a change worth reporting
		{"new run", parser.WorkflowActivity{Runs: 3, Agents: 10, LastWrite: base}, true},
		{"new agent transcript", parser.WorkflowActivity{Runs: 2, Agents: 11, LastWrite: base}, true},
		{"fresher write", parser.WorkflowActivity{Runs: 2, Agents: 10, LastWrite: base.Add(time.Second)}, true},
		{"older write only", parser.WorkflowActivity{Runs: 2, Agents: 10, LastWrite: base.Add(-time.Second)}, false},
	}
	for _, tt := range tests {
		if got := workflowAdvanced(prev, tt.cur); got != tt.want {
			t.Errorf("%s: workflowAdvanced = %v, want %v", tt.name, got, tt.want)
		}
	}

	if workflowAdvanced(parser.WorkflowActivity{}, parser.WorkflowActivity{}) {
		t.Error("zero vs zero: workflowAdvanced = true, want false (sessions without workflows must not signal)")
	}
}
