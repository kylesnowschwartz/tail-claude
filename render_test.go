package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/kylesnowschwartz/agent-ouija/claude/agents"
	"github.com/kylesnowschwartz/agent-ouija/claude/debuglog"
	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
	zone "github.com/lrstanley/bubblezone/v2"
)

func TestNewRendered(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLines int
	}{
		{
			name:      "single line",
			input:     "hello world",
			wantLines: 1,
		},
		{
			name:      "two lines",
			input:     "line one\nline two",
			wantLines: 2,
		},
		{
			name:      "three lines",
			input:     "a\nb\nc",
			wantLines: 3,
		},
		{
			name:      "empty string counts as one line",
			input:     "",
			wantLines: 1,
		},
		{
			name:      "trailing newline",
			input:     "hello\n",
			wantLines: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRendered(tt.input)
			if r.content != tt.input {
				t.Errorf("content = %q, want %q", r.content, tt.input)
			}
			if r.lines != tt.wantLines {
				t.Errorf("lines = %d, want %d", r.lines, tt.wantLines)
			}
		})
	}
}

func TestContentWidth(t *testing.T) {
	tests := []struct {
		cardWidth int
		want      int
	}{
		{120, 114}, // 120 - 6 = 114
		{80, 74},   // 80 - 6 = 74
		{30, 24},   // 30 - 6 = 24
		{26, 20},   // 26 - 6 = 20 (at floor)
		{10, 20},   // 10 - 6 = 4 → floored to 20
		{0, 20},    // negative → floored to 20
	}
	for _, tt := range tests {
		got := contentWidth(tt.cardWidth)
		if got != tt.want {
			t.Errorf("contentWidth(%d) = %d, want %d", tt.cardWidth, got, tt.want)
		}
	}
}

func TestTruncateLines(t *testing.T) {
	t.Run("under limit returns content unchanged with 0 hidden", func(t *testing.T) {
		content := "line1\nline2\nline3"
		got, hidden := truncateLines(content, 5)
		if got != content {
			t.Errorf("content = %q, want unchanged %q", got, content)
		}
		if hidden != 0 {
			t.Errorf("hidden = %d, want 0", hidden)
		}
	})

	t.Run("at limit returns content unchanged with 0 hidden", func(t *testing.T) {
		content := "line1\nline2\nline3"
		got, hidden := truncateLines(content, 3)
		if got != content {
			t.Errorf("content = %q, want unchanged %q", got, content)
		}
		if hidden != 0 {
			t.Errorf("hidden = %d, want 0", hidden)
		}
	})

	t.Run("over limit returns truncated content and hidden count", func(t *testing.T) {
		content := "line1\nline2\nline3\nline4\nline5"
		got, hidden := truncateLines(content, 3)
		wantContent := "line1\nline2\nline3"
		if got != wantContent {
			t.Errorf("content = %q, want %q", got, wantContent)
		}
		if hidden != 2 {
			t.Errorf("hidden = %d, want 2", hidden)
		}
	})

	t.Run("single line under limit", func(t *testing.T) {
		got, hidden := truncateLines("only one line", 6)
		if got != "only one line" {
			t.Errorf("content = %q, want unchanged", got)
		}
		if hidden != 0 {
			t.Errorf("hidden = %d, want 0", hidden)
		}
	})
}

func TestSpaceBetween(t *testing.T) {
	t.Run("normal gap fills to width", func(t *testing.T) {
		left := "left"
		right := "right"
		width := 20
		got := spaceBetween(left, right, width)
		// Total visual width should equal the input width
		gotWidth := lipgloss.Width(got)
		if gotWidth != width {
			t.Errorf("spaceBetween width = %d, want %d (got %q)", gotWidth, width, got)
		}
		if !strings.HasPrefix(got, left) {
			t.Errorf("result should start with left %q, got %q", left, got)
		}
		if !strings.HasSuffix(got, right) {
			t.Errorf("result should end with right %q, got %q", right, got)
		}
	})

	t.Run("tight width floors gap at 2 spaces", func(t *testing.T) {
		left := "loooooooooooooooooooong-left"
		right := "right"
		// Width is smaller than left+right combined — should still have 2-space gap
		got := spaceBetween(left, right, 10)
		gap := strings.Index(got, right) - len(left)
		if gap < 2 {
			t.Errorf("gap = %d, want at least 2 (got %q)", gap, got)
		}
	})

	t.Run("ANSI-coded inputs still measure correctly", func(t *testing.T) {
		left := StylePrimaryBold.Render("bold text")
		right := StyleDim.Render("dim text")
		width := 60
		got := spaceBetween(left, right, width)
		gotWidth := lipgloss.Width(got)
		if gotWidth != width {
			t.Errorf("spaceBetween with ANSI codes: width = %d, want %d", gotWidth, width)
		}
	})
}

