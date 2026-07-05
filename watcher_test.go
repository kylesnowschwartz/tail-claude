package main

import (
	"testing"
	"time"

	"github.com/kylesnowschwartz/agent-ouija/claude/agents"
)

func TestWorkflowAdvanced(t *testing.T) {
	base := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	prev := agents.WorkflowActivity{Runs: 2, Agents: 10, LastWrite: base}

	tests := []struct {
		name string
		cur  agents.WorkflowActivity
		want bool
	}{
		{"unchanged", agents.WorkflowActivity{Runs: 2, Agents: 10, LastWrite: base}, false},
		{"no workflows at all", agents.WorkflowActivity{}, true}, // run dirs deleted — still a change worth reporting
		{"new run", agents.WorkflowActivity{Runs: 3, Agents: 10, LastWrite: base}, true},
		{"new agent transcript", agents.WorkflowActivity{Runs: 2, Agents: 11, LastWrite: base}, true},
		{"fresher write", agents.WorkflowActivity{Runs: 2, Agents: 10, LastWrite: base.Add(time.Second)}, true},
		{"older write only", agents.WorkflowActivity{Runs: 2, Agents: 10, LastWrite: base.Add(-time.Second)}, false},
	}
	for _, tt := range tests {
		if got := workflowAdvanced(prev, tt.cur); got != tt.want {
			t.Errorf("%s: workflowAdvanced = %v, want %v", tt.name, got, tt.want)
		}
	}

	if workflowAdvanced(agents.WorkflowActivity{}, agents.WorkflowActivity{}) {
		t.Error("zero vs zero: workflowAdvanced = true, want false (sessions without workflows must not signal)")
	}
}
