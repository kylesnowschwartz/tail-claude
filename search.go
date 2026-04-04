package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// pickerSearchResultMsg delivers filtered search results to the model.
type pickerSearchResultMsg struct {
	results []pickerItem
	gen     int
}

// pickerPreviewLoadedMsg delivers a parsed session for the preview pane.
type pickerPreviewLoadedMsg struct {
	path     string
	messages []message
	gen      int
}

// pickerPreviewTickMsg fires after a debounce delay to trigger preview loading.
type pickerPreviewTickMsg struct{ gen int }

// searchSessionsCmd scans sessions for a case-insensitive query match.
// Checks metadata first (FirstMessage, Cwd, GitBranch), then falls back to
// scanning the JSONL file line by line. Returns results preserving the
// original date-group order.
func searchSessionsCmd(query string, sessions []parser.SessionInfo, gen int) tea.Cmd {
	return func() tea.Msg {
		if query == "" {
			return pickerSearchResultMsg{
				results: rebuildPickerItems(sessions),
				gen:     gen,
			}
		}

		lower := strings.ToLower(query)
		var matched []parser.SessionInfo

		for _, s := range sessions {
			if matchesSessionMetadata(s, lower) || matchesSessionContent(s.Path, lower) {
				matched = append(matched, s)
			}
		}

		results := rebuildPickerItems(matched)
		if results == nil {
			results = []pickerItem{} // empty, not nil — distinguishes "no results" from "no search"
		}
		return pickerSearchResultMsg{
			results: results,
			gen:     gen,
		}
	}
}

// matchesSessionMetadata checks if any session metadata field contains the query.
func matchesSessionMetadata(s parser.SessionInfo, lowerQuery string) bool {
	return strings.Contains(strings.ToLower(s.FirstMessage), lowerQuery) ||
		strings.Contains(strings.ToLower(s.Cwd), lowerQuery) ||
		strings.Contains(strings.ToLower(s.GitBranch), lowerQuery)
}

// matchesSessionContent scans a JSONL file for the query string in conversation
// content. Only checks user and assistant message lines — skips system entries,
// tool definitions, and other boilerplate that would produce false positives.
func matchesSessionContent(path, lowerQuery string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow up to 1MB per line (JSONL lines can be large).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		// Only scan conversation messages, not system/meta/snapshot entries.
		// Checking for type prefixes avoids parsing JSON on every line.
		if !isConversationLine(line) {
			continue
		}
		if strings.Contains(strings.ToLower(line), lowerQuery) {
			return true
		}
	}
	return false
}

// isConversationLine returns true if the JSONL line is a user or assistant
// message (the actual conversation content worth searching).
func isConversationLine(line string) bool {
	// Fast prefix check: JSONL lines start with {"type":"...
	// User messages: {"type":"user"
	// Assistant messages: {"type":"assistant"
	return strings.Contains(line[:min(30, len(line))], `"type":"user"`) ||
		strings.Contains(line[:min(35, len(line))], `"type":"assistant"`)
}

// loadPreviewCmd loads a session's messages for the preview pane.
func loadPreviewCmd(session parser.SessionInfo, gen int) tea.Cmd {
	return func() tea.Msg {
		result, err := loadSession(session.Path)
		if err != nil {
			return pickerPreviewLoadedMsg{path: session.Path, gen: gen}
		}
		return pickerPreviewLoadedMsg{
			path:     session.Path,
			messages: result.messages,
			gen:      gen,
		}
	}
}

// previewDebounceCmd returns a tick that fires after 100ms for preview debouncing.
func previewDebounceCmd(gen int) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return pickerPreviewTickMsg{gen: gen}
	})
}

// lookupPreviewCache checks the LRU cache for a previously loaded preview.
func (m model) lookupPreviewCache(path string) ([]message, bool) {
	for _, entry := range m.pickerPreviewCache {
		if entry.path == path {
			return entry.messages, true
		}
	}
	return nil, false
}

