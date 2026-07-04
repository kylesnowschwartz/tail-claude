package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
	zone "github.com/lrstanley/bubblezone/v2"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// version is set at build time via ldflags. Falls back to Go module info
// (populated by go install) or "dev" for plain go build.
var version = ""

func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

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
	viewDebug                   // debug log viewer
	viewTeam                    // team task board
	viewStats                   // per-session tool usage stats
)

// staleSessionThreshold controls when an auto-discovered session is
// considered too old to show on startup. If the most recent session
// hasn't been touched in this long, we land on the picker instead.
const staleSessionThreshold = 12 * time.Hour

// tickMsg drives the activity indicator animation. The seq field ties each
// tick to a specific chain — when switchSession or a rising edge starts a new
// chain, the old chain's ticks are silently dropped because their seq no
// longer matches model.tickSeq.
type tickMsg struct{ seq int }

// tickCmd returns a Bubble Tea command that fires a tickMsg every 100ms.
// The seq parameter must match model.tickSeq for the tick to be processed.
func tickCmd(seq int) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{seq: seq}
	})
}

// ongoingGracePeriod is how long the ongoing indicator stays visible after
// the content says "not ongoing." Bridges gaps between API round-trips where
// Claude is thinking but hasn't written new content yet.
const ongoingGracePeriod = 5 * time.Second

// ongoingIdleTimeout is a failsafe: if no tailUpdateMsg arrives within this
// window while the indicator is showing, assume the session is idle. This
// catches cases where every watcher update reports ongoing=true (e.g. pending
// tool calls from context compaction or the active session's own writes) but
// the session is actually between turns with no real activity.
const ongoingIdleTimeout = 15 * time.Second

// ongoingGraceExpiredMsg fires when the grace period elapses without new
// file activity. The seq field matches model.ongoingGraceSeq so stale
// timers (superseded by newer writes) are silently ignored.
type ongoingGraceExpiredMsg struct{ seq int }

func ongoingGraceCmd(seq int) tea.Cmd {
	return tea.Tick(ongoingGracePeriod, func(time.Time) tea.Msg {
		return ongoingGraceExpiredMsg{seq: seq}
	})
}

// gitDirtyTickMsg triggers a periodic check of the git working-tree state.
type gitDirtyTickMsg struct{}

// gitDirtyTickCmd schedules a gitDirtyTickMsg every 3 seconds.
// This is independent of the JSONL watcher so file edits are detected
// even when no new session entries are written.
func gitDirtyTickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return gitDirtyTickMsg{}
	})
}

// debugUpdateMsg carries a rebuilt debug entry list after an incremental read.
type debugUpdateMsg struct {
	entries []parser.DebugEntry
}

// flashClearMsg fires after a delay to clear the ephemeral flash status.
type flashClearMsg struct{}

// flashClearCmd returns a command that clears the flash status after 2 seconds.
func flashClearCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return flashClearMsg{}
	})
}

// editorFinishedMsg is sent when the external $EDITOR process exits.
type editorFinishedMsg struct{ err error }

// displayItem is a structured element within an AI message's detail view.
// Mirrors parser.DisplayItem but with pre-formatted fields for rendering.
type displayItem struct {
	itemType        parser.DisplayItemType
	text            string
	toolName        string
	toolSummary     string
	toolCategory    parser.ToolCategory
	toolInput       string // formatted JSON for display
	toolResult      string
	toolError       bool
	durationMs      int64
	tokenCount      int
	subagentType    string
	subagentDesc    string
	teamMemberName  string // team member name (e.g. "file-counter")
	teammateID      string
	teamColor       string                  // team color name (e.g. "blue", "green")
	subagentProcess *parser.SubagentProcess // linked subagent execution trace
	subagentOngoing bool                    // linked subagent session is still in progress
}

type message struct {
	role             string
	model            string
	content          string
	thinkingCount    int
	toolCallCount    int
	outputCount      int
	tokensRaw        int
	contextTokens    int                  // input + cache tokens (context window snapshot, excludes output)
	contextDelta     *parser.ContextDelta // per-chunk window evolution; nil when no token data
	durationMs       int64
	timestamp        string
	items            []displayItem
	lastOutput       *parser.LastOutput
	expandedPrompt   string // expanded skill/command prompt (shown on expand)
	subagentLabel    string // non-empty for trace views: "Explore", "Plan", etc.
	teammateSpawns   int    // count of distinct team-spawned subagent Task calls
	teammateMessages int    // count of distinct teammate IDs sending messages
	isError          bool   // system message: bash stderr or killed task
}

