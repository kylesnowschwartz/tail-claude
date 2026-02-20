package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

type message struct {
	role       string
	model      string
	content    string
	thinking   int
	toolCalls  int
	messages   int
	contextD   int
	tokens     string
	tokensRaw  int
	phase      string
	duration   string
	durationMs int64
	timestamp  string
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
	detailScroll    int // scroll offset within the detail view
	detailMaxScroll int // cached max scroll for detail view, updated on enter/resize

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
				role:       RoleClaude,
				model:      shortModel(c.Model),
				content:    c.Text,
				thinking:   c.Thinking,
				toolCalls:  len(c.ToolCalls),
				tokensRaw:  c.Usage.TotalTokens(),
				durationMs: c.DurationMs,
				timestamp:  formatTime(c.Timestamp),
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

// shortModel turns "claude-opus-4-6" into "opus4.6".
func shortModel(m string) string {
	m = strings.TrimPrefix(m, "claude-")
	parts := strings.SplitN(m, "-", 2)
	if len(parts) == 2 {
		return parts[0] + strings.ReplaceAll(parts[1], "-", ".")
	}
	return m
}

// formatTime renders a timestamp for the message header.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("3:04:05 PM")
}

func initialModel(msgs []message) model {
	return model{
		messages: msgs,
		expanded: make(map[int]bool), // all messages start collapsed
		cursor:   0,
	}
}