// addPreviewCache adds a preview to the LRU cache, evicting the oldest if full.
func (m *model) addPreviewCache(path string, messages []message) {
	// Remove existing entry for this path (move to front).
	for i, entry := range m.pickerPreviewCache {
		if entry.path == path {
			m.pickerPreviewCache = append(m.pickerPreviewCache[:i], m.pickerPreviewCache[i+1:]...)
			break
		}
	}

	m.pickerPreviewCache = append(m.pickerPreviewCache, previewCacheEntry{
		path:     path,
		messages: messages,
	})

	// Keep at most 5 entries.
	if len(m.pickerPreviewCache) > 5 {
		m.pickerPreviewCache = m.pickerPreviewCache[1:]
	}
}

// pickerSearchSelectedSession returns the session at the cursor in search results.
func (m model) pickerSearchSelectedSession() *parser.SessionInfo {
	items := m.activePickerItems()
	if m.pickerCursor < 0 || m.pickerCursor >= len(items) {
		return nil
	}
	item := items[m.pickerCursor]
	if item.typ != pickerItemSession {
		return nil
	}
	return item.session
}

// activePickerItems returns the appropriate item list based on search mode.
func (m model) activePickerItems() []pickerItem {
	if m.pickerSearchMode && m.pickerSearchResults != nil {
		return m.pickerSearchResults
	}
	return m.pickerItems
}

// updatePickerSearch handles key events while picker search mode is active.
// Delegates to typing or navigation sub-handler based on pickerSearchTyping.
func (m model) updatePickerSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.pickerSearchTyping {
		return m.updatePickerSearchTyping(msg)
	}
	return m.updatePickerSearchNav(msg)
}

// updatePickerSearchTyping handles keys while the search input is focused.
// All printable characters go to the query. Only esc, enter, and backspace
// are special.
func (m model) updatePickerSearchTyping(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "enter":
		// Commit query and switch to navigation mode.
		m.pickerSearchTyping = false
		cmd := m.schedulePreviewLoad()
		return m, cmd
	case "esc", "escape":
		m.pickerSearchMode = false
		m.pickerSearchTyping = false
		m.pickerSearchQuery = ""
		m.pickerSearchResults = nil
		m.pickerPreviewMessages = nil
		m.pickerPreviewPath = ""
		m.pickerPreviewLoading = false
		return m, nil
	case "backspace":
		if len(m.pickerSearchQuery) > 0 {
			m.pickerSearchQuery = m.pickerSearchQuery[:len(m.pickerSearchQuery)-1]
			m.pickerSearchGen++
			m.pickerCursor = 0
			m.pickerScroll = 0
			return m, searchSessionsCmd(m.pickerSearchQuery, m.pickerSessions, m.pickerSearchGen)
		}
		// Empty backspace exits search mode.
		m.pickerSearchMode = false
		m.pickerSearchTyping = false
		m.pickerSearchResults = nil
		m.pickerPreviewMessages = nil
		m.pickerPreviewPath = ""
		m.pickerPreviewLoading = false
		return m, nil
	case "ctrl+c":
		if m.pickerWatcher != nil {
			m.pickerWatcher.stop()
			m.pickerWatcher = nil
		}
		return m, tea.Quit
	default:
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.pickerSearchQuery += key
			m.pickerSearchGen++
			m.pickerCursor = 0
			m.pickerScroll = 0
			return m, searchSessionsCmd(m.pickerSearchQuery, m.pickerSessions, m.pickerSearchGen)
		}
	}
	return m, nil
}

