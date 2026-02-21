package main

import "strings"

// computeLineOffsets calculates the starting line of each message in the
// rendered output. Must mirror View()'s rendering to keep scroll accurate.
func (m *model) computeLineOffsets() {
	if m.width == 0 || len(m.messages) == 0 {
		return
	}
	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	m.lineOffsets = make([]int, len(m.messages))
	m.messageLines = make([]int, len(m.messages))
	currentLine := 0
	for i, msg := range m.messages {
		m.lineOffsets[i] = currentLine
		r := m.renderMessage(msg, width, false, m.expanded[i])
		m.messageLines[i] = r.lines
		currentLine += r.lines
		if i < len(m.messages)-1 {
			currentLine++ // blank line from "\n\n" join separator
		}
	}

	if len(m.messages) > 0 {
		last := len(m.messages) - 1
		m.totalRenderedLines = m.lineOffsets[last] + m.messageLines[last]
	} else {
		m.totalRenderedLines = 0
	}
}

// ensureCursorVisible adjusts scroll so the cursor's message is within
// the visible viewport.
func (m *model) ensureCursorVisible() {
	if len(m.lineOffsets) == 0 || m.height == 0 {
		return
	}
	viewHeight := m.listViewHeight()

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

// clampListScroll caps the list scroll offset so it can't exceed the content.
func (m *model) clampListScroll() {
	maxScroll := m.totalRenderedLines - m.listViewHeight()
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

// computeDetailMaxScroll caches the maximum scroll offset for the detail view.
// Called when entering detail view and on window resize.
func (m *model) computeDetailMaxScroll() {
	if m.width == 0 || m.height == 0 {
		m.detailMaxScroll = 0
		return
	}

	msg := m.currentDetailMsg()
	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	r := m.renderDetailContent(msg, width)
	// Trim trailing newlines that lipgloss may add (phantom blank lines).
	trimmed := strings.TrimRight(r.content, "\n")
	totalLines := strings.Count(trimmed, "\n") + 1

	m.detailMaxScroll = totalLines - m.detailViewHeight()
	if m.detailMaxScroll < 0 {
		m.detailMaxScroll = 0
	}
}

// detailCursorLine returns the absolute line offset of the current detail
// cursor item. Counts header lines + item rows + expanded content lines for
// all items before the cursor.
func (m *model) detailCursorLine() int {
	msg := m.currentDetailMsg()
	if len(msg.items) == 0 {
		return 0
	}

	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	// Count header lines (header + blank separator)
	header := m.renderDetailHeader(msg, width)
	cursorLine := header.lines + 1 // +1 for blank line separator from "\n\n"

	// Count lines for items before the cursor
	for i := 0; i < m.detailCursor && i < len(msg.items); i++ {
		cursorLine++ // the item row itself
		if m.detailExpanded[i] {
			expanded := m.renderDetailItemExpanded(msg.items[i], width)
			cursorLine += expanded.lines
		}
	}
	return cursorLine
}

// ensureDetailCursorVisible adjusts detailScroll so the current detail cursor
// item is within the visible viewport. Uses detailCursorLine to find the
// cursor's absolute line position, then scrolls to keep the full item
// (including expanded content) visible.
func (m *model) ensureDetailCursorVisible() {
	if m.width == 0 || m.height == 0 {
		return
	}
	msg := m.currentDetailMsg()
	if len(msg.items) == 0 {
		return
	}

	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	cursorLine := m.detailCursorLine()

	// Count lines for the cursor item itself (row + expanded content)
	cursorEnd := cursorLine // the row line
	if m.detailCursor < len(msg.items) && m.detailExpanded[m.detailCursor] {
		expanded := m.renderDetailItemExpanded(msg.items[m.detailCursor], width)
		cursorEnd += expanded.lines
	}

	viewHeight := m.detailViewHeight()

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
