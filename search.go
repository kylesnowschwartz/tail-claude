package main

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// searchState is the picker search UI mode. A single enum (rather than two
// bools) makes the invalid "typing but not in search" combination
// unrepresentable and gives every exit path one field to reset.
type searchState int

const (
	searchOff    searchState = iota // search inactive; picker shows the full list
	searchTyping                    // text input focused; printable keys edit the query
	searchNav                       // query committed; j/k navigate results
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

// pickerSearchTickMsg fires after a debounce delay to trigger a content scan.
type pickerSearchTickMsg struct{ gen int }

// searchDebounceCmd returns a tick that fires after 150ms for search
// debouncing, so rapid keystrokes don't each spawn a full-corpus scan.
func searchDebounceCmd(gen int) tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return pickerSearchTickMsg{gen: gen}
	})
}

// bumpSearchGen invalidates pending and in-flight search work. The atomic
// mirror is shared with running scan goroutines: a copied gen alone can only
// discard a stale result after the scan finishes, whereas the mirror lets the
// scan observe the bump mid-run and bail between files.
func (m *model) bumpSearchGen() {
	m.pickerSearchGen++
	if m.pickerSearchLiveGen != nil {
		m.pickerSearchLiveGen.Store(int64(m.pickerSearchGen))
	}
}

// searchSessionsCmd scans sessions for a case-insensitive query match.
// Checks metadata first (FirstMessage, Cwd, GitBranch), then falls back to
// scanning the JSONL file line by line. Returns results preserving the
// original date-group order.
func searchSessionsCmd(query string, sessions []parser.SessionInfo, gen int, liveGen *atomic.Int64) tea.Cmd {
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
			// Bail between files once a newer generation owns the results;
			// the discarded partial scan would fail the staleness check anyway.
			if liveGen != nil && int(liveGen.Load()) != gen {
				return nil
			}
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
	return strings.Contains(strings.ToLower(s.Title), lowerQuery) ||
		strings.Contains(strings.ToLower(s.FirstMessage), lowerQuery) ||
		strings.Contains(strings.ToLower(s.LastPrompt), lowerQuery) ||
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

	found := false
	// parser.ScanLines skips oversized lines instead of aborting the scan,
	// so one huge line (e.g. pasted image data) can't hide later matches.
	// A mid-file read error just means we searched what we could.
	_ = parser.ScanLines(f, func(line string) bool {
		// Only scan conversation messages, not system/meta/snapshot entries.
		// Checking for type markers avoids parsing JSON on every line.
		if !isConversationLine(line) {
			return true
		}
		if strings.Contains(strings.ToLower(line), lowerQuery) {
			found = true
			return false
		}
		return true
	})
	return found
}

// isConversationLine returns true if the JSONL line is a user or assistant
// message (the actual conversation content worth searching).
func isConversationLine(line string) bool {
	// Match anywhere in the line: current sessions put the type field ~100+
	// bytes in (after parentUuid, isSidechain, ...), older sessions order
	// fields differently, so a fixed prefix window misses both.
	return strings.Contains(line, `"type":"user"`) ||
		strings.Contains(line, `"type":"assistant"`)
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
	if m.pickerSearchState != searchOff && m.pickerSearchResults != nil {
		return m.pickerSearchResults
	}
	return m.pickerItems
}

// exitPickerSearch leaves search mode and clears all query, result, and
// preview state. Bumps both generation counters so in-flight scans and
// preview loads land stale instead of mutating the pane after it's gone.
func (m *model) exitPickerSearch() {
	m.pickerSearchState = searchOff
	m.pickerSearchQuery = ""
	m.pickerSearchResults = nil
	m.pickerPreviewMessages = nil
	m.pickerPreviewPath = ""
	m.pickerPreviewLoading = false
	m.bumpSearchGen()
	m.pickerPreviewGen++
}

