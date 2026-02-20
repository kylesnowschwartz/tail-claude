package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Message roles
const (
	RoleClaude = "claude"
	RoleUser   = "user"
	RoleSystem = "system"
)

// View states
type viewState int

const (
	viewList   viewState = iota // message list (main view)
	viewDetail                  // full-screen single message
	viewPicker                  // session picker
)

// displayItem is a structured element within an AI message's detail view.
// Mirrors parser.DisplayItem but with pre-formatted fields for rendering.
type displayItem struct {
	itemType    parser.DisplayItemType
	text        string
	toolName    string
	toolSummary string
	toolInput   string // formatted JSON for display
	toolResult  string
	toolError   bool
	durationMs  int64
	tokenCount  int
}

type message struct {
	role          string
	model         string
	content       string
	thinkingCount int
	toolCallCount int
	messages      int
	tokensRaw     int
	durationMs    int64
	timestamp     string
	items         []displayItem
}

type model struct {
	messages     []message
	expanded     map[int]bool // which messages are expanded
	cursor       int          // selected message index
	width        int
	height       int
	scroll       int
	lineOffsets  []int // starting line of each message in rendered output
	messageLines []int // number of rendered lines per message

	totalRenderedLines int // total lines in list view, updated by computeLineOffsets

	// Detail view state
	view            viewState
	detailScroll    int          // scroll offset within the detail view
	detailMaxScroll int          // cached max scroll for detail view, updated on enter/resize
	detailCursor    int          // selected item index within the detail message
	detailExpanded  map[int]bool // which detail items are expanded

	// Markdown rendering
	md *mdRenderer

	// Live tailing state
	sessionPath string
	watching    bool
	watcher     *sessionWatcher
	tailSub     chan []message
	tailErrc    chan error

	// Session picker state
	pickerSessions []parser.SessionInfo
	pickerCursor   int
	pickerScroll   int
}

// loadResult holds everything needed to bootstrap the TUI and watcher.
type loadResult struct {
	messages   []message
	path       string
	classified []parser.ClassifiedMsg
	offset     int64
}

// loadSession reads a JSONL session file and converts chunks to display messages.
// Auto-discovers the latest session when path is empty. Returns the full load
// result so the caller can hand off classified messages and offset to the watcher.
func loadSession(path string) (loadResult, error) {
	if path == "" {
		discovered, err := parser.DiscoverLatestSession()
		if err != nil {
			return loadResult{}, fmt.Errorf("no session found: %w", err)
		}
		path = discovered
	}

	classified, offset, err := parser.ReadSessionIncremental(path, 0)
	if err != nil {
		return loadResult{}, fmt.Errorf("reading session %s: %w", path, err)
	}

	chunks := parser.BuildChunks(classified)
	if len(chunks) == 0 {
		return loadResult{}, fmt.Errorf("session %s has no messages", path)
	}

	return loadResult{
		messages:   chunksToMessages(chunks),
		path:       path,
		classified: classified,
		offset:     offset,
	}, nil
}

// chunksToMessages maps parser output into the TUI's message type.
func chunksToMessages(chunks []parser.Chunk) []message {
	msgs := make([]message, 0, len(chunks))
	for _, c := range chunks {
		switch c.Type {
		case parser.UserChunk:
			msgs = append(msgs, message{
				role:      RoleUser,
				content:   c.UserText,
				timestamp: formatTime(c.Timestamp),
			})
		case parser.AIChunk:
			msgs = append(msgs, message{
				role:          RoleClaude,
				model:         shortModel(c.Model),
				content:       c.Text,
				thinkingCount: c.ThinkingCount,
				toolCallCount: len(c.ToolCalls),
				messages:      countOutputItems(c.Items),
				tokensRaw:     c.Usage.TotalTokens(),
				durationMs:    c.DurationMs,
				timestamp:     formatTime(c.Timestamp),
				items:         convertDisplayItems(c.Items),
			})
		case parser.SystemChunk:
			msgs = append(msgs, message{
				role:      RoleSystem,
				content:   c.Output,
				timestamp: formatTime(c.Timestamp),
			})
		}
	}
	return msgs
}

