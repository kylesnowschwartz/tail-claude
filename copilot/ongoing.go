package copilot

import (
	"time"

	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
)

// IsOngoing reports whether a Copilot session appears live. Copilot streams
// events continuously during a turn, so a fresh mtime without a terminal
// event (session.shutdown or abort) means the session is still running.
// An empty lastEventType (no events read yet) is never ongoing.
func IsOngoing(lastEventType string, modTime time.Time) bool {
	if lastEventType == "" {
		return false
	}
	if time.Since(modTime) >= transcript.OngoingStalenessThreshold {
		return false
	}
	return lastEventType != "session.shutdown" && lastEventType != "abort"
}