// updatePickerSearch handles key events while picker search mode is active.
// Delegates to typing or navigation sub-handler based on pickerSearchState.
func (m model) updatePickerSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.pickerSearchState == searchTyping {
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
		m.pickerSearchState = searchNav
		cmd := m.schedulePreviewLoad()
		return m, cmd
	case "esc", "escape":
		m.exitPickerSearch()
		return m, nil
	case "backspace":
		if len(m.pickerSearchQuery) > 0 {
			m.pickerSearchQuery = m.pickerSearchQuery[:len(m.pickerSearchQuery)-1]
			m.bumpSearchGen()
			m.pickerCursor = 0
			m.pickerScroll = 0
			return m, searchDebounceCmd(m.pickerSearchGen)
		}
		// Empty backspace exits search mode.
		m.exitPickerSearch()
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
			m.bumpSearchGen()
			m.pickerCursor = 0
			m.pickerScroll = 0
			return m, searchDebounceCmd(m.pickerSearchGen)
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
			m.exitPickerSearch()
			return m, nil
		}
		m.pickerSearchState = searchTyping
		return m, nil
	case "q":
		m.exitPickerSearch()
		return m, nil
	case "ctrl+c":
		if m.pickerWatcher != nil {
			m.pickerWatcher.stop()
			m.pickerWatcher = nil
		}
		return m, tea.Quit
	case "/":
		// Re-focus the text input for further typing.
		m.pickerSearchState = searchTyping
		return m, nil
	case "enter":
		if s := m.pickerSearchSelectedSession(); s != nil {
			if m.pickerWatcher != nil {
				m.pickerWatcher.stop()
				m.pickerWatcher = nil
			}
			m.exitPickerSearch()
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
		m.pickerCursorDown()
		m.ensureSearchPickerVisible()
		cmd := m.schedulePreviewLoad()
		return m, cmd
	case "k", "up":
		m.pickerCursorUp()
		m.ensureSearchPickerVisible()
		cmd := m.schedulePreviewLoad()
		return m, cmd
	case "G":
		m.pickerCursorLast()
		m.ensureSearchPickerVisible()
		cmd := m.schedulePreviewLoad()
		return m, cmd
	case "g":
		m.pickerCursorFirst()
		m.ensureSearchPickerVisible()
		cmd := m.schedulePreviewLoad()
		return m, cmd
	}
	return m, nil
}

// searchPickerItemHeight returns the rendered line count of an item in the
// search left pane, mirroring renderSearchPickerItems: headers are a text line
// plus a trailing blank (plus a leading blank when not first); sessions are
// preview + metadata + separator. pickerItemHeight can't be used here — it
// indexes m.pickerItems, which is the unfiltered list.
func searchPickerItemHeight(index int, typ pickerItemType) int {
	if typ == pickerItemHeader && index == 0 {
		return 2
	}
	return 3
}

// searchPickerTotalLines returns the total rendered line count of the active
// (possibly filtered) item list in the search left pane.
func (m model) searchPickerTotalLines() int {
	items := m.activePickerItems()
	total := 0
	for i, item := range items {
		total += searchPickerItemHeight(i, item.typ)
	}
	return total
}

// ensureSearchPickerVisible adjusts pickerScroll so the cursor stays inside
// the search left pane viewport. Search-mode counterpart of
// ensurePickerVisible, computed against the filtered active item list.
func (m *model) ensureSearchPickerVisible() {
	items := m.activePickerItems()
	viewHeight := m.contentHeight(1, 0)

	cursorLineStart := 0
	for i := 0; i < m.pickerCursor && i < len(items); i++ {
		cursorLineStart += searchPickerItemHeight(i, items[i].typ)
	}
	cursorLineEnd := cursorLineStart
	if m.pickerCursor >= 0 && m.pickerCursor < len(items) {
		cursorLineEnd += searchPickerItemHeight(m.pickerCursor, items[m.pickerCursor].typ) - 1
	}

	if cursorLineStart < m.pickerScroll {
		m.pickerScroll = cursorLineStart
	}
	if cursorLineEnd >= m.pickerScroll+viewHeight {
		m.pickerScroll = cursorLineEnd - viewHeight + 1
	}
}

