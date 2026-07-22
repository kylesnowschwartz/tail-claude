package main

import (
	"strings"

	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
)

// clampWidth returns m.width capped at maxContentWidth.
func (m model) clampWidth() int {
	if m.width > maxContentWidth {
		return maxContentWidth
	}
	return m.width
}

// dumpHeight returns the screen height at which viewList shows the whole
// message list with no blank padding: exactly the laid-out content plus the
// fixed sections viewList's viewport math subtracts. --dump renders through
// the interactive screen assembler, whose contract is "output is exactly
// screenH lines" — the height must therefore be the content's natural height,
// not an arbitrarily large one. Call after layoutList (it reads
// totalRenderedLines).
func (m model) dumpHeight() int {
	return m.totalRenderedLines + m.footerHeight() + m.activityIndicatorHeight()
}

// layoutList renders every message once, caching both the rendered content
// (listParts) and the line-offset metadata used by scroll math. viewList
// assembles its output from listParts, so layout and view always agree.
func (m *model) layoutList() {
	if m.width == 0 || len(m.messages) == 0 {
		return
	}
	width := m.clampWidth()

	m.listParts = make([]string, len(m.messages))
	m.lineOffsets = make([]int, len(m.messages))
	m.messageLines = make([]int, len(m.messages))
	currentLine := 0
	for i, msg := range m.messages {
		m.lineOffsets[i] = currentLine
		r := m.renderMessage(msg, width, i == m.cursor, m.expanded[i])
		m.listParts[i] = r.content
		m.messageLines[i] = r.lines
		currentLine += r.lines
	}

	if len(m.messages) > 0 {
		last := len(m.messages) - 1
		m.totalRenderedLines = m.lineOffsets[last] + m.messageLines[last]
	} else {
		m.totalRenderedLines = 0
	}
	m.listLayoutStale = false
}

// rerenderListMessage re-renders a single message into the layout caches.
// Selection changes only alter colors, never line counts, so lineOffsets stay
// valid. If the caches are missing, stale (messages changed while another
// view was active), or a re-render unexpectedly changes the line count, fall
// back to a full layoutList so scroll math never reads stale offsets.
func (m *model) rerenderListMessage(i int) {
	if m.listLayoutStale || m.width == 0 || i < 0 || i >= len(m.messages) || len(m.listParts) != len(m.messages) {
		m.layoutList()
		return
	}
	r := m.renderMessage(m.messages[i], m.clampWidth(), i == m.cursor, m.expanded[i])
	if r.lines != m.messageLines[i] {
		m.layoutList()
		return
	}
	m.listParts[i] = r.content
}

// moveListCursor moves the list cursor to target, re-rendering only the two
// messages whose selection state changed. A full layoutList would push every
// message through markdown rendering just to recolor two borders.
func (m *model) moveListCursor(target int) {
	if target < 0 || target >= len(m.messages) || target == m.cursor {
		return
	}
	prev := m.cursor
	m.cursor = target
	m.rerenderListMessage(prev)
	m.rerenderListMessage(target)
}

// listHasAnimatedRows reports whether the list view currently shows a spinner
// (an expanded message with an ongoing subagent item). Spinners are the only
// list content that changes with animFrame, so animation ticks can skip the
// full-list relayout when none are visible.
func (m model) listHasAnimatedRows() bool {
	for i := range m.messages {
		if !m.expanded[i] {
			continue
		}
		for _, item := range m.messages[i].items {
			if item.subagentOngoing {
				return true
			}
		}
	}
	return false
}