// previewCacheEntry holds a parsed session for the search preview LRU cache.
type previewCacheEntry struct {
	path     string
	messages []message
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
	listParts    []string // cached per-message rendered content, set by layoutList
	lineOffsets  []int    // starting line of each message in rendered output
	messageLines []int    // number of rendered lines per message

	totalRenderedLines int // total lines in list view, updated by layoutList

	// Detail view state
	view                viewState
	detailScroll        int                    // scroll offset within the detail view
	detailMaxScroll     int                    // cached max scroll for detail view, updated on enter/resize
	detailCursor        int                    // selected row in the flat visible-row list
	detailExpanded      map[int]bool           // which parent items are expanded
	detailChildExpanded map[visibleRowKey]bool // which child items have expanded content

	// Markdown rendering
	md *mdRenderer

	// JSON syntax highlighting
	jsonHL *jsonHL

	// Live tailing state
	sessionPath     string
	watching        bool
	watcher         *sessionWatcher
	tailSub         chan tailUpdateMsg
	tailErrc        chan error
	sessionOngoing  bool                    // whether the watched session is still in progress
	sessionWorkflow parser.WorkflowActivity // background Workflow runs (agent count, last write)
	ongoingGraceSeq int       // sequence counter for grace period timers (stale timers ignored)
	tickSeq         int       // sequence counter for tick chains (stale ticks from old chains ignored)
	lastTailUpdate  time.Time // when the last tailUpdateMsg arrived (ongoing staleness failsafe)
	animFrame       int       // animation frame counter for activity indicator

	// Subagent trace drill-down state
	traceMsg    *message          // non-nil when viewing a subagent's execution trace
	savedDetail *savedDetailState // parent detail state to restore on drill-back

	// Session metadata (extracted once on load, displayed in info bar)
	sessionCwd       string
	sessionGitBranch string // git branch from session JSONL (for project name resolution)
	sessionMode      string

	// Live git context — based on where tail-claude is invoked from (os.Getwd),
	// not the session's cwd. This correctly reflects worktrees and the user's
	// actual current branch, rather than historical data from the JSONL.
	gitCwd     string
	liveBranch string // current branch at gitCwd
	liveDirty  bool   // true when gitCwd working tree has uncommitted changes

	// Footer toggle (? key)
	showKeybinds bool

	// Project directories for session discovery. Set once at startup from
	// CurrentProjectDir(). Exact match only -- no prefix expansion.
	projectDir  string
	projectDirs []string

	// Worktree session discovery
	worktreeProjectDirs []string // extra project dirs from git worktrees (set once at startup)
	pickerWorktreeMode  bool     // true = show sessions from all worktrees

	// Session picker state
	sessionCache       *parser.SessionCache
	pickerSessions     []parser.SessionInfo
	pickerItems        []pickerItem
	pickerCursor       int
	pickerScroll       int
	pickerWatcher      *pickerWatcher
	pickerAnimFrame    int          // spinner frame counter, incremented each tick
	pickerHasOngoing   bool         // true when any session is still in progress
	pickerLoading      bool         // true while initial session discovery is in progress
	pickerExpanded     map[int]bool // tab-expanded previews in picker
	pickerUniformModel bool         // all sessions share the same model family

	// Team task board state
	teams      []parser.TeamSnapshot
	teamScroll int

	// Debug log viewer state
	debugEntries    []parser.DebugEntry // raw parsed entries (before filter/collapse)
	debugFiltered   []parser.DebugEntry // after level filter + duplicate collapse
	debugCursor     int
	debugScroll     int
	debugExpanded   map[int]bool      // which multi-line entries are expanded
	debugMinLevel   parser.DebugLevel // current filter: LevelDebug (all), LevelWarn, LevelError
	debugPath       string            // path to the debug .txt file
	debugWatcher    *debugLogWatcher  // live tailing watcher for debug file
	debugFilterText string            // text search query (stacks with level filter)
	debugFilterMode bool              // true when the / input prompt is active

	// Flash status (ephemeral notification in the info bar, e.g. "Copied: /path/to/file").
	flashStatus string

	// Modal popup (e.g. delete confirmation). When non-nil, captures all input.
	popup *popup

	// Update notification (set once by background version check)
	updateAvailable string // non-empty when a newer version exists (e.g. "v0.9.0")

	// Picker search mode state
	pickerSearchMode    bool         // true when / search is active
	pickerSearchTyping  bool         // true while text input is focused (all chars → input)
	pickerSearchQuery   string       // current search text
	pickerSearchResults []pickerItem // filtered picker items when search active
	pickerSearchGen     int          // generation counter to cancel stale scans

	// Picker preview pane (right side of search split view)
	pickerPreviewMessages []message           // parsed messages for preview pane
	pickerPreviewPath     string              // path of currently loaded preview session
	pickerPreviewLoading  bool                // true while preview session is being parsed
	pickerPreviewGen      int                 // generation counter to cancel stale preview loads
	pickerPreviewCache    []previewCacheEntry // LRU cache of last 5 previews

	// Resume: set before tea.Quit to exec into claude --resume after exit
	resumeSession *parser.SessionInfo
}

