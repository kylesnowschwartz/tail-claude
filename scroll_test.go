package main

import (
	"strings"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
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
	// footerHeight with showKeybinds=true: infoBarHeight(1) + keybindBarHeight(3) = 4

	t.Run("listViewHeight normal", func(t *testing.T) {
		m := model{height: 40, showKeybinds: true}
		// 40 - footerHeight(4) - activityIndicatorHeight(0) - 1 = 35
		got := m.listViewHeight()
		if got != 35 {
			t.Errorf("listViewHeight = %d, want 35", got)
		}
	})

	t.Run("detailViewHeight normal", func(t *testing.T) {
		m := model{height: 40, showKeybinds: true}
		// 40 - 4 - 0 = 36
		got := m.detailViewHeight()
		if got != 36 {
			t.Errorf("detailViewHeight = %d, want 36", got)
		}
	})

	t.Run("pickerViewHeight normal", func(t *testing.T) {
		m := model{height: 40, showKeybinds: true}
		// 40 - 2 - 4 = 34
		got := m.pickerViewHeight()
		if got != 34 {
			t.Errorf("pickerViewHeight = %d, want 34", got)
		}
	})

	t.Run("tiny height — listViewHeight guards against zero/negative", func(t *testing.T) {
		m := model{height: 4, showKeybinds: true} // exactly footerHeight
		// 4 - 4 - 0 - 1 = -1 → returns 1
		got := m.listViewHeight()
		if got != 1 {
			t.Errorf("listViewHeight(%d) = %d, want 1 (guard)", m.height, got)
		}
	})

	t.Run("tiny height — detailViewHeight guards", func(t *testing.T) {
		m := model{height: 3, showKeybinds: true}
		// 3 - 4 = -1 → returns 1
		got := m.detailViewHeight()
		if got != 1 {
			t.Errorf("detailViewHeight(%d) = %d, want 1 (guard)", m.height, got)
		}
	})

	t.Run("tiny height — pickerViewHeight guards", func(t *testing.T) {
		m := model{height: 5, showKeybinds: true}
		// 5 - 2 - 4 = -1 → returns 1
		got := m.pickerViewHeight()
		if got != 1 {
			t.Errorf("pickerViewHeight(%d) = %d, want 1 (guard)", m.height, got)
		}
	})

	t.Run("with activity indicator — list view shrinks by 1", func(t *testing.T) {
		m := model{
			height:         40,
			showKeybinds:   true,
			watching:       true,
			sessionOngoing: true,
		}
		// activityIndicatorHeight returns 1 when watching && ongoing
		// 40 - 4 - 1 - 1 = 34
		got := m.listViewHeight()
		if got != 34 {
			t.Errorf("listViewHeight with indicator = %d, want 34", got)
		}
	})

	t.Run("keybinds hidden — footer shrinks to info bar only", func(t *testing.T) {
		m := model{height: 40, showKeybinds: false}
		// footerHeight with showKeybinds=false: infoBarHeight(1) = 1
		// 40 - 1 - 0 - 1 = 38
		got := m.listViewHeight()
		if got != 38 {
			t.Errorf("listViewHeight (keybinds hidden) = %d, want 38", got)
		}
	})
}

// --- layoutList / viewList agreement --------------------------------------

func TestLayoutListAgreement(t *testing.T) {
	t.Run("listParts line counts match lineOffsets and messageLines", func(t *testing.T) {
		msgs := []message{
			{role: RoleUser, content: "Hello", timestamp: "10:00:00 AM"},
			{
				role: RoleClaude, model: "opus4.6", content: "Response here",
				thinkingCount: 1, toolCallCount: 2, timestamp: "10:00:01 AM",
				items: []displayItem{
					{itemType: parser.ItemThinking, text: "thinking"},
					{itemType: parser.ItemToolCall, toolName: "Read", toolSummary: "file.go"},
				},
			},
			{role: RoleSystem, content: "system note", timestamp: "10:00:02 AM"},
			{role: RoleCompact, content: "--- context window ---"},
		}
		m := initialModel(msgs, true)
		m.width = 120
		m.height = 40
		// Expand the Claude message so it renders items.
		m.expanded[1] = true
		m.layoutList()

		if len(m.listParts) != len(msgs) {
			t.Fatalf("listParts length = %d, want %d", len(m.listParts), len(msgs))
		}

		// Join listParts the same way viewList does and verify totals.
		joined := strings.Join(m.listParts, "\n")
		totalLines := strings.Count(joined, "\n") + 1

		// Each part's line count should match messageLines.
		for i, part := range m.listParts {
			got := strings.Count(part, "\n") + 1
			if got != m.messageLines[i] {
				t.Errorf("message %d: listParts lines = %d, messageLines = %d", i, got, m.messageLines[i])
			}
		}

		// lineOffsets should be cumulative.
		wantOffset := 0
		for i := range msgs {
			if m.lineOffsets[i] != wantOffset {
				t.Errorf("message %d: lineOffsets = %d, want %d", i, m.lineOffsets[i], wantOffset)
			}
			wantOffset += m.messageLines[i]
		}

		if m.totalRenderedLines != totalLines {
			t.Errorf("totalRenderedLines = %d, joined line count = %d", m.totalRenderedLines, totalLines)
		}
	})
}
