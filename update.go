package main

import (
	"strings"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "charm.land/bubbletea/v2"
)

// resetDetailState zeroes the detail view cursor, scroll, and expansion maps.
func (m *model) resetDetailState() {
	m.detailCursor = 0
	m.detailScroll = 0
	m.detailExpanded = make(map[int]bool)
	m.detailChildExpanded = make(map[visibleRowKey]bool)
}

// updateList handles key events in the message list view.
func (m model) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "escape", "backspace":
		return m, loadPickerSessionsCmd(m.projectDirs, m.sessionCache)
	case "j":
		if m.cursor < len(m.messages)-1 {
			m.cursor++
		}
		m.layoutList()
		m.ensureCursorVisible()
	case "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.layoutList()
		m.ensureCursorVisible()
	case "down":
		m.scroll += 3
		m.clampListScroll()
	case "up":
		m.scroll -= 3
		if m.scroll < 0 {
			m.scroll = 0
		}
	case "G":
		if len(m.messages) > 0 {
			m.cursor = len(m.messages) - 1
			m.layoutList()
			m.ensureCursorVisible()
		}
	case "g":
		m.cursor = 0
		m.scroll = 0
		m.layoutList()
	case "tab":
		// Toggle expand/collapse for Claude and User messages
		if m.cursor < len(m.messages) {
			role := m.messages[m.cursor].role
			if role == RoleClaude || role == RoleUser {
				m.expanded[m.cursor] = !m.expanded[m.cursor]
			}
		}
		m.layoutList()
		m.clampListScroll()
	case "enter":
		// Enter detail view for current message
		if len(m.messages) > 0 {
			m.view = viewDetail
			m.resetDetailState()
			m.traceMsg = nil
			m.savedDetail = nil
			m.computeDetailMaxScroll()
		}
	case "e":
		// Expand all Claude messages
		for i, msg := range m.messages {
			if msg.role == RoleClaude {
				m.expanded[i] = true
			}
		}
		m.layoutList()
		m.ensureCursorVisible()
	case "c":
		// Collapse all Claude messages
		for i, msg := range m.messages {
			if msg.role == RoleClaude {
				m.expanded[i] = false
			}
		}
		m.layoutList()
		m.ensureCursorVisible()
	case "s":
		// Open session picker
		return m, loadPickerSessionsCmd(m.projectDirs, m.sessionCache)
	case "J", "ctrl+d":
		// Scroll viewport down (half page)
		m.scroll += m.height / 2
		m.clampListScroll()
	case "K", "ctrl+u":
		// Scroll viewport up (half page)
		m.scroll -= m.height / 2
		if m.scroll < 0 {
			m.scroll = 0
		}
	case "t":
		// Open team task board (only when teams exist).
		if len(m.teams) > 0 {
			m.teamScroll = 0
			m.view = viewTeam
		}
	case "d":
		// Open debug log viewer for current session.
		debugPath := parser.DebugLogPath(m.sessionPath)
		if debugPath == "" {
			return m, nil // no debug file, no-op
		}
		entries, offset, err := parser.ReadDebugLog(debugPath)
		if err != nil {
			return m, nil
		}
		m.debugEntries = entries
		m.debugPath = debugPath
		m.debugCursor = 0
		m.debugScroll = 0
		m.debugMinLevel = parser.LevelDebug
		m.debugExpanded = make(map[int]bool)
		m.applyDebugFilters()
		m.view = viewDebug

		// Start debug file watcher for live tailing.
		m.stopDebugWatcher()
		dw := newDebugLogWatcher(debugPath, offset)
		go dw.run()
		m.debugWatcher = dw
		return m, waitForDebugUpdate(dw.sub)
	case "?":
		m.showKeybinds = !m.showKeybinds
		m.layoutList()
		m.clampListScroll()
	}
	return m, nil
}

// detailHasItems returns true when the current detail message has structured items.
func (m model) detailHasItems() bool {
	return len(m.currentDetailMsg().items) > 0
}

// detailVisibleRows builds the flat visible row list for the current detail message.
func (m model) detailVisibleRows() []visibleRow {
	return buildVisibleRows(m.currentDetailMsg().items, m.detailExpanded)
}

