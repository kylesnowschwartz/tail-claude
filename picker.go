package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pickerSessionsMsg delivers discovered sessions to the model.
type pickerSessionsMsg struct {
	sessions []parser.SessionInfo
	err      error
}

// loadSessionMsg delivers a parsed session ready for the list view,
// including classified messages and offset for watcher handoff.
type loadSessionMsg struct {
	messages   []message
	path       string
	classified []parser.ClassifiedMsg
	offset     int64
	err        error
}

// loadPickerSessionsCmd discovers sessions for the current project.
func loadPickerSessionsCmd() tea.Msg {
	projectDir, err := parser.CurrentProjectDir()
	if err != nil {
		return pickerSessionsMsg{err: err}
	}
	sessions, err := parser.DiscoverProjectSessions(projectDir)
	return pickerSessionsMsg{sessions: sessions, err: err}
}

// loadSessionCmd returns a command that loads a session file into messages.
// Uses ReadSessionIncremental so the result includes classified messages and
// offset for handing off to a new watcher.
func loadSessionCmd(path string) tea.Cmd {
	return func() tea.Msg {
		classified, offset, err := parser.ReadSessionIncremental(path, 0)
		if err != nil {
			return loadSessionMsg{err: err, path: path}
		}
		chunks := parser.BuildChunks(classified)
		return loadSessionMsg{
			messages:   chunksToMessages(chunks),
			path:       path,
			classified: classified,
			offset:     offset,
		}
	}
}

// --- Flattened virtual list ---

// pickerItemType discriminates between session rows and group headers.
type pickerItemType int

const (
	pickerItemSession pickerItemType = iota
	pickerItemHeader
)

// pickerItem is an entry in the flattened picker list.
type pickerItem struct {
	typ      pickerItemType
	session  *parser.SessionInfo // nil for headers
	category parser.DateCategory // set for headers
}

// pickerItemLines returns the display height in lines.
func (p pickerItem) lines() int {
	if p.typ == pickerItemHeader {
		return 1
	}
	return 2 // two-line session rows
}

// rebuildPickerItems flattens sessions into headers + session rows.
// Within each date group, ongoing sessions sort first (stable sort preserves
// mod-time order from DiscoverProjectSessions).
func rebuildPickerItems(sessions []parser.SessionInfo) []pickerItem {
	groups := parser.GroupSessionsByDate(sessions)

	var items []pickerItem
	for _, g := range groups {
		items = append(items, pickerItem{
			typ:      pickerItemHeader,
			category: g.Category,
		})

		// Stable sort: ongoing first within each group.
		sorted := make([]parser.SessionInfo, len(g.Sessions))
		copy(sorted, g.Sessions)
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].IsOngoing != sorted[j].IsOngoing {
				return sorted[i].IsOngoing
			}
			return false
		})

		for i := range sorted {
			items = append(items, pickerItem{
				typ:     pickerItemSession,
				session: &sorted[i],
			})
		}
	}
	return items
}

// --- Picker update ---

// updatePicker handles key events in the session picker view.
func (m model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "escape":
		if m.pickerWatcher != nil {
			m.pickerWatcher.stop()
			m.pickerWatcher = nil
		}
		m.view = viewList
		return m, nil
	case "ctrl+c":
		if m.pickerWatcher != nil {
			m.pickerWatcher.stop()
			m.pickerWatcher = nil
		}
		return m, tea.Quit
	case "j", "down":
		m.pickerCursorDown()
		m.ensurePickerVisible()
	case "k", "up":
		m.pickerCursorUp()
		m.ensurePickerVisible()
	case "G":
		m.pickerCursorLast()
		m.ensurePickerVisible()
	case "g":
		m.pickerCursorFirst()
	case "enter":
		if s := m.pickerSelectedSession(); s != nil {
			if m.pickerWatcher != nil {
				m.pickerWatcher.stop()
				m.pickerWatcher = nil
			}
			return m, loadSessionCmd(s.Path)
		}
	}
	return m, nil
}

