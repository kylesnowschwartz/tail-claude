package main

import (
	"strings"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

// clampWidth returns m.width capped at maxContentWidth.
func (m model) clampWidth() int {
	if m.width > maxContentWidth {
		return maxContentWidth
	}
	return m.width
}

// computeLineOffsets calculates the starting line of each message in the
// rendered output. Must mirror View()'s rendering to keep scroll accurate.
func (m *model) computeLineOffsets() {
	if m.width == 0 || len(m.messages) == 0 {
		return
	}
	width := m.clampWidth()

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
	width := m.clampWidth()

	r := m.renderDetailContent(msg, width)
	// Trim trailing newlines that lipgloss may add (phantom blank lines).
	trimmed := strings.TrimRight(r.content, "\n")
	totalLines := strings.Count(trimmed, "\n") + 1

	m.detailMaxScroll = totalLines - m.detailViewHeight()
	if m.detailMaxScroll < 0 {
		m.detailMaxScroll = 0
	}
}

// detailRowLines returns the number of rendered lines a single visible row
// occupies, including any expanded content or trace header below it.
func (m *model) detailRowLines(row visibleRow, width int) int {
	lines := 1 // the row itself

	childWidth := width - 4
	if childWidth < 20 {
		childWidth = 20
	}

	if row.childIndex == -1 {
		// Parent row.
		if m.detailExpanded[row.parentIndex] {
			if row.item.itemType == parser.ItemSubagent && row.item.subagentProcess != nil {
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
