package main

import tea "github.com/charmbracelet/bubbletea"

// updateList handles key events in the message list view.
func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "escape", "backspace":
		return m, loadPickerSessionsCmd
	case "j", "down":
		if m.cursor < len(m.messages)-1 {
			m.cursor++
		}
		m.computeLineOffsets()
		m.ensureCursorVisible()
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		m.computeLineOffsets()
		m.ensureCursorVisible()
	case "G":
		if len(m.messages) > 0 {
			m.cursor = len(m.messages) - 1
			m.computeLineOffsets()
			m.ensureCursorVisible()
		}
	case "g":
		m.cursor = 0
		m.scroll = 0
	case "tab":
		// Toggle expand/collapse for Claude and User messages
		if m.cursor < len(m.messages) {
			role := m.messages[m.cursor].role
			if role == RoleClaude || role == RoleUser {
				m.expanded[m.cursor] = !m.expanded[m.cursor]
			}
		}
		m.computeLineOffsets()
		m.clampListScroll()
	case "enter":
		// Enter detail view for current message
		if len(m.messages) > 0 {
			m.view = viewDetail
			m.detailScroll = 0
			m.detailCursor = 0
			m.detailExpanded = make(map[int]bool)
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
		m.computeLineOffsets()
		m.ensureCursorVisible()
	case "c":
		// Collapse all Claude messages
		for i, msg := range m.messages {
			if msg.role == RoleClaude {
				m.expanded[i] = false
			}
		}
		m.computeLineOffsets()
		m.ensureCursorVisible()
	case "s":
		// Open session picker
		return m, loadPickerSessionsCmd
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
	}
	return m, nil
}

// detailHasItems returns true when the current detail message has structured items.
func (m model) detailHasItems() bool {
	return len(m.currentDetailMsg().items) > 0
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
			m.traceMsg = nil
			m.savedDetail = nil
			m.computeDetailMaxScroll()
		} else {
			m.view = viewList
			m.detailCursor = 0
			m.detailExpanded = make(map[int]bool)
		}
	case "tab":
		if hasItems {
			visualRow := m.detailCursorLine() - m.detailScroll
			m.detailExpanded[m.detailCursor] = !m.detailExpanded[m.detailCursor]
			m.computeDetailMaxScroll()
			m.detailScroll = m.detailCursorLine() - visualRow
			if m.detailScroll < 0 {
				m.detailScroll = 0
			}
			if m.detailScroll > m.detailMaxScroll {
				m.detailScroll = m.detailMaxScroll
			}
		}
	case "enter":
		if hasItems {
			item := detailMsg.items[m.detailCursor]
			if item.subagentProcess != nil {
				// Drill into subagent execution trace.
				synth := buildSubagentMessage(item.subagentProcess, item.subagentType)
				cloned := make(map[int]bool, len(m.detailExpanded))
				for k, v := range m.detailExpanded {
					cloned[k] = v
				}
				// Build breadcrumb label from parent message.
				parentLabel := detailMsg.subagentLabel
				if parentLabel == "" {
					parentLabel = "Claude"
				}
				if detailMsg.model != "" {
					parentLabel += " " + detailMsg.model
				}
				m.savedDetail = &savedDetailState{
					cursor:   m.detailCursor,
					scroll:   m.detailScroll,
					expanded: cloned,
					label:    parentLabel,
				}
				m.traceMsg = &synth
				m.detailCursor = 0
				m.detailScroll = 0
				m.detailExpanded = make(map[int]bool)
				m.computeDetailMaxScroll()
			} else {
				visualRow := m.detailCursorLine() - m.detailScroll
				m.detailExpanded[m.detailCursor] = !m.detailExpanded[m.detailCursor]
				m.computeDetailMaxScroll()
				m.detailScroll = m.detailCursorLine() - visualRow
				if m.detailScroll < 0 {
					m.detailScroll = 0
				}
				if m.detailScroll > m.detailMaxScroll {
					m.detailScroll = m.detailMaxScroll
				}
			}
		} else {
			m.view = viewList
			m.detailCursor = 0
			m.detailExpanded = make(map[int]bool)
		}
	case "j", "down":
		if hasItems {
			if m.detailCursor < len(detailMsg.items)-1 {
				m.detailCursor++
			}
			m.ensureDetailCursorVisible()
		} else {
			m.detailScroll++
		}
	case "k", "up":
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
	case "J", "ctrl+d":
		m.detailScroll += m.height / 2
	case "K", "ctrl+u":
		m.detailScroll -= m.height / 2
		if m.detailScroll < 0 {
			m.detailScroll = 0
		}
	case "G":
		if hasItems {
			m.detailCursor = len(detailMsg.items) - 1
		}
		m.detailScroll = m.detailMaxScroll
	case "g":
		m.detailScroll = 0
		if hasItems {
			m.detailCursor = 0
		}
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