// convertDisplayItems maps parser.DisplayItem to the TUI's displayItem type.
func convertDisplayItems(items []parser.DisplayItem) []displayItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]displayItem, len(items))
	for i, it := range items {
		input := ""
		if len(it.ToolInput) > 0 {
			var pretty bytes.Buffer
			if json.Indent(&pretty, it.ToolInput, "", "  ") == nil {
				input = pretty.String()
			} else {
				input = string(it.ToolInput)
			}
		}
		out[i] = displayItem{
			itemType:    it.Type,
			text:        it.Text,
			toolName:    it.ToolName,
			toolSummary: it.ToolSummary,
			toolInput:   input,
			toolResult:  it.ToolResult,
			toolError:   it.ToolError,
			durationMs:  it.DurationMs,
			tokenCount:  it.TokenCount,
		}
	}
	return out
}

// shortModel turns "claude-opus-4-6" into "opus4.6".
func shortModel(m string) string {
	m = strings.TrimPrefix(m, "claude-")
	parts := strings.SplitN(m, "-", 2)
	if len(parts) == 2 {
		return parts[0] + strings.ReplaceAll(parts[1], "-", ".")
	}
	return m
}

// modelColor returns a color based on the Claude model family.
func modelColor(model string) lipgloss.AdaptiveColor {
	switch {
	case strings.Contains(model, "opus"):
		return ColorModelOpus
	case strings.Contains(model, "sonnet"):
		return ColorModelSonnet
	case strings.Contains(model, "haiku"):
		return ColorModelHaiku
	default:
		return ColorTextSecondary
	}
}

// countOutputItems counts text output items in a display items slice.
func countOutputItems(items []parser.DisplayItem) int {
	n := 0
	for _, it := range items {
		if it.Type == parser.ItemOutput {
			n++
		}
	}
	return n
}

// formatTime renders a timestamp for the message header.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("3:04:05 PM")
}

func initialModel(msgs []message, hasDarkBg bool) model {
	return model{
		messages:       msgs,
		expanded:       make(map[int]bool), // all messages start collapsed
		cursor:         0,
		detailExpanded: make(map[int]bool),
		md:             newMdRenderer(hasDarkBg),
	}
}

func (m model) Init() tea.Cmd {
	if m.watching {
		return tea.Batch(
			waitForTailUpdate(m.tailSub),
			waitForWatcherErr(m.tailErrc),
		)
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.computeLineOffsets()
		m.ensureCursorVisible()
		if m.view == viewDetail {
			m.computeDetailMaxScroll()
		}
		return m, nil

	case tailUpdateMsg:
		// Auto-follow: if cursor was on the last message, track the new tail.
		wasAtEnd := m.cursor >= len(m.messages)-1
		m.messages = msg.messages
		if wasAtEnd && len(m.messages) > 0 {
			m.cursor = len(m.messages) - 1
		}
		// Clamp cursor if the message list somehow shrank.
		if m.cursor >= len(m.messages) && len(m.messages) > 0 {
			m.cursor = len(m.messages) - 1
		}
		m.computeLineOffsets()
		m.ensureCursorVisible()
		return m, waitForTailUpdate(m.tailSub)

	case watcherErrMsg:
		// Transient watcher errors: re-subscribe and keep going.
		return m, waitForWatcherErr(m.tailErrc)

	case pickerSessionsMsg:
		if msg.err != nil {
			// Fall back to list view on error.
			return m, nil
		}
		m.pickerSessions = msg.sessions
		m.pickerCursor = 0
		m.pickerScroll = 0
		m.view = viewPicker
		return m, nil

	case loadSessionMsg:
		if msg.err != nil || len(msg.messages) == 0 {
			return m, nil
		}
		// Stop the old watcher before switching sessions.
		if m.watcher != nil {
			m.watcher.stop()
		}
		m.messages = msg.messages
		m.expanded = make(map[int]bool)
		m.detailExpanded = make(map[int]bool)
		m.cursor = 0
		m.scroll = 0
		m.detailCursor = 0
		m.sessionPath = msg.path
		m.view = viewList
		m.computeLineOffsets()

		// Start a new watcher for the selected session.
		w := newSessionWatcher(msg.path, msg.classified, msg.offset)
		go w.run()
		m.watcher = w
		m.watching = true
		m.tailSub = w.sub
		m.tailErrc = w.errc
		return m, tea.Batch(waitForTailUpdate(m.tailSub), waitForWatcherErr(m.tailErrc))

	case tea.KeyMsg:
		switch m.view {
		case viewDetail:
			return m.updateDetail(msg)
		case viewPicker:
			return m.updatePicker(msg)
		default:
			return m.updateList(msg)
		}

	case tea.MouseMsg:
		if m.view == viewDetail {
			return m.updateDetailMouse(msg)
		}
		return m.updateListMouse(msg)
	}

	return m, nil
}