// updatePickerSearchNav handles keys after the search query is committed.
// j/k navigate, r resumes, enter opens, esc returns to typing, / re-focuses input.
func (m model) updatePickerSearchNav(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "escape":
		// Return to typing mode so the user can refine the query.
		// If query is empty, exit search entirely.
		if m.pickerSearchQuery == "" {
			m.pickerSearchMode = false
			m.pickerSearchResults = nil
			m.pickerPreviewMessages = nil
			m.pickerPreviewPath = ""
			m.pickerPreviewLoading = false
			return m, nil
		}
		m.pickerSearchTyping = true
		return m, nil
	case "q":
		m.pickerSearchMode = false
		m.pickerSearchTyping = false
		m.pickerSearchQuery = ""
		m.pickerSearchResults = nil
		m.pickerPreviewMessages = nil
		m.pickerPreviewPath = ""
		m.pickerPreviewLoading = false
		return m, nil
	case "ctrl+c":
		if m.pickerWatcher != nil {
			m.pickerWatcher.stop()
			m.pickerWatcher = nil
		}
		return m, tea.Quit
	case "/":
		// Re-focus the text input for further typing.
		m.pickerSearchTyping = true
		return m, nil
	case "enter":
		if s := m.pickerSearchSelectedSession(); s != nil {
			if m.pickerWatcher != nil {
				m.pickerWatcher.stop()
				m.pickerWatcher = nil
			}
			m.pickerSearchMode = false
			m.pickerSearchTyping = false
			return m, loadSessionCmd(s.Path)
		}
	case "r":
		if s := m.pickerSearchSelectedSession(); s != nil {
			si := *s // copy for closure
			m.popup = newPopup(
				"Resume session?",
				"claude --resume "+formatSessionName(s.SessionID),
				func() (tea.Model, tea.Cmd) {
					m.resumeSession = &si
					return m, tea.Quit
				},
			)
			return m, nil
		}
	case "j", "down":
		m.pickerSearchCursorDown()
		cmd := m.schedulePreviewLoad()
		return m, cmd
	case "k", "up":
		m.pickerSearchCursorUp()
		cmd := m.schedulePreviewLoad()
		return m, cmd
	case "G":
		items := m.activePickerItems()
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].typ == pickerItemSession {
				m.pickerCursor = i
				break
			}
		}
		cmd := m.schedulePreviewLoad()
		return m, cmd
	case "g":
		items := m.activePickerItems()
		m.pickerScroll = 0
		for i, item := range items {
			if item.typ == pickerItemSession {
				m.pickerCursor = i
				break
			}
		}
		cmd := m.schedulePreviewLoad()
		return m, cmd
	}
	return m, nil
}

// pickerSearchCursorDown moves cursor to next session in the active item list.
func (m *model) pickerSearchCursorDown() {
	items := m.activePickerItems()
	for i := m.pickerCursor + 1; i < len(items); i++ {
		if items[i].typ == pickerItemSession {
			m.pickerCursor = i
			return
		}
	}
}

// pickerSearchCursorUp moves cursor to previous session in the active item list.
func (m *model) pickerSearchCursorUp() {
	items := m.activePickerItems()
	for i := m.pickerCursor - 1; i >= 0; i-- {
		if items[i].typ == pickerItemSession {
			m.pickerCursor = i
			return
		}
	}
}

// schedulePreviewLoad starts a debounced preview load for the currently
// selected session. Uses the LRU cache when available.
func (m *model) schedulePreviewLoad() tea.Cmd {
	s := m.pickerSearchSelectedSession()
	if s == nil {
		m.pickerPreviewMessages = nil
		m.pickerPreviewPath = ""
		m.pickerPreviewLoading = false
		return nil
	}

	// Already showing this session's preview.
	if s.Path == m.pickerPreviewPath && !m.pickerPreviewLoading {
		return nil
	}

	// Check cache first.
	if msgs, ok := m.lookupPreviewCache(s.Path); ok {
		m.pickerPreviewMessages = msgs
		m.pickerPreviewPath = s.Path
		m.pickerPreviewLoading = false
		return nil
	}

	// Debounce: bump generation and schedule a tick.
	m.pickerPreviewGen++
	m.pickerPreviewLoading = true
	return previewDebounceCmd(m.pickerPreviewGen)
}

