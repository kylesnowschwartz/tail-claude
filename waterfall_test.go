package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestRenderTimeAxisHeaderRightmostTick verifies that the 100% tick label is not
// truncated to just "|". The rightmost tick at barWidth-1 must render its full
// time string (e.g. "|10.0s") in the output.
func TestRenderTimeAxisHeaderRightmostTick(t *testing.T) {
	const barWidth = 40
	const gutterWidth = 12
	const totalMs = int64(10_000) // 10 seconds

	rendered := renderTimeAxisHeader(gutterWidth, barWidth, totalMs)
	// Strip ANSI escapes so we can inspect plain text.
	plain := ansi.Strip(rendered)

	// The rightmost tick label should be "|10.0s" (not just "|").
	if !strings.Contains(plain, "|10.0s") {
		t.Errorf("renderTimeAxisHeader: expected rightmost label '|10.0s' in output, got: %q", plain)
	}
}

// TestRenderTimeAxisHeaderAllTicks verifies that all five tick labels appear in
// the rendered header for a 10-second session.
func TestRenderTimeAxisHeaderAllTicks(t *testing.T) {
	const barWidth = 80
	const gutterWidth = 12
	const totalMs = int64(10_000)

	rendered := renderTimeAxisHeader(gutterWidth, barWidth, totalMs)
	plain := ansi.Strip(rendered)

	want := []string{"|0ms", "|2.5s", "|5.0s", "|7.5s", "|10.0s"}
	for _, label := range want {
		if !strings.Contains(plain, label) {
			t.Errorf("renderTimeAxisHeader: expected label %q in output, got: %q", label, plain)
		}
	}
}

// TestRenderTimeAxisHeaderNeverExceedsBarWidth verifies that the plain-text
// portion of the axis header (after trimming trailing spaces) does not exceed
// gutterWidth + barWidth + len(rightmost label), as the buffer is intentionally
// larger to accommodate the last label.
func TestRenderTimeAxisHeaderNeverExceedsBarWidth(t *testing.T) {
	for _, barWidth := range []int{20, 40, 80, 120} {
		const gutterWidth = 12
		const totalMs = int64(30_000) // 30 seconds

		rendered := renderTimeAxisHeader(gutterWidth, barWidth, totalMs)
		plain := ansi.Strip(rendered)

		// The gutter plus the bar area (with trailing last-label overhang) should
		// not exceed gutterWidth + barWidth + 16 (our headroom constant).
		maxLen := gutterWidth + barWidth + 16
		if len(plain) > maxLen {
			t.Errorf("barWidth=%d: plain output length %d exceeds max %d", barWidth, len(plain), maxLen)
		}
	}
}