// updateList handles key events in the message list view.
func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
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
		m.ensureCursorVisible()
	case "enter":
		// Enter detail view for current message
		if len(m.messages) > 0 {
			m.view = viewDetail
			m.detailScroll = 0
			m.detailCursor = 0
			m.detailExpanded = make(map[int]bool)
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
	if m.cursor < 0 || m.cursor >= len(m.messages) {
		return false
	}
	return len(m.messages[m.cursor].items) > 0
}

// updateDetail handles key events in the full-screen detail view.
func (m model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	hasItems := m.detailHasItems()

	switch msg.String() {
	case "q", "escape":
		m.view = viewList
		m.detailCursor = 0
		m.detailExpanded = make(map[int]bool)
	case "enter":
		if hasItems {
			m.detailExpanded[m.detailCursor] = !m.detailExpanded[m.detailCursor]
			m.computeDetailMaxScroll()
			m.ensureDetailCursorVisible()
		} else {
			m.view = viewList
			m.detailCursor = 0
			m.detailExpanded = make(map[int]bool)
		}
	case "j", "down":
		if hasItems {
			itemCount := len(m.messages[m.cursor].items)
			if m.detailCursor < itemCount-1 {
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
			m.detailCursor = len(m.messages[m.cursor].items) - 1
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

// computeLineOffsets calculates the starting line of each message in the
// rendered output. Must mirror View()'s rendering to keep scroll accurate.
func (m *model) computeLineOffsets() {
	if m.width == 0 || len(m.messages) == 0 {
		return
	}
	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	m.lineOffsets = make([]int, len(m.messages))
	m.messageLines = make([]int, len(m.messages))
	currentLine := 0
	for i, msg := range m.messages {
		m.lineOffsets[i] = currentLine
		rendered := m.renderMessage(msg, width, false, m.expanded[i])
		lineCount := strings.Count(rendered, "\n") + 1
		m.messageLines[i] = lineCount
		currentLine += lineCount
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
	viewHeight := m.height - statusBarHeight - 1 // content area above status bar
	if viewHeight <= 0 {
		return
	}

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
	viewHeight := m.height - statusBarHeight - 1 // content area above status bar
	maxScroll := m.totalRenderedLines - viewHeight
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

// renderDetailContent renders the full detail content for the current message.
// Used by both computeDetailMaxScroll and viewDetail to avoid duplication.
func (m model) renderDetailContent(msg message, width int) string {
	// AI messages with items get the structured items view.
	if msg.role == RoleClaude && len(msg.items) > 0 {
		return m.renderDetailItemsContent(msg, width)
	}

	var header, body string
	switch msg.role {
	case RoleClaude:
		header = m.renderDetailHeader(msg, width)
		body = m.md.renderMarkdown(msg.content, width-4)
	case RoleUser:
		header = lipgloss.NewStyle().Foreground(ColorTextDim).Render(msg.timestamp) +
			"  " + lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary).Render("You") +
			" " + lipgloss.NewStyle().Foreground(ColorTextSecondary).Render(IconUser)
		body = m.md.renderMarkdown(msg.content, width-4)
	case RoleSystem:
		header = lipgloss.NewStyle().Foreground(ColorTextMuted).Render(IconSystem) +
			" " + lipgloss.NewStyle().Foreground(ColorTextSecondary).Render("System") +
			"  " + lipgloss.NewStyle().Foreground(ColorTextDim).Render(msg.timestamp)
		body = lipgloss.NewStyle().Foreground(ColorTextDim).Render(msg.content)
	}

	return header + "\n\n" + body
}

// computeDetailMaxScroll caches the maximum scroll offset for the detail view.
// Called when entering detail view and on window resize.
func (m *model) computeDetailMaxScroll() {
	if m.cursor < 0 || m.cursor >= len(m.messages) || m.width == 0 || m.height == 0 {
		m.detailMaxScroll = 0
		return
	}

	msg := m.messages[m.cursor]
	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	content := m.renderDetailContent(msg, width)
	content = strings.TrimRight(content, "\n")
	totalLines := strings.Count(content, "\n") + 1

	viewHeight := m.height - statusBarHeight
	if viewHeight <= 0 {
		viewHeight = 1
	}
	m.detailMaxScroll = totalLines - viewHeight
	if m.detailMaxScroll < 0 {
		m.detailMaxScroll = 0
	}
}

// ensureDetailCursorVisible adjusts detailScroll so the current detail cursor
// item is within the visible viewport. Computes the cursor's line position by
// counting header lines + item rows + expanded content lines before it.
func (m *model) ensureDetailCursorVisible() {
	if m.cursor < 0 || m.cursor >= len(m.messages) || m.width == 0 || m.height == 0 {
		return
	}
	msg := m.messages[m.cursor]
	if len(msg.items) == 0 {
		return
	}

	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	// Count header lines (header + blank separator)
	header := m.renderDetailHeader(msg, width)
	headerLines := strings.Count(header, "\n") + 1 // header rendered lines
	headerLines += 1                               // blank line separator from "\n\n"

	// Count lines for items before the cursor
	cursorLine := headerLines
	for i := 0; i < m.detailCursor && i < len(msg.items); i++ {
		cursorLine++ // the item row itself
		if m.detailExpanded[i] {
			expanded := m.renderDetailItemExpanded(msg.items[i], width)
			if expanded != "" {
				cursorLine += strings.Count(expanded, "\n") + 1
			}
		}
	}

	// Count lines for the cursor item itself (row + expanded content)
	cursorEnd := cursorLine // the row line
	if m.detailCursor < len(msg.items) && m.detailExpanded[m.detailCursor] {
		expanded := m.renderDetailItemExpanded(msg.items[m.detailCursor], width)
		if expanded != "" {
			cursorEnd += strings.Count(expanded, "\n") + 1
		}
	}

	viewHeight := m.height - statusBarHeight
	if viewHeight <= 0 {
		viewHeight = 1
	}

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

// formatTokens formats a token count for display: 1234 -> "1.2k", 123456 -> "123.5k", 1234567 -> "1.2M"
func formatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// formatDuration formats milliseconds into human-readable duration: 71000 -> "1m 11s", 3500 -> "3.5s"
func formatDuration(ms int64) string {
	secs := float64(ms) / 1000
	switch {
	case secs >= 60:
		mins := int(secs) / 60
		rem := int(secs) % 60
		return fmt.Sprintf("%dm %ds", mins, rem)
	case secs >= 10:
		return fmt.Sprintf("%.0fs", secs)
	default:
		return fmt.Sprintf("%.1fs", secs)
	}
}

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	switch m.view {
	case viewDetail:
		return m.viewDetail()
	case viewPicker:
		return m.viewPicker()
	default:
		return m.viewList()
	}
}

// viewList renders the message list (main view).
func (m model) viewList() string {
	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	var rendered []string
	for i, msg := range m.messages {
		isSelected := i == m.cursor
		isExpanded := m.expanded[i]
		rendered = append(rendered, m.renderMessage(msg, width, isSelected, isExpanded))
	}

	content := strings.Join(rendered, "\n\n")

	// Simple line-based scroll
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	if m.scroll > 0 && m.scroll < totalLines {
		lines = lines[m.scroll:]
	}

	// Truncate to viewport height minus status bar
	viewHeight := m.height - statusBarHeight - 1
	if viewHeight > 0 && len(lines) > viewHeight {
		lines = lines[:viewHeight]
	}

	output := strings.Join(lines, "\n")

	// Status bar
	status := m.renderStatusBar(
		"j/k", "nav",
		"G/g", "jump",
		"tab", "toggle",
		"enter", "detail",
		"e/c", "expand/collapse",
		"s", "sessions",
		"q", "quit",
	)

	return output + "\n" + status
}

// viewDetail renders a single message full-screen with scrolling.
func (m model) viewDetail() string {
	if m.cursor < 0 || m.cursor >= len(m.messages) {
		return ""
	}

	msg := m.messages[m.cursor]
	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	content := m.renderDetailContent(msg, width)

	// Strip trailing newlines that lipgloss may add -- they create phantom blank
	// lines when we split on \n, wasting a viewport line and pushing the status
	// bar off-screen.
	content = strings.TrimRight(content, "\n")

	// Scroll the content
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Reserve lines for the status bar.
	viewHeight := m.height - statusBarHeight
	if viewHeight <= 0 {
		viewHeight = 1
	}
	maxScroll := totalLines - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.detailScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}

	if scroll > 0 && scroll < totalLines {
		lines = lines[scroll:]
	}
	if len(lines) > viewHeight {
		lines = lines[:viewHeight]
	}

	output := strings.Join(lines, "\n")

	// Scroll position indicator
	scrollInfo := ""
	if totalLines > viewHeight {
		pct := 0
		if maxScroll > 0 {
			pct = scroll * 100 / maxScroll
		}
		scrollInfo = fmt.Sprintf("  %d%% (%d/%d)", pct, scroll+viewHeight, totalLines)
	}

	// Status bar varies by message type
	hasItems := msg.role == RoleClaude && len(msg.items) > 0
	var status string
	if hasItems {
		status = m.renderStatusBar(
			"j/k", "items",
			"enter", "expand",
			"J/K", "scroll",
			"G/g", "jump",
			"q/esc", "back"+scrollInfo,
		)
	} else {
		status = m.renderStatusBar(
			"j/k", "scroll",
			"G/g", "jump",
			"q/esc", "back"+scrollInfo,
		)
	}

	return output + "\n" + status
}

// renderDetailItemsContent renders the full content for an AI message with
// structured items (header + items list + expanded content). Returns the
// complete string before scrolling is applied.
func (m model) renderDetailItemsContent(msg message, width int) string {
	header := m.renderDetailHeader(msg, width)

	var itemLines []string
	for i, item := range msg.items {
		row := m.renderDetailItemRow(item, i, width)

		if m.detailExpanded[i] {
			expanded := m.renderDetailItemExpanded(item, width)
			if expanded != "" {
				row += "\n" + expanded
			}
		}
		itemLines = append(itemLines, row)
	}

	return header + "\n\n" + strings.Join(itemLines, "\n")
}

// renderDetailItemRow renders a single item row in the detail view.
// Format: {cursor} {indicator} {name:<12} {summary}  {tokens} {duration}
func (m model) renderDetailItemRow(item displayItem, index int, width int) string {
	// Cursor indicator
	cursor := "  "
	if index == m.detailCursor {
		cursor = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).Render(IconCursor + " ")
	}

	// Type indicator and name
	var indicator, name string
	dim := lipgloss.NewStyle().Foreground(ColorTextDim)
	green := lipgloss.NewStyle().Foreground(ColorSuccess)
	red := lipgloss.NewStyle().Foreground(ColorError)

	blue := lipgloss.NewStyle().Foreground(ColorInfo)

	switch item.itemType {
	case parser.ItemThinking:
		indicator = dim.Render(IconThinking)
		name = "Thinking"
	case parser.ItemOutput:
		indicator = blue.Render(IconOutput)
		name = "Output"
	case parser.ItemToolCall:
		if item.toolError {
			indicator = red.Render(IconToolErr)
		} else {
			indicator = green.Render(IconToolOk)
		}
		name = item.toolName
	}

	// Pad name to 12 chars
	nameStr := fmt.Sprintf("%-12s", name)
	nameRendered := lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary).Render(nameStr)

	// Summary
	var summary string
	switch item.itemType {
	case parser.ItemThinking, parser.ItemOutput:
		summary = truncate(item.text, 40)
	case parser.ItemToolCall:
		summary = item.toolSummary
	}
	summaryRendered := lipgloss.NewStyle().Foreground(ColorTextSecondary).Render(summary)

	// Right-side: tokens + duration
	var rightParts []string
	if item.tokenCount > 0 {
		tokStr := fmt.Sprintf("~%s tok", formatTokens(item.tokenCount))
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(ColorTextDim).Render(tokStr))
	}
	if item.durationMs > 0 {
		durStr := fmt.Sprintf("%dms", item.durationMs)
		if item.durationMs >= 1000 {
			durStr = formatDuration(item.durationMs)
		}
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(ColorTextDim).Render(durStr))
	}
	rightSide := strings.Join(rightParts, "  ")

	left := cursor + indicator + " " + nameRendered + " " + summaryRendered
	return spaceBetween(left, rightSide, width)
}