// applyDebugFilters rebuilds debugFiltered from debugEntries using the current
// level filter, text filter, and duplicate collapsing. Clamps cursor to valid range.
func (m *model) applyDebugFilters() {
	filtered := parser.FilterByLevel(m.debugEntries, m.debugMinLevel)
	filtered = parser.FilterByText(filtered, m.debugFilterText)
	m.debugFiltered = parser.CollapseDuplicates(filtered)
	if m.debugCursor >= len(m.debugFiltered) {
		m.debugCursor = max(len(m.debugFiltered)-1, 0)
	}
}

// stopDebugWatcher stops the debug log watcher if one is running.
func (m *model) stopDebugWatcher() {
	if m.debugWatcher != nil {
		m.debugWatcher.stop()
		m.debugWatcher = nil
	}
}

// sessionState is the output of the shared rebuild pipeline: everything the
// initial load and the watcher's incremental rebuilds derive from a chunk list.
type sessionState struct {
	messages     []message
	teams        []parser.TeamSnapshot
	allProcs     []parser.SubagentProcess
	ongoing      bool
	hasTeamTasks bool
	workflow     parser.WorkflowActivity
}

// buildSessionState runs the discover/link/ongoing/teams pipeline shared by
// loadSession and sessionWatcher.readAndRebuild. One implementation guarantees
// the initial load and every tail update agree on session state — the two
// copies had previously drifted on ongoing detection.
//
// Ongoing detection: the parent-file staleness guard applies only to the
// parent predicate; subagents carry their own staleness check inside
// isSubagentOngoing, so a fresh subagent keeps the spinner alive even when
// the parent file is stale.
func buildSessionState(path string, chunks []parser.Chunk) sessionState {
	subagents, _ := parser.DiscoverSubagents(path)
	teamProcs, _ := parser.DiscoverTeamSessions(path, chunks)
	allProcs := append(subagents, teamProcs...)
	colorMap := parser.LinkSubagents(allProcs, chunks, path)

	ongoing := parser.IsOngoing(chunks)
	if ongoing {
		// Chunk heuristics can report a false positive on finished sessions;
		// a parent file untouched past the threshold means the process is gone.
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > parser.OngoingStalenessThreshold {
			ongoing = false
		}
	}
	if !ongoing {
		// Parent may be idle while subagents/team members are still working.
		for i := range allProcs {
			if isSubagentOngoing(&allProcs[i]) {
				ongoing = true
				break
			}
		}
	}
	// Background workflows keep working after the parent turn "ends".
	workflow := parser.ScanWorkflowActivity(path)
	if !ongoing && workflow.Active(parser.OngoingStalenessThreshold) {
		ongoing = true
	}

	return sessionState{
		messages:     chunksToMessages(chunks, allProcs, colorMap),
		teams:        parser.ReconstructTeams(chunks, allProcs),
		allProcs:     allProcs,
		ongoing:      ongoing,
		hasTeamTasks: hasTeamTaskItems(chunks),
		workflow:     workflow,
	}
}

// loadResult holds everything needed to bootstrap the TUI and watcher.
type loadResult struct {
	messages     []message
	teams        []parser.TeamSnapshot
	path         string
	classified   []parser.ClassifiedMsg
	offset       int64
	ongoing      bool
	hasTeamTasks bool
	meta         parser.SessionMeta // cwd, branch, permission mode
	workflow     parser.WorkflowActivity
}

// loadSession reads a JSONL session file and converts chunks to display messages.
// The path must be non-empty — callers resolve auto-discovery before calling.
func loadSession(path string) (loadResult, error) {
	if path == "" {
		return loadResult{}, fmt.Errorf("no session path provided")
	}

	classified, offset, err := parser.ReadSessionIncremental(path, 0)
	if err != nil {
		return loadResult{}, fmt.Errorf("reading session %s: %w", path, err)
	}

	chunks := parser.BuildChunks(classified)
	if len(chunks) == 0 {
		return loadResult{}, fmt.Errorf("session %s has no messages", path)
	}

	state := buildSessionState(path, chunks)

	return loadResult{
		messages:     state.messages,
		teams:        state.teams,
		path:         path,
		classified:   classified,
		offset:       offset,
		ongoing:      state.ongoing,
		hasTeamTasks: state.hasTeamTasks,
		meta:         parser.ExtractSessionMeta(path),
		workflow:     state.workflow,
	}, nil
}

