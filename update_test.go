package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kylesnowschwartz/tail-claude/parser"
)

// asModel extracts the model from an Update return value.
// Panics when the type assertion fails — a test bug, not a production bug.
func asModel(t tea.Model) model {
	return t.(model)
}

// isQuit returns true when cmd is the Quit command.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// --- TestUpdateList --------------------------------------------------------

func TestUpdateList(t *testing.T) {
	t.Run("j increments cursor", func(t *testing.T) {
		m := testModel()
		m.cursor = 0
		result, cmd := m.updateList(key("j"))
		got := asModel(result)
		if got.cursor != 1 {
			t.Errorf("cursor = %d, want 1", got.cursor)
		}
		if cmd != nil {
			t.Errorf("j should not emit a command, got %T", cmd)
		}
	})

	t.Run("j does not exceed last message", func(t *testing.T) {
		m := testModel()
		m.cursor = len(m.messages) - 1 // already at end
		result, _ := m.updateList(key("j"))
		got := asModel(result)
		if got.cursor != len(m.messages)-1 {
			t.Errorf("cursor = %d, want %d (clamped)", got.cursor, len(m.messages)-1)
		}
	})

	t.Run("down key scrolls viewport", func(t *testing.T) {
		m := testModel()
		m.totalRenderedLines = 100
		m.scroll = 0
		result, _ := m.updateList(key("down"))
		got := asModel(result)
		if got.scroll != 3 {
			t.Errorf("scroll = %d, want 3 (scrolled by 3)", got.scroll)
		}
		// cursor must not change
		if got.cursor != 0 {
			t.Errorf("cursor = %d, want 0 (unchanged)", got.cursor)
		}
	})

	t.Run("k decrements cursor", func(t *testing.T) {
		m := testModel()
		m.cursor = 2
		result, _ := m.updateList(key("k"))
		got := asModel(result)
		if got.cursor != 1 {
			t.Errorf("cursor = %d, want 1", got.cursor)
		}
	})

	t.Run("k does not go below 0", func(t *testing.T) {
		m := testModel()
		m.cursor = 0
		result, _ := m.updateList(key("k"))
		got := asModel(result)
		if got.cursor != 0 {
			t.Errorf("cursor = %d, want 0 (clamped)", got.cursor)
		}
	})

	t.Run("up key scrolls viewport", func(t *testing.T) {
		m := testModel()
		m.scroll = 9
		result, _ := m.updateList(key("up"))
		got := asModel(result)
		if got.scroll != 6 {
			t.Errorf("scroll = %d, want 6 (scrolled by 3)", got.scroll)
		}
		// cursor must not change
		if got.cursor != 0 {
			t.Errorf("cursor = %d, want 0 (unchanged)", got.cursor)
		}
	})

	t.Run("G jumps to last message", func(t *testing.T) {
		m := testModel()
		m.cursor = 0
		result, _ := m.updateList(key("G"))
		got := asModel(result)
		want := len(m.messages) - 1
		if got.cursor != want {
			t.Errorf("cursor = %d, want %d (last)", got.cursor, want)
		}
	})

	t.Run("g jumps to first message and resets scroll", func(t *testing.T) {
		m := testModel()
		m.cursor = 2
		m.scroll = 50
		result, _ := m.updateList(key("g"))
		got := asModel(result)
		if got.cursor != 0 {
			t.Errorf("cursor = %d, want 0", got.cursor)
		}
		if got.scroll != 0 {
			t.Errorf("scroll = %d, want 0 (reset)", got.scroll)
		}
	})

	t.Run("tab toggles expanded for Claude message", func(t *testing.T) {
		m := testModel()
		m.cursor = 1 // claude message (index 1 in testModel)
		if m.messages[1].role != RoleClaude {
			t.Fatalf("test setup: expected claude msg at index 1, got %q", m.messages[1].role)
		}

		result, _ := m.updateList(key("tab"))
		got := asModel(result)
		if !got.expanded[1] {
			t.Errorf("claude message should be expanded after tab, expanded[1] = %v", got.expanded[1])
		}

		// Tab again collapses
		result2, _ := got.updateList(key("tab"))
		got2 := asModel(result2)
		if got2.expanded[1] {
			t.Errorf("claude message should be collapsed after second tab, expanded[1] = %v", got2.expanded[1])
		}
	})

	t.Run("tab toggles expanded for User message", func(t *testing.T) {
		m := testModel()
		m.cursor = 0 // user message (index 0)
		result, _ := m.updateList(key("tab"))
		got := asModel(result)
		if !got.expanded[0] {
			t.Errorf("user message should be expanded after tab")
		}
	})

	t.Run("tab is no-op for System message", func(t *testing.T) {
		m := testModel()
		m.cursor = 2 // system message
		if m.messages[2].role != RoleSystem {
			t.Fatalf("test setup: expected system msg at index 2, got %q", m.messages[2].role)
		}
		result, _ := m.updateList(key("tab"))
		got := asModel(result)
		if got.expanded[2] {
			t.Errorf("system message should not be expandable, expanded[2] = %v", got.expanded[2])
		}
	})

	t.Run("enter switches to viewDetail", func(t *testing.T) {
		m := testModel()
		m.cursor = 0
		result, _ := m.updateList(key("enter"))
		got := asModel(result)
		if got.view != viewDetail {
			t.Errorf("view = %v, want viewDetail", got.view)
		}
		if got.detailScroll != 0 {
			t.Errorf("detailScroll = %d, want 0 (reset)", got.detailScroll)
		}
		if got.detailCursor != 0 {
			t.Errorf("detailCursor = %d, want 0 (reset)", got.detailCursor)
		}
		if got.traceMsg != nil {
			t.Error("traceMsg should be nil on entering detail")
		}
	})

	t.Run("e expands all Claude messages", func(t *testing.T) {
		m := testModel()
		result, _ := m.updateList(key("e"))
		got := asModel(result)
		for i, msg := range got.messages {
			if msg.role == RoleClaude && !got.expanded[i] {
				t.Errorf("expanded[%d] = false, want true for claude message", i)
			}
		}
	})

	t.Run("c collapses all Claude messages", func(t *testing.T) {
		m := testModel()
		// First expand everything
		for i := range m.messages {
			m.expanded[i] = true
		}
		result, _ := m.updateList(key("c"))
		got := asModel(result)
		for i, msg := range got.messages {
			if msg.role == RoleClaude && got.expanded[i] {
				t.Errorf("expanded[%d] = true, want false after c", i)
			}
		}
	})

	t.Run("ctrl+c returns Quit", func(t *testing.T) {
		m := testModel()
		_, cmd := m.updateList(key("ctrl+c"))
		if !isQuit(cmd) {
			t.Errorf("ctrl+c should return Quit command, got %T", cmd)
		}
	})
}

