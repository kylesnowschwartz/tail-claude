package main

import tea "github.com/charmbracelet/bubbletea"

// resetDetailState zeroes the detail view cursor, scroll, and expansion maps.
func (m *model) resetDetailState() {
	m.detailCursor = 0
	m.detailScroll = 0
	m.detailExpanded = make(map[int]bool)
	m.detailChildExpanded = make(map[visibleRowKey]bool)
}

// updateList handles key events in the message list view.
func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "escape", "backspace":
		return m, loadPickerSessionsCmd(m.projectDir, m.sessionCache)
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
		return m, loadPickerSessionsCmd(m.projectDir, m.sessionCache)
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
func (m model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

// updateListMouse handles mouse events in the list view.
func (m model) updateListMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if m.scroll > 0 {
			m.scroll -= 3
			if m.scroll < 0 {
				m.scroll = 0
			}
		}
	case tea.MouseButtonWheelDown:
		m.scroll += 3
		m.clampListScroll()
	}
	return m, nil
}

// updateDetailMouse handles mouse events in the detail view.
func (m model) updateDetailMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if m.detailScroll > 0 {
			m.detailScroll -= 3
			if m.detailScroll < 0 {
				m.detailScroll = 0
			}
		}
	case tea.MouseButtonWheelDown:
		m.detailScroll += 3
		if m.detailScroll > m.detailMaxScroll {
			m.detailScroll = m.detailMaxScroll
		}
	}
	return m, nil
}
