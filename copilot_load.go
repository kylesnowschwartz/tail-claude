package main

import (
	"fmt"
	"os"

	"github.com/kylesnowschwartz/tail-claude/copilot"
)

// loadCopilotSession is the Copilot counterpart of the Claude branch in
// loadSession: read events.jsonl, build chunks, convert to display messages.
// No subagent/team/workflow discovery — those scans are Claude-layout only
// (Copilot subagent drill-down is a v1 non-goal; nested agentId events are
// dropped by the parser and the parent task renders as a plain tool call).
func loadCopilotSession(path string) (loadResult, error) {
	sess, err := copilot.ReadSession(path)
	if err != nil {
		return loadResult{}, fmt.Errorf("reading session %s: %w", path, err)
	}

	chunks := copilot.BuildChunks(sess.Msgs)
	if len(chunks) == 0 {
		return loadResult{}, fmt.Errorf("session %s has no messages", path)
	}

	ongoing := false
	if info, err := os.Stat(path); err == nil {
		ongoing = copilot.IsOngoing(sess.Reader.LastEventType, info.ModTime())
	}

	return loadResult{
		messages:      chunksToMessages(chunks, nil, nil),
		path:          path,
		classified:    sess.Msgs,
		offset:        sess.Offset,
		ongoing:       ongoing,
		meta:          sess.Reader.Meta,
		copilotReader: sess.Reader,
	}, nil
}