// switchSession replaces the current session with a new one, stopping the old
// watcher and starting a new one. Centralizes the state reset that happens when
// the user picks a different session from the picker.
func (m model) switchSession(result loadResult) (model, tea.Cmd) {
	if m.watcher != nil {
		m.watcher.stop()
	}
	m.stopDebugWatcher()

	m.messages = result.messages
	m.teams = result.teams
	m.teamScroll = 0
	m.expanded = make(map[int]bool)
	m.resetDetailState()
	m.cursor = 0
	m.scroll = 0
	m.sessionPath = result.path
	m.sessionOngoing = result.ongoing
	m.sessionWorkflow = result.workflow
	m.sessionCwd = result.meta.Cwd
	m.sessionGitBranch = result.meta.GitBranch
	m.liveBranch = checkGitBranch(m.gitCwd)
	m.sessionMode = result.meta.PermissionMode
	m.liveDirty = checkGitDirty(m.gitCwd)
	// Reset the idle-failsafe clock: it still holds the previous session's last
	// update time, which would immediately clear the new session's ongoing flag.
	m.lastTailUpdate = time.Now()
	m.animFrame = 0
	m.view = viewList
	m.layoutList()

	w := newSessionWatcher(result.path, result.classified, result.offset)
	w.hasTeamTasks = result.hasTeamTasks
	go w.run()
	m.watcher = w
	m.watching = true
	m.tailSub = w.sub
	m.tailErrc = w.errc

	// Start the tick chain unconditionally — see Init for the rationale.
	m.tickSeq++
	cmds := []tea.Cmd{
		waitForTailUpdate(m.tailSub),
		waitForWatcherErr(m.tailErrc),
		tickCmd(m.tickSeq),
	}
	return m, tea.Batch(cmds...)
}