// --- TestUpdateDetail ------------------------------------------------------

func TestUpdateDetail(t *testing.T) {
	// claudeMsgWithItems builds a claude message that has detail items,
	// needed for the item-navigation branches.
	claudeMsgWithItems := func() message {
		return claudeMsg(func(m *message) {
			m.items = []displayItem{
				{itemType: parser.ItemThinking, text: "let me think"},
				{itemType: parser.ItemToolCall, toolName: "Read", toolSummary: "main.go"},
				{itemType: parser.ItemOutput, text: "done"},
			}
		})
	}

	t.Run("q returns to list view", func(t *testing.T) {
		m := testModel()
		m.view = viewDetail
		result, cmd := m.updateDetail(key("q"))
		got := asModel(result)
		if got.view != viewList {
			t.Errorf("view = %v, want viewList", got.view)
		}
		if cmd != nil {
			t.Errorf("q should not emit a command")
		}
	})

	t.Run("esc returns to list view", func(t *testing.T) {
		m := testModel()
		m.view = viewDetail
		result, _ := m.updateDetail(key("esc"))
		got := asModel(result)
		if got.view != viewList {
			t.Errorf("view = %v, want viewList", got.view)
		}
	})

	t.Run("esc pops trace stack instead of going to list", func(t *testing.T) {
		m := testModel()
		m.view = viewDetail
		trace := &message{role: RoleClaude, subagentLabel: "Explore"}
		saved := &savedDetailState{cursor: 2, scroll: 0, expanded: map[int]bool{1: true}}
		m.traceMsg = trace
		m.savedDetail = saved

		result, _ := m.updateDetail(key("esc"))
		got := asModel(result)

		// Should restore parent state, not go to list view.
		if got.view == viewList {
			t.Error("esc with trace should pop stack, not go to list")
		}
		if got.traceMsg != nil {
			t.Error("traceMsg should be cleared after pop")
		}
		if got.savedDetail != nil {
			t.Error("savedDetail should be cleared after pop")
		}
		if got.detailCursor != 2 {
			t.Errorf("detailCursor = %d, want 2 (restored)", got.detailCursor)
		}
		// The restored expanded map should include the saved state.
		if !got.detailExpanded[1] {
			t.Errorf("detailExpanded[1] = false, want true (restored from saved)")
		}
	})

	t.Run("j moves detail cursor when items exist", func(t *testing.T) {
		m := testModel()
		m.messages = []message{claudeMsgWithItems()}
		m.expanded = make(map[int]bool)
		m.cursor = 0
		m.view = viewDetail
		m.detailCursor = 0
		m.computeLineOffsets()

		result, _ := m.updateDetail(key("j"))
		got := asModel(result)
		if got.detailCursor != 1 {
			t.Errorf("detailCursor = %d, want 1", got.detailCursor)
		}
	})

	t.Run("j does not exceed last item", func(t *testing.T) {
		m := testModel()
		m.messages = []message{claudeMsgWithItems()}
		m.expanded = make(map[int]bool)
		m.cursor = 0
		m.view = viewDetail
		m.detailCursor = 2 // at last item (3 items: 0,1,2)
		m.computeLineOffsets()

		result, _ := m.updateDetail(key("j"))
		got := asModel(result)
		if got.detailCursor != 2 {
			t.Errorf("detailCursor = %d, want 2 (clamped at last item)", got.detailCursor)
		}
	})

	t.Run("j scrolls when no items", func(t *testing.T) {
		m := testModel()
		m.cursor = 0 // user message — no items
		m.view = viewDetail
		m.detailScroll = 0
		// Set maxScroll > 0 so the global clamp doesn't zero out the increment.
		m.detailMaxScroll = 20

		result, _ := m.updateDetail(key("j"))
		got := asModel(result)
		if got.detailScroll != 1 {
			t.Errorf("detailScroll = %d, want 1 (scrolled)", got.detailScroll)
		}
	})

	t.Run("k moves detail cursor up when items exist", func(t *testing.T) {
		m := testModel()
		m.messages = []message{claudeMsgWithItems()}
		m.expanded = make(map[int]bool)
		m.cursor = 0
		m.view = viewDetail
		m.detailCursor = 2
		m.computeLineOffsets()

		result, _ := m.updateDetail(key("k"))
		got := asModel(result)
		if got.detailCursor != 1 {
			t.Errorf("detailCursor = %d, want 1", got.detailCursor)
		}
	})

	t.Run("k does not go below 0", func(t *testing.T) {
		m := testModel()
		m.messages = []message{claudeMsgWithItems()}
		m.expanded = make(map[int]bool)
		m.cursor = 0
		m.view = viewDetail
		m.detailCursor = 0
		m.computeLineOffsets()

		result, _ := m.updateDetail(key("k"))
		got := asModel(result)
		if got.detailCursor != 0 {
			t.Errorf("detailCursor = %d, want 0 (clamped)", got.detailCursor)
		}
	})

	t.Run("G jumps to end of items", func(t *testing.T) {
		m := testModel()
		m.messages = []message{claudeMsgWithItems()}
		m.expanded = make(map[int]bool)
		m.cursor = 0
		m.view = viewDetail
		m.detailCursor = 0
		m.computeLineOffsets()

		result, _ := m.updateDetail(key("G"))
		got := asModel(result)
		if got.detailCursor != 2 {
			t.Errorf("detailCursor = %d, want 2 (last item)", got.detailCursor)
		}
	})

	t.Run("g jumps to start", func(t *testing.T) {
		m := testModel()
		m.messages = []message{claudeMsgWithItems()}
		m.expanded = make(map[int]bool)
		m.cursor = 0
		m.view = viewDetail
		m.detailCursor = 2
		m.detailScroll = 10
		m.computeLineOffsets()

		result, _ := m.updateDetail(key("g"))
		got := asModel(result)
		if got.detailCursor != 0 {
			t.Errorf("detailCursor = %d, want 0", got.detailCursor)
		}
		if got.detailScroll != 0 {
			t.Errorf("detailScroll = %d, want 0 (reset)", got.detailScroll)
		}
	})

	t.Run("tab toggles item expansion", func(t *testing.T) {
		m := testModel()
		m.messages = []message{claudeMsgWithItems()}
		m.expanded = make(map[int]bool)
		m.cursor = 0
		m.view = viewDetail
		m.detailCursor = 1
		m.width = 120
		m.height = 40
		m.computeLineOffsets()

		result, _ := m.updateDetail(key("tab"))
		got := asModel(result)
		if !got.detailExpanded[1] {
			t.Errorf("detailExpanded[1] = false, want true after tab")
		}
	})

	t.Run("ctrl+c returns Quit", func(t *testing.T) {
		m := testModel()
		m.view = viewDetail
		_, cmd := m.updateDetail(key("ctrl+c"))
		if !isQuit(cmd) {
			t.Errorf("ctrl+c should return Quit command, got %T", cmd)
		}
	})
}