// renderDetailItemExpanded renders the expanded content for a detail item.
// Indented 4 spaces, word-wrapped to width-8.
func (m model) renderDetailItemExpanded(item displayItem, width int) string {
	wrapWidth := width - 8
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	indent := "    "

	switch item.itemType {
	case parser.ItemThinking, parser.ItemOutput:
		text := strings.TrimSpace(item.text)
		if text == "" {
			return ""
		}
		rendered := m.md.renderMarkdown(text, wrapWidth)
		return indentBlock(rendered, indent)

	case parser.ItemToolCall:
		var sections []string

		if item.toolInput != "" {
			headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorTextSecondary)
			sections = append(sections, indent+headerStyle.Render("Input:"))
			inputStyle := lipgloss.NewStyle().
				Foreground(ColorTextDim).
				Width(wrapWidth)
			sections = append(sections, indentBlock(inputStyle.Render(item.toolInput), indent))
		}

		if item.toolResult != "" || item.toolError {
			if len(sections) > 0 {
				sepStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)
				sections = append(sections, indent+sepStyle.Render(strings.Repeat("-", wrapWidth)))
			}

			if item.toolError {
				headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorError)
				sections = append(sections, indent+headerStyle.Render("Error:"))
			} else {
				headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorTextSecondary)
				sections = append(sections, indent+headerStyle.Render("Result:"))
			}

			resultStyle := lipgloss.NewStyle().
				Foreground(ColorTextDim).
				Width(wrapWidth)
			sections = append(sections, indentBlock(resultStyle.Render(item.toolResult), indent))
		}

		if len(sections) == 0 {
			return ""
		}
		return strings.Join(sections, "\n")
	}

	return ""
}

