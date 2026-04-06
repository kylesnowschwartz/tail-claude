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

// TestRenderTimeAxisHeaderRightmostTick verifies that the last tick label is not
// truncated. For a 10-second session with 1s intervals the last tick at 10s
// must render its full label "|10.0s".
func TestRenderTimeAxisHeaderRightmostTick(t *testing.T) {
	const barWidth = 40
	const gutterWidth = 12
	const totalMs = int64(10_000) // 10 seconds

	rendered := renderTimeAxisHeader(gutterWidth, barWidth, totalMs, parser.TimeMap{})
	// Strip ANSI escapes so we can inspect plain text.
	plain := ansi.Strip(rendered)

	// The last tick label should be "|10.0s" (not just "|").
	if !strings.Contains(plain, "|10.0s") {
		t.Errorf("renderTimeAxisHeader: expected last label '|10.0s' in output, got: %q", plain)
	}
}

// TestRenderTimeAxisHeaderNiceIntervals verifies that smart round-number ticks
// are used instead of fixed percentages. For a 10-second session with target 8
// ticks the interval is 1s, so labels like "|0ms", "|1.0s", "|2.0s" appear.
func TestRenderTimeAxisHeaderNiceIntervals(t *testing.T) {
	const barWidth = 80
	const gutterWidth = 12
	const totalMs = int64(10_000)

	rendered := renderTimeAxisHeader(gutterWidth, barWidth, totalMs, parser.TimeMap{})
	plain := ansi.Strip(rendered)

	// With 1s interval, these ticks must be present.
	want := []string{"|0ms", "|1.0s", "|2.0s", "|5.0s", "|10.0s"}
	for _, label := range want {
		if !strings.Contains(plain, label) {
			t.Errorf("renderTimeAxisHeader: expected label %q in output, got: %q", label, plain)
		}
	}
	// Old fixed-percentage labels (2.5s, 7.5s) should NOT appear.
	notWant := []string{"|2.5s", "|7.5s"}
	for _, label := range notWant {
		if strings.Contains(plain, label) {
			t.Errorf("renderTimeAxisHeader: unexpected old-style label %q in output: %q", label, plain)
		}
	}
}