func initialModel(msgs []message, hasDarkBg bool) model {
	return model{
		messages:            msgs,
		expanded:            make(map[int]bool), // all messages start collapsed
		cursor:              0,
		showKeybinds:        false,
		detailExpanded:      make(map[int]bool),
		detailChildExpanded: make(map[visibleRowKey]bool),
		md:                  newMdRenderer(hasDarkBg),
		jsonHL:              newJSONHL(hasDarkBg),
	}
}

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd

	if m.watching {
		cmds = append(cmds,
			waitForTailUpdate(m.tailSub),
			waitForWatcherErr(m.tailErrc),
			// Init is called with a value receiver; any `m.tickSeq++` here
			// would be thrown away when Init returns, and the returning tick
			// would be dropped as stale. Fire the tick with the current seq
			// (zero at startup) so it matches the stored model's seq and the
			// chain self-perpetuates. switchSession handles the bump on
			// re-arm because Update-returned models persist.
			tickCmd(m.tickSeq),
		)
	}

	// When starting in picker view (e.g. stale session or empty project),
	// kick off session discovery across all project dirs (main + worktrees).
	if m.view == viewPicker && len(m.projectDirs) > 0 {
		cmds = append(cmds, loadPickerSessionsCmd(m.projectDirs, m.sessionCache))
		if m.pickerLoading {
			cmds = append(cmds, pickerTickCmd())
		}
	}

	// Check for newer version in the background.
	cmds = append(cmds, checkLatestVersionCmd(resolveVersion()))

	// Poll git dirty state every 3 seconds regardless of JSONL activity.
	if m.gitCwd != "" {
		cmds = append(cmds, gitDirtyTickCmd())
	}

	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutList()
		m.ensureCursorVisible()
		if m.view == viewDetail {
			m.computeDetailMaxScroll()
		}
		return m, nil

	case tickMsg:
		if msg.seq != m.tickSeq || !m.watching {
			return m, nil
		}
		// Failsafe: if no file activity in ongoingIdleTimeout, clear the
		// sessionOngoing flag so renderers hide the parent activity indicator.
		// The chain keeps ticking because subagents may still be active; their
		// own predicates gate spinner visibility.
		if !m.lastTailUpdate.IsZero() && time.Since(m.lastTailUpdate) > ongoingIdleTimeout {
			m.sessionOngoing = false
		}
		m.animFrame++
		if m.view == viewList {
			m.layoutList()
		}
		return m, tickCmd(m.tickSeq)

	case ongoingGraceExpiredMsg:
		// Grace period elapsed. If no newer timer was started (seq matches),
		// the session is genuinely idle — turn off the indicator.
		if msg.seq == m.ongoingGraceSeq {
			m.sessionOngoing = false
		}
		return m, nil

	case gitDirtyTickMsg:
		m.liveDirty = checkGitDirty(m.gitCwd)
		return m, gitDirtyTickCmd()

	case tailUpdateMsg:
		// An update from the previous watcher may already be queued when the
		// user switches (or reloads the same) session. Compare channel identity
		// rather than path so a stopped watcher's update is dropped even when
		// the new session has the same path. Drop without re-arming: the new
		// watcher's sub was armed by switchSession, and re-arming here would
		// add a duplicate receiver.
		if msg.sub != m.tailSub {
			return m, nil
		}
		m.lastTailUpdate = time.Now()

		// Auto-follow only when the user is in the list view AND the cursor
		// is already on the last message. Other views (detail, picker) should
		// receive fresh data but not have their cursor or scroll disturbed.
		wasAtEnd := m.view == viewList && m.cursor >= len(m.messages)-1
		m.messages = msg.messages
		m.teams = msg.teams
		m.sessionWorkflow = msg.workflow
		if msg.permissionMode != "" {
			m.sessionMode = msg.permissionMode
		}
		m.liveDirty = checkGitDirty(m.gitCwd)

		// Clamp cursor if the message list somehow shrank.
		if m.cursor >= len(m.messages) && len(m.messages) > 0 {
			m.cursor = len(m.messages) - 1
		}

		if wasAtEnd && len(m.messages) > 0 {
			m.cursor = len(m.messages) - 1
		}

		// Only recompute list layout when we're looking at it.
		if m.view == viewList {
			m.layoutList()
			if wasAtEnd {
				m.ensureCursorVisible()
			}
		} else if m.view == viewDetail {
			// The current detail message may have grown (new tool calls,
			// streaming text). Recompute max scroll so the user can reach
			// the new content, but don't move their scroll position.
			m.computeDetailMaxScroll()
		}

		// Ongoing indicator with grace period. The tick chain is already
		// running (armed at load/Init), so rising edges only need to flip the
		// flag; falling edges delay the flip by ongoingGracePeriod so the
		// indicator stays steady between API round-trips.
		cmds := []tea.Cmd{waitForTailUpdate(m.tailSub)}
		if msg.ongoing {
			m.sessionOngoing = true
			m.ongoingGraceSeq++ // cancel any pending grace timer
		} else if m.sessionOngoing {
			m.ongoingGraceSeq++
			cmds = append(cmds, ongoingGraceCmd(m.ongoingGraceSeq))
		}
		return m, tea.Batch(cmds...)

	case watcherErrMsg:
		// Transient watcher errors: re-subscribe and keep going.
		return m, waitForWatcherErr(m.tailErrc)

	case pickerTickMsg:
		// Picker spinner visibility is gated per-session by s.IsOngoing at the
		// render site; the tick chain just advances the frame counter while
		// we're in picker view. View-switch drops the chain naturally.
		if m.view == viewPicker {
			m.pickerAnimFrame++
			return m, pickerTickCmd()
		}
		return m, nil

	case pickerSessionsMsg:
		m.pickerLoading = false
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

		m.updatePickerSessionState()
		cmds := []tea.Cmd{pickerTickCmd()}

		// Start picker directory watcher for live refresh.
		if m.pickerWatcher == nil && len(m.projectDirs) > 0 {
			pw := newPickerWatcher(m.projectDirs, m.sessionCache)
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

		m.updatePickerSessionState()

		// Re-subscribe for next refresh.
		var cmds []tea.Cmd
		if m.pickerWatcher != nil {
			cmds = append(cmds, waitForPickerRefresh(m.pickerWatcher.sub))
		}
		return m, tea.Batch(cmds...)

	case loadSessionMsg:
		if msg.err != nil || len(msg.messages) == 0 {
			return m, nil
		}
		return m.switchSession(msg.loadResult)

	case debugUpdateMsg:
		m.debugEntries = msg.entries
		m.applyDebugFilters()
		cmds := []tea.Cmd{}
		if m.debugWatcher != nil {
			cmds = append(cmds, waitForDebugUpdate(m.debugWatcher.sub))
		}
		return m, tea.Batch(cmds...)

	case pickerSearchResultMsg:
		if msg.gen != m.pickerSearchGen {
			return m, nil // stale result
		}
		m.pickerSearchResults = msg.results
		// Set cursor to first session in results.
		m.pickerCursor = 0
		for i, item := range msg.results {
			if item.typ == pickerItemSession {
				m.pickerCursor = i
				break
			}
		}
		m.pickerScroll = 0
		cmd := m.schedulePreviewLoad()
		return m, cmd

	case pickerPreviewTickMsg:
		if msg.gen != m.pickerPreviewGen {
			return m, nil // stale debounce
		}
		s := m.pickerSearchSelectedSession()
		if s == nil {
			m.pickerPreviewLoading = false
			return m, nil
		}
		return m, loadPreviewCmd(*s, m.pickerPreviewGen)

	case pickerPreviewLoadedMsg:
		if msg.gen != m.pickerPreviewGen {
			return m, nil // stale load
		}
		m.pickerPreviewMessages = msg.messages
		m.pickerPreviewPath = msg.path
		m.pickerPreviewLoading = false
		if msg.messages != nil {
			m.addPreviewCache(msg.path, msg.messages)
		}
		return m, nil

	case flashClearMsg:
		m.flashStatus = ""
		return m, nil

	case updateAvailableMsg:
		if msg.version != "" {
			m.updateAvailable = msg.version
		}
		return m, nil

	case editorFinishedMsg:
		// Re-layout after returning from external editor.
		m.layoutList()
		if m.view == viewDetail {
			m.computeDetailMaxScroll()
		}
		return m, nil

	case tea.KeyPressMsg:
		// Suspend on ctrl+z before dispatching to per-view handlers.
		if msg.String() == "ctrl+z" {
			return m, tea.Suspend
		}
		// Modal popup captures all input when active.
		if m.popup != nil {
			return m.updatePopup(msg)
		}
		switch m.view {
		case viewDetail:
			return m.updateDetail(msg)
		case viewPicker:
			return m.updatePicker(msg)
		case viewDebug:
			return m.updateDebug(msg)
		case viewTeam:
			return m.updateTeam(msg)
		case viewStats:
			return m.updateStats(msg)
		default:
			return m.updateList(msg)
		}

	case tea.ResumeMsg:
		// Returned from suspend (fg). Re-layout for potentially changed terminal size.
		m.layoutList()
		if m.view == viewDetail {
			m.computeDetailMaxScroll()
		}
		return m, nil

	case tea.MouseMsg:
		switch m.view {
		case viewPicker:
			return m.updatePickerMouse(msg)
		case viewDetail:
			return m.updateDetailMouse(msg)
		case viewDebug:
			return m.updateDebugMouse(msg)
		case viewTeam:
			return m.updateTeamMouse(msg)
		default:
			return m.updateListMouse(msg)
		}
	}

	return m, nil
}

