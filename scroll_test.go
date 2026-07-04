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

		viewH := m.contentHeight(0, 0) // 40 - 3 - 0 = 37
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
		// contentHeight(0, 0) = 10 - 3 - 0 = 7
		// cursor at message 2: lines 12..16, end at line 16
		// viewport shows lines [0..6] → cursor end (16) is beyond
		m.cursor = 2
		m.scroll = 0
		m.ensureCursorVisible()
		// scroll should move so cursorEnd (16) is at the bottom of viewport
		viewH := m.contentHeight(0, 0)                        // 7
		cursorEnd := m.lineOffsets[2] + m.messageLines[2] - 1 // 12 + 5 - 1 = 16
		want := cursorEnd - viewH + 1                         // 16 - 7 + 1 = 10
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
	// footerHeight with showKeybinds=true at width 200: infoBarHeight(1) + keybindBar(3) = 4.
	// Width must be set so renderKeybindBox can measure wrapping correctly.
	// At width 200, all views fit in one content line (border top + content + border bottom = 3).

	t.Run("contentHeight for list (no activity)", func(t *testing.T) {
		m := model{height: 40, width: 200, showKeybinds: true}
		// 40 - footerHeight(4) - 0 = 36
		got := m.contentHeight(0, m.activityIndicatorHeight())
		if got != 36 {
			t.Errorf("contentHeight(0, activity) = %d, want 36", got)
		}
	})

	t.Run("contentHeight for detail (no activity)", func(t *testing.T) {
		m := model{height: 40, width: 200, showKeybinds: true}
		// 40 - 4 - 0 - 0 = 36
		got := m.contentHeight(0, m.activityIndicatorHeight())
		if got != 36 {
			t.Errorf("contentHeight(0, activity) = %d, want 36", got)
		}
	})

	t.Run("contentHeight for picker", func(t *testing.T) {
		m := model{height: 40, width: 200, showKeybinds: true, view: viewPicker}
		// 40 - 4 (footer) - 1 (header) = 35
		got := m.contentHeight(1, 0)
		if got != 35 {
			t.Errorf("contentHeight(1, 0) = %d, want 35", got)
		}
	})

	t.Run("tiny height — contentHeight for list guards against zero/negative", func(t *testing.T) {
		m := model{height: 4, width: 200, showKeybinds: true}
		// 4 - 4 - 0 = 0 → returns 1
		got := m.contentHeight(0, m.activityIndicatorHeight())
		if got != 1 {
			t.Errorf("contentHeight(%d) = %d, want 1 (guard)", m.height, got)
		}
	})

	t.Run("tiny height — contentHeight for detail guards", func(t *testing.T) {
		m := model{height: 3, width: 200, showKeybinds: true}
		// 3 - 4 - 0 = -1 → returns 1
		got := m.contentHeight(0, m.activityIndicatorHeight())
		if got != 1 {
			t.Errorf("contentHeight(%d) = %d, want 1 (guard)", m.height, got)
		}
	})

	t.Run("tiny height — contentHeight for picker guards", func(t *testing.T) {
		m := model{height: 5, width: 200, showKeybinds: true, view: viewPicker}
		// 5 - 4 - 1 = 0 → returns 1
		got := m.contentHeight(1, 0)
		if got != 1 {
			t.Errorf("contentHeight(1, 0) for height=%d = %d, want 1 (guard)", m.height, got)
		}
	})

	t.Run("with activity indicator — list view shrinks by 1", func(t *testing.T) {
		m := model{
			height:         40,
			width:          200,
			showKeybinds:   true,
			watching:       true,
			sessionOngoing: true,
		}
		// activityIndicatorHeight returns 1 when watching && ongoing
		// 40 - 4 - 1 = 35
		got := m.contentHeight(0, m.activityIndicatorHeight())
		if got != 35 {
			t.Errorf("contentHeight with indicator = %d, want 35", got)
		}
	})

	t.Run("keybinds hidden — footer shrinks to info bar only", func(t *testing.T) {
		m := model{height: 40, showKeybinds: false}
		// footerHeight with showKeybinds=false: infoBarHeight(1) = 1
		// 40 - 1 - 0 = 39
		got := m.contentHeight(0, m.activityIndicatorHeight())
		if got != 39 {
			t.Errorf("contentHeight (keybinds hidden) = %d, want 39", got)
		}
	})

	t.Run("narrow terminal wraps keybinds — footer grows", func(t *testing.T) {
		wide := model{height: 40, width: 200, showKeybinds: true}
		narrow := model{height: 40, width: 60, showKeybinds: true}
		wideH := wide.footerHeight()
		narrowH := narrow.footerHeight()
		if narrowH <= wideH {
			t.Errorf("narrow footerHeight (%d) should exceed wide footerHeight (%d) due to wrapping", narrowH, wideH)
		}
		// Narrow terminal should give less viewport space.
		wideContent := wide.contentHeight(0, 0)
		narrowContent := narrow.contentHeight(0, 0)
		if narrowContent >= wideContent {
			t.Errorf("narrow contentHeight (%d) should be less than wide (%d)", narrowContent, wideContent)
		}
	})

	// The next two subtests guard against footerHeight measuring a stale copy
	// of the view's keybind pairs instead of the ones actually rendered.

	t.Run("debug filter text grows measured footer with the rendered bar", func(t *testing.T) {
		base := model{height: 40, width: 50, showKeybinds: true, view: viewDebug}
		filtered := base
		filtered.debugFilterText = strings.Repeat("x", 40)
		if filtered.footerHeight() <= base.footerHeight() {
			t.Errorf("footerHeight with long filter text (%d) should exceed base (%d) — dynamic label must be measured", filtered.footerHeight(), base.footerHeight())
		}
	})

	t.Run("picker footer measurement tracks empty vs populated list", func(t *testing.T) {
		empty := model{height: 40, width: 60, showKeybinds: true, view: viewPicker}
		populated := empty
		populated.pickerItems = []pickerItem{{typ: pickerItemSession}}
		if populated.footerHeight() <= empty.footerHeight() {
			t.Errorf("populated picker footerHeight (%d) should exceed empty-state footerHeight (%d) at narrow width", populated.footerHeight(), empty.footerHeight())
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

// --- moveListCursor ---------------------------------------------------------

func TestMoveListCursor(t *testing.T) {
	t.Run("selective re-render matches a full relayout", func(t *testing.T) {
		m := testModel()
		m.moveListCursor(1)

		want := testModel()
		want.cursor = 1
		want.layoutList()

		if m.cursor != 1 {
			t.Fatalf("cursor = %d, want 1", m.cursor)
		}
		for i := range want.listParts {
			if m.listParts[i] != want.listParts[i] {
				t.Errorf("message %d: selective re-render differs from full layoutList", i)
			}
		}
		for i := range want.lineOffsets {
			if m.lineOffsets[i] != want.lineOffsets[i] {
				t.Errorf("message %d: lineOffsets = %d, want %d", i, m.lineOffsets[i], want.lineOffsets[i])
			}
		}
		if m.totalRenderedLines != want.totalRenderedLines {
			t.Errorf("totalRenderedLines = %d, want %d", m.totalRenderedLines, want.totalRenderedLines)
		}
	})

	t.Run("out-of-range target is a no-op", func(t *testing.T) {
		m := testModel()
		m.moveListCursor(len(m.messages))
		if m.cursor != 0 {
			t.Errorf("cursor = %d, want 0 (unchanged)", m.cursor)
		}
		m.moveListCursor(-1)
		if m.cursor != 0 {
			t.Errorf("cursor = %d, want 0 (unchanged)", m.cursor)
		}
	})

	t.Run("missing layout caches fall back to full relayout", func(t *testing.T) {
		m := testModel()
		m.listParts = nil // simulate a model whose layout never ran
		m.moveListCursor(1)
		if len(m.listParts) != len(m.messages) {
			t.Errorf("listParts length = %d, want %d (full relayout)", len(m.listParts), len(m.messages))
		}
	})

	t.Run("stale layout falls back to full relayout", func(t *testing.T) {
		m := testModel()
		// Same-length content change while another view was active: cache
		// lengths still match, so only the stale flag reveals the drift.
		m.messages[2].content = "changed while in detail view"
		m.listLayoutStale = true
		m.moveListCursor(1)

		want := testModel()
		want.messages[2].content = "changed while in detail view"
		want.cursor = 1
		want.layoutList()
		if m.listParts[2] != want.listParts[2] {
			t.Error("cursor move on a stale layout kept the stale render for an unselected row")
		}
		if m.listLayoutStale {
			t.Error("listLayoutStale = true, want false after relayout")
		}
	})
}

// --- detail render cache -----------------------------------------------------

func TestDetailRenderCache(t *testing.T) {
	newDetail := func(items []displayItem) model {
		m := initialModel([]message{claudeMsg(func(msg *message) {
			msg.items = items
		})}, true)
		m.width = 120
		m.height = 40
		m.view = viewDetail
		return m
	}
	plainItems := []displayItem{
		{itemType: parser.ItemThinking, text: "thinking hard"},
		{itemType: parser.ItemToolCall, toolName: "Read", toolSummary: "file.go"},
	}

	t.Run("computeDetailMaxScroll memoizes the render", func(t *testing.T) {
		m := newDetail(plainItems)
		m.computeDetailMaxScroll()
		if !m.detailCacheValid {
			t.Fatal("detailCacheValid = false, want true after computeDetailMaxScroll")
		}
		want := m.renderDetailContent(m.currentDetailMsg(), m.clampWidth())
		if m.detailCache.content != want.content {
			t.Error("cached content differs from a fresh renderDetailContent")
		}
	})

	t.Run("matching key serves the cache", func(t *testing.T) {
		m := newDetail(plainItems)
		m.computeDetailMaxScroll()
		msg := m.currentDetailMsg()
		if got := m.detailContent(msg, m.clampWidth()); got != m.detailCache {
			t.Error("detailContent did not serve the memoized render on a key match")
		}
	})

	t.Run("cursor jump without recompute is not served stale", func(t *testing.T) {
		m := newDetail(plainItems)
		m.computeDetailMaxScroll() // cache rendered with detailCursor = 0
		m.detailCursor = 1         // "g"-style jump that skips the recompute

		msg := m.currentDetailMsg()
		got := m.detailContent(msg, m.clampWidth())
		want := m.renderDetailContent(msg, m.clampWidth())
		if got != want {
			t.Error("detailContent served the stale cursor-0 cache after a cursor jump")
		}
	})

	t.Run("spinner frame advance is not served stale", func(t *testing.T) {
		m := newDetail([]displayItem{
			{itemType: parser.ItemSubagent, subagentType: "Explore", subagentOngoing: true},
		})
		m.computeDetailMaxScroll()
		m.animFrame++ // animation tick between recomputes

		msg := m.currentDetailMsg()
		got := m.detailContent(msg, m.clampWidth())
		want := m.renderDetailContent(msg, m.clampWidth())
		if got != want {
			t.Error("detailContent served a stale spinner frame from the cache")
		}
	})
}

// --- contentHeight -----------------------------------------------------------

func TestContentHeight(t *testing.T) {
	t.Run("no header no middle", func(t *testing.T) {
		m := model{height: 40, width: 200}
		// footerHeight = infoBarHeight(1) = 1; 40 - 1 - 0 - 0 = 39
		got := m.contentHeight(0, 0)
		if got != 39 {
			t.Errorf("contentHeight(0,0) = %d, want 39", got)
		}
	})

	t.Run("with header", func(t *testing.T) {
		m := model{height: 40, width: 200}
		// 40 - 1 - 2 - 0 = 37
		got := m.contentHeight(2, 0)
		if got != 37 {
			t.Errorf("contentHeight(2,0) = %d, want 37", got)
		}
	})

	t.Run("with middle", func(t *testing.T) {
		m := model{height: 40, width: 200}
		// 40 - 1 - 0 - 1 = 38
		got := m.contentHeight(0, 1)
		if got != 38 {
			t.Errorf("contentHeight(0,1) = %d, want 38", got)
		}
	})

	t.Run("with keybinds shown", func(t *testing.T) {
		m := model{height: 40, width: 200, showKeybinds: true}
		// footerHeight = 1 + 3 = 4; 40 - 4 - 0 - 0 = 36
		got := m.contentHeight(0, 0)
		if got != 36 {
			t.Errorf("contentHeight(0,0) with keybinds = %d, want 36", got)
		}
	})

	t.Run("guards against zero", func(t *testing.T) {
		m := model{height: 3, width: 200, showKeybinds: true}
		// footerHeight = 4; 3 - 4 - 0 - 0 = -1 → 1
		got := m.contentHeight(0, 0)
		if got != 1 {
			t.Errorf("contentHeight(0,0) tiny = %d, want 1 (guard)", got)
		}
	})
}