// toggleDetailExpansion preserves the cursor's visual position while toggling
// expansion state. Shared by tab and enter-on-non-drillable handlers.
func (m *model) toggleDetailExpansion() {
	rows := m.detailVisibleRows()
	if m.detailCursor >= len(rows) {
		return
	}

	visualRow := m.detailCursorLine() - m.detailScroll
	row := rows[m.detailCursor]

	if row.childIndex == -1 {
		// Parent row: toggle parent expansion.
		m.detailExpanded[row.parentIndex] = !m.detailExpanded[row.parentIndex]
	} else {
		// Child row: toggle child content expansion.
		key := visibleRowKey{row.parentIndex, row.childIndex}
		m.detailChildExpanded[key] = !m.detailChildExpanded[key]
	}

	m.computeDetailMaxScroll()
	m.detailScroll = m.detailCursorLine() - visualRow
	if m.detailScroll < 0 {
		m.detailScroll = 0
	}
	if m.detailScroll > m.detailMaxScroll {
		m.detailScroll = m.detailMaxScroll
	}
}

// updateDetail handles key events in the full-screen detail view.
func (m model) updateDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	hasItems := m.detailHasItems()
	detailMsg := m.currentDetailMsg()

	switch msg.String() {
	case "q", "esc", "escape", "backspace":
		if m.traceMsg != nil {
			// Pop back to parent detail view.
			m.detailCursor = m.savedDetail.cursor
			m.detailScroll = m.savedDetail.scroll
			m.detailExpanded = m.savedDetail.expanded
			m.detailChildExpanded = m.savedDetail.childExpanded
			m.traceMsg = nil
			m.savedDetail = nil
			m.computeDetailMaxScroll()
		} else {
			m.view = viewList
			m.resetDetailState()
		}
	case "tab":
		if hasItems {
			m.toggleDetailExpansion()
		}
	case "enter":
		if hasItems {
			rows := m.detailVisibleRows()
			if m.detailCursor < len(rows) {
				row := rows[m.detailCursor]
				// Only parent subagent rows with a linked process drill in.
				if row.childIndex == -1 && row.item.subagentProcess != nil {
					synth := buildSubagentMessage(row.item.subagentProcess, row.item.subagentType)
					clonedExp := make(map[int]bool, len(m.detailExpanded))
					for k, v := range m.detailExpanded {
						clonedExp[k] = v
					}
					clonedChild := make(map[visibleRowKey]bool, len(m.detailChildExpanded))
					for k, v := range m.detailChildExpanded {
						clonedChild[k] = v
					}
					parentLabel := detailMsg.subagentLabel
					if parentLabel == "" {
						parentLabel = "Claude"
					}
					if detailMsg.model != "" {
						parentLabel += " " + detailMsg.model
					}
					m.savedDetail = &savedDetailState{
						cursor:        m.detailCursor,
						scroll:        m.detailScroll,
						expanded:      clonedExp,
						childExpanded: clonedChild,
						label:         parentLabel,
					}
					m.traceMsg = &synth
					m.resetDetailState()
					m.computeDetailMaxScroll()
				} else {
					// All other rows: toggle expansion (same as tab).
					m.toggleDetailExpansion()
				}
			}
		} else {
			m.view = viewList
			m.resetDetailState()
		}
	case "j":
		if hasItems {
			rows := m.detailVisibleRows()
			if m.detailCursor < len(rows)-1 {
				m.detailCursor++
			}
			m.ensureDetailCursorVisible()
		} else {
			m.detailScroll++
		}
	case "k":
		if hasItems {
			if m.detailCursor > 0 {
				m.detailCursor--
			}
			m.ensureDetailCursorVisible()
		} else {
			if m.detailScroll > 0 {
				m.detailScroll--
			}
		}
	case "down":
		m.detailScroll += 3
	case "up":
		m.detailScroll -= 3
		if m.detailScroll < 0 {
			m.detailScroll = 0
		}
	case "J", "ctrl+d":
		m.detailScroll += m.height / 2
	case "K", "ctrl+u":
		m.detailScroll -= m.height / 2
		if m.detailScroll < 0 {
			m.detailScroll = 0
		}
	case "G":
		if hasItems {
			rows := m.detailVisibleRows()
			m.detailCursor = len(rows) - 1
		}
		m.computeDetailMaxScroll()
		m.detailScroll = m.detailMaxScroll
	case "g":
		m.detailScroll = 0
		if hasItems {
			m.detailCursor = 0
		}
	case "?":
		m.showKeybinds = !m.showKeybinds
		m.computeDetailMaxScroll()
	case "ctrl+c":
		return m, tea.Quit
	}
	// Clamp to valid range after any modification
	if m.detailScroll > m.detailMaxScroll {
		m.detailScroll = m.detailMaxScroll
	}
	if m.detailScroll < 0 {
		m.detailScroll = 0
	}
	return m, nil
}