// indentBlock adds a prefix to every line of a block of text.
func indentBlock(text string, indent string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

// renderDetailHeader renders metadata for the detail view header.
// An optional leftSuffix is appended after the stats (used for the chevron
// in list view). Matches the list view header layout for visual consistency.
func (m model) renderDetailHeader(msg message, width int, leftSuffix ...string) string {
	icon := lipgloss.NewStyle().Foreground(ColorInfo).Bold(true).Render(IconClaude)
	modelName := lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary).Render("Claude")
	modelVer := lipgloss.NewStyle().Foreground(modelColor(msg.model)).Render(msg.model)

	var statParts []string
	if msg.thinkingCount > 0 {
		statParts = append(statParts, fmt.Sprintf("%d thinking", msg.thinkingCount))
	}
	if msg.toolCallCount > 0 {
		tcLabel := "tool calls"
		if msg.toolCallCount == 1 {
			tcLabel = "tool call"
		}
		statParts = append(statParts, fmt.Sprintf("%d %s", msg.toolCallCount, tcLabel))
	}
	if msg.messages > 0 {
		label := "messages"
		if msg.messages == 1 {
			label = "message"
		}
		statParts = append(statParts, fmt.Sprintf("%d %s", msg.messages, label))
	}

	stats := ""
	if len(statParts) > 0 {
		dot := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(" " + IconDot + " ")
		stats = dot + lipgloss.NewStyle().Foreground(ColorTextSecondary).Render(strings.Join(statParts, ", "))
	}

	left := icon + " " + modelName + " " + modelVer + stats
	for _, s := range leftSuffix {
		left += " " + s
	}

	// Right-side metadata
	var rightParts []string

	if msg.tokensRaw > 0 {
		coin := lipgloss.NewStyle().Foreground(ColorTokenIcon).Render(IconToken)
		rightParts = append(rightParts, coin+" "+lipgloss.NewStyle().
			Foreground(ColorTextSecondary).
			Render(formatTokens(msg.tokensRaw)))
	}

	if msg.durationMs > 0 {
		clock := lipgloss.NewStyle().Foreground(ColorTextDim).Render(IconClock)
		rightParts = append(rightParts, clock+" "+lipgloss.NewStyle().
			Foreground(ColorTextSecondary).
			Render(formatDuration(msg.durationMs)))
	}

	if msg.timestamp != "" {
		rightParts = append(rightParts, lipgloss.NewStyle().
			Foreground(ColorTextDim).
			Render(msg.timestamp))
	}

	return spaceBetween(left, strings.Join(rightParts, "  "), width)
}