// viewPickerSearch renders the search split-pane view.
// Left pane: search input + filtered session list. Right pane: session preview.
func (m model) viewPickerSearch() string {
	width := m.clampWidth()
	leftWidth := width/2 - 1
	rightWidth := width - leftWidth - 1 // 1 for the divider

	// --- Left pane: filtered session list ---
	items := m.activePickerItems()

	// --- Header: search input + result count ---
	queryDisplay := m.pickerSearchQuery
	resultCount := 0
	for _, item := range items {
		if item.typ == pickerItemSession {
			resultCount++
		}
	}

	var header string
	if m.pickerSearchTyping {
		cursor := StyleAccentBold.Render("\u2588") // block cursor
		header = StyleAccentBold.Render("/") + " " + queryDisplay + cursor
	} else {
		countStr := StyleDim.Render(fmt.Sprintf("(%d results)", resultCount))
		header = StyleAccentBold.Render("/") + " " +
			StyleSearchHighlight.Render(queryDisplay) + " " + countStr
	}

	leftLines := m.renderSearchPickerItems(items, leftWidth)

	// --- Right pane: preview or loading ---
	var rightLines []string
	if m.pickerPreviewLoading {
		frame := SpinnerFrames[m.pickerAnimFrame%len(SpinnerFrames)]
		rightLines = []string{StyleDim.Render(frame + " Loading preview...")}
	} else if len(m.pickerPreviewMessages) > 0 {
		rightLines = m.renderPreviewPane(rightWidth)
	} else if m.pickerSearchSelectedSession() != nil {
		rightLines = []string{StyleDim.Render("No preview available")}
	}

	// --- Compose split view ---
	viewHeight := m.contentHeight(1, 0) // 1 for header
	divider := StyleMuted.Render("\u2502")

	// Pad/truncate both panes to viewHeight.
	leftLines = padLines(leftLines, viewHeight)
	rightLines = padLines(rightLines, viewHeight)

	// Scroll left pane.
	if m.pickerScroll > 0 && m.pickerScroll < len(leftLines) {
		leftLines = leftLines[m.pickerScroll:]
	}
	if len(leftLines) > viewHeight {
		leftLines = leftLines[:viewHeight]
	}
	leftLines = padLines(leftLines, viewHeight)

	// Truncate right pane.
	if len(rightLines) > viewHeight {
		rightLines = rightLines[:viewHeight]
	}
	rightLines = padLines(rightLines, viewHeight)

	// Join side by side.
	var combined []string
	for i := 0; i < viewHeight; i++ {
		left := truncateToWidth(leftLines[i], leftWidth)
		left = padToWidth(left, leftWidth)
		right := truncateToWidth(rightLines[i], rightWidth)
		combined = append(combined, left+divider+right)
	}

	// Footer varies by sub-mode.
	var footerPairs []string
	if m.pickerSearchTyping {
		footerPairs = []string{
			"enter", "search",
			"esc", "cancel",
		}
	} else {
		footerPairs = []string{
			"j/k", "nav",
			"enter", "open",
			"r", "resume",
			"/", "edit query",
			"esc", "back",
			"q", "close",
		}
	}

	return (screenLayout{
		header:  header,
		lines:   combined,
		footer:  m.renderFooter(footerPairs...),
		screenH: m.height,
		width:   m.width,
		cw:      width,
	}).assemble()
}

// renderSearchPickerItems renders picker items for the left pane of search view.
// Simplified version of renderPickerItems with narrower width.
func (m model) renderSearchPickerItems(items []pickerItem, width int) []string {
	if len(items) == 0 {
		if m.pickerSearchQuery != "" {
			return []string{StyleDim.Render("  No results")}
		}
		return []string{StyleDim.Render("  No sessions")}
	}

	var lines []string
	for i, item := range items {
		switch item.typ {
		case pickerItemHeader:
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, m.renderPickerHeader(item.category, width))
			lines = append(lines, "")
		case pickerItemSession:
			isSelected := i == m.pickerCursor
			lines = append(lines, m.renderSearchPickerSession(item.session, isSelected, width)...)
		}
	}
	return lines
}