// TestWaterfallBarDurText verifies the duration label used on waterfall bars.
// Zero and sub-ms durations show "0ms"; otherwise the label delegates to formatDuration.
func TestWaterfallBarDurText(t *testing.T) {
	tests := []struct {
		name       string
		durationMs int64
		want       string
	}{
		// Zero and sub-millisecond cases.
		{"zero", 0, "0ms"},
		{"negative", -1, "0ms"},
		// Sub-second.
		{"45ms", 45, "0.0s"},
		{"500ms", 500, "0.5s"},
		{"999ms", 999, "1.0s"},
		// Multi-second.
		{"3.5s", 3500, "3.5s"},
		{"15s", 15000, "15s"},
		// Multi-minute.
		{"1m 11s", 71000, "1m 11s"},
		{"2m 5s", 125000, "2m 5s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := waterfallBarDurText(tt.durationMs)
			if got != tt.want {
				t.Errorf("waterfallBarDurText(%d) = %q, want %q", tt.durationMs, got, tt.want)
			}
		})
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

// TestRenderTimeAxisHeaderZeroTotalMs verifies that when totalMs is zero the
// time axis renders no tick marks (spec: "When totalMs is zero, no ticks").
func TestRenderTimeAxisHeaderZeroTotalMs(t *testing.T) {
	const barWidth = 80
	const gutterWidth = 12

	rendered := renderTimeAxisHeader(gutterWidth, barWidth, 0, parser.TimeMap{})
	plain := ansi.Strip(rendered)

	if strings.Contains(plain, "|") {
		t.Errorf("renderTimeAxisHeader with totalMs=0: expected no ticks, got: %q", plain)
	}
}

// TestRenderTimeAxisHeaderNarrowTerminal verifies that a narrow terminal
// (barWidth < 60) targets ~4 ticks instead of ~8.
func TestRenderTimeAxisHeaderNarrowTerminal(t *testing.T) {
	const gutterWidth = 12
	const totalMs = int64(120_000) // 2 minutes

	// Wide: barWidth=80 → target 8 ticks → rawInterval=15s → nice=15s
	wideRendered := renderTimeAxisHeader(gutterWidth, 80, totalMs, parser.TimeMap{})
	widePlain := ansi.Strip(wideRendered)

	// Narrow: barWidth=40 → target 4 ticks → rawInterval=30s → nice=30s
	narrowRendered := renderTimeAxisHeader(gutterWidth, 40, totalMs, parser.TimeMap{})
	narrowPlain := ansi.Strip(narrowRendered)

	// Count pipe characters as a proxy for tick count.
	wideTicks := strings.Count(widePlain, "|")
	narrowTicks := strings.Count(narrowPlain, "|")

	if narrowTicks >= wideTicks {
		t.Errorf("narrow terminal should produce fewer ticks than wide: narrow=%d wide=%d", narrowTicks, wideTicks)
	}
}

// TestRenderTimeAxisHeaderTickPositions verifies tick positions for sessions of
// 5s, 2m, and 30m duration (spec requirement).
func TestRenderTimeAxisHeaderTickPositions(t *testing.T) {
	cases := []struct {
		name     string
		totalMs  int64
		barWidth int
		// wantLabels are tick labels that must appear in the output.
		wantLabels []string
	}{
		{
			name:       "5s session",
			totalMs:    5_000,
			barWidth:   80,
			wantLabels: []string{"|0ms", "|1.0s", "|2.0s", "|3.0s", "|4.0s", "|5.0s"},
		},
		{
			name:       "2m session",
			totalMs:    120_000,
			barWidth:   80,
			wantLabels: []string{"|0ms", "|15.0s", "|30.0s", "|1m00s", "|1m30s", "|2m00s"},
		},
		{
			name:       "30m session",
			totalMs:    1_800_000,
			barWidth:   80,
			wantLabels: []string{"|0ms", "|5m00s", "|10m00s", "|15m00s", "|20m00s", "|25m00s", "|30m00s"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rendered := renderTimeAxisHeader(12, tc.barWidth, tc.totalMs, parser.TimeMap{})
			plain := ansi.Strip(rendered)
			for _, label := range tc.wantLabels {
				if !strings.Contains(plain, label) {
					t.Errorf("%s: expected label %q in output, got: %q", tc.name, label, plain)
				}
			}
		})
	}
}

// TestUserSeparatorMarkerColumn verifies that a user separator row renders a
// dimmed vertical bar character (│) at the column position corresponding to the
// separator's StartMs offset within the barWidth. The gutter (12 chars) precedes
// the bar area, so the bar character appears at index gutterWidth+col in the
// stripped line.
func TestUserSeparatorMarkerColumn(t *testing.T) {
	// totalMs = 10000, StartMs = 5000 => midpoint => col = barWidth/2.
	const totalMs = int64(10_000)
	const separatorStartMs = int64(5_000)
	// The constant gutterWidth=12 in renderWaterfallTimeline controls barWidth
	// (width - 12), but the rendered gutter string "+%-8s  " is 11 chars wide.
	const gutterConstant = 12
	const renderedGutterWidth = 11
	const barWidth = 80

	// Build a minimal model with one user separator row and enough height to render it.
	m := model{
		wfRows: []parser.WaterfallRow{
			{IsUserSeparator: true, StartMs: separatorStartMs},
		},
		wfTimeAxis: parser.TimeAxis{TotalMs: totalMs},
		// wfTimeMap zero value: CompressedTotalMs=0, MapToDisplay returns identity.
		height: 10, // enough rows to show the separator
	}

	rendered := m.renderWaterfallTimeline(gutterConstant + barWidth)
	lines := strings.Split(rendered, "\n")
	// Line 0 is the time axis header; line 1 is the separator row.
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}
	separatorLine := ansi.Strip(lines[1])

	// The gutter format is "+%-8s  " = 1 + 8 + 2 = 11 rendered chars.
	// ColOffset(5000, 10000, 80) = 40. Marker is at index renderedGutterWidth + 40 = 51.
	expectedCol := parser.ColOffset(separatorStartMs, totalMs, barWidth)
	barAreaOffset := renderedGutterWidth + expectedCol

	runes := []rune(separatorLine)
	if barAreaOffset >= len(runes) {
		t.Fatalf("line too short: expected marker at rune index %d, line has %d runes: %q",
			barAreaOffset, len(runes), separatorLine)
	}
	if runes[barAreaOffset] != '\u2502' {
		t.Errorf("expected vertical bar (│ U+2502) at column %d, got %U (line: %q)",
			barAreaOffset, runes[barAreaOffset], separatorLine)
	}
}