// statusBarHeight is the number of rendered lines the status bar occupies.
// Rounded border: top + content + bottom = 3 lines.
const statusBarHeight = 3

// renderStatusBar renders key hints in a rounded-border box.
// When m.watching is true, a green LIVE badge is prepended.
func (m model) renderStatusBar(pairs ...string) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(ColorTextDim)

	sep := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(" " + IconDot + " ")

	var hints []string

	if m.watching {
		liveBadge := lipgloss.NewStyle().
			Background(ColorLiveBg).
			Foreground(ColorLiveFg).
			Bold(true).
			Padding(0, 1).
			Render("LIVE")
		hints = append(hints, liveBadge)
	}

	for i := 0; i+1 < len(pairs); i += 2 {
		hints = append(hints, keyStyle.Render(pairs[i])+" "+descStyle.Render(pairs[i+1]))
	}

	barStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Width(m.width-2). // border chars take 2 columns
		Padding(0, 1)

	return barStyle.Render(strings.Join(hints, sep))
}

func (m model) renderMessage(msg message, containerWidth int, isSelected, isExpanded bool) string {
	switch msg.role {
	case RoleClaude:
		return m.renderClaudeMessage(msg, containerWidth, isSelected, isExpanded)
	case RoleUser:
		return m.renderUserMessage(msg, containerWidth, isSelected, isExpanded)
	case RoleSystem:
		return renderSystemMessage(msg, containerWidth, isSelected, isExpanded)
	default:
		return msg.content
	}
}

