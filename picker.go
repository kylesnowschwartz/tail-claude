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
	messages     []message
	path         string
	classified   []parser.ClassifiedMsg
	offset       int64
	ongoing      bool
	hasTeamTasks bool
	err          error
}

// pickerTickMsg drives the ongoing-dot blink animation (500ms interval).
type pickerTickMsg time.Time

func pickerTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return pickerTickMsg(t)
	})
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
		subagents, _ := parser.DiscoverSubagents(path)
		teamSessions, _ := parser.DiscoverTeamSessions(path, chunks)
		allSubagents := append(subagents, teamSessions...)
		parser.LinkSubagents(allSubagents, chunks, path)
		return loadSessionMsg{
			messages:     chunksToMessages(chunks, allSubagents),
			path:         path,
			classified:   classified,
			offset:       offset,
			ongoing:      parser.IsOngoing(chunks),
			hasTeamTasks: len(teamSessions) > 0 || hasTeamTaskItems(chunks),
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
	case "tab":
		if m.pickerExpanded == nil {
			m.pickerExpanded = make(map[int]bool)
		}
		if m.pickerItems[m.pickerCursor].typ == pickerItemSession {
			m.pickerExpanded[m.pickerCursor] = !m.pickerExpanded[m.pickerCursor]
			m.ensurePickerVisible()
		}
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
// Uses pickerItemHeight for variable-height items (expanded previews, headers with gaps).
func (m *model) ensurePickerVisible() {
	viewHeight := m.height - 2 - statusBarHeight // header (2 lines) + status bar
	if viewHeight <= 0 {
		return
	}

	// Compute line position of cursor item.
	cursorLineStart := 0
	for i := 0; i < m.pickerCursor && i < len(m.pickerItems); i++ {
		cursorLineStart += m.pickerItemHeight(i)
	}
	cursorLineEnd := cursorLineStart
	if m.pickerCursor < len(m.pickerItems) {
		cursorLineEnd += m.pickerItemHeight(m.pickerCursor) - 1
	}

	if cursorLineStart < m.pickerScroll {
		m.pickerScroll = cursorLineStart
	}
	if cursorLineEnd >= m.pickerScroll+viewHeight {
		m.pickerScroll = cursorLineEnd - viewHeight + 1
	}
}

// updatePickerSessionState sets derived state: ongoing flag, uniform model, tick.
// Called when sessions arrive or refresh.
func (m *model) updatePickerSessionState() tea.Cmd {
	m.pickerHasOngoing = false
	for _, s := range m.pickerSessions {
		if s.IsOngoing {
			m.pickerHasOngoing = true
			break
		}
	}

	// Uniform model: all non-empty models share the same family.
	m.pickerUniformModel = true
	firstFamily := ""
	for _, s := range m.pickerSessions {
		fam := modelFamily(s.Model)
		if fam == "" {
			continue
		}
		if firstFamily == "" {
			firstFamily = fam
		} else if fam != firstFamily {
			m.pickerUniformModel = false
			break
		}
	}

	if m.pickerHasOngoing {
		return pickerTickCmd()
	}
	return nil
}

// modelFamily extracts the family name from a model string (e.g. "opus" from "claude-opus-4-6").
func modelFamily(model string) string {
	for _, fam := range []string{"opus", "sonnet", "haiku"} {
		if strings.Contains(model, fam) {
			return fam
		}
	}
	return ""
}

// pickerItemHeight returns the display height for a picker item.
// Sessions: content lines (2 base + expanded) + 1 for the separator below.
// Headers: 1 line (first) or 2 (blank + text).
func (m model) pickerItemHeight(index int) int {
	item := m.pickerItems[index]
	if item.typ == pickerItemHeader {
		if m.pickerIsFirstHeader(index) {
			return 1
		}
		return 2 // blank line + header text
	}

	contentLines := 2 // preview + metadata
	if m.pickerExpanded[index] {
		width := m.width
		if width > maxContentWidth {
			width = maxContentWidth
		}
		innerWidth := width - 4 // indent (2) + gutter (2)
		if innerWidth < 20 {
			innerWidth = 20
		}
		preview := item.session.FirstMessage
		if preview != "" {
			wrapped := wrapText(preview, innerWidth)
			contentLines += len(wrapped)
		}
	}

	return contentLines + 1 // +1 for separator below
}

// pickerIsFirstHeader returns true if index is the first header in the items list.
func (m model) pickerIsFirstHeader(index int) bool {
	for i := 0; i < index; i++ {
		if m.pickerItems[i].typ == pickerItemHeader {
			return false
		}
	}
	return true
}

// pickerTotalLines returns the total line count of all picker items.
func (m model) pickerTotalLines() int {
	total := 0
	for i := range m.pickerItems {
		total += m.pickerItemHeight(i)
	}
	return total
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
			if !m.pickerIsFirstHeader(i) {
				allLines = append(allLines, "")
			}
			allLines = append(allLines, m.renderPickerHeader(item.category, width))
		case pickerItemSession:
			isSelected := i == m.pickerCursor
			lines := m.renderPickerSession(item.session, isSelected, width, i)
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

	// Scroll position indicator.
	scrollInfo := ""
	totalLines := m.pickerTotalLines()
	if totalLines > viewHeight {
		maxScroll := totalLines - viewHeight
		if maxScroll < 0 {
			maxScroll = 0
		}
		pct := 0
		if maxScroll > 0 {
			pct = m.pickerScroll * 100 / maxScroll
		}
		if pct > 100 {
			pct = 100
		}
		scrollInfo = fmt.Sprintf("  %d%%", pct)
	}

	status := m.renderStatusBar(
		"j/k", "nav",
		"tab", "preview",
		"enter", "open",
		"G/g", "jump",
		"q/esc", "back"+scrollInfo,
	)

	return content + "\n" + status
}

// renderPickerHeader renders a date group header with underline rule.
func (m model) renderPickerHeader(category parser.DateCategory, width int) string {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorTextSecondary)
	label := labelStyle.Render(string(category))
	labelWidth := lipgloss.Width(label)

	// Thin rule extending to fill width.
	ruleLen := width - labelWidth - 3 // 2 indent + 1 space
	if ruleLen < 0 {
		ruleLen = 0
	}
	rule := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(strings.Repeat("─", ruleLen))

	return "  " + label + " " + rule
}