func (m model) View() tea.View {
	var content string
	if m.width == 0 {
		content = "Loading..."
	} else {
		switch m.view {
		case viewDetail:
			content = m.viewDetail()
		case viewPicker:
			content = m.viewPicker()
		case viewDebug:
			content = m.viewDebugLog()
		case viewTeam:
			content = m.viewTeamBoard()
		case viewStats:
			content = m.viewStats()
		default:
			content = m.viewList()
		}
	}
	if m.popup != nil {
		content = m.renderPopup(content)
	}
	v := tea.NewView(zone.Scan(content))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// viewList renders the message list (main view).
// Content comes from listParts, populated by layoutList — one render pass,
// one source of truth for both layout metadata and display content.
func (m model) viewList() string {
	content := strings.Join(m.listParts, "\n")

	// Simple line-based scroll
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	if m.scroll > 0 && m.scroll < totalLines {
		lines = lines[m.scroll:]
	}

	// Truncate to viewport height to avoid building a full string we'd discard.
	viewHeight := m.contentHeight(0, m.activityIndicatorHeight())
	if len(lines) > viewHeight {
		lines = lines[:viewHeight]
	}

	// Footer: info bar + optional keybind hints
	footerPairs := []string{
		"j/k", "nav",
		"↑/↓", "scroll",
		"G/g", "jump",
		"tab", "toggle",
		"enter", "detail",
		"d", "debug log",
	}
	if len(m.teams) > 0 {
		footerPairs = append(footerPairs, "t", "tasks")
	}
	footerPairs = append(footerPairs,
		"e/c", "expand/collapse",
		"y", "copy path",
		"O", "editor",
		"q/esc", "sessions",
		"?", "keys",
	)

	return (screenLayout{
		lines:   lines,
		middle:  m.renderActivityIndicator(m.width),
		footer:  m.renderFooter(footerPairs...),
		screenH: m.height,
		width:   m.width,
		cw:      m.clampWidth(),
	}).assemble()
}

// viewDetail renders a single message full-screen with scrolling.
func (m model) viewDetail() string {
	msg := m.currentDetailMsg()
	width := m.clampWidth()

	r := m.renderDetailContent(msg, width)

	// Strip trailing newlines that lipgloss may add -- they create phantom blank
	// lines when we split on \n, wasting a viewport line and pushing the status
	// bar off-screen.
	content := strings.TrimRight(r.content, "\n")

	// Scroll the content
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	viewHeight := m.contentHeight(0, m.activityIndicatorHeight())
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

	// Footer varies by message type
	hasItems := msg.role == RoleClaude && len(msg.items) > 0
	var footer string
	if hasItems {
		footer = m.renderFooter(
			"j/k", "items",
			"tab", "toggle",
			"enter", "open",
			"↑/↓", "scroll",
			"J/K", "page",
			"G/g", "jump",
			"q/esc", "back",
			"?", "keys",
		)
	} else {
		footer = m.renderFooter(
			"j/k", "scroll",
			"↑/↓", "scroll",
			"G/g", "jump",
			"q/esc", "back",
			"?", "keys",
		)
	}

	return (screenLayout{
		lines:   lines,
		middle:  m.renderActivityIndicator(m.width),
		footer:  footer,
		screenH: m.height,
		width:   m.width,
		cw:      width,
	}).assemble()
}

// runAndMaybeResume runs the Bubble Tea program. If the user triggered a
// resume action, it exec's into `claude --resume <id>` after the TUI exits.
// This never returns when resuming (syscall.Exec replaces the process).
func runAndMaybeResume(p *tea.Program) {
	fm, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	m := fm.(model)
	if m.resumeSession == nil {
		return
	}

	// Change to the session's working directory so claude picks up context.
	if m.resumeSession.Cwd != "" {
		os.Chdir(m.resumeSession.Cwd)
	}

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tail-claude: claude not found in PATH\n")
		os.Exit(1)
	}
	syscall.Exec(claudePath, []string{"claude", "--resume", m.resumeSession.SessionID}, os.Environ())
}