// chevron returns the expand/collapse indicator
func chevron(expanded bool) string {
	if expanded {
		return lipgloss.NewStyle().Foreground(ColorTextPrimary).Render(IconExpanded)
	}
	return lipgloss.NewStyle().Foreground(ColorTextDim).Render(IconCollapsed)
}

// selectionIndicator returns a left-margin marker for the selected message
func selectionIndicator(selected bool) string {
	if selected {
		return lipgloss.NewStyle().Foreground(ColorAccent).Render(IconSelected + " ")
	}
	return "  "
}

// truncate delegates to parser.Truncate for consistent truncation behavior.
func truncate(s string, maxLen int) string {
	return parser.Truncate(s, maxLen)
}

// maxContentWidth is the maximum width for content rendering.
const maxContentWidth = 120

// contentWidth returns the inner width for card content, given a card width.
// Subtracts border (2) + padding (4) and floors at 20.
func contentWidth(cardWidth int) int {
	w := cardWidth - 4
	if w < 20 {
		w = 20
	}
	return w
}

// spaceBetween lays out left and right strings with gap-fill spacing to span width.
func spaceBetween(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) renderClaudeMessage(msg message, containerWidth int, isSelected, isExpanded bool) string {
	sel := selectionIndicator(isSelected)
	chev := chevron(isExpanded)
	maxWidth := containerWidth - 4 // selection indicator (2) + gutter (2)

	// Delegate to renderDetailHeader with chevron appended after stats.
	headerLine := sel + "  " + m.renderDetailHeader(msg, maxWidth, chev)

	// Render the card body -- truncate when collapsed
	content := msg.content
	if !isExpanded {
		lines := strings.Split(content, "\n")
		if len(lines) > maxCollapsedLines {
			content = strings.Join(lines[:maxCollapsedLines], "\n")
			hint := fmt.Sprintf("\u2026 (%d lines hidden)", len(lines)-maxCollapsedLines)
			content += "\n" + hint
		}
	}

	contentWidth := contentWidth(maxWidth)
	body := m.md.renderMarkdown(content, contentWidth)

	cardBorderColor := ColorBorder
	if isSelected {
		cardBorderColor = ColorAccent
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cardBorderColor).
		Width(maxWidth).
		Padding(0, 2)

	card := cardStyle.Render(body)

	// Indent card to align with header content
	cardLines := strings.Split(card, "\n")
	var indented []string
	for _, line := range cardLines {
		indented = append(indented, sel+"  "+line)
	}

	return headerLine + "\n" + strings.Join(indented, "\n")
}

// maxCollapsedLines is the maximum content lines shown when a message is collapsed.
const maxCollapsedLines = 6

