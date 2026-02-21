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
	RoleClaude  = "claude"
	RoleUser    = "user"
	RoleSystem  = "system"
	RoleCompact = "compact"
)

// View states
type viewState int

const (
	viewList   viewState = iota // message list (main view)
	viewDetail                  // full-screen single message
	viewPicker                  // session picker
)

// tickMsg drives the activity indicator animation.
type tickMsg time.Time

// tickCmd returns a Bubble Tea command that fires a tickMsg every 150ms.
func tickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// displayItem is a structured element within an AI message's detail view.
// Mirrors parser.DisplayItem but with pre-formatted fields for rendering.
type displayItem struct {
	itemType        parser.DisplayItemType
	text            string
	toolName        string
	toolSummary     string
	toolInput       string // formatted JSON for display
	toolResult      string
	toolError       bool
	durationMs      int64
	tokenCount      int
	subagentType    string
	subagentDesc    string
	teammateID      string
	subagentProcess *parser.SubagentProcess // linked subagent execution trace
}

type message struct {
	role             string
	model            string
	content          string
	thinkingCount    int
	toolCallCount    int
	messages         int
	tokensRaw        int
	durationMs       int64
	timestamp        string
	items            []displayItem
	lastOutput       *parser.LastOutput
	subagentLabel    string // non-empty for trace views: "Explore", "Plan", etc.
	teammateSpawns   int    // count of distinct team-spawned subagent Task calls
	teammateMessages int    // count of distinct teammate IDs sending messages
}

// savedDetailState preserves parent detail view state when drilling into a
// subagent trace. Restored on Escape.
type savedDetailState struct {
	cursor   int
	scroll   int
	expanded map[int]bool
	label    string // breadcrumb label for the parent view, e.g. "Claude opus4.6"
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
	sessionPath    string
	watching       bool
	watcher        *sessionWatcher
	tailSub        chan tailUpdate
	tailErrc       chan error
	sessionOngoing bool // whether the watched session is still in progress
	animFrame      int  // animation frame counter for activity indicator

	// Subagent trace drill-down state
	traceMsg    *message          // non-nil when viewing a subagent's execution trace
	savedDetail *savedDetailState // parent detail state to restore on drill-back

	// Session picker state
	pickerSessions     []parser.SessionInfo
	pickerItems        []pickerItem
	pickerCursor       int
	pickerScroll       int
	pickerWatcher      *pickerWatcher
	pickerAnimFrame    int          // 0 or 1, toggled by tick for ongoing dot blink
	pickerHasOngoing   bool         // gates tick command
	pickerExpanded     map[int]bool // tab-expanded previews in picker
	pickerUniformModel bool         // all sessions share the same model family
}

// loadResult holds everything needed to bootstrap the TUI and watcher.
type loadResult struct {
	messages     []message
	path         string
	classified   []parser.ClassifiedMsg
	offset       int64
	ongoing      bool
	hasTeamTasks bool
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

	// Discover and link subagent execution traces.
	subagents, _ := parser.DiscoverSubagents(path)
	teamSessions, _ := parser.DiscoverTeamSessions(path, chunks)
	allSubagents := append(subagents, teamSessions...)
	parser.LinkSubagents(allSubagents, chunks, path)

	return loadResult{
		messages:     chunksToMessages(chunks, allSubagents),
		path:         path,
		classified:   classified,
		offset:       offset,
		ongoing:      parser.IsOngoing(chunks),
		hasTeamTasks: len(teamSessions) > 0 || hasTeamTaskItems(chunks),
	}, nil
}