// updateDebug handles key events in the debug log viewer.
func (m model) updateDebug(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "escape", "backspace":
		m.stopDebugWatcher()
		m.view = viewList
	case "j":
		if m.debugCursor < len(m.debugFiltered)-1 {
			m.debugCursor++
		}
		m.ensureDebugCursorVisible()
	case "k":
		if m.debugCursor > 0 {
			m.debugCursor--
		}
		m.ensureDebugCursorVisible()
	case "down":
		m.debugScroll += 3
		m.clampDebugScroll()
	case "up":
		m.debugScroll -= 3
		if m.debugScroll < 0 {
			m.debugScroll = 0
		}
	case "G":
		if len(m.debugFiltered) > 0 {
			m.debugCursor = len(m.debugFiltered) - 1
		}
		m.debugScroll = m.debugMaxScroll()
	case "g":
		m.debugCursor = 0
		m.debugScroll = 0
	case "J", "ctrl+d":
		m.debugScroll += m.height / 2
		m.clampDebugScroll()
	case "K", "ctrl+u":
		m.debugScroll -= m.height / 2
		if m.debugScroll < 0 {
			m.debugScroll = 0
		}
	case "tab":
		// Toggle multi-line entry expansion.
		if m.debugCursor < len(m.debugFiltered) && m.debugFiltered[m.debugCursor].HasExtra() {
			m.debugExpanded[m.debugCursor] = !m.debugExpanded[m.debugCursor]
		}
	case "f":
		// Cycle level filter: All -> Warn+ -> Error -> All.
		switch m.debugMinLevel {
		case parser.LevelDebug:
			m.debugMinLevel = parser.LevelWarn
		case parser.LevelWarn:
			m.debugMinLevel = parser.LevelError
		case parser.LevelError:
			m.debugMinLevel = parser.LevelDebug
		}
		m.debugExpanded = make(map[int]bool)
		m.applyDebugFilters()
		m.debugScroll = 0
	case "?":
		m.showKeybinds = !m.showKeybinds
	}
	return m, nil
}

// debugTotalLines returns the total rendered lines in the debug view.
func (m model) debugTotalLines() int {
	total := 0
	for i, entry := range m.debugFiltered {
		total++ // header line
		if m.debugExpanded[i] && entry.HasExtra() {
			total += entry.ExtraLineCount()
		}
	}
	return total
}