// --- TestUpdateListMouse --------------------------------------------------

func TestUpdateListMouse(t *testing.T) {
	t.Run("WheelUp decreases scroll", func(t *testing.T) {
		m := testModel()
		m.scroll = 9
		result, cmd := m.updateListMouse(mouseScroll(tea.MouseButtonWheelUp))
		got := asModel(result)
		if got.scroll != 6 {
			t.Errorf("scroll = %d, want 6 (decreased by 3)", got.scroll)
		}
		if cmd != nil {
			t.Errorf("wheel should not emit a command")
		}
	})

	t.Run("WheelUp clamps at 0", func(t *testing.T) {
		m := testModel()
		m.scroll = 1
		result, _ := m.updateListMouse(mouseScroll(tea.MouseButtonWheelUp))
		got := asModel(result)
		if got.scroll != 0 {
			t.Errorf("scroll = %d, want 0 (clamped)", got.scroll)
		}
	})

	t.Run("WheelUp at 0 stays 0", func(t *testing.T) {
		m := testModel()
		m.scroll = 0
		result, _ := m.updateListMouse(mouseScroll(tea.MouseButtonWheelUp))
		got := asModel(result)
		if got.scroll != 0 {
			t.Errorf("scroll = %d, want 0 (already at top)", got.scroll)
		}
	})

	t.Run("WheelDown increases scroll", func(t *testing.T) {
		m := testModel()
		m.totalRenderedLines = 100
		m.scroll = 0
		result, _ := m.updateListMouse(mouseScroll(tea.MouseButtonWheelDown))
		got := asModel(result)
		if got.scroll != 3 {
			t.Errorf("scroll = %d, want 3 (increased by 3)", got.scroll)
		}
	})

	t.Run("WheelDown clamps at maxScroll", func(t *testing.T) {
		m := testModel()
		m.totalRenderedLines = 10
		m.scroll = 8 // near the end; maxScroll = 10 - listViewHeight
		result, _ := m.updateListMouse(mouseScroll(tea.MouseButtonWheelDown))
		got := asModel(result)
		// listViewHeight = 40 - 3 - 0 - 1 = 36; maxScroll = max(0, 10-36) = 0
		if got.scroll != 0 {
			t.Errorf("scroll = %d, want 0 (clamped at max)", got.scroll)
		}
	})
}