// chunksToMessages maps parser output into the TUI's message type.
// Discovered subagent processes are linked to their corresponding
// ItemSubagent display items by matching ParentTaskID to ToolID.
func chunksToMessages(chunks []parser.Chunk, subagents []parser.SubagentProcess) []message {
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
			// Count distinct team-spawned subagents and teammate message senders.
			var teamSpawns int
			teammateIDs := make(map[string]bool)
			for _, it := range c.Items {
				if it.Type == parser.ItemSubagent && isTeamTaskItem(&it) {
					teamSpawns++
				}
				if it.Type == parser.ItemTeammateMessage && it.TeammateID != "" {
					teammateIDs[it.TeammateID] = true
				}
			}
			msgs = append(msgs, message{
				role:             RoleClaude,
				model:            shortModel(c.Model),
				content:          c.Text,
				thinkingCount:    c.ThinkingCount,
				toolCallCount:    len(c.ToolCalls),
				messages:         countOutputItems(c.Items),
				tokensRaw:        c.Usage.TotalTokens(),
				durationMs:       c.DurationMs,
				timestamp:        formatTime(c.Timestamp),
				items:            convertDisplayItems(c.Items, subagents),
				lastOutput:       parser.FindLastOutput(c.Items),
				teammateSpawns:   teamSpawns,
				teammateMessages: len(teammateIDs),
			})
		case parser.SystemChunk:
			msgs = append(msgs, message{
				role:      RoleSystem,
				content:   c.Output,
				timestamp: formatTime(c.Timestamp),
			})
		case parser.CompactChunk:
			msgs = append(msgs, message{
				role:      RoleCompact,
				content:   c.Output,
				timestamp: formatTime(c.Timestamp),
			})
		}
	}
	return msgs
}

// displayItemFromParser maps a single parser.DisplayItem to the TUI's displayItem,
// including JSON pretty-printing of tool input.
func displayItemFromParser(it parser.DisplayItem) displayItem {
	input := ""
	if len(it.ToolInput) > 0 {
		var pretty bytes.Buffer
		if json.Indent(&pretty, it.ToolInput, "", "  ") == nil {
			input = pretty.String()
		} else {
			input = string(it.ToolInput)
		}
	}
	return displayItem{
		itemType:     it.Type,
		text:         it.Text,
		toolName:     it.ToolName,
		toolSummary:  it.ToolSummary,
		toolInput:    input,
		toolResult:   it.ToolResult,
		toolError:    it.ToolError,
		durationMs:   it.DurationMs,
		tokenCount:   it.TokenCount,
		subagentType: it.SubagentType,
		subagentDesc: it.SubagentDesc,
		teammateID:   it.TeammateID,
	}
}

// convertDisplayItems maps parser.DisplayItem to the TUI's displayItem type.
// Links ItemSubagent items to their discovered SubagentProcess by matching
// ToolID to ParentTaskID.
func convertDisplayItems(items []parser.DisplayItem, subagents []parser.SubagentProcess) []displayItem {
	if len(items) == 0 {
		return nil
	}

	// Build ParentTaskID -> SubagentProcess index for O(1) lookup.
	procByTaskID := make(map[string]*parser.SubagentProcess, len(subagents))
	for i := range subagents {
		if subagents[i].ParentTaskID != "" {
			procByTaskID[subagents[i].ParentTaskID] = &subagents[i]
		}
	}

	out := make([]displayItem, len(items))
	for i, it := range items {
		out[i] = displayItemFromParser(it)
		// Link subagent process if available.
		if it.Type == parser.ItemSubagent {
			out[i].subagentProcess = procByTaskID[it.ToolID]
		}
	}
	return out
}

// currentDetailMsg returns the message being viewed in detail view.
// Returns the trace message when drilled into a subagent, otherwise the
// selected message from the list.
func (m model) currentDetailMsg() message {
	if m.traceMsg != nil {
		return *m.traceMsg
	}
	if m.cursor >= 0 && m.cursor < len(m.messages) {
		return m.messages[m.cursor]
	}
	return message{}
}