// ensureCursorVisible adjusts scroll so the cursor's message is within
// the visible viewport.
func (m *model) ensureCursorVisible() {
	if len(m.lineOffsets) == 0 || m.height == 0 {
		return
	}
	viewHeight := m.contentHeight(0, m.activityIndicatorHeight())

	cursorStart := m.lineOffsets[m.cursor]
	cursorEnd := cursorStart + m.messageLines[m.cursor] - 1

	if cursorStart < m.scroll {
		m.scroll = cursorStart
	}
	if cursorEnd >= m.scroll+viewHeight {
		m.scroll = cursorEnd - viewHeight + 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// scrollWindow returns the slice of lines visible within a viewport,
// clamping scroll to [0, maxScroll] so callers can pass raw scroll state.
func scrollWindow(lines []string, viewHeight, scroll int) []string {
	maxScroll := len(lines) - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	visible := lines[scroll:]
	if len(visible) > viewHeight {
		visible = visible[:viewHeight]
	}
	return visible
}

// clampListScroll caps the list scroll offset so it can't exceed the content.
func (m *model) clampListScroll() {
	maxScroll := m.totalRenderedLines - m.contentHeight(0, m.activityIndicatorHeight())
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// detailRenderKey guards the detail render cache. Every state change that
// alters detail content routes through computeDetailMaxScroll (enter detail,
// resize, tail update, expansion toggle, drill-in/out), which rewrites the
// cache; the key covers the remaining inputs that can drift without such a
// call — cursor jumps ("g") and spinner animation frames.
type detailRenderKey struct {
	width  int
	cursor int
	frame  int // animFrame when the message has an ongoing subagent, else 0
}

// detailAnimFrame returns the animFrame component of the detail cache key:
// the live frame when the message shows an ongoing-subagent spinner (so each
// tick misses the cache and the spinner animates), 0 otherwise.
func (m model) detailAnimFrame(msg message) int {
	for _, item := range msg.items {
		if item.subagentOngoing {
			return m.animFrame
		}
	}
	return 0
}

// currentDetailRenderKey builds the cache key for the current detail state.
func (m model) currentDetailRenderKey(msg message) detailRenderKey {
	return detailRenderKey{
		width:  m.clampWidth(),
		cursor: m.detailCursor,
		frame:  m.detailAnimFrame(msg),
	}
}

// detailContent returns the detail render for viewDetail, reusing the render
// memoized by computeDetailMaxScroll. A key mismatch (cursor jumped without a
// recompute, or a spinner frame advanced) falls back to a fresh render —
// View's value receiver can't store the result here.
func (m model) detailContent(msg message, width int) rendered {
	if m.detailCacheValid && m.detailCacheKey == m.currentDetailRenderKey(msg) {
		return m.detailCache
	}
	return m.renderDetailContent(msg, width)
}

// computeDetailMaxScroll caches the maximum scroll offset for the detail view.
// Called when entering detail view and on window resize. Also memoizes the
// rendered content so viewDetail doesn't repeat the full render — without the
// cache every keypress renders the entire detail content 2-3 times.
func (m *model) computeDetailMaxScroll() {
	if m.width == 0 || m.height == 0 {
		m.detailMaxScroll = 0
		m.detailCacheValid = false
		return
	}

	msg := m.currentDetailMsg()
	width := m.clampWidth()

	r := m.renderDetailContent(msg, width)
	m.detailCache = r
	m.detailCacheKey = m.currentDetailRenderKey(msg)
	m.detailCacheValid = true
	// Trim trailing newlines that lipgloss may add (phantom blank lines).
	trimmed := strings.TrimRight(r.content, "\n")
	totalLines := strings.Count(trimmed, "\n") + 1

	m.detailMaxScroll = totalLines - m.contentHeight(0, m.activityIndicatorHeight())
	if m.detailMaxScroll < 0 {
		m.detailMaxScroll = 0
	}
}

// detailRowLines returns the number of rendered lines a single visible row
// occupies, including any expanded content or trace header below it.
func (m *model) detailRowLines(row visibleRow, width int) int {
	lines := 1 // the row itself

	childWidth := max(width-4, 20)

	if row.childIndex == -1 {
		// Parent row.
		if m.detailExpanded[row.parentIndex] {
			if row.item.itemType == transcript.ItemSubagent && row.item.subagentProcess != nil {
				lines++ // trace header line
			} else {
				expanded := m.renderDetailItemExpanded(row.item, width)
				if expanded.content != "" {
					lines += expanded.lines
				}
			}
		}
	} else {
		// Child row.
		key := visibleRowKey{row.parentIndex, row.childIndex}
		if m.detailChildExpanded[key] {
			expanded := m.renderDetailItemExpanded(row.item, childWidth)
			if expanded.content != "" {
				lines += expanded.lines
			}
		}
	}

	return lines
}

// detailCursorLine returns the absolute line offset of the current detail
// cursor row. Iterates visible rows, counting each row's lines (including
// expanded content and trace headers) for all rows before the cursor.
func (m *model) detailCursorLine() int {
	msg := m.currentDetailMsg()
	if len(msg.items) == 0 {
		return 0
	}

	width := m.clampWidth()

	// Count header lines (header + blank separator)
	header := m.renderDetailHeader(msg, width)
	cursorLine := header.lines + 1 // +1 for blank line separator from "\n\n"

	rows := buildVisibleRows(msg.items, m.detailExpanded)

	// Count lines for rows before the cursor.
	for i := 0; i < m.detailCursor && i < len(rows); i++ {
		cursorLine += m.detailRowLines(rows[i], width)
	}
	return cursorLine
}

// ensureDetailCursorVisible adjusts detailScroll so the current detail cursor
// row is within the visible viewport. Uses detailCursorLine to find the
// cursor's absolute line position, then scrolls to keep the full row
// (including expanded content) visible.
func (m *model) ensureDetailCursorVisible() {
	if m.width == 0 || m.height == 0 {
		return
	}
	msg := m.currentDetailMsg()
	if len(msg.items) == 0 {
		return
	}

	width := m.clampWidth()

	cursorLine := m.detailCursorLine()

	// Count lines for the cursor row itself (row + expanded content).
	cursorEnd := cursorLine
	rows := buildVisibleRows(msg.items, m.detailExpanded)
	if m.detailCursor < len(rows) {
		cursorEnd += m.detailRowLines(rows[m.detailCursor], width) - 1
	}

	viewHeight := m.contentHeight(0, m.activityIndicatorHeight())

	// Scroll up if cursor is above viewport
	if cursorLine < m.detailScroll {
		m.detailScroll = cursorLine
	}
	// Scroll down if cursor end (including expanded content) is below viewport
	if cursorEnd >= m.detailScroll+viewHeight {
		m.detailScroll = cursorEnd - viewHeight + 1
	}

	// Recompute max scroll after potential expansion changes
	m.computeDetailMaxScroll()
	if m.detailScroll > m.detailMaxScroll {
		m.detailScroll = m.detailMaxScroll
	}
	if m.detailScroll < 0 {
		m.detailScroll = 0
	}
}