func TestIndentBlock(t *testing.T) {
	t.Run("single line", func(t *testing.T) {
		got := indentBlock("hello", "  ")
		if got != "  hello" {
			t.Errorf("indentBlock = %q, want %q", got, "  hello")
		}
	})

	t.Run("multi-line indents every line", func(t *testing.T) {
		got := indentBlock("line1\nline2\nline3", "  ")
		want := "  line1\n  line2\n  line3"
		if got != want {
			t.Errorf("indentBlock =\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("empty string gets indent prefix", func(t *testing.T) {
		got := indentBlock("", "  ")
		if got != "  " {
			t.Errorf("indentBlock(\"\") = %q, want %q", got, "  ")
		}
	})

	t.Run("custom indent string", func(t *testing.T) {
		got := indentBlock("a\nb", "    ")
		want := "    a\n    b"
		if got != want {
			t.Errorf("indentBlock = %q, want %q", got, want)
		}
	})
}

func TestDetailHeaderStats(t *testing.T) {
	t.Run("empty message returns empty string", func(t *testing.T) {
		got := detailHeaderStats(message{})
		if got != "" {
			t.Errorf("detailHeaderStats(empty) = %q, want empty", got)
		}
	})

	t.Run("tool calls show icon and count", func(t *testing.T) {
		msg := message{toolCallCount: 3}
		got := detailHeaderStats(msg)
		if !strings.Contains(got, Icon.Tool.Ok.Glyph) {
			t.Errorf("should contain tool icon %q, got %q", Icon.Tool.Ok.Glyph, got)
		}
		if !strings.Contains(got, "3") {
			t.Errorf("should contain count '3', got %q", got)
		}
	})

	t.Run("thinking shows icon and count", func(t *testing.T) {
		msg := message{thinkingCount: 2, toolCallCount: 3}
		got := detailHeaderStats(msg)
		if !strings.Contains(got, Icon.Thinking.Glyph) {
			t.Errorf("should contain thinking icon %q, got %q", Icon.Thinking.Glyph, got)
		}
		if !strings.Contains(got, "2") {
			t.Errorf("should contain count '2', got %q", got)
		}
	})

	t.Run("messages show icon and count", func(t *testing.T) {
		msg := message{outputCount: 1}
		got := detailHeaderStats(msg)
		if !strings.Contains(got, Icon.Output.Glyph) {
			t.Errorf("should contain output icon %q, got %q", Icon.Output.Glyph, got)
		}
		if !strings.Contains(got, "1") {
			t.Errorf("should contain count '1', got %q", got)
		}
	})
}

func TestDetailHeaderMeta(t *testing.T) {
	t.Run("all fields present", func(t *testing.T) {
		msg := message{
			tokensRaw:  1500,
			durationMs: 3500,
			timestamp:  "10:00:00 AM",
		}
		got := detailHeaderMeta(msg)
		if !strings.Contains(got, "1.5k") {
			t.Errorf("should contain token count '1.5k', got %q", got)
		}
		if !strings.Contains(got, "3.5s") {
			t.Errorf("should contain duration '3.5s', got %q", got)
		}
		if !strings.Contains(got, "10:00:00 AM") {
			t.Errorf("should contain timestamp '10:00:00 AM', got %q", got)
		}
	})

	t.Run("zero tokens omitted", func(t *testing.T) {
		msg := message{tokensRaw: 0, durationMs: 1000, timestamp: "10:00:00 AM"}
		got := detailHeaderMeta(msg)
		// Should have duration and timestamp but no token part
		if !strings.Contains(got, "1.0s") {
			t.Errorf("should contain duration, got %q", got)
		}
	})

	t.Run("zero duration omitted", func(t *testing.T) {
		msg := message{tokensRaw: 500, durationMs: 0, timestamp: "10:00:00 AM"}
		got := detailHeaderMeta(msg)
		if !strings.Contains(got, "500") {
			t.Errorf("should contain token count, got %q", got)
		}
	})

	t.Run("all zero values — empty string", func(t *testing.T) {
		got := detailHeaderMeta(message{})
		if got != "" {
			t.Errorf("detailHeaderMeta(empty) = %q, want empty", got)
		}
	})
}

// debugAllLines is the pre-optimization reference: render every entry, then
// slice. Used to verify debugVisibleLines matches it for any window.
func debugAllLines(m model, width int) []string {
	var lines []string
	for i, entry := range m.debugFiltered {
		lines = append(lines, m.renderDebugEntry(entry, i, i == m.debugCursor, width))
		if m.debugExpanded[i] && entry.HasExtra() {
			for _, el := range strings.Split(entry.Extra, "\n") {
				lines = append(lines, "  "+StyleDim.Render(el))
			}
		}
	}
	return lines
}

func TestDebugVisibleLines(t *testing.T) {
	entries := make([]debuglog.DebugEntry, 8)
	for i := range entries {
		entries[i] = debuglog.DebugEntry{
			Timestamp: time.Date(2026, 7, 4, 12, 0, i, 0, time.UTC),
			Level:     debuglog.LevelDebug,
			Message:   fmt.Sprintf("entry %d", i),
			LineNum:   i + 1,
			Count:     1,
		}
	}
	// Entry 2 has three continuation lines and is expanded; entry 5 has
	// extras but stays collapsed (must still count as one line).
	entries[2].Extra = "extra a\nextra b\nextra c"
	entries[5].Extra = "hidden extra"

	m := model{
		width:         100,
		height:        24,
		debugFiltered: entries,
		debugExpanded: map[int]bool{2: true},
		debugCursor:   4,
	}
	width := 100
	all := debugAllLines(m, width)
	if len(all) != 11 { // 8 headers + 3 expanded extras
		t.Fatalf("reference render = %d lines, want 11", len(all))
	}

	for _, tt := range []struct {
		name               string
		scroll, viewHeight int
	}{
		{"full window from top", 0, 11},
		{"window taller than content", 0, 50},
		{"top of log", 0, 5},
		{"window starts inside expanded extras", 4, 4},
		{"window ends inside expanded extras", 2, 2},
		{"bottom of log", 6, 5},
		{"single line window", 3, 1},
		{"zero-height window", 3, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			end := min(tt.scroll+tt.viewHeight, len(all))
			want := all[min(tt.scroll, len(all)):end]
			got := m.debugVisibleLines(tt.scroll, tt.viewHeight, width)
			if len(got) != len(want) {
				t.Fatalf("got %d lines, want %d", len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], want[i])
				}
			}
		})
	}
}