// --- TestUpdateDetailMouse ------------------------------------------------

func TestUpdateDetailMouse(t *testing.T) {
	t.Run("WheelUp decreases detailScroll", func(t *testing.T) {
		m := testModel()
		m.view = viewDetail
		m.detailScroll = 9
		m.detailMaxScroll = 20
		result, cmd := m.updateDetailMouse(mouseScroll(tea.MouseButtonWheelUp))
		got := asModel(result)
		if got.detailScroll != 6 {
			t.Errorf("detailScroll = %d, want 6 (decreased by 3)", got.detailScroll)
		}
		if cmd != nil {
			t.Errorf("wheel should not emit a command")
		}
	})

	t.Run("WheelUp clamps at 0", func(t *testing.T) {
		m := testModel()
		m.view = viewDetail
		m.detailScroll = 1
		m.detailMaxScroll = 20
		result, _ := m.updateDetailMouse(mouseScroll(tea.MouseButtonWheelUp))
		got := asModel(result)
		if got.detailScroll != 0 {
			t.Errorf("detailScroll = %d, want 0 (clamped)", got.detailScroll)
		}
	})

	t.Run("WheelDown increases detailScroll", func(t *testing.T) {
		m := testModel()
		m.view = viewDetail
		m.detailScroll = 0
		m.detailMaxScroll = 20
		result, _ := m.updateDetailMouse(mouseScroll(tea.MouseButtonWheelDown))
		got := asModel(result)
		if got.detailScroll != 3 {
			t.Errorf("detailScroll = %d, want 3 (increased by 3)", got.detailScroll)
		}
	})

	t.Run("WheelDown clamps at detailMaxScroll", func(t *testing.T) {
		m := testModel()
		m.view = viewDetail
		m.detailScroll = 19
		m.detailMaxScroll = 20
		result, _ := m.updateDetailMouse(mouseScroll(tea.MouseButtonWheelDown))
		got := asModel(result)
		if got.detailScroll != 20 {
			t.Errorf("detailScroll = %d, want 20 (clamped at max)", got.detailScroll)
		}
	})
}
