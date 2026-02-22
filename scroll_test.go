package main

import (
	"testing"
)

// scrollModel builds a model with pre-populated scroll state so tests don't
// need to trigger rendering. Enough messages to have meaningful offsets.
func scrollModel(totalLines, viewportH int) model {
	m := model{
		width:               120,
		height:              viewportH,
		totalRenderedLines:  totalLines,
		messages:            make([]message, 3),
		expanded:            make(map[int]bool),
		detailExpanded:      make(map[int]bool),
		detailChildExpanded: make(map[visibleRowKey]bool),
		md:                  newMdRenderer(true),
	}
	// Three 5-line messages at lines 0, 6, 12 (5 lines + 1 separator each).
	m.lineOffsets = []int{0, 6, 12}
	m.messageLines = []int{5, 5, 5}
	return m
}

// --- clampListScroll -------------------------------------------------------

func TestClampListScroll(t *testing.T) {
	t.Run("scroll exceeds content — clamped to max", func(t *testing.T) {
		m := scrollModel(100, 40)
		m.scroll = 200
		m.clampListScroll()

		viewH := m.listViewHeight() // 40 - 3 - 0 - 1 = 36
		want := 100 - viewH
		if m.scroll != want {
			t.Errorf("scroll = %d, want %d (max)", m.scroll, want)
		}
	})

	t.Run("scroll within range — unchanged", func(t *testing.T) {
		m := scrollModel(100, 40)
		m.scroll = 10
		m.clampListScroll()
		if m.scroll != 10 {
			t.Errorf("scroll = %d, want 10 (unchanged)", m.scroll)
		}
	})

	t.Run("negative scroll clamped to 0", func(t *testing.T) {
		m := scrollModel(100, 40)
		m.scroll = -5
		m.clampListScroll()
		if m.scroll != 0 {
			t.Errorf("scroll = %d, want 0", m.scroll)
		}
	})

	t.Run("zero content — scroll stays 0", func(t *testing.T) {
		m := scrollModel(0, 40)
		m.scroll = 5
		m.clampListScroll()
		if m.scroll != 0 {
			t.Errorf("scroll = %d, want 0 for empty content", m.scroll)
		}
	})
}

// --- ensureCursorVisible --------------------------------------------------

func TestEnsureCursorVisible(t *testing.T) {
	t.Run("cursor above viewport — scroll up", func(t *testing.T) {
		// cursor at message 0 (line 0), but scroll is at 10 — cursor is hidden
		m := scrollModel(20, 40)
		m.cursor = 0
		m.scroll = 10 // cursor at line 0 is above the viewport start
		m.ensureCursorVisible()
		if m.scroll != 0 {
			t.Errorf("scroll = %d, want 0 (scrolled to show cursor)", m.scroll)
		}
	})

	t.Run("cursor below viewport — scroll down", func(t *testing.T) {
		// Tiny viewport height so cursor at message 2 (line 12..16) is below
		m := scrollModel(20, 10)
		// listViewHeight = 10 - 3 - 0 - 1 = 6
		// cursor at message 2: lines 12..16, end at line 16
		// viewport shows lines [0..5] → cursor end (16) is beyond
		m.cursor = 2
		m.scroll = 0
		m.ensureCursorVisible()
		// scroll should move so cursorEnd (16) is at the bottom of viewport
		viewH := m.listViewHeight()                           // 6
		cursorEnd := m.lineOffsets[2] + m.messageLines[2] - 1 // 12 + 5 - 1 = 16
		want := cursorEnd - viewH + 1                         // 16 - 6 + 1 = 11
		if m.scroll != want {
			t.Errorf("scroll = %d, want %d (scrolled to show cursor)", m.scroll, want)
		}
	})

	t.Run("cursor within viewport — no change", func(t *testing.T) {
		m := scrollModel(20, 40)
		m.cursor = 1 // message 1 at line 6..10, well within height=40 viewport
		m.scroll = 0
		m.ensureCursorVisible()
		if m.scroll != 0 {
			t.Errorf("scroll = %d, want 0 (cursor already visible)", m.scroll)
		}
	})
}

// --- view height methods --------------------------------------------------

func TestViewHeights(t *testing.T) {
	t.Run("listViewHeight normal", func(t *testing.T) {
		m := model{height: 40}
		// 40 - statusBarHeight(3) - activityIndicatorHeight(0) - 1 = 36
		got := m.listViewHeight()
		if got != 36 {
			t.Errorf("listViewHeight = %d, want 36", got)
		}
	})

	t.Run("detailViewHeight normal", func(t *testing.T) {
		m := model{height: 40}
		// 40 - 3 - 0 = 37
		got := m.detailViewHeight()
		if got != 37 {
			t.Errorf("detailViewHeight = %d, want 37", got)
		}
	})

	t.Run("pickerViewHeight normal", func(t *testing.T) {
		m := model{height: 40}
		// 40 - 2 - 3 = 35
		got := m.pickerViewHeight()
		if got != 35 {
			t.Errorf("pickerViewHeight = %d, want 35", got)
		}
	})

	t.Run("tiny height — listViewHeight guards against zero/negative", func(t *testing.T) {
		m := model{height: 3} // exactly statusBarHeight
		// 3 - 3 - 0 - 1 = -1 → returns 1
		got := m.listViewHeight()
		if got != 1 {
			t.Errorf("listViewHeight(%d) = %d, want 1 (guard)", m.height, got)
		}
	})

	t.Run("tiny height — detailViewHeight guards", func(t *testing.T) {
		m := model{height: 2}
		// 2 - 3 = -1 → returns 1
		got := m.detailViewHeight()
		if got != 1 {
			t.Errorf("detailViewHeight(%d) = %d, want 1 (guard)", m.height, got)
		}
	})

	t.Run("tiny height — pickerViewHeight guards", func(t *testing.T) {
		m := model{height: 4}
		// 4 - 2 - 3 = -1 → returns 1
		got := m.pickerViewHeight()
		if got != 1 {
			t.Errorf("pickerViewHeight(%d) = %d, want 1 (guard)", m.height, got)
		}
	})

	t.Run("with activity indicator — list view shrinks by 1", func(t *testing.T) {
		m := model{
			height:         40,
			watching:       true,
			sessionOngoing: true,
		}
		// activityIndicatorHeight returns 1 when watching && ongoing
		// 40 - 3 - 1 - 1 = 35
		got := m.listViewHeight()
		if got != 35 {
			t.Errorf("listViewHeight with indicator = %d, want 35", got)
		}
	})
}