// pickerSelectedSession returns the session at the current cursor, or nil.
func (m model) pickerSelectedSession() *parser.SessionInfo {
	if m.pickerCursor < 0 || m.pickerCursor >= len(m.pickerItems) {
		return nil
	}
	item := m.pickerItems[m.pickerCursor]
	if item.typ != pickerItemSession {
		return nil
	}
	return item.session
}

// pickerCursorDown moves cursor to next session item (skipping headers).
func (m *model) pickerCursorDown() {
	for i := m.pickerCursor + 1; i < len(m.pickerItems); i++ {
		if m.pickerItems[i].typ == pickerItemSession {
			m.pickerCursor = i
			return
		}
	}
}

// pickerCursorUp moves cursor to previous session item (skipping headers).
func (m *model) pickerCursorUp() {
	for i := m.pickerCursor - 1; i >= 0; i-- {
		if m.pickerItems[i].typ == pickerItemSession {
			m.pickerCursor = i
			return
		}
	}
}

// pickerCursorLast moves cursor to the last session item.
func (m *model) pickerCursorLast() {
	for i := len(m.pickerItems) - 1; i >= 0; i-- {
		if m.pickerItems[i].typ == pickerItemSession {
			m.pickerCursor = i
			return
		}
	}
}

// pickerCursorFirst moves cursor to the first session item.
func (m *model) pickerCursorFirst() {
	m.pickerScroll = 0
	for i := 0; i < len(m.pickerItems); i++ {
		if m.pickerItems[i].typ == pickerItemSession {
			m.pickerCursor = i
			return
		}
	}
}

// ensurePickerVisible adjusts pickerScroll so the cursor is visible.
// Accounts for variable-height items (headers=1 line, sessions=2 lines).
func (m *model) ensurePickerVisible() {
	viewHeight := m.height - 2 - statusBarHeight // header (2 lines) + status bar
	if viewHeight <= 0 {
		return
	}

	// Compute line position of cursor item.
	cursorLineStart := 0
	for i := 0; i < m.pickerCursor && i < len(m.pickerItems); i++ {
		cursorLineStart += m.pickerItems[i].lines()
	}
	cursorLineEnd := cursorLineStart
	if m.pickerCursor < len(m.pickerItems) {
		cursorLineEnd += m.pickerItems[m.pickerCursor].lines() - 1
	}

	if cursorLineStart < m.pickerScroll {
		m.pickerScroll = cursorLineStart
	}
	if cursorLineEnd >= m.pickerScroll+viewHeight {
		m.pickerScroll = cursorLineEnd - viewHeight + 1
	}
}

// --- Picker rendering ---

// viewPicker renders the session picker screen.
func (m model) viewPicker() string {
	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	// Header
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	countStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	header := titleStyle.Render("Sessions") + " " +
		countStyle.Render(fmt.Sprintf("(%d)", len(m.pickerSessions)))
	header += "\n"

	// Empty state
	if len(m.pickerItems) == 0 {
		dim := lipgloss.NewStyle().Foreground(ColorTextDim)
		return header + "\n" + dim.Render("No sessions found for this project.")
	}

	// Render all items into lines, then apply scroll window.
	var allLines []string
	for i, item := range m.pickerItems {
		switch item.typ {
		case pickerItemHeader:
			allLines = append(allLines, m.renderPickerHeader(item.category))
		case pickerItemSession:
			isSelected := i == m.pickerCursor
			lines := m.renderPickerSession(item.session, isSelected, width)
			allLines = append(allLines, lines...)
		}
	}

	// Apply scroll.
	viewHeight := m.height - 2 - statusBarHeight
	if viewHeight <= 0 {
		viewHeight = 1
	}

	start := m.pickerScroll
	if start > len(allLines) {
		start = len(allLines)
	}
	visible := allLines[start:]
	if len(visible) > viewHeight {
		visible = visible[:viewHeight]
	}

	content := header + "\n" + strings.Join(visible, "\n")

	// Pad to fill viewport so status bar stays at bottom.
	renderedLines := strings.Count(content, "\n") + 1
	if renderedLines < m.height-2 {
		content += strings.Repeat("\n", m.height-2-renderedLines)
	}

	status := m.renderStatusBar(
		"j/k", "nav",
		"G/g", "jump",
		"enter", "open",
		"q/esc", "back",
	)

	return content + "\n" + status
}