// debugMaxScroll returns the maximum scroll offset for the debug view.
func (m model) debugMaxScroll() int {
	total := m.debugTotalLines()
	viewHeight := m.debugViewHeight()
	maxScroll := total - viewHeight
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

// debugViewHeight returns the visible content lines in the debug view.
func (m model) debugViewHeight() int {
	h := m.height - m.footerHeight()
	if h <= 0 {
		return 1
	}
	return h
}

// debugCursorLine returns the absolute line offset of the debug cursor.
func (m model) debugCursorLine() int {
	line := 0
	for i := 0; i < m.debugCursor && i < len(m.debugFiltered); i++ {
		line++ // header line
		if m.debugExpanded[i] && m.debugFiltered[i].HasExtra() {
			line += m.debugFiltered[i].ExtraLineCount()
		}
	}
	return line
}

// ensureDebugCursorVisible adjusts debugScroll to keep the cursor in view.
func (m *model) ensureDebugCursorVisible() {
	cursorLine := m.debugCursorLine()
	viewHeight := m.debugViewHeight()

	if cursorLine < m.debugScroll {
		m.debugScroll = cursorLine
	}

	// Include expanded content in cursor end calculation.
	cursorEnd := cursorLine
	if m.debugCursor < len(m.debugFiltered) {
		if m.debugExpanded[m.debugCursor] && m.debugFiltered[m.debugCursor].HasExtra() {
			cursorEnd += m.debugFiltered[m.debugCursor].ExtraLineCount()
		}
	}
	if cursorEnd >= m.debugScroll+viewHeight {
		m.debugScroll = cursorEnd - viewHeight + 1
	}

	m.clampDebugScroll()
}

// clampDebugScroll caps the debug scroll offset to valid range.
func (m *model) clampDebugScroll() {
	maxScroll := m.debugMaxScroll()
	if m.debugScroll > maxScroll {
		m.debugScroll = maxScroll
	}
	if m.debugScroll < 0 {
		m.debugScroll = 0
	}
}

// updateDebugMouse handles mouse events in the debug view.
func (m model) updateDebugMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		if m.debugScroll > 0 {
			m.debugScroll -= 3
			if m.debugScroll < 0 {
				m.debugScroll = 0
			}
		}
	case tea.MouseWheelDown:
		m.debugScroll += 3
		m.clampDebugScroll()
	}
	return m, nil
}

// updateListMouse handles mouse events in the list view.
func (m model) updateListMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		if m.scroll > 0 {
			m.scroll -= 3
			if m.scroll < 0 {
				m.scroll = 0
			}
		}
	case tea.MouseWheelDown:
		m.scroll += 3
		m.clampListScroll()
	}
	return m, nil
}

// updateDetailMouse handles mouse events in the detail view.
func (m model) updateDetailMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		if m.detailScroll > 0 {
			m.detailScroll -= 3
			if m.detailScroll < 0 {
				m.detailScroll = 0
			}
		}
	case tea.MouseWheelDown:
		m.detailScroll += 3
		if m.detailScroll > m.detailMaxScroll {
			m.detailScroll = m.detailMaxScroll
		}
	}
	return m, nil
}

// updateTeam handles key events in the team task board view.
func (m model) updateTeam(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "escape", "backspace":
		m.view = viewList
	case "j", "down":
		m.teamScroll += 3
		m.clampTeamScroll()
	case "k", "up":
		m.teamScroll -= 3
		if m.teamScroll < 0 {
			m.teamScroll = 0
		}
	case "J", "ctrl+d":
		m.teamScroll += m.height / 2
		m.clampTeamScroll()
	case "K", "ctrl+u":
		m.teamScroll -= m.height / 2
		if m.teamScroll < 0 {
			m.teamScroll = 0
		}
	case "G":
		m.teamScroll = m.teamMaxScroll()
	case "g":
		m.teamScroll = 0
	case "?":
		m.showKeybinds = !m.showKeybinds
	}
	return m, nil
}

// updateTeamMouse handles mouse events in the team task board view.
func (m model) updateTeamMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		m.teamScroll -= 3
		if m.teamScroll < 0 {
			m.teamScroll = 0
		}
	case tea.MouseWheelDown:
		m.teamScroll += 3
		m.clampTeamScroll()
	}
	return m, nil
}

// teamMaxScroll returns the maximum scroll offset for the team view.
func (m model) teamMaxScroll() int {
	content := m.renderTeamContent(m.clampWidth(), m.animFrame)
	totalLines := strings.Count(content, "\n") + 1
	viewHeight := m.teamViewHeight()
	maxScroll := totalLines - viewHeight
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

// clampTeamScroll caps the team scroll offset to valid range.
func (m *model) clampTeamScroll() {
	maxScroll := m.teamMaxScroll()
	if m.teamScroll > maxScroll {
		m.teamScroll = maxScroll
	}
	if m.teamScroll < 0 {
		m.teamScroll = 0
	}
}
