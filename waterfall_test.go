package main

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/kylesnowschwartz/tail-claude/parser"
)

// TestFractionalBlock verifies the 1/8th-precision left-block rune mapping.
func TestFractionalBlock(t *testing.T) {
	cases := []struct {
		f    float64
		want rune
	}{
		{0.0, 0},
		{0.0624, 0},           // just below 1/16 boundary -- rounds to 0
		{0.0625, '\u258F'},    // at 1/16 boundary -- rounds up to 1/8
		{0.125, '\u258F'},     // exactly 1/8 ▏
		{0.25, '\u258E'},      // 2/8 ▎
		{0.375, '\u258D'},     // 3/8 ▍
		{0.5, '\u258C'},       // 4/8 ▌
		{0.625, '\u258B'},     // 5/8 ▋
		{0.75, '\u258A'},      // 6/8 ▊
		{0.875, '\u2589'},     // 7/8 ▉
		{0.999, '\u2588'},     // just under 1.0 -- rounds to full
		{1.0, '\u2588'},       // exactly 1.0 -- full block
		{1.5, '\u2588'},       // over 1.0 -- full block
	}
	for _, c := range cases {
		got := fractionalBlock(c.f)
		if got != c.want {
			t.Errorf("fractionalBlock(%v) = %U, want %U", c.f, got, c.want)
		}
	}
}

// TestRenderBarStringEmpty verifies that zero or negative width returns empty string.
func TestRenderBarStringEmpty(t *testing.T) {
	col := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	if s := renderBarString(0, col); s != "" {
		t.Errorf("renderBarString(0, ...) should be empty, got %q", s)
	}
	if s := renderBarString(-1, col); s != "" {
		t.Errorf("renderBarString(-1, ...) should be empty, got %q", s)
	}
}

// TestRenderBarStringFullBlocks verifies that integer widths produce only full
// blocks (no trailing fractional character) and the visible width matches.
func TestRenderBarStringFullBlocks(t *testing.T) {
	col := color.RGBA{R: 0, G: 128, B: 255, A: 255}
	for _, n := range []float64{1, 3, 5} {
		s := renderBarString(n, col)
		plain := ansi.Strip(s)
		// Count runes in plain text -- should equal n full blocks.
		runes := []rune(plain)
		if len(runes) != int(n) {
			t.Errorf("renderBarString(%v): expected %d runes, got %d (plain: %q)", n, int(n), len(runes), plain)
		}
		for _, r := range runes {
			if r != '\u2588' {
				t.Errorf("renderBarString(%v): expected full block, got %U", n, r)
			}
		}
	}
}

// TestRenderBarStringFractionalTrail verifies that a fractional width produces
// a trailing partial block rune after any full blocks.
func TestRenderBarStringFractionalTrail(t *testing.T) {
	col := color.RGBA{R: 0, G: 200, B: 100, A: 255}
	// 2.5 cells: 2 full blocks + trailing ▌ (4/8)
	s := renderBarString(2.5, col)
	plain := ansi.Strip(s)
	runes := []rune(plain)
	if len(runes) != 3 {
		t.Fatalf("renderBarString(2.5): expected 3 runes, got %d (plain: %q)", len(runes), plain)
	}
	if runes[0] != '\u2588' || runes[1] != '\u2588' {
		t.Errorf("renderBarString(2.5): first two runes should be full blocks, got %U %U", runes[0], runes[1])
	}
	if runes[2] != '\u258C' { // ▌ = 4/8
		t.Errorf("renderBarString(2.5): trailing rune should be ▌ (U+258C), got %U", runes[2])
	}
}

// TestRenderTimeAxisHeaderRightmostTick verifies that the 100% tick label is not
// truncated to just "|". The rightmost tick at barWidth-1 must render its full
// time string (e.g. "|10.0s") in the output.
func TestRenderTimeAxisHeaderRightmostTick(t *testing.T) {
	const barWidth = 40
	const gutterWidth = 12
	const totalMs = int64(10_000) // 10 seconds

	rendered := renderTimeAxisHeader(gutterWidth, barWidth, totalMs, parser.TimeMap{})
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

	rendered := renderTimeAxisHeader(gutterWidth, barWidth, totalMs, parser.TimeMap{})
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

		rendered := renderTimeAxisHeader(gutterWidth, barWidth, totalMs, parser.TimeMap{})
		plain := ansi.Strip(rendered)

		// The gutter plus the bar area (with trailing last-label overhang) should
		// not exceed gutterWidth + barWidth + 16 (our headroom constant).
		maxLen := gutterWidth + barWidth + 16
		if len(plain) > maxLen {
			t.Errorf("barWidth=%d: plain output length %d exceeds max %d", barWidth, len(plain), maxLen)
		}
	}
}