// renderSearchPickerSession renders a compact session row for the search left pane.
// Two lines: preview text + metadata, plus a separator.
func (m model) renderSearchPickerSession(s *parser.SessionInfo, isSelected bool, width int) []string {
	indent := "  "
	innerWidth := max(width-4, 20)

	// Line 1: preview text with query highlighting
	preview := s.FirstMessage
	if preview == "" {
		preview = "Untitled"
	}
	if lipgloss.Width(preview) > innerWidth {
		preview = parser.TruncateWord(preview, innerWidth)
	}

	// Highlight query matches before applying foreground style so the highlight
	// background stands out against the normal text color.
	highlighted := highlightQuery(preview, m.pickerSearchQuery)

	previewColor := ColorTextPrimary
	if isSelected {
		previewColor = ColorTextSecondary
	}
	previewStyle := lipgloss.NewStyle().Foreground(previewColor)
	if isSelected {
		previewStyle = previewStyle.Background(ColorPickerSelectedBg)
	}
	line1 := indent + previewStyle.Render(highlighted)

	// Line 2: compact metadata with git branch (may match query)
	metaColor := ColorTextMuted
	var metaParts []string
	if s.Model != "" {
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(metaColor).Render(shortModel(s.Model)))
	}
	if s.GitBranch != "" {
		branch := s.GitBranch
		if len(branch) > 20 {
			branch = branch[:17] + "..."
		}
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(metaColor).Render(
			highlightQuery(branch, m.pickerSearchQuery)))
	}
	// Show cwd when the query matched it (otherwise it's noise).
	if s.Cwd != "" && m.pickerSearchQuery != "" &&
		strings.Contains(strings.ToLower(s.Cwd), strings.ToLower(m.pickerSearchQuery)) {
		cwd := s.Cwd
		if len(cwd) > 30 {
			cwd = "..." + cwd[len(cwd)-27:]
		}
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(metaColor).Render(
			highlightQuery(cwd, m.pickerSearchQuery)))
	}
	if s.TurnCount > 0 {
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(metaColor).Render(fmt.Sprintf("%d turns", s.TurnCount)))
	}
	metaParts = append(metaParts, lipgloss.NewStyle().Foreground(metaColor).Render(relativeTime(s.ModTime)))
	line2 := indent + strings.Join(metaParts, StyleMuted.Render(" "+Icon.Dot.Glyph+" "))

	lines := []string{line1, line2}

	if isSelected {
		bgStyle := lipgloss.NewStyle().Background(ColorPickerSelectedBg).Width(width)
		lines[0] = bgStyle.Render(lines[0])
	}

	// Separator
	sep := StyleMuted.Render(indent + strings.Repeat("\u2500", max(width-4, 0)))
	lines = append(lines, sep)

	return lines
}

// renderPreviewPane renders the right pane content from the preview messages.
// Uses the existing message rendering but at the narrower preview width.
func (m model) renderPreviewPane(width int) []string {
	var lines []string
	for i, msg := range m.pickerPreviewMessages {
		r := m.renderMessage(msg, width, false, false)
		lines = append(lines, strings.Split(r.content, "\n")...)
		if i < len(m.pickerPreviewMessages)-1 {
			lines = append(lines, "")
		}
	}
	return lines
}

// highlightQuery wraps case-insensitive occurrences of query in the search
// highlight style. Operates on plain text only (ANSI sequences will break
// matching — call before styling the string).
func highlightQuery(text, query string) string {
	if query == "" {
		return text
	}
	lower := strings.ToLower(text)
	lowerQ := strings.ToLower(query)
	qLen := len(lowerQ)

	var b strings.Builder
	b.Grow(len(text) + len(text)/4) // rough estimate
	pos := 0
	for {
		idx := strings.Index(lower[pos:], lowerQ)
		if idx < 0 {
			b.WriteString(text[pos:])
			break
		}
		// Write text before match.
		b.WriteString(text[pos : pos+idx])
		// Write highlighted match (using the original case from text).
		b.WriteString(StyleSearchHighlight.Render(text[pos+idx : pos+idx+qLen]))
		pos += idx + qLen
	}
	return b.String()
}

// padLines pads or truncates a slice of lines to exactly n lines.
func padLines(lines []string, n int) []string {
	if len(lines) >= n {
		return lines[:n]
	}
	result := make([]string, n)
	copy(result, lines)
	return result
}

// truncateToWidth truncates a string to at most w visible columns, preserving ANSI.
func truncateToWidth(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

// padToWidth pads a string with spaces to exactly w visible columns.
func padToWidth(s string, w int) string {
	vis := lipgloss.Width(s)
	if vis >= w {
		return s
	}
	return s + strings.Repeat(" ", w-vis)
}