// --- TestInfoBarHeightMatchesRender -------------------------------------------

func TestInfoBarHeightMatchesRender(t *testing.T) {
	initZone.Do(zone.NewGlobal)

	t.Run("flash collapses colored-mode bar to one line", func(t *testing.T) {
		m := testModel()
		m.sessionMode = "plan"

		if h := m.infoBarHeight(); h != 3 {
			t.Fatalf("infoBarHeight = %d, want 3 for plan mode", h)
		}
		m.flashStatus = "Copied: /tmp/x.jsonl"
		if h := m.infoBarHeight(); h != 1 {
			t.Errorf("infoBarHeight = %d, want 1 while flash is active", h)
		}
	})

	t.Run("reported height matches rendered height", func(t *testing.T) {
		for _, mode := range []string{"", "default", "plan", "acceptEdits", "bypassPermissions"} {
			for _, flash := range []string{"", "Copied: /tmp/x.jsonl"} {
				m := testModel()
				m.sessionMode = mode
				m.flashStatus = flash
				if got, want := lipgloss.Height(m.renderInfoBar()), m.infoBarHeight(); got != want {
					t.Errorf("mode=%q flash=%q: rendered height %d != infoBarHeight %d", mode, flash, got, want)
				}
			}
		}
	})
}

func TestHasExpandedContent(t *testing.T) {
	cases := []struct {
		name string
		item displayItem
		want bool
	}{
		{"thinking with text", displayItem{itemType: transcript.ItemThinking, text: "let me think"}, true},
		{"thinking empty", displayItem{itemType: transcript.ItemThinking, text: "  \n"}, false},
		{"output with text", displayItem{itemType: transcript.ItemOutput, text: "done"}, true},
		{"output empty", displayItem{itemType: transcript.ItemOutput}, false},
		{"tool call with input", displayItem{itemType: transcript.ItemToolCall, toolInput: `{"file":"x"}`}, true},
		{"tool call errored", displayItem{itemType: transcript.ItemToolCall, toolError: true}, true},
		{"tool call bare", displayItem{itemType: transcript.ItemToolCall, toolName: "Read"}, false},
		{"subagent linked", displayItem{itemType: transcript.ItemSubagent, subagentProcess: &agents.SubagentProcess{}}, true},
		{"subagent bare", displayItem{itemType: transcript.ItemSubagent}, false},
		{"memory load", displayItem{itemType: transcript.ItemMemoryLoad, text: "MEMORY.md"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasExpandedContent(c.item); got != c.want {
				t.Errorf("hasExpandedContent(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}