// resolveSessionName maps a non-path positional argument to a session file
// by matching the session's Title (custom-title or ai-title). It first
// searches the current project and its worktrees (cheap — usually dozens
// of files), then falls back to every Claude project directory on disk.
// This biases the common case — "find the session I just named here" —
// toward a fast answer while still supporting cross-project lookup.
func resolveSessionName(name string) (string, error) {
	localDirs := localProjectDirsForResolver()

	matches, err := parser.FindTitleMatches(name, localDirs)
	if err != nil {
		return "", fmt.Errorf("tail-claude: searching local projects: %w", err)
	}
	if len(matches) == 0 && len(localDirs) > 0 {
		all, err := parser.ListAllProjectDirs()
		if err != nil {
			return "", fmt.Errorf("tail-claude: listing projects: %w", err)
		}
		remainder := excludeDirs(all, localDirs)
		matches, err = parser.FindTitleMatches(name, remainder)
		if err != nil {
			return "", fmt.Errorf("tail-claude: searching all projects: %w", err)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("tail-claude: no session found matching %q (not a readable file and no session title matched)", name)
	case 1:
		return matches[0].Path, nil
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "tail-claude: %d sessions match %q:\n", len(matches), name)
		for _, m := range matches {
			fmt.Fprintf(&b, "  %s  %-40s  %s\n", m.ModTime.Format("2006-01-02 15:04"), m.Title, m.Path)
		}
		b.WriteString("Narrow the query or pass the path directly.")
		return "", fmt.Errorf("%s", b.String())
	}
}

// localProjectDirsForResolver returns the current project's session directory
// plus any worktree directories. Returns nil when the CWD isn't inside a
// tracked project — the resolver then jumps straight to global search.
func localProjectDirsForResolver() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	projectDir, err := parser.CurrentProjectDir()
	if err != nil || projectDir == "" {
		return nil
	}
	dirs := []string{projectDir}
	for _, wt := range discoverWorktreeDirs(cwd) {
		if wtDir, err := parser.ProjectDirForPath(wt); err == nil && wtDir != projectDir {
			dirs = append(dirs, wtDir)
		}
	}
	return dedup(dirs)
}

// excludeDirs returns the elements of all that are not in exclude.
func excludeDirs(all, exclude []string) []string {
	skip := make(map[string]struct{}, len(exclude))
	for _, d := range exclude {
		skip[d] = struct{}{}
	}
	out := make([]string, 0, len(all))
	for _, d := range all {
		if _, ok := skip[d]; ok {
			continue
		}
		out = append(out, d)
	}
	return out
}

