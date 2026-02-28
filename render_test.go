package main

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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
		if !strings.Contains(got, IconToolOk.Glyph) {
			t.Errorf("should contain tool icon %q, got %q", IconToolOk.Glyph, got)
		}
		if !strings.Contains(got, "3") {
			t.Errorf("should contain count '3', got %q", got)
		}
	})

	t.Run("thinking shows icon and count", func(t *testing.T) {
		msg := message{thinkingCount: 2, toolCallCount: 3}
		got := detailHeaderStats(msg)
		if !strings.Contains(got, IconThinking.Glyph) {
			t.Errorf("should contain thinking icon %q, got %q", IconThinking.Glyph, got)
		}
		if !strings.Contains(got, "2") {
			t.Errorf("should contain count '2', got %q", got)
		}
	})

	t.Run("messages show icon and count", func(t *testing.T) {
		msg := message{outputCount: 1}
		got := detailHeaderStats(msg)
		if !strings.Contains(got, IconOutput.Glyph) {
			t.Errorf("should contain output icon %q, got %q", IconOutput.Glyph, got)
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