// renderPickerHeader renders a date group header line.
func (m model) renderPickerHeader(category parser.DateCategory) string {
	style := lipgloss.NewStyle().Bold(true).Foreground(ColorTextDim)
	return "  " + style.Render(string(category))
}

// renderPickerSession renders a two-line session row.
//
// Line 1: selection indicator + ongoing dot + preview text
// Line 2: model (colored) + turn count + duration + tokens + relative time (right-aligned)
func (m model) renderPickerSession(s *parser.SessionInfo, isSelected bool, width int) []string {
	sel := selectionIndicator(isSelected)
	selWidth := lipgloss.Width(sel)

	// --- Line 1: preview ---
	var line1Parts []string
	line1Parts = append(line1Parts, sel)

	if s.IsOngoing {
		dot := lipgloss.NewStyle().Foreground(ColorOngoing).Render(IconLive)
		line1Parts = append(line1Parts, dot+" ")
	}

	preview := s.FirstMessage
	if preview == "" {
		preview = "Untitled"
	}

	previewStyle := lipgloss.NewStyle().Foreground(ColorTextPrimary)
	if !isSelected {
		previewStyle = previewStyle.Foreground(ColorTextSecondary)
	}

	// Compute available width for preview text.
	usedWidth := selWidth
	if s.IsOngoing {
		usedWidth += 2 // dot + space
	}
	previewMaxWidth := width - usedWidth - 2
	if previewMaxWidth < 20 {
		previewMaxWidth = 20
	}
	if lipgloss.Width(preview) > previewMaxWidth {
		preview = truncate(preview, previewMaxWidth)
	}

	line1Parts = append(line1Parts, previewStyle.Render(preview))
	line1 := strings.Join(line1Parts, "")

	// --- Line 2: metadata ---
	indent := strings.Repeat(" ", selWidth)

	var metaParts []string
	dot := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(IconDot)

	// Model (colored by family).
	if s.Model != "" {
		short := shortModel(s.Model)
		mColor := modelColor(s.Model)
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(mColor).Render(short))
	}

	// Turn count.
	if s.TurnCount > 0 {
		chatIcon := lipgloss.NewStyle().Foreground(ColorTextDim).Render(IconChat)
		countStr := lipgloss.NewStyle().Foreground(ColorTextDim).Render(fmt.Sprintf("%d", s.TurnCount))
		metaParts = append(metaParts, chatIcon+" "+countStr)
	}

	// Duration.
	if s.DurationMs > 0 {
		durStr := formatSessionDuration(s.DurationMs)
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(ColorTextDim).Render(durStr))
	}

	// Tokens.
	if s.TotalTokens > 0 {
		tokStr := formatTokens(s.TotalTokens)
		tokColor := ColorTextDim
		if s.TotalTokens > 150_000 {
			tokColor = ColorTokenHigh
		}
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(tokColor).Render(tokStr))
	}

	left2 := indent + strings.Join(metaParts, " "+dot+" ")

	// Right-aligned relative time.
	timeStr := relativeTime(s.ModTime)
	right2 := lipgloss.NewStyle().Foreground(ColorTextDim).Render(timeStr)
	line2 := spaceBetween(left2, right2, width)

	return []string{line1, line2}
}

// formatSessionDuration formats session duration for the picker.
// Shorter format than formatDuration: "5s", "2m", "1h", "3h".
func formatSessionDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

// relativeTime formats a time.Time as a human-readable relative duration.
func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}
