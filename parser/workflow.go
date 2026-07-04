package parser

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WorkflowActivity summarizes background Workflow-tool activity for a
// session. Workflow runs (Claude Code's multi-agent orchestration) write
// their agent transcripts under {session}/subagents/workflows/wf_{runId}/,
// and the parent session file stays silent while they work — so parent-file
// heuristics alone make a session with a running workflow look idle.
//
// Populated by ScanWorkflowActivity via a cheap directory scan: file counts
// and modtimes only, no JSONL parsing. Full workflow drill-down (per-agent
// traces linked to the Workflow tool call) is a separate feature.
type WorkflowActivity struct {
	Runs      int       // wf_* directories on disk
	Agents    int       // agent-*.jsonl transcripts across all runs
	LastWrite time.Time // most recent write to any file in any run dir
}

// Active reports whether any workflow file was written within the threshold —
// the signal that agents are still working.
func (w WorkflowActivity) Active(threshold time.Duration) bool {
	return w.Agents > 0 && !w.LastWrite.IsZero() && time.Since(w.LastWrite) <= threshold
}

// ScanWorkflowActivity scans a session's workflow directories. Returns the
// zero value when the session has no workflows (or on any error) — absence
// of activity, not a failure.
func ScanWorkflowActivity(sessionPath string) WorkflowActivity {
	base := strings.TrimSuffix(sessionPath, ".jsonl")
	wfRoot := filepath.Join(base, "subagents", "workflows")

	runs, err := os.ReadDir(wfRoot)
	if err != nil {
		return WorkflowActivity{}
	}

	var act WorkflowActivity
	for _, run := range runs {
		if !run.IsDir() || !strings.HasPrefix(run.Name(), "wf_") {
			continue
		}
		act.Runs++
		files, err := os.ReadDir(filepath.Join(wfRoot, run.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			if strings.HasPrefix(f.Name(), "agent-") && strings.HasSuffix(f.Name(), ".jsonl") {
				act.Agents++
			}
			// Any file's write (agent transcripts, journal.jsonl) counts as
			// activity — the journal updates on every agent start/result.
			if info, err := f.Info(); err == nil && info.ModTime().After(act.LastWrite) {
				act.LastWrite = info.ModTime()
			}
		}
	}
	return act
}
