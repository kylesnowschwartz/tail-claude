package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/kylesnowschwartz/agent-ouija/claude/debuglog"
	zone "github.com/lrstanley/bubblezone/v2"

	tea "charm.land/bubbletea/v2"
)

// resetDetailState zeroes the detail view cursor, scroll, and expansion maps.
func (m *model) resetDetailState() {
	m.detailCursor = 0
	m.detailScroll = 0
	m.detailExpanded = make(map[int]bool)
	m.detailChildExpanded = make(map[visibleRowKey]bool)
}

// openPicker switches to the picker view and kicks off async session
// discovery. The view flips at dispatch time so a slow discovery result
// can't hijack whatever view the user navigates to in the meantime.
func (m model) openPicker() (tea.Model, tea.Cmd) {
	m.view = viewPicker
	m.pickerLoading = true
	m.pickerLoadGen++
	return m, tea.Batch(loadPickerSessionsCmd(m.pickerLoadGen, m.projectDirs, m.sessionCache), pickerTickCmd())
}

// updateList handles key events in the message list view.
func (m model) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "escape", "backspace":
		return m.openPicker()
	case "j":
		m.moveListCursor(m.cursor + 1)
		m.ensureCursorVisible()
	case "k":
		m.moveListCursor(m.cursor - 1)
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
			m.moveListCursor(len(m.messages) - 1)
			m.ensureCursorVisible()
		}
	case "g":
		m.moveListCursor(0)
		m.scroll = 0
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
		return m.openPicker()
	case "S":
		// Open per-session tool-usage stats view
		m.view = viewStats
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
		// Open debug log viewer for current session. Copilot sessions have
		// no Claude debug log — debugLogPathFor would fabricate a Claude path.
		if sourceForPath(m.sessionPath) == sourceCopilot {
			m.flashStatus = "No debug log for Copilot sessions"
			return m, flashClearCmd()
		}
		debugPath := debugLogPathFor(m.sessionPath)
		if debugPath == "" {
			m.flashStatus = "No debug log (start Claude with --debug)"
			return m, flashClearCmd()
		}
		entries, _, err := debuglog.ReadDebugLog(debugPath)
		if err != nil {
			return m, nil
		}
		m.debugEntries = entries
		m.debugPath = debugPath
		m.debugCursor = 0
		m.debugScroll = 0
		m.debugMinLevel = debuglog.LevelDebug
		m.debugExpanded = make(map[int]bool)
		m.applyDebugFilters()
		m.view = viewDebug

		// Start debug file watcher for live tailing.
		m.stopDebugWatcher()
		dw := newDebugLogWatcher(debugPath)
		if err := dw.start(); err != nil {
			// The static entries above are still shown; just no live tailing.
			m.flashStatus = "debug watch failed: " + err.Error()
			return m, flashClearCmd()
		}
		go dw.run()
		m.debugWatcher = dw
		return m, waitForDebugUpdate(dw.sub)
	case "y":
		// Copy session JSONL path to clipboard.
		if m.sessionPath != "" {
			m.flashStatus = "Copied: " + m.sessionPath
			return m, tea.Batch(tea.SetClipboard(m.sessionPath), flashClearCmd())
		}
	case "O":
		// Open session JSONL in $EDITOR.
		if cmd := editorCmd(m.sessionPath); cmd != nil {
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				return editorFinishedMsg{err}
			})
		}
		if m.sessionPath != "" {
			m.flashStatus = "No $EDITOR set"
			return m, flashClearCmd()
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

	// Nothing to reveal: don't record a meaningless "expanded" state.
	if !hasExpandedContent(row.item) {
		return
	}

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
			m.enterList()
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
			m.enterList()
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
	// When the text filter input is active, route all keys there.
	if m.debugFilterMode {
		return m.updateDebugFilter(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc", "escape", "backspace":
		if m.debugFilterText != "" {
			// First press clears the active text filter; second press exits.
			m.debugFilterText = ""
			m.reapplyDebugFilters()
			return m, nil
		}
		m.stopDebugWatcher()
		m.enterList()
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
		case debuglog.LevelDebug:
			m.debugMinLevel = debuglog.LevelWarn
		case debuglog.LevelWarn:
			m.debugMinLevel = debuglog.LevelError
		case debuglog.LevelError:
			m.debugMinLevel = debuglog.LevelDebug
		}
		m.reapplyDebugFilters()
	case "/":
		// Enter text filter input mode.
		m.debugFilterMode = true
		m.debugFilterText = ""
	case "y":
		// Copy debug log path to clipboard.
		if m.debugPath != "" {
			m.flashStatus = "Copied: " + m.debugPath
			return m, tea.Batch(tea.SetClipboard(m.debugPath), flashClearCmd())
		}
	case "O":
		// Open debug log in $EDITOR.
		if cmd := editorCmd(m.debugPath); cmd != nil {
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				return editorFinishedMsg{err}
			})
		}
		if m.debugPath != "" {
			m.flashStatus = "No $EDITOR set"
			return m, flashClearCmd()
		}
	case "?":
		m.showKeybinds = !m.showKeybinds
	}
	return m, nil
}