func main() {
	// Detect terminal background ONCE, before Bubble Tea takes over.
	// lipgloss queries via OSC 11 which can fail in alt-screen mode.
	hasDarkBg := lipgloss.HasDarkBackground(os.Stdin, os.Stderr)
	initTheme(hasDarkBg)
	initIcons()
	zone.NewGlobal()

	dumpMode := false
	expandAll := false
	dumpWidth := 0
	var sessionPath string

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch {
		case arg == "--version" || arg == "-v":
			fmt.Println("tail-claude", resolveVersion())
			os.Exit(0)
		case arg == "--help" || arg == "-h":
			fmt.Printf(`Usage: tail-claude [flags] [session.jsonl]

Without arguments, auto-discovers the most recent session and opens
the interactive TUI.

Pass a JSONL path to view a specific session:
  tail-claude ~/.claude/projects/-Users-me-Code-foo/abc123.jsonl

Pass a session name (custom-title or ai-title, case-insensitive) to find
a session by the name shown in /rename or --name across all projects:
  tail-claude worklog-cron-config

Flags:
  --dump          Print rendered output to stdout (no interactive TUI)
  --expand        Expand all messages (use with --dump)
  --width N       Set terminal width for --dump output (default 160, min 40)
  --update        Update to the latest version via go install
  -v, --version   Show version
  -h, --help      Show this help
`)
			os.Exit(0)
		case arg == "--dump":
			dumpMode = true
		case arg == "--expand":
			expandAll = true
		case arg == "--width":
			i++
			if i >= len(os.Args) {
				fmt.Fprintln(os.Stderr, "--width requires a value")
				os.Exit(1)
			}
			n, err := strconv.Atoi(os.Args[i])
			if err != nil || n < 40 {
				fmt.Fprintln(os.Stderr, "--width must be an integer >= 40")
				os.Exit(1)
			}
			dumpWidth = n
		case arg == "--update":
			fmt.Println("Updating tail-claude...")
			cmd := exec.Command("go", "install", "github.com/kylesnowschwartz/tail-claude@latest")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintln(os.Stderr, "Update failed:", err)
				os.Exit(1)
			}
			fmt.Println("Updated to latest version.")
			os.Exit(0)
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", arg)
			os.Exit(1)
		default:
			sessionPath = arg
		}
	}

	// A non-file positional arg is a name. Resolve it against session titles
	// across all Claude project dirs. Exact title match wins; otherwise
	// substring match. Zero matches → error; multiple → list and exit.
	if sessionPath != "" {
		if _, err := os.Stat(sessionPath); err != nil {
			resolved, rerr := resolveSessionName(sessionPath)
			if rerr != nil {
				fmt.Fprintln(os.Stderr, rerr)
				os.Exit(1)
			}
			sessionPath = resolved
		}
	}

	// Capture the directory tail-claude was invoked from for live git queries.
	invokedFrom, _ := os.Getwd()

	// Resolve the CWD's project directory once — this is the single source of
	// truth for picker discovery and the picker watcher.
	projectDir, _ := parser.CurrentProjectDir()

	var projectDirs []string
	if projectDir != "" {
		projectDirs = []string{projectDir}
	}

	// Discover worktree project dirs for the toggle feature.
	var worktreeProjectDirs []string
	inWorktree := false
	if projectDir != "" {
		for _, wtPath := range discoverWorktreeDirs(invokedFrom) {
			wtDir, err := parser.ProjectDirForPath(wtPath)
			if err != nil || wtDir == projectDir {
				continue
			}
			worktreeProjectDirs = append(worktreeProjectDirs, wtDir)
		}
		// If invoked from inside a worktree, default to showing all worktree
		// sessions so the user sees the session they're actually working in.
		if len(worktreeProjectDirs) > 0 {
			inWorktree = parser.ResolveGitRoot(invokedFrom) != invokedFrom
			if inWorktree {
				projectDirs = dedup(append([]string{projectDir}, worktreeProjectDirs...))
			}
		}
	}

	// When no explicit path was given, find the latest session across the
	// main project and any worktree directories.
	autoDiscovered := sessionPath == ""
	if sessionPath == "" && len(projectDirs) > 0 {
		if sessions, err := parser.DiscoverAllProjectSessions(projectDirs); err == nil && len(sessions) > 0 {
			sessionPath = sessions[0].Path
		}
	}

	// Empty project, no session to show.
	if sessionPath == "" {
		if dumpMode {
			fmt.Fprintln(os.Stderr, "No sessions found for this project.")
			os.Exit(1)
		}

		// Bootstrap an empty picker that live-updates when sessions appear.
		// Ensure the project directory exists so fsnotify can watch it.
		if projectDir != "" {
			os.MkdirAll(projectDir, 0o700)
		}

		m := initialModel(nil, hasDarkBg)
		m.projectDir = projectDir
		m.projectDirs = projectDirs
		m.worktreeProjectDirs = worktreeProjectDirs
		m.pickerWorktreeMode = inWorktree
		m.gitCwd = invokedFrom
		m.liveBranch = checkGitBranch(invokedFrom)
		m.liveDirty = checkGitDirty(invokedFrom)
		m.sessionCache = parser.NewSessionCache()
		m.view = viewPicker
		m.pickerLoading = true

		p := tea.NewProgram(m)
		runAndMaybeResume(p)
		return
	}

	result, err := loadSession(sessionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if dumpMode {
		width := maxContentWidth
		if dumpWidth > 0 {
			width = dumpWidth
		}
		m := initialModel(result.messages, hasDarkBg)
		m.width = width
		m.height = 1_000_000
		m.gitCwd = invokedFrom
		m.sessionCwd = result.meta.Cwd
		m.sessionGitBranch = result.meta.GitBranch
		m.liveBranch = checkGitBranch(invokedFrom)
		m.sessionMode = result.meta.PermissionMode
		m.liveDirty = checkGitDirty(invokedFrom)
		if expandAll {
			for i := range m.messages {
				m.expanded[i] = true
			}
		}
		m.layoutList()
		fmt.Println(m.viewList())
		return
	}

	// Session metadata cache for the picker — unchanged files skip rescanning.
	sessionCache := parser.NewSessionCache()

	// Start the file watcher for live tailing.
	watcher := newSessionWatcher(result.path, result.classified, result.offset)
	watcher.hasTeamTasks = result.hasTeamTasks
	go watcher.run()

	m := initialModel(result.messages, hasDarkBg)
	m.sessionPath = result.path
	m.projectDir = projectDir
	m.projectDirs = projectDirs
	m.worktreeProjectDirs = worktreeProjectDirs
	m.pickerWorktreeMode = inWorktree
	m.watching = true
	m.watcher = watcher
	m.tailSub = watcher.sub
	m.tailErrc = watcher.errc
	m.sessionOngoing = result.ongoing
	m.gitCwd = invokedFrom
	m.sessionCwd = result.meta.Cwd
	m.sessionGitBranch = result.meta.GitBranch
	m.liveBranch = checkGitBranch(invokedFrom)
	m.sessionMode = result.meta.PermissionMode
	m.liveDirty = checkGitDirty(invokedFrom)
	m.teams = result.teams
	m.sessionCache = sessionCache

	// When the session was auto-discovered (no explicit path) and it's stale,
	// start on the picker so the user can choose instead of seeing old output.
	if autoDiscovered && !result.ongoing {
		if info, err := os.Stat(result.path); err == nil {
			if time.Since(info.ModTime()) > staleSessionThreshold {
				m.view = viewPicker
				m.pickerLoading = true
			}
		}
	}

	p := tea.NewProgram(m)
	runAndMaybeResume(p)
}
