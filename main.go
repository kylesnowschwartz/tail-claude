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

// shortModel turns "claude-opus-4-6" into "opus4.6".
func shortModel(m string) string {
	m = strings.TrimPrefix(m, "claude-")
	parts := strings.SplitN(m, "-", 2)
	if len(parts) == 2 {
		modelFamily := parts[0]
		// Keep major-minor only, drop patch/build metadata (e.g. "4-6-20250101" -> "4-6").
		vParts := strings.SplitN(parts[1], "-", 3)
		modelVersion := vParts[0]
		if len(vParts) >= 2 {
			modelVersion = vParts[0] + "-" + vParts[1]
		}
		return modelFamily + strings.ReplaceAll(modelVersion, "-", ".")
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

// isTeamTaskItem checks whether a DisplayItem is a team Task call by looking
// for team_name and name in ToolInput. Thin wrapper matching parser.isTeamTask
// but takes a pointer to avoid allocation.
func isTeamTaskItem(it *parser.DisplayItem) bool {
	if len(it.ToolInput) == 0 {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(it.ToolInput, &fields); err != nil {
		return false
	}
	_, hasTeamName := fields["team_name"]
	_, hasName := fields["name"]
	return hasTeamName && hasName
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
	case "q", "escape":
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
			m.detailExpanded[m.detailCursor] = !m.detailExpanded[m.detailCursor]
			m.computeDetailMaxScroll()
			m.ensureDetailCursorVisible()
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
				m.detailExpanded[m.detailCursor] = !m.detailExpanded[m.detailCursor]
				m.computeDetailMaxScroll()
				m.ensureDetailCursorVisible()
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
	viewHeight := m.height - statusBarHeight - m.activityIndicatorHeight() - 1 // content area above status bar
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
	viewHeight := m.height - statusBarHeight - m.activityIndicatorHeight() - 1 // content area above status bar
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

// computeDetailMaxScroll caches the maximum scroll offset for the detail view.
// Called when entering detail view and on window resize.
func (m *model) computeDetailMaxScroll() {
	if m.width == 0 || m.height == 0 {
		m.detailMaxScroll = 0
		return
	}

	msg := m.currentDetailMsg()
	width := m.width
	if width > maxContentWidth {
		width = maxContentWidth
	}

	content := m.renderDetailContent(msg, width)
	content = strings.TrimRight(content, "\n")
	totalLines := strings.Count(content, "\n") + 1

	viewHeight := m.height - statusBarHeight - m.activityIndicatorHeight()
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
	if m.width == 0 || m.height == 0 {
		return
	}
	msg := m.currentDetailMsg()
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

	viewHeight := m.height - statusBarHeight - m.activityIndicatorHeight()
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

	// Truncate to viewport height minus status bar and activity indicator
	indicatorHeight := m.activityIndicatorHeight()
	viewHeight := m.height - statusBarHeight - indicatorHeight - 1
	if viewHeight > 0 && len(lines) > viewHeight {
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
		"s", "sessions",
		"q", "quit",
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

	content := m.renderDetailContent(msg, width)

	// Strip trailing newlines that lipgloss may add -- they create phantom blank
	// lines when we split on \n, wasting a viewport line and pushing the status
	// bar off-screen.
	content = strings.TrimRight(content, "\n")

	// Scroll the content
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Reserve lines for the status bar and activity indicator.
	indicatorHeight := m.activityIndicatorHeight()
	viewHeight := m.height - statusBarHeight - indicatorHeight
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

// truncate delegates to parser.Truncate for consistent truncation behavior.
func truncate(s string, maxLen int) string {
	return parser.Truncate(s, maxLen)
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