// updateDebugFilter handles key events while the / text filter input is active.
func (m model) updateDebugFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "enter":
		// Commit filter and exit input mode.
		m.debugFilterMode = false
		m.reapplyDebugFilters()
		m.debugCursor = 0
	case "esc", "escape":
		// Cancel: discard any typed text, exit input mode.
		m.debugFilterMode = false
		m.debugFilterText = ""
		m.reapplyDebugFilters()
		m.debugCursor = 0
	case "backspace":
		if len(m.debugFilterText) > 0 {
			m.debugFilterText = m.debugFilterText[:len(m.debugFilterText)-1]
			m.reapplyDebugFilters()
			m.debugCursor = 0
		}
	case "ctrl+c":
		return m, tea.Quit
	default:
		// Append printable characters.
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.debugFilterText += key
			m.reapplyDebugFilters()
			m.debugCursor = 0
		}
	}
	return m, nil
}

// enterList switches to the list view, re-running layout if a tail update
// arrived while another view was active. layoutList only runs in list view,
// so the cached layout can be stale on re-entry; every return-to-list
// transition must route through here or it renders one stale frame.
func (m *model) enterList() {
	m.view = viewList
	if m.listLayoutStale {
		m.layoutList()
	}
}

// editorCmd returns an *exec.Cmd to open filePath in the user's $EDITOR.
// Returns nil if no editor is configured or filePath is empty.
func editorCmd(filePath string) *exec.Cmd {
	if filePath == "" {
		return nil
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		return nil
	}
	return exec.Command(editor, filePath)
}

// debugEntryLines returns the rendered line count of debug entry i: the
// header line plus extra lines when expanded. Single source of the height
// rule — debugVisibleLines, debugTotalLines, and debugCursorLine must all
// agree or scroll math disagrees with the screen.
func (m model) debugEntryLines(i int) int {
	lines := 1 // header line
	if m.debugExpanded[i] && m.debugFiltered[i].HasExtra() {
		lines += m.debugFiltered[i].ExtraLineCount()
	}
	return lines
}

// debugTotalLines returns the total rendered lines in the debug view.
func (m model) debugTotalLines() int {
	total := 0
	for i := range m.debugFiltered {
		total += m.debugEntryLines(i)
	}
	return total
}

// debugMaxScroll returns the maximum scroll offset for the debug view.
func (m model) debugMaxScroll() int {
	total := m.debugTotalLines()
	viewHeight := m.contentHeight(0, m.debugFilterPromptHeight())
	maxScroll := total - viewHeight
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

// debugFilterPromptHeight returns 1 when the debug filter input is active, 0 otherwise.
func (m model) debugFilterPromptHeight() int {
	if m.debugFilterMode {
		return 1
	}
	return 0
}

// debugCursorLine returns the absolute line offset of the debug cursor.
func (m model) debugCursorLine() int {
	line := 0
	for i := 0; i < m.debugCursor && i < len(m.debugFiltered); i++ {
		line += m.debugEntryLines(i)
	}
	return line
}

// ensureDebugCursorVisible adjusts debugScroll to keep the cursor in view.
func (m *model) ensureDebugCursorVisible() {
	cursorLine := m.debugCursorLine()
	viewHeight := m.contentHeight(0, m.debugFilterPromptHeight())

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
	case tea.MouseLeft:
		if zone.Get(zoneHelp).InBounds(msg) {
			m.showKeybinds = !m.showKeybinds
		}
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
	case tea.MouseLeft:
		if zone.Get(zoneHelp).InBounds(msg) {
			m.showKeybinds = !m.showKeybinds
		}
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
	case tea.MouseLeft:
		if zone.Get(zoneHelp).InBounds(msg) {
			m.showKeybinds = !m.showKeybinds
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
		m.enterList()
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
	case tea.MouseLeft:
		if zone.Get(zoneHelp).InBounds(msg) {
			m.showKeybinds = !m.showKeybinds
		}
	}
	return m, nil
}

// teamMaxScroll returns the maximum scroll offset for the team view.
func (m model) teamMaxScroll() int {
	content := m.renderTeamContent(m.clampWidth(), m.animFrame)
	totalLines := strings.Count(content, "\n") + 1
	viewHeight := m.contentHeight(0, 0)
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
