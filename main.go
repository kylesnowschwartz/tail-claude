package main

import (
	"fmt"
	"os"
	"path/filepath"
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

// staleSessionThreshold controls when an auto-discovered session is
// considered too old to show on startup. If the most recent session
// hasn't been touched in this long, we land on the picker instead.
const staleSessionThreshold = 12 * time.Hour

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
	cursor        int
	scroll        int
	expanded      map[int]bool
	childExpanded map[visibleRowKey]bool
	label         string // breadcrumb label for the parent view, e.g. "Claude opus4.6"
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
	view                viewState
	detailScroll        int                    // scroll offset within the detail view
	detailMaxScroll     int                    // cached max scroll for detail view, updated on enter/resize
	detailCursor        int                    // selected row in the flat visible-row list
	detailExpanded      map[int]bool           // which parent items are expanded
	detailChildExpanded map[visibleRowKey]bool // which child items have expanded content

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
	pickerAnimFrame    int          // spinner frame counter, incremented each tick
	pickerHasOngoing   bool         // true when any session is still in progress
	pickerTickActive   bool         // true while the picker tick loop is running
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
		// Prefer the CWD project's most recent session so the initial view
		// matches what the picker will show. Fall back to global discovery
		// when the CWD has no Claude sessions (e.g. running from /tmp).
		if projectDir, err := parser.CurrentProjectDir(); err == nil {
			if sessions, err := parser.DiscoverProjectSessions(projectDir); err == nil && len(sessions) > 0 {
				path = sessions[0].Path
			}
		}
		if path == "" {
			discovered, err := parser.DiscoverLatestSession()
			if err != nil {
				return loadResult{}, fmt.Errorf("no session found: %w", err)
			}
			path = discovered
		}
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

	ongoing := parser.IsOngoing(chunks)
	if ongoing {
		if info, err := os.Stat(path); err == nil {
			if time.Since(info.ModTime()) > parser.OngoingStalenessThreshold {
				ongoing = false
			}
		}
	}

	return loadResult{
		messages:     chunksToMessages(chunks, allSubagents),
		path:         path,
		classified:   classified,
		offset:       offset,
		ongoing:      ongoing,
		hasTeamTasks: len(teamSessions) > 0 || hasTeamTaskItems(chunks),
	}, nil
}

// switchSession replaces the current session with a new one, stopping the old
// watcher and starting a new one. Centralizes the state reset that happens when
// the user picks a different session from the picker.
func (m model) switchSession(result loadResult) (model, tea.Cmd) {
	if m.watcher != nil {
		m.watcher.stop()
	}

	m.messages = result.messages
	m.expanded = make(map[int]bool)
	m.detailExpanded = make(map[int]bool)
	m.detailChildExpanded = make(map[visibleRowKey]bool)
	m.cursor = 0
	m.scroll = 0
	m.detailCursor = 0
	m.sessionPath = result.path
	m.sessionOngoing = result.ongoing
	m.animFrame = 0
	m.view = viewList
	m.computeLineOffsets()

	w := newSessionWatcher(result.path, result.classified, result.offset)
	w.hasTeamTasks = result.hasTeamTasks
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
}

func initialModel(msgs []message, hasDarkBg bool) model {
	return model{
		messages:            msgs,
		expanded:            make(map[int]bool), // all messages start collapsed
		cursor:              0,
		detailExpanded:      make(map[int]bool),
		detailChildExpanded: make(map[visibleRowKey]bool),
		md:                  newMdRenderer(hasDarkBg),
	}
}

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd

	if m.watching {
		cmds = append(cmds,
			waitForTailUpdate(m.tailSub),
			waitForWatcherErr(m.tailErrc),
		)
		if m.sessionOngoing {
			cmds = append(cmds, tickCmd())
		}
	}

	// When starting in picker view (e.g. stale session), kick off session discovery.
	if m.view == viewPicker {
		cmds = append(cmds, loadPickerSessionsCmd(m.sessionPath))
	}

	return tea.Batch(cmds...)
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
		// Auto-follow only when the user is in the list view AND the cursor
		// is already on the last message. Other views (detail, picker) should
		// receive fresh data but not have their cursor or scroll disturbed.
		wasAtEnd := m.view == viewList && m.cursor >= len(m.messages)-1
		m.messages = msg.messages

		// Clamp cursor if the message list somehow shrank.
		if m.cursor >= len(m.messages) && len(m.messages) > 0 {
			m.cursor = len(m.messages) - 1
		}

		if wasAtEnd && len(m.messages) > 0 {
			m.cursor = len(m.messages) - 1
		}

		// Only recompute list layout when we're looking at it.
		if m.view == viewList {
			m.computeLineOffsets()
			if wasAtEnd {
				m.ensureCursorVisible()
			}
		} else if m.view == viewDetail {
			// The current detail message may have grown (new tool calls,
			// streaming text). Recompute max scroll so the user can reach
			// the new content, but don't move their scroll position.
			m.computeDetailMaxScroll()
		}

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
			m.pickerAnimFrame++
			return m, pickerTickCmd()
		}
		m.pickerTickActive = false
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
		// Derive project dir from the loaded session path, not the CWD.
		if m.pickerWatcher == nil && m.sessionPath != "" {
			pw := newPickerWatcher(filepath.Dir(m.sessionPath))
			go pw.run()
			m.pickerWatcher = pw
			cmds = append(cmds, waitForPickerRefresh(pw.sub))
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
		return m.switchSession(loadResult{
			messages:     msg.messages,
			path:         msg.path,
			classified:   msg.classified,
			offset:       msg.offset,
			ongoing:      msg.ongoing,
			hasTeamTasks: msg.hasTeamTasks,
		})

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

	content := strings.Join(parts, "\n")

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
		"↑/↓", "scroll",
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
			"↑/↓", "scroll",
			"J/K", "page",
			"G/g", "jump",
			"q/esc", "back"+scrollInfo,
		)
	} else {
		status = m.renderStatusBar(
			"j/k", "scroll",
			"↑/↓", "scroll",
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

	// When the session was auto-discovered (no explicit path) and it's stale,
	// start on the picker so the user can choose instead of seeing old output.
	if sessionPath == "" && !result.ongoing {
		if info, err := os.Stat(result.path); err == nil {
			if time.Since(info.ModTime()) > staleSessionThreshold {
				m.view = viewPicker
			}
		}
	}

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