func sampleMessages() []message {
	return []message{
		{
			role:      RoleClaude,
			model:     "opus4.6",
			content:   "Let me first smoke-test the Claude CLI to understand the exact message flow.",
			thinking:  1,
			toolCalls: 7,
			messages:  1,
			contextD:  3,
			tokens:    "100.3k",
			phase:     "Phase 1/3",
			duration:  "1m 11s",
			timestamp: "5:59:25 PM",
		},
		{
			role:      RoleUser,
			content:   "Hm this isn't working. Can you do a simple `claude -p \"hello\"`?",
			timestamp: "6:00:55 PM",
		},
		{
			role:      RoleClaude,
			model:     "opus4.6",
			content:   "That returned but with no visible output (just system noise from hooks). Let me check if the env unset is actually working:\n\nI'll try running it with verbose output to see what's happening under the hood. The issue might be that the CLAUDE_CODE_SSE_PORT env var is still set from a parent process.",
			toolCalls: 2,
			contextD:  2,
			tokens:    "100.9k",
			phase:     "Phase 1/3",
			duration:  "35.8s",
			timestamp: "6:01:18 PM",
		},
		{
			role:      RoleUser,
			content:   "/exit",
			timestamp: "6:01:41 PM",
		},
		{
			role:      RoleSystem,
			content:   "Goodbye!",
			timestamp: "6:01:41 PM",
		},
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
		m.cursor = 0
		m.scroll = 0
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

// updateDetail handles key events in the full-screen detail view.
func (m model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "escape", "enter":
		m.view = viewList
	case "j", "down":
		m.detailScroll++
	case "k", "up":
		if m.detailScroll > 0 {
			m.detailScroll--
		}
	case "J", "ctrl+d":
		m.detailScroll += m.height / 2
	case "K", "ctrl+u":
		m.detailScroll -= m.height / 2
		if m.detailScroll < 0 {
			m.detailScroll = 0
		}
	case "G":
		m.detailScroll = m.detailMaxScroll
	case "g":
		m.detailScroll = 0
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
	if width > 120 {
		width = 120
	}

	m.lineOffsets = make([]int, len(m.messages))
	m.messageLines = make([]int, len(m.messages))
	currentLine := 0
	for i, msg := range m.messages {
		m.lineOffsets[i] = currentLine
		rendered := renderMessage(msg, width, false, m.expanded[i])
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
	viewHeight := m.height - 2 // reserve for status bar
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
	viewHeight := m.height - 2 // reserve for status bar
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

// computeDetailMaxScroll renders the detail content for the current cursor
// position and caches the maximum scroll offset. Called when entering detail
// view and on window resize so that updateDetail can clamp scroll without
// needing to re-render.
func (m *model) computeDetailMaxScroll() {
	if m.cursor < 0 || m.cursor >= len(m.messages) || m.width == 0 || m.height == 0 {
		m.detailMaxScroll = 0
		return
	}

	msg := m.messages[m.cursor]
	width := m.width
	if width > 120 {
		width = 120
	}

	var header, body string
	switch msg.role {
	case RoleClaude:
		header = m.renderDetailHeader(msg, width)
		bodyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Width(width - 4)
		body = bodyStyle.Render(msg.content)
	case RoleUser:
		header = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).Render("You") +
			"  " + lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(msg.timestamp)
		bodyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Width(width - 4)
		body = bodyStyle.Render(msg.content)
	case RoleSystem:
		header = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("System") +
			"  " + lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(msg.timestamp)
		body = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(msg.content)
	}

	content := header + "\n\n" + body
	content = strings.TrimRight(content, "\n")

	totalLines := strings.Count(content, "\n") + 1
	viewHeight := m.height - 1 // reserve 1 line for status bar
	if viewHeight <= 0 {
		viewHeight = 1
	}
	m.detailMaxScroll = totalLines - viewHeight
	if m.detailMaxScroll < 0 {
		m.detailMaxScroll = 0
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
	if width > 120 {
		width = 120
	}

	var rendered []string
	for i, msg := range m.messages {
		isSelected := i == m.cursor
		isExpanded := m.expanded[i]
		rendered = append(rendered, renderMessage(msg, width, isSelected, isExpanded))
	}

	content := strings.Join(rendered, "\n\n")

	// Simple line-based scroll
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	if m.scroll > 0 && m.scroll < totalLines {
		lines = lines[m.scroll:]
	}

	// Truncate to viewport height minus status bar (2 lines)
	viewHeight := m.height - 2
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
	if width > 120 {
		width = 120
	}

	// Render full message content without truncation
	var header, body string

	switch msg.role {
	case RoleClaude:
		header = m.renderDetailHeader(msg, width)
		bodyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Width(width - 4)
		body = bodyStyle.Render(msg.content)
	case RoleUser:
		header = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).Render("You") +
			"  " + lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(msg.timestamp)
		bodyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Width(width - 4)
		body = bodyStyle.Render(msg.content)
	case RoleSystem:
		header = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("System") +
			"  " + lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(msg.timestamp)
		body = lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(msg.content)
	}

	content := header + "\n\n" + body

	// Strip trailing newlines that lipgloss may add — they create phantom blank
	// lines when we split on \n, wasting a viewport line and pushing the status
	// bar off-screen.
	content = strings.TrimRight(content, "\n")

	// Scroll the content
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Reserve 1 line for the status bar.
	viewHeight := m.height - 1
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

	status := m.renderStatusBar(
		"j/k", "scroll",
		"G/g", "jump",
		"q/esc", "back"+scrollInfo,
	)

	return output + "\n" + status
}

// renderDetailHeader renders metadata for the detail view header.
func (m model) renderDetailHeader(msg message, width int) string {
	icon := lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Bold(true).Render("C")
	modelName := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).Render("Claude")
	modelVer := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(msg.model)

	var parts []string
	if msg.thinking > 0 {
		parts = append(parts, fmt.Sprintf("%d thinking", msg.thinking))
	}
	if msg.toolCalls > 0 {
		parts = append(parts, fmt.Sprintf("%d tool calls", msg.toolCalls))
	}

	stats := ""
	if len(parts) > 0 {
		stats = "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(strings.Join(parts, ", "))
	}

	tokenStr := ""
	if msg.tokensRaw > 0 {
		tokenStr = formatTokens(msg.tokensRaw)
	} else if msg.tokens != "" {
		tokenStr = msg.tokens
	}

	durStr := ""
	if msg.durationMs > 0 {
		durStr = formatDuration(msg.durationMs)
	} else if msg.duration != "" {
		durStr = msg.duration
	}

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render

	right := ""
	if tokenStr != "" {
		right += lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(tokenStr)
	}
	if durStr != "" {
		if right != "" {
			right += dim(" | ")
		}
		right += lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(durStr)
	}
	if msg.timestamp != "" {
		if right != "" {
			right += "  "
		}
		right += lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(msg.timestamp)
	}

	left := icon + " " + modelName + " " + modelVer + stats
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		gap = 2
	}

	return left + strings.Repeat(" ", gap) + right
}

// renderStatusBar renders a key-hint status bar from alternating key/description pairs.
// When m.watching is true, a green LIVE badge is prepended.
func (m model) renderStatusBar(pairs ...string) string {
	statusStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Width(m.width).
		Padding(0, 1)

	dimKey := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("250")).
		Bold(true)

	dimDesc := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("243"))

	var parts []string

	if m.watching {
		liveBadge := lipgloss.NewStyle().
			Background(lipgloss.Color("28")).
			Foreground(lipgloss.Color("255")).
			Bold(true).
			Padding(0, 1).
			Render("LIVE")
		parts = append(parts, liveBadge)
	}

	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, dimKey.Render(pairs[i])+dimDesc.Render(":"+pairs[i+1]))
	}

	return statusStyle.Render(strings.Join(parts, "  "))
}

func renderMessage(msg message, containerWidth int, isSelected, isExpanded bool) string {
	switch msg.role {
	case RoleClaude:
		return renderClaudeMessage(msg, containerWidth, isSelected, isExpanded)
	case RoleUser:
		return renderUserMessage(msg, containerWidth, isSelected, isExpanded)
	case RoleSystem:
		return renderSystemMessage(msg, containerWidth, isSelected, isExpanded)
	default:
		return msg.content
	}
}

// chevron returns the expand/collapse indicator
func chevron(expanded bool) string {
	if expanded {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render("▼")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render("▶")
}

// selectionIndicator returns a left-margin marker for the selected message
func selectionIndicator(selected bool) string {
	if selected {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Render("│ ")
	}
	return "  "
}

// truncate cuts a string to maxLen and adds ellipsis
func truncate(s string, maxLen int) string {
	// Collapse newlines for the collapsed preview
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen-1] + "…"
	}
	return s
}