// buildSubagentMessage creates a synthetic message from a subagent's execution
// trace. The message contains all items (Input, Output, Tool calls) from the
// subagent's chunks, suitable for rendering in the detail view.
func buildSubagentMessage(proc *parser.SubagentProcess, subagentType string) message {
	var items []displayItem
	var toolCount, thinkCount, msgCount int

	for _, c := range proc.Chunks {
		switch c.Type {
		case parser.UserChunk:
			items = append(items, displayItem{
				itemType: parser.ItemOutput,
				toolName: "Input",
				text:     c.UserText,
			})
			msgCount++
		case parser.AIChunk:
			for _, it := range c.Items {
				items = append(items, displayItemFromParser(it))
				switch it.Type {
				case parser.ItemThinking:
					thinkCount++
				case parser.ItemToolCall, parser.ItemSubagent:
					toolCount++
				case parser.ItemOutput:
					msgCount++
				}
			}
		}
	}

	mdl := ""
	for _, c := range proc.Chunks {
		if c.Type == parser.AIChunk && c.Model != "" {
			mdl = shortModel(c.Model)
			break
		}
	}

	return message{
		role:          RoleClaude,
		model:         mdl,
		items:         items,
		thinkingCount: thinkCount,
		toolCallCount: toolCount,
		messages:      msgCount,
		tokensRaw:     proc.Usage.TotalTokens(),
		durationMs:    proc.DurationMs,
		timestamp:     formatTime(proc.StartTime),
		subagentLabel: subagentType,
	}
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
		cmds := []tea.Cmd{
			waitForTailUpdate(m.tailSub),
			waitForWatcherErr(m.tailErrc),
		}
		if m.sessionOngoing {
			cmds = append(cmds, tickCmd())
		}
		return tea.Batch(cmds...)
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

	case tickMsg:
		if m.watching && m.sessionOngoing {
			m.animFrame++
			return m, tickCmd()
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

		// Start or stop the animation tick based on ongoing state.
		wasOngoing := m.sessionOngoing
		m.sessionOngoing = msg.ongoing
		cmds := []tea.Cmd{waitForTailUpdate(m.tailSub)}
		if m.sessionOngoing && !wasOngoing {
			cmds = append(cmds, tickCmd())
		}
		return m, tea.Batch(cmds...)

	case watcherErrMsg:
		// Transient watcher errors: re-subscribe and keep going.
		return m, waitForWatcherErr(m.tailErrc)

	case pickerTickMsg:
		if m.view == viewPicker && m.pickerHasOngoing {
			m.pickerAnimFrame = 1 - m.pickerAnimFrame
			return m, pickerTickCmd()
		}
		return m, nil

	case pickerSessionsMsg:
		if msg.err != nil {
			// Fall back to list view on error.
			return m, nil
		}
		m.pickerSessions = msg.sessions
		m.pickerItems = rebuildPickerItems(msg.sessions)
		m.pickerScroll = 0
		m.pickerExpanded = make(map[int]bool)
		m.view = viewPicker

		// Set cursor to first session item (skip header).
		m.pickerCursor = 0
		for i, item := range m.pickerItems {
			if item.typ == pickerItemSession {
				m.pickerCursor = i
				break
			}
		}

		// Derive ongoing/uniform state and start tick if needed.
		var cmds []tea.Cmd
		if tickCmd := m.updatePickerSessionState(); tickCmd != nil {
			cmds = append(cmds, tickCmd)
		}

		// Start picker directory watcher for live refresh.
		if m.pickerWatcher == nil {
			projectDir, err := parser.CurrentProjectDir()
			if err == nil {
				pw := newPickerWatcher(projectDir)
				go pw.run()
				m.pickerWatcher = pw
				cmds = append(cmds, waitForPickerRefresh(pw.sub))
			}
		}

		return m, tea.Batch(cmds...)

	case pickerRefreshMsg:
		m.pickerSessions = msg.sessions
		m.pickerItems = rebuildPickerItems(msg.sessions)

		// Preserve cursor position by matching session ID.
		oldSession := m.pickerSelectedSession()
		if oldSession != nil {
			for i, item := range m.pickerItems {
				if item.typ == pickerItemSession && item.session.SessionID == oldSession.SessionID {
					m.pickerCursor = i
					break
				}
			}
		}

		// Clamp cursor.
		if m.pickerCursor >= len(m.pickerItems) {
			m.pickerCursorLast()
		}
		m.ensurePickerVisible()

		// Refresh ongoing/uniform state.
		var cmds []tea.Cmd
		if tickCmd := m.updatePickerSessionState(); tickCmd != nil {
			cmds = append(cmds, tickCmd)
		}

		// Re-subscribe for next refresh.
		if m.pickerWatcher != nil {
			cmds = append(cmds, waitForPickerRefresh(m.pickerWatcher.sub))
		}
		return m, tea.Batch(cmds...)

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
		m.sessionOngoing = msg.ongoing
		m.animFrame = 0
		m.view = viewList
		m.computeLineOffsets()

		// Start a new watcher for the selected session.
		w := newSessionWatcher(msg.path, msg.classified, msg.offset)
		w.hasTeamTasks = msg.hasTeamTasks
		go w.run()
		m.watcher = w
		m.watching = true
		m.tailSub = w.sub
		m.tailErrc = w.errc
		cmds := []tea.Cmd{waitForTailUpdate(m.tailSub), waitForWatcherErr(m.tailErrc)}
		if m.sessionOngoing {
			cmds = append(cmds, tickCmd())
		}
		return m, tea.Batch(cmds...)

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

	var parts []string
	for i, msg := range m.messages {
		isSelected := i == m.cursor
		isExpanded := m.expanded[i]
		parts = append(parts, m.renderMessage(msg, width, isSelected, isExpanded).content)
	}

	content := strings.Join(parts, "\n\n")

	// Simple line-based scroll
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	if m.scroll > 0 && m.scroll < totalLines {
		lines = lines[m.scroll:]
	}

	// Truncate to viewport height
	viewHeight := m.listViewHeight()
	if len(lines) > viewHeight {
		lines = lines[:viewHeight]
	}

	output := strings.Join(lines, "\n")

	// Activity indicator (above status bar, only when ongoing)
	indicator := m.renderActivityIndicator(m.width)
	if indicator != "" {
		output += "\n" + indicator
	}

	// Status bar
	status := m.renderStatusBar(
		"j/k", "nav",
		"G/g", "jump",
		"tab", "toggle",
		"enter", "detail",
		"e/c", "expand/collapse",
		"q/esc", "sessions",
		"^C", "quit",
	)

	return output + "\n" + status
}

// viewDetail renders a single message full-screen with scrolling.
func (m model) viewDetail() string {
	msg := m.currentDetailMsg()
	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	r := m.renderDetailContent(msg, width)

	// Strip trailing newlines that lipgloss may add -- they create phantom blank
	// lines when we split on \n, wasting a viewport line and pushing the status
	// bar off-screen.
	content := strings.TrimRight(r.content, "\n")

	// Scroll the content
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	viewHeight := m.detailViewHeight()
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

	// Activity indicator (above status bar, only when ongoing)
	indicator := m.renderActivityIndicator(m.width)
	if indicator != "" {
		output += "\n" + indicator
	}

	// Status bar varies by message type
	hasItems := msg.role == RoleClaude && len(msg.items) > 0
	var status string
	if hasItems {
		status = m.renderStatusBar(
			"j/k", "items",
			"tab", "toggle",
			"enter", "open",
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
	watcher.hasTeamTasks = result.hasTeamTasks
	go watcher.run()

	m := initialModel(result.messages, hasDarkBg)
	m.sessionPath = result.path
	m.watching = true
	m.watcher = watcher
	m.tailSub = watcher.sub
	m.tailErrc = watcher.errc
	m.sessionOngoing = result.ongoing

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