// schedulePreviewLoad starts a debounced preview load for the currently
// selected session. Uses the LRU cache when available.
func (m *model) schedulePreviewLoad() tea.Cmd {
	s := m.pickerSearchSelectedSession()
	if s == nil {
		// Bump the gen so an in-flight load for a previous selection can't
		// land later and repopulate the pane we just cleared.
		m.pickerPreviewGen++
		m.pickerPreviewMessages = nil
		m.pickerPreviewPath = ""
		m.pickerPreviewLoading = false
		return nil
	}

	// Already showing this session's preview.
	if s.Path == m.pickerPreviewPath && !m.pickerPreviewLoading {
		return nil
	}

	// Check cache first. Bump the gen so an in-flight load for a previously
	// selected session can't land later and overwrite this cached preview.
	if msgs, ok := m.lookupPreviewCache(s.Path); ok {
		m.pickerPreviewGen++
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

// searchKeybindPairs returns the search split-pane's footer pairs, branching
// on typing sub-mode. Shared by viewPickerSearch and footerHeight (via
// pickerKeybindPairs) so the measured bar matches the drawn one.
func (m model) searchKeybindPairs() []string {
	if m.pickerSearchState == searchTyping {
		return []string{
			"enter", "search",
			"esc", "cancel",
		}
	}
	return []string{
		"j/k", "nav",
		"enter", "open",
		"r", "resume",
		"/", "edit query",
		"esc", "back",
		"q", "close",
	}
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
	if m.pickerSearchState == searchTyping {
		cursor := StyleAccentBold.Render("\u2588") // block cursor
		header = StyleAccentBold.Render("/") + " " + queryDisplay + cursor
	} else {
		countStr := StyleDim.Render(fmt.Sprintf("(%d results)", resultCount))
		header = StyleAccentBold.Render("/") + " " +
			StyleSearchHighlight.Render(queryDisplay) + " " + countStr
	}

	leftLines := m.renderSearchPickerItems(items, leftWidth)

	viewHeight := m.contentHeight(1, 0) // 1 for header

	// --- Right pane: preview or loading ---
	var rightLines []string
	if m.pickerPreviewLoading {
		frame := SpinnerFrames[m.pickerAnimFrame%len(SpinnerFrames)]
		rightLines = []string{StyleDim.Render(frame + " Loading preview...")}
	} else if len(m.pickerPreviewMessages) > 0 {
		rightLines = m.renderPreviewPane(rightWidth, viewHeight)
	} else if m.pickerSearchSelectedSession() != nil {
		rightLines = []string{StyleDim.Render("No preview available")}
	}

	// --- Compose split view ---
	divider := StyleMuted.Render("\u2502")

	// Scroll the left pane before padding — padLines truncates, so padding
	// first would discard everything past the first screenful.
	leftLines = padLines(scrollWindow(leftLines, viewHeight, m.pickerScroll), viewHeight)
	rightLines = padLines(rightLines, viewHeight)

	// Join side by side.
	var combined []string
	for i := 0; i < viewHeight; i++ {
		left := truncateToWidth(leftLines[i], leftWidth)
		left = padToWidth(left, leftWidth)
		right := truncateToWidth(rightLines[i], rightWidth)
		combined = append(combined, left+divider+right)
	}

	return (screenLayout{
		header:  header,
		lines:   combined,
		footer:  m.renderFooter(m.searchKeybindPairs()...),
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

	// Line 1: title or first message with query highlighting
	preview := s.FirstMessage
	if s.Title != "" {
		preview = s.Title
	}
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
		branch := parser.Truncate(s.GitBranch, 20)
		metaParts = append(metaParts, lipgloss.NewStyle().Foreground(metaColor).Render(
			highlightQuery(branch, m.pickerSearchQuery)))
	}
	// Show cwd when the query matched it (otherwise it's noise).
	if s.Cwd != "" && m.pickerSearchQuery != "" &&
		strings.Contains(strings.ToLower(s.Cwd), strings.ToLower(m.pickerSearchQuery)) {
		cwd := s.Cwd
		// Keep the tail of the path (most specific part), cutting on rune
		// boundaries so multi-byte characters don't render as mojibake.
		if r := []rune(cwd); len(r) > 30 {
			cwd = "..." + string(r[len(r)-27:])
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
// Uses the existing message rendering but at the narrower preview width. The
// pane is never scrolled, so rendering stops once maxLines lines exist rather
// than paying a markdown render for every message in the session. Results are
// memoized in pickerPreviewRender keyed by (path, width) — View runs on every
// keystroke and spinner tick, so an uncached pass per call is the hot path.
func (m model) renderPreviewPane(width, maxLines int) []string {
	c := m.pickerPreviewRender
	if c != nil && c.path == m.pickerPreviewPath && c.width == width &&
		(c.complete || len(c.lines) >= maxLines) {
		return c.lines
	}

	var lines []string
	complete := true
	for i, msg := range m.pickerPreviewMessages {
		r := m.renderMessage(msg, width, false, false)
		lines = append(lines, strings.Split(r.content, "\n")...)
		if len(lines) >= maxLines {
			complete = i == len(m.pickerPreviewMessages)-1
			break
		}
		if i < len(m.pickerPreviewMessages)-1 {
			lines = append(lines, "")
		}
	}

	if c != nil {
		*c = previewRenderCache{
			path:     m.pickerPreviewPath,
			width:    width,
			lines:    lines,
			complete: complete,
		}
	}
	return lines
}

// highlightQuery wraps case-insensitive occurrences of query in the search
// highlight style. Operates on plain text only (ANSI sequences will break
// matching — call before styling the string).
func highlightQuery(text, query string) string {
	return highlightMatches(text, query, func(s string) string { return s }, StyleSearchHighlight)
}

// highlightMatches renders every case-insensitive occurrence of query in hl
// and each unmatched segment through base. Matching runs on rune indices, not
// byte offsets: lowercasing can change a rune's byte length (U+0130 "İ" is
// 2 bytes but lowers to 1-byte "i"), so offsets found in the lowered string
// can't safely slice the original.
func highlightMatches(text, query string, base func(string) string, hl lipgloss.Style) string {
	if query == "" {
		return base(text)
	}
	runes := []rune(text)
	// unicode.ToLower is a 1:1 rune mapping (same as strings.ToLower), so
	// lower and runes stay index-aligned.
	lower := make([]rune, len(runes))
	for i, r := range runes {
		lower[i] = unicode.ToLower(r)
	}
	lowerQ := []rune(strings.ToLower(query))

	var b strings.Builder
	b.Grow(len(text) + len(text)/4) // rough estimate incl. styling overhead
	pos := 0
	for {
		idx := indexRunes(lower[pos:], lowerQ)
		if idx < 0 {
			break
		}
		if idx > 0 {
			b.WriteString(base(string(runes[pos : pos+idx])))
		}
		// Highlight the match using the original case from text.
		b.WriteString(hl.Render(string(runes[pos+idx : pos+idx+len(lowerQ)])))
		pos += idx + len(lowerQ)
	}
	if pos < len(runes) {
		b.WriteString(base(string(runes[pos:])))
	}
	return b.String()
}

// indexRunes returns the rune index of the first occurrence of q in s, or -1.
func indexRunes(s, q []rune) int {
	for i := 0; i+len(q) <= len(s); i++ {
		match := true
		for j := range q {
			if s[i+j] != q[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
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
