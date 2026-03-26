package main

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

func TestAssemble(t *testing.T) {
	t.Run("content and footer only", func(t *testing.T) {
		sl := screenLayout{
			lines:   []string{"line1", "line2", "line3"},
			footer:  "footer",
			screenH: 6,
			width:   40,
			cw:      40,
		}
		got := sl.assemble()
		if h := lipgloss.Height(got); h != 6 {
			t.Errorf("height = %d, want 6\n%s", h, got)
		}
	})

	t.Run("with header", func(t *testing.T) {
		sl := screenLayout{
			header:  "=== header ===",
			lines:   []string{"a", "b"},
			footer:  "footer",
			screenH: 6,
			width:   40,
			cw:      40,
		}
		got := sl.assemble()
		if h := lipgloss.Height(got); h != 6 {
			t.Errorf("height = %d, want 6\n%s", h, got)
		}
		if !strings.Contains(got, "=== header ===") {
			t.Error("header missing from output")
		}
	})

	t.Run("with middle", func(t *testing.T) {
		sl := screenLayout{
			lines:   []string{"a", "b"},
			middle:  "-- middle --",
			footer:  "footer",
			screenH: 6,
			width:   40,
			cw:      40,
		}
		got := sl.assemble()
		if h := lipgloss.Height(got); h != 6 {
			t.Errorf("height = %d, want 6\n%s", h, got)
		}
		if !strings.Contains(got, "-- middle --") {
			t.Error("middle missing from output")
		}
	})

	t.Run("all sections", func(t *testing.T) {
		sl := screenLayout{
			header:  "header",
			lines:   []string{"a", "b", "c"},
			middle:  "mid",
			footer:  "foot",
			screenH: 10,
			width:   40,
			cw:      40,
		}
		got := sl.assemble()
		if h := lipgloss.Height(got); h != 10 {
			t.Errorf("height = %d, want 10\n%s", h, got)
		}
	})

	t.Run("overflow truncation", func(t *testing.T) {
		// More lines than can fit: screenH=5, footer=1, so contentH=4,
		// but we provide 10 lines.
		sl := screenLayout{
			lines:   []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"},
			footer:  "f",
			screenH: 5,
			width:   40,
			cw:      40,
		}
		got := sl.assemble()
		if h := lipgloss.Height(got); h != 5 {
			t.Errorf("height = %d, want 5\n%s", h, got)
		}
	})

	t.Run("underflow padding", func(t *testing.T) {
		// Fewer lines than space: screenH=10, footer=1, so contentH=9,
		// but only 2 lines provided.
		sl := screenLayout{
			lines:   []string{"a", "b"},
			footer:  "f",
			screenH: 10,
			width:   40,
			cw:      40,
		}
		got := sl.assemble()
		if h := lipgloss.Height(got); h != 10 {
			t.Errorf("height = %d, want 10\n%s", h, got)
		}
	})

	t.Run("empty header and middle skipped", func(t *testing.T) {
		sl := screenLayout{
			header:  "",
			lines:   []string{"a"},
			middle:  "",
			footer:  "foot",
			screenH: 5,
			width:   40,
			cw:      40,
		}
		got := sl.assemble()
		if h := lipgloss.Height(got); h != 5 {
			t.Errorf("height = %d, want 5\n%s", h, got)
		}
		// Content should get all non-footer lines (4 lines).
		lines := strings.Split(got, "\n")
		// Last line is footer.
		if lines[len(lines)-1] != "foot" {
			t.Errorf("last line = %q, want %q", lines[len(lines)-1], "foot")
		}
	})

	t.Run("multi-line footer", func(t *testing.T) {
		sl := screenLayout{
			lines:   []string{"a", "b"},
			footer:  "foot1\nfoot2\nfoot3",
			screenH: 8,
			width:   40,
			cw:      40,
		}
		got := sl.assemble()
		if h := lipgloss.Height(got); h != 8 {
			t.Errorf("height = %d, want 8\n%s", h, got)
		}
	})

	t.Run("centering applies", func(t *testing.T) {
		sl := screenLayout{
			lines:   []string{"x"},
			footer:  "f",
			screenH: 3,
			width:   20,
			cw:      10,
		}
		got := sl.assemble()
		if h := lipgloss.Height(got); h != 3 {
			t.Errorf("height = %d, want 3\n%s", h, got)
		}
		// Content line "x" should be indented by (20-10)/2 = 5 spaces.
		contentLine := strings.Split(got, "\n")[0]
		if !strings.HasPrefix(contentLine, "     x") {
			t.Errorf("content not centered: %q", contentLine)
		}
	})

	t.Run("zero lines still fills", func(t *testing.T) {
		sl := screenLayout{
			lines:   nil,
			footer:  "f",
			screenH: 5,
			width:   40,
			cw:      40,
		}
		got := sl.assemble()
		if h := lipgloss.Height(got); h != 5 {
			t.Errorf("height = %d, want 5\n%s", h, got)
		}
	})
}
