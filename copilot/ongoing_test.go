package copilot

import (
	"testing"
	"time"
)

func TestIsOngoing(t *testing.T) {
	fresh := time.Now().Add(-5 * time.Second)
	stale := time.Now().Add(-10 * time.Minute)

	cases := []struct {
		lastEventType string
		modTime       time.Time
		want          bool
	}{
		{"user.message", fresh, true},
		{"tool.execution_start", fresh, true},
		{"assistant.message", fresh, true},
		{"session.shutdown", fresh, false},
		{"abort", fresh, false},
		{"user.message", stale, false},
		{"tool.execution_start", stale, false},
		{"", fresh, false},
	}
	for _, c := range cases {
		if got := IsOngoing(c.lastEventType, c.modTime); got != c.want {
			t.Errorf("IsOngoing(%q, %v ago) = %v, want %v",
				c.lastEventType, time.Since(c.modTime).Round(time.Second), got, c.want)
		}
	}
}
