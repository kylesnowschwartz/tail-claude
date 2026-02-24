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
	loadResult
	err error
}

// pickerTickMsg drives the ongoing spinner animation (100ms interval).
type pickerTickMsg time.Time

func pickerTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return pickerTickMsg(t)
	})
}

// loadPickerSessionsCmd discovers sessions across the given project directories
// (main repo + worktree dirs). When cache is non-nil, unchanged files return
// cached metadata.
func loadPickerSessionsCmd(projectDirs []string, cache *parser.SessionCache) tea.Cmd {
	return func() tea.Msg {
		var sessions []parser.SessionInfo
		var err error
		if cache != nil {
			sessions, err = cache.DiscoverAllProjectSessions(projectDirs)
		} else {
			sessions, err = parser.DiscoverAllProjectSessions(projectDirs)
		}
		return pickerSessionsMsg{sessions: sessions, err: err}
	}
}

// loadSessionCmd returns a command that loads a session file into messages.
// Delegates to loadSession so the parsing pipeline lives in one place.
func loadSessionCmd(path string) tea.Cmd {
	return func() tea.Msg {
		result, err := loadSession(path)
		if err != nil {
			return loadSessionMsg{err: err}
		}
		return loadSessionMsg{loadResult: result}
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
	case "q", "esc", "escape", "backspace":
		if m.pickerWatcher != nil {
			m.pickerWatcher.stop()
			m.pickerWatcher = nil
		}
		// No session loaded — nothing to go back to, so quit.
		if len(m.messages) == 0 {
			return m, tea.Quit
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
	case "?":
		m.showKeybinds = !m.showKeybinds
		m.ensurePickerVisible()
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
	viewHeight := m.pickerViewHeight()

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
// Called when sessions arrive or refresh. Uses the same rising/falling-edge
// grace period as the main session watcher to avoid spinner churn when a
// session briefly appears not-ongoing between API round-trips.
func (m *model) updatePickerSessionState() tea.Cmd {
	hadOngoing := m.pickerHasOngoing
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
		// Rising edge: cancel any pending grace timer, start tick if not running.
		m.pickerOngoingGraceSeq++
		if !m.pickerTickActive {
			m.pickerTickActive = true
			return pickerTickCmd()
		}
	} else if hadOngoing && m.pickerTickActive {
		// Falling edge: don't stop immediately — start grace period so the
		// spinner stays visible across short gaps between tool-call round-trips.
		m.pickerOngoingGraceSeq++
		return pickerOngoingGraceCmd(m.pickerOngoingGraceSeq)
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
			return 2
		}
		return 3 // blank line + header text
	}

	contentLines := 2 // preview + metadata
	if m.pickerExpanded[index] {
		width := m.clampWidth()
		innerWidth := max(width-4, 20) // indent (2) + gutter (2)
		preview := item.session.FirstMessage
		if preview != "" {
			wrapped := wrapText(preview, innerWidth)
			contentLines += len(wrapped)
		}
	}

	return contentLines + 1 // +1 for separator below
}

// pickerIsFirstHeader returns true if index is the first header in the items list.
// rebuildPickerItems always starts with a header, so this reduces to index == 0.
func (m model) pickerIsFirstHeader(index int) bool {
	return index == 0
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
	width := m.clampWidth()

	// Header
	header := StyleAccentBold.Render("Sessions") + " " +
		StyleDim.Render(fmt.Sprintf("(%d)", len(m.pickerSessions)))
	header += "\n"

	// Empty state
	if len(m.pickerItems) == 0 {
		return header + "\n" + StyleDim.Render("No sessions found for this project.")
	}

	allLines := m.renderPickerItems(width)
	visible := scrollWindow(allLines, m.pickerViewHeight(), m.pickerScroll)

	content := header + "\n" + strings.Join(visible, "\n")

	// Center content within the terminal when wider than the content cap.
	content = centerBlock(content, width, m.width)

	// Pad to fill viewport so footer stays at bottom.
	targetLines := m.height - m.footerHeight()
	renderedLines := strings.Count(content, "\n") + 1
	if renderedLines < targetLines {
		content += strings.Repeat("\n", targetLines-renderedLines)
	}

	// Scroll position indicator.
	scrollInfo := m.pickerScrollInfo()

	footer := m.renderFooter(
		"j/k", "nav",
		"tab", "preview",
		"enter", "open",
		"G/g", "jump",
		"q/esc", "back"+scrollInfo,
		"?", "keys",
	)

	return content + "\n" + footer
}

// renderPickerItems renders all picker items (headers + sessions) into lines.
func (m model) renderPickerItems(width int) []string {
	var lines []string
	for i, item := range m.pickerItems {
		switch item.typ {
		case pickerItemHeader:
			if !m.pickerIsFirstHeader(i) {
				lines = append(lines, "")
			}
			lines = append(lines, m.renderPickerHeader(item.category, width))
			lines = append(lines, "")
		case pickerItemSession:
			isSelected := i == m.pickerCursor
			lines = append(lines, m.renderPickerSession(item.session, isSelected, width, i)...)
		}
	}
	return lines
}

// scrollWindow returns a slice of lines visible within a viewport.
func scrollWindow(lines []string, viewHeight, scroll int) []string {
	start := scroll
	if start > len(lines) {
		start = len(lines)
	}
	visible := lines[start:]
	if len(visible) > viewHeight {
		visible = visible[:viewHeight]
	}
	return visible
}

// pickerScrollInfo returns a scroll percentage string, or "" if all content fits.
func (m model) pickerScrollInfo() string {
	viewHeight := m.pickerViewHeight()
	totalLines := m.pickerTotalLines()
	if totalLines <= viewHeight {
		return ""
	}
	maxScroll := totalLines - viewHeight
	if maxScroll <= 0 {
		return ""
	}
	pct := m.pickerScroll * 100 / maxScroll
	if pct > 100 {
		pct = 100
	}
	return fmt.Sprintf("  %d%%", pct)
}

// renderPickerHeader renders a date group header with underline rule.
func (m model) renderPickerHeader(category parser.DateCategory, width int) string {
	labelStyle := StyleSecondaryBold
	label := labelStyle.Render(string(category))
	labelWidth := lipgloss.Width(label)

	// Thin rule extending to fill width.
	ruleLen := width - labelWidth - 3 // 2 indent + 1 space
	if ruleLen < 0 {
		ruleLen = 0
	}
	rule := StyleMuted.Render(strings.Repeat("─", ruleLen))

	return "  " + label + " " + rule
}

// renderPickerSession renders a flat session row + bottom separator.
// Selected: background highlight band. Unselected: plain text.
// Matches claude-devtools: every row has a bottom border, selected gets bg.
func (m model) renderPickerSession(s *parser.SessionInfo, isSelected bool, width int, itemIndex int) []string {
	indent := "  "
	innerWidth := max(width-4, 20) // indent (2) + right gutter (2)

	// --- Line 1: ongoing dot + preview text ---
	var line1Parts []string

	if s.IsOngoing {
		frame := SpinnerFrames[m.pickerAnimFrame%len(SpinnerFrames)]
		spinStyle := lipgloss.NewStyle().Foreground(ColorOngoing)
		if isSelected {
			spinStyle = spinStyle.Background(ColorPickerSelectedBg)
		}
		// Render glyph+space together so the background spans both.
		line1Parts = append(line1Parts, spinStyle.Render(frame+" "))
	}

	preview := s.FirstMessage
	if preview == "" {
		preview = "Untitled"
	}

	previewColor := ColorTextPrimary
	if isSelected {
		previewColor = ColorTextSecondary
	}

	previewMaxWidth := innerWidth
	if s.IsOngoing {
		previewMaxWidth -= 2
	}
	previewMaxWidth = max(previewMaxWidth, 20)
	if lipgloss.Width(preview) > previewMaxWidth {
		preview = parser.TruncateWord(preview, previewMaxWidth)
	}

	// Bake background into preview when selected; prevents ANSI reset from the
	// spinner glyph's closing sequence from stripping the highlight off this text.
	previewStyle := lipgloss.NewStyle().Foreground(previewColor)
	if isSelected {
		previewStyle = previewStyle.Background(ColorPickerSelectedBg)
	}
	line1Parts = append(line1Parts, previewStyle.Render(preview))
	line1 := indent + strings.Join(line1Parts, "")

	// --- Line 2: metadata ---
	metaColor := ColorTextMuted

	var metaParts []string
	dot := IconDot.Render()

	if s.Model != "" {
		short := fmt.Sprintf("%-10s", shortModel(s.Model))
		mColor := modelColor(s.Model)
		if m.pickerUniformModel {
			mColor = metaColor
		}
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(mColor).Render(short))
	}

	if s.TurnCount > 0 {
		chatIcon := IconChat.WithColor(metaColor)
		countStr := lipgloss.NewStyle().Foreground(metaColor).Render(fmt.Sprintf("%3d", s.TurnCount))
		metaParts = append(metaParts, chatIcon+" "+countStr)
	}

	if s.DurationMs > 0 {
		durStr := fmt.Sprintf("%4s", formatSessionDuration(s.DurationMs))
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(metaColor).Render(durStr))
	}

	if s.TotalTokens > 0 {
		tokStr := fmt.Sprintf("%6s", formatTokens(s.TotalTokens))
		tokColor := metaColor
		if s.TotalTokens > 150_000 {
			tokColor = ColorTokenHigh
		}
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(tokColor).Render(tokStr))
	}

	if s.SessionID != "" {
		sessionIcon := IconSession.WithColor(metaColor)
		nameStr := lipgloss.NewStyle().Foreground(metaColor).Render(formatSessionName(s.SessionID))
		metaParts = append(metaParts, sessionIcon+" "+nameStr)
	}

	metaLeft := indent + strings.Join(metaParts, " "+dot+" ")
	timeStr := fmt.Sprintf("%8s", relativeTime(s.ModTime))
	timeRendered := lipgloss.NewStyle().Foreground(metaColor).Render(timeStr)
	line2 := spaceBetween(metaLeft, timeRendered, width)

	lines := []string{line1, line2}

	// Tab-expanded preview.
	if m.pickerExpanded[itemIndex] && s.FirstMessage != "" {
		wrapWidth := max(innerWidth, 20)
		expandStyle := StyleSecondary
		wrapped := wrapText(s.FirstMessage, wrapWidth)
		for _, wl := range wrapped {
			lines = append(lines, indent+"  "+expandStyle.Render(wl))
		}
	}

	// Background highlight for selected session (preview line only).
	if isSelected {
		bgStyle := lipgloss.NewStyle().Background(ColorPickerSelectedBg).Width(width)
		lines[0] = bgStyle.Render(lines[0])
	}

	// Bottom separator (thin rule).
	sep := StyleMuted.Render(
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