func renderClaudeMessage(msg message, containerWidth int, isSelected, isExpanded bool) string {
	sel := selectionIndicator(isSelected)
	chev := chevron(isExpanded)
	maxWidth := containerWidth - 6 // account for selection indicator + chevron + padding

	// Header: icon + model + stats on left, metadata badges on right
	icon := lipgloss.NewStyle().
		Foreground(lipgloss.Color("69")).
		Bold(true).
		Render("C")

	modelName := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("252")).
		Render("Claude")

	modelVersion := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render(msg.model)

	// Tool call summary
	var statParts []string
	if msg.thinking > 0 {
		statParts = append(statParts, fmt.Sprintf("%d thinking", msg.thinking))
	}
	if msg.toolCalls > 0 {
		statParts = append(statParts, fmt.Sprintf("%d tool calls", msg.toolCalls))
	}
	if msg.messages > 0 {
		statParts = append(statParts, fmt.Sprintf("%d message", msg.messages))
	}

	stats := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render("  " + strings.Join(statParts, ", "))

	leftHeader := icon + " " + modelName + " " + modelVersion + stats

	// Right-side metadata badges
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render

	contextBadge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("75")).
		Render(fmt.Sprintf("Ctx +%d", msg.contextD))

	// Token display: prefer pre-formatted string, fall back to raw count
	tokenStr := msg.tokens
	if tokenStr == "" && msg.tokensRaw > 0 {
		tokenStr = formatTokens(msg.tokensRaw)
	}
	tokenCount := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render(tokenStr)

	phaseBadge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("212")).
		Background(lipgloss.Color("53")).
		Padding(0, 1).
		Render(msg.phase)

	// Duration display: prefer pre-formatted string, fall back to raw ms
	durStr := msg.duration
	if durStr == "" && msg.durationMs > 0 {
		durStr = formatDuration(msg.durationMs)
	}
	dur := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render(durStr)

	ts := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Render(msg.timestamp)

	rightHeader := contextBadge + dim(" | ") + tokenCount + " " + phaseBadge + dim(" | ") + dur + " " + ts

	// Lay out header on one line
	rightLen := lipgloss.Width(rightHeader)
	leftLen := lipgloss.Width(leftHeader)
	gap := maxWidth - leftLen - rightLen
	if gap < 2 {
		gap = 2
	}
	headerLine := sel + chev + " " + leftHeader + strings.Repeat(" ", gap) + rightHeader

	// Render the card body — truncate when collapsed
	content := msg.content
	if !isExpanded {
		lines := strings.Split(content, "\n")
		if len(lines) > maxCollapsedLines {
			content = strings.Join(lines[:maxCollapsedLines], "\n")
			hint := fmt.Sprintf("… (%d lines hidden)", len(lines)-maxCollapsedLines)
			content += "\n" + hint
		}
	}

	bodyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Width(maxWidth-4).
		Padding(0, 2)

	body := bodyStyle.Render(content)

	cardBorderColor := lipgloss.Color("60")
	if isSelected {
		cardBorderColor = lipgloss.Color("75")
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cardBorderColor).
		Width(maxWidth).
		Padding(0, 1)

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

func renderUserMessage(msg message, containerWidth int, isSelected, isExpanded bool) string {
	sel := selectionIndicator(isSelected)
	maxBubbleWidth := containerWidth * 3 / 4

	// Header: timestamp + You, right-aligned
	ts := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Render(msg.timestamp)

	youLabel := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("252")).
		Render("You")

	rightPart := ts + "  " + youLabel
	leftPart := sel

	headerGap := containerWidth - lipgloss.Width(leftPart) - lipgloss.Width(rightPart)
	if headerGap < 0 {
		headerGap = 0
	}
	header := leftPart + strings.Repeat(" ", headerGap) + rightPart

	bubbleBorderColor := lipgloss.Color("240")
	if isSelected {
		bubbleBorderColor = lipgloss.Color("75")
	}

	content := msg.content

	// Truncate long user messages when collapsed
	if !isExpanded {
		lines := strings.Split(content, "\n")
		if len(lines) > maxCollapsedLines {
			content = strings.Join(lines[:maxCollapsedLines], "\n")
			hint := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).
				Render(fmt.Sprintf("… (%d lines hidden)", len(lines)-maxCollapsedLines))
			content += "\n" + hint
		}
	}

	bubbleStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(bubbleBorderColor).
		Background(lipgloss.Color("237")).
		Foreground(lipgloss.Color("252")).
		Padding(0, 2).
		MaxWidth(maxBubbleWidth)

	bubble := bubbleStyle.Render(content)
	alignedBubble := lipgloss.PlaceHorizontal(containerWidth, lipgloss.Right, bubble)

	return header + "\n" + alignedBubble
}

func renderSystemMessage(msg message, containerWidth int, isSelected, _ bool) string {
	// System messages always show inline -- they're short
	sel := selectionIndicator(isSelected)

	label := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render("System")

	ts := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Render(msg.timestamp)

	content := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		Render(msg.content)

	return sel + label + "  ·  " + ts + "  " + content
}

func main() {
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
		width := 120
		m := initialModel(result.messages)
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

	m := initialModel(result.messages)
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