// renderPickerSession renders a flat session row + bottom separator.
// Selected: background highlight band. Unselected: plain text.
// Matches claude-devtools: every row has a bottom border, selected gets bg.
func (m model) renderPickerSession(s *parser.SessionInfo, isSelected bool, width int, itemIndex int) []string {
	isMicro := s.TurnCount > 0 && s.TurnCount < 3 && !isSelected
	indent := "  "
	innerWidth := width - 4 // indent (2) + right gutter (2)
	if innerWidth < 20 {
		innerWidth = 20
	}

	// --- Line 1: ongoing dot + preview text ---
	var line1Parts []string

	if s.IsOngoing {
		dotColor := ColorOngoing
		if m.pickerAnimFrame == 1 {
			dotColor = ColorOngoingDim
		}
		dot := IconLive.WithColor(dotColor)
		line1Parts = append(line1Parts, dot+" ")
	}

	preview := s.FirstMessage
	if preview == "" {
		preview = "Untitled"
	}

	previewColor := ColorTextPrimary
	if isSelected {
		previewColor = ColorTextSecondary
	}
	if isMicro {
		previewColor = ColorTextDim
	}

	previewMaxWidth := innerWidth
	if s.IsOngoing {
		previewMaxWidth -= 2
	}
	if previewMaxWidth < 20 {
		previewMaxWidth = 20
	}
	if lipgloss.Width(preview) > previewMaxWidth {
		preview = parser.TruncateWord(preview, previewMaxWidth)
	}

	line1Parts = append(line1Parts, lipgloss.NewStyle().Foreground(previewColor).Render(preview))
	line1 := indent + strings.Join(line1Parts, "")

	// --- Line 2: metadata ---
	metaColor := ColorTextMuted

	var metaParts []string
	dot := IconDot.Render()

	if s.Model != "" {
		short := shortModel(s.Model)
		mColor := modelColor(s.Model)
		if m.pickerUniformModel || isMicro {
			mColor = metaColor
		}
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(mColor).Render(short))
	}

	if s.TurnCount > 0 {
		chatIcon := IconChat.WithColor(metaColor)
		countStr := lipgloss.NewStyle().Foreground(metaColor).Render(fmt.Sprintf("%d", s.TurnCount))
		metaParts = append(metaParts, chatIcon+" "+countStr)
	}

	if s.DurationMs > 0 {
		durStr := formatSessionDuration(s.DurationMs)
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(metaColor).Render(durStr))
	}

	if s.TotalTokens > 0 {
		tokStr := formatTokens(s.TotalTokens)
		tokColor := metaColor
		if s.TotalTokens > 150_000 && !isMicro {
			tokColor = ColorTokenHigh
		}
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(tokColor).Render(tokStr))
	}

	metaLeft := indent + strings.Join(metaParts, " "+dot+" ")
	timeStr := fmt.Sprintf("%8s", relativeTime(s.ModTime))
	timeRendered := lipgloss.NewStyle().Foreground(metaColor).Render(timeStr)
	line2 := spaceBetween(metaLeft, timeRendered, width)

	lines := []string{line1, line2}

	// Tab-expanded preview.
	if m.pickerExpanded[itemIndex] && s.FirstMessage != "" {
		wrapWidth := innerWidth
		if wrapWidth < 20 {
			wrapWidth = 20
		}
		expandStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)
		wrapped := wrapText(s.FirstMessage, wrapWidth)
		for _, wl := range wrapped {
			lines = append(lines, indent+"  "+expandStyle.Render(wl))
		}
	}

	// Background highlight for selected session.
	if isSelected {
		bgStyle := lipgloss.NewStyle().Background(ColorPickerSelectedBg).Width(width)
		for i, line := range lines {
			lines[i] = bgStyle.Render(line)
		}
	}

	// Bottom separator (thin rule).
	sep := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(
		indent + strings.Repeat("─", width-4))
	lines = append(lines, sep)

	return lines
}

// wrapText breaks text into lines of at most maxWidth runes.
func wrapText(s string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{s}
	}
	var lines []string
	runes := []rune(s)
	for len(runes) > 0 {
		if len(runes) <= maxWidth {
			lines = append(lines, string(runes))
			break
		}
		// Find last space within maxWidth.
		cut := maxWidth
		for i := maxWidth; i > maxWidth-20 && i > 0; i-- {
			if runes[i] == ' ' {
				cut = i
				break
			}
		}
		lines = append(lines, string(runes[:cut]))
		runes = runes[cut:]
		// Skip leading space on next line.
		if len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	return lines
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