func (m model) renderUserMessage(msg message, containerWidth int, isSelected, isExpanded bool) string {
	sel := selectionIndicator(isSelected)
	maxBubbleWidth := containerWidth * 3 / 4

	// Use full terminal width for alignment so user messages right-align to
	// the terminal edge, not just within the 120-col content area.
	alignWidth := m.width
	if alignWidth < containerWidth {
		alignWidth = containerWidth
	}

	// Header: timestamp + You + icon, right-aligned to terminal edge
	ts := lipgloss.NewStyle().
		Foreground(ColorTextDim).
		Render(msg.timestamp)

	youLabel := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorTextPrimary).
		Render("You")

	userIcon := lipgloss.NewStyle().
		Foreground(ColorTextSecondary).
		Render(IconUser)

	rightPart := ts + "  " + youLabel + " " + userIcon
	leftPart := sel

	headerGap := alignWidth - lipgloss.Width(leftPart) - lipgloss.Width(rightPart)
	if headerGap < 0 {
		headerGap = 0
	}
	header := leftPart + strings.Repeat(" ", headerGap) + rightPart

	bubbleBorderColor := ColorTextMuted
	if isSelected {
		bubbleBorderColor = ColorAccent
	}

	content := msg.content
	var hint string

	// Truncate long user messages when collapsed
	if !isExpanded {
		lines := strings.Split(content, "\n")
		if len(lines) > maxCollapsedLines {
			content = strings.Join(lines[:maxCollapsedLines], "\n")
			hint = lipgloss.NewStyle().Foreground(ColorTextDim).
				Render(fmt.Sprintf("… (%d lines hidden)", len(lines)-maxCollapsedLines))
		}
	}

	// Render markdown content inside the bubble, then append the hint
	bubbleInnerWidth := maxBubbleWidth - 6 // subtract border (2) + padding (4)
	if bubbleInnerWidth < 20 {
		bubbleInnerWidth = 20
	}
	rendered := m.md.renderMarkdown(content, bubbleInnerWidth)
	if hint != "" {
		rendered += "\n" + hint
	}

	bubbleStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(bubbleBorderColor).
		Padding(0, 2).
		MaxWidth(maxBubbleWidth)

	bubble := bubbleStyle.Render(rendered)
	alignedBubble := lipgloss.PlaceHorizontal(alignWidth, lipgloss.Right, bubble)

	return header + "\n" + alignedBubble
}

func renderSystemMessage(msg message, containerWidth int, isSelected, _ bool) string {
	// System messages always show inline -- they're short
	sel := selectionIndicator(isSelected)

	sysIcon := lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Render(IconSystem)

	label := lipgloss.NewStyle().
		Foreground(ColorTextSecondary).
		Render("System")

	ts := lipgloss.NewStyle().
		Foreground(ColorTextDim).
		Render(msg.timestamp)

	content := lipgloss.NewStyle().
		Foreground(ColorTextDim).
		Render(msg.content)

	return sel + sysIcon + " " + label + "  " + IconDot + "  " + ts + "  " + content
}

func main() {
	// Detect terminal background ONCE, before Bubble Tea takes over.
	// termenv queries via OSC 11 which can fail in alt-screen mode.
	// Tell lipgloss explicitly so AdaptiveColor agrees with glamour.
	hasDarkBg := termenv.HasDarkBackground()
	lipgloss.SetHasDarkBackground(hasDarkBg)

	dumpMode := false
	expandAll := false
	var sessionPath string

	for _, arg := range os.Args[1:] {
		switch {
		case arg == "--dump":
			dumpMode = true
		case arg == "--expand":
			expandAll = true
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", arg)
			os.Exit(1)
		default:
			sessionPath = arg
		}
	}

	result, err := loadSession(sessionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if dumpMode {
		width := maxContentWidth
		m := initialModel(result.messages, hasDarkBg)
		m.width = width
		m.height = 1_000_000
		if expandAll {
			for i := range m.messages {
				m.expanded[i] = true
			}
		}
		fmt.Println(m.View())
		return
	}

	// Start the file watcher for live tailing.
	watcher := newSessionWatcher(result.path, result.classified, result.offset)
	go watcher.run()

	m := initialModel(result.messages, hasDarkBg)
	m.sessionPath = result.path
	m.watching = true
	m.watcher = watcher
	m.tailSub = watcher.sub
	m.tailErrc = watcher.errc

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
