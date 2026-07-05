package main

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
	"github.com/kylesnowschwartz/agent-ouija/claude/agents"
	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
)

// watcherDebounce is the delay after the last file-write event before
// triggering a rebuild. 500ms coalesces rapid writes (e.g. tool call
// round-trips) into a single re-read, reducing visual churn.
const watcherDebounce = 500 * time.Millisecond

// workflowPollInterval is the cadence for scanning workflow directories.
// Comfortably inside OngoingStalenessThreshold (2m), and a scan is a
// handful of ReadDir/Stat calls — free for sessions without workflows.
const workflowPollInterval = 2 * time.Second

// tailUpdateMsg carries the full rebuilt message list after an incremental read.
// We send the complete list (not a diff) because BuildChunks merges consecutive
// AI messages -- the last chunk can grow as new tool calls or text arrive.
type tailUpdateMsg struct {
	// sub identifies the watcher this update came from (stamped by
	// waitForTailUpdate). An update queued by the old watcher can arrive after
	// switchSession installs a new one; Update drops messages whose sub does
	// not match m.tailSub so they can't overwrite the new session. Channel
	// identity (not path) so reloading the same session path also invalidates
	// updates from the stopped watcher.
	sub            chan tailUpdateMsg
	messages       []message
	teams          []agents.TeamSnapshot
	ongoing        bool   // whether the session appears to still be in progress
	permissionMode string // last-seen permissionMode from new entries; empty if unchanged
	workflow       agents.WorkflowActivity
}

// watcherErrMsg reports errors from the file watcher goroutine.
type watcherErrMsg struct {
	// errc identifies the watcher this error came from (stamped by
	// waitForWatcherErr). An error queued by the old watcher can arrive
	// after switchSession installs a new one; Update drops mismatches so a
	// stale fatal error can't mark the new session's watcher as dead.
	errc  chan error
	err   error
	fatal bool // run() exited before its watch loop; tailing will never work
}

// fatalWatcherErr wraps errors sent before the watch loop started. run()
// has already returned when one of these arrives, so the session can never
// receive tail updates — the TUI must stop presenting it as live.
type fatalWatcherErr struct{ err error }

func (e fatalWatcherErr) Error() string { return e.err.Error() }

// sessionWatcher monitors a JSONL session file for appended lines and pushes
// rebuilt message lists through a channel. Also watches the project directory
// for new .jsonl files so team member sessions are discovered promptly.
//
// All data processing (offset, allClassified, rebuilds) happens on the single
// run() goroutine. Timer callbacks send signals instead of calling methods
// directly, avoiding data races.
type sessionWatcher struct {
	path          string
	offset        int64
	allClassified []transcript.ClassifiedMsg
	sub           chan tailUpdateMsg
	errc          chan error
	done          chan struct{}
	signals       chan struct{} // debounced rebuild trigger; capacity 1

	// Guards debounce timers so stop() can cancel them safely.
	// Does NOT guard data fields — those are only touched by run().
	mu           sync.Mutex
	debounce     *time.Timer
	dirDebounce  *time.Timer
	teamDebounce *time.Timer
	hasTeamTasks bool // true when parent chunks contain team Task items

	// fsnotify watcher and tracked team session files.
	// Set by run(), used by readAndRebuild to add newly discovered team files.
	fsWatcher        *fsnotify.Watcher
	watchedProcPaths map[string]bool // subagent/team file paths already watched

	// Last workflow activity reported by a rebuild. The poll ticker compares
	// fresh scans against this to signal rebuilds only on new activity.
	// Only touched by run() — no synchronization needed.
	lastWorkflow agents.WorkflowActivity
}

func newSessionWatcher(path string, initialClassified []transcript.ClassifiedMsg, initialOffset int64) *sessionWatcher {
	return &sessionWatcher{
		path:          path,
		offset:        initialOffset,
		allClassified: initialClassified,
		sub:           make(chan tailUpdateMsg, 1),
		errc:          make(chan error, 1),
		done:          make(chan struct{}),
		signals:       make(chan struct{}, 1),
	}
}

// stop signals the watcher goroutine to exit and cancels any pending debounce.
func (w *sessionWatcher) stop() {
	close(w.done)
	w.mu.Lock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	if w.dirDebounce != nil {
		w.dirDebounce.Stop()
	}
	if w.teamDebounce != nil {
		w.teamDebounce.Stop()
	}
	w.mu.Unlock()
}

// sendSignal does a non-blocking send on the signals channel.
// If a signal is already pending, this is a no-op (the pending signal
// will trigger a full rebuild anyway).
func (w *sessionWatcher) sendSignal() {
	select {
	case w.signals <- struct{}{}:
	default:
	}
}

// run starts the fsnotify watcher loop. Intended to be called as a goroutine.
// Watches both the session file (for appended lines) and the project directory
// (for new team member session files). Debounces events so rapid writes
// coalesce into a single rebuild.
//
// Closes sub and errc on exit so blocked waitForTailUpdate/waitForWatcherErr
// Cmds unblock and return nil instead of leaking goroutines.
func (w *sessionWatcher) run() {
	defer close(w.sub)
	defer close(w.errc)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.errc <- fatalWatcherErr{err}
		return
	}
	defer watcher.Close()

	if err := watcher.Add(w.path); err != nil {
		w.errc <- fatalWatcherErr{err}
		return
	}

	// Watch the project directory for new team session files.
	// Non-fatal if this fails (directory watch is an optimization).
	projectDir := filepath.Dir(w.path)
	_ = watcher.Add(projectDir)

	// Store fsnotify watcher so readAndRebuild can add team session files.
	w.fsWatcher = watcher
	w.watchedProcPaths = make(map[string]bool)

	// Workflow activity is polled, never fsnotify-watched: agents write under
	// {session}/subagents/workflows/wf_*/ while the parent file stays silent,
	// but macOS kqueue opens one file descriptor per file in a watched
	// directory — a workflow-heavy session holds hundreds of transcripts,
	// which exhausts the fd budget and breaks select(2)-based readers
	// (FD_SETSIZE caps at 1024). ScanWorkflowActivity holds no descriptors.
	wfPoll := time.NewTicker(workflowPollInterval)
	defer wfPoll.Stop()

	for {
		select {
		case <-w.done:
			return

		case <-wfPoll.C:
			if act := agents.ScanWorkflowActivity(w.path); workflowAdvanced(w.lastWorkflow, act) {
				w.lastWorkflow = act
				w.sendSignal()
			}

		case <-w.signals:
			// Debounced rebuild trigger. Read any new parent data,
			// then rebuild everything (chunks, subagents, team sessions).
			w.readAndRebuild()

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if event.Name == w.path && event.Has(fsnotify.Write) {
				// Parent session file changed — debounce and signal.
				w.mu.Lock()
				if w.debounce != nil {
					w.debounce.Stop()
				}
				w.debounce = time.AfterFunc(watcherDebounce, w.sendSignal)
				w.mu.Unlock()
			} else if event.Has(fsnotify.Create) && w.hasTeamTasks {
				// New file in project directory while we have team tasks.
				// Longer debounce — team sessions need a moment to populate.
				w.mu.Lock()
				if w.dirDebounce != nil {
					w.dirDebounce.Stop()
				}
				w.dirDebounce = time.AfterFunc(500*time.Millisecond, w.sendSignal)
				w.mu.Unlock()
			} else if event.Has(fsnotify.Write) && w.watchedProcPaths[event.Name] {
				// Team session file written to — agent is working. Debounce
				// with a longer window to avoid rebuilding on every tool call.
				w.mu.Lock()
				if w.teamDebounce != nil {
					w.teamDebounce.Stop()
				}
				w.teamDebounce = time.AfterFunc(2*time.Second, w.sendSignal)
				w.mu.Unlock()
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// Non-fatal: forward to TUI, don't log to stderr (leaks through alt screen).
			select {
			case w.errc <- err:
			default:
			}
		}
	}
}

// workflowAdvanced reports whether cur shows workflow activity that prev
// hasn't already reported: a new run, a new agent transcript, or a fresher
// write to any run file.
func workflowAdvanced(prev, cur agents.WorkflowActivity) bool {
	return cur.Runs != prev.Runs || cur.Agents != prev.Agents || cur.LastWrite.After(prev.LastWrite)
}

// readAndRebuild reads any new parent data, rebuilds chunks from all
// classified messages, discovers subagents, and sends the update.
// Only called from run() — no synchronization needed on data fields.
func (w *sessionWatcher) readAndRebuild() {
	// Staleness guard: Claude Code can rewrite a session file in place
	// (resume dedupe, sanitization), leaving it shorter than our offset.
	// Reading from a stale offset would yield garbage mid-line bytes, so
	// restart from the top and rebuild the classified list from scratch.
	if info, err := os.Stat(w.path); err == nil && info.Size() < w.offset {
		w.offset = 0
		w.allClassified = nil
	}

	newMsgs, newOffset, err := transcript.ReadSessionIncremental(w.path, w.offset)
	if err != nil {
		select {
		case w.errc <- err:
		default:
		}
		return
	}

	// Update offset and classified messages if there's new data.
	// Scan new messages for the last-seen permissionMode while we have them.
	var permissionMode string
	if len(newMsgs) > 0 || newOffset != w.offset {
		w.offset = newOffset
		w.allClassified = append(w.allClassified, newMsgs...)

		for i := len(newMsgs) - 1; i >= 0; i-- {
			if u, ok := newMsgs[i].(transcript.UserMsg); ok && u.PermissionMode != "" {
				permissionMode = u.PermissionMode
				break
			}
		}
	}

	chunks := transcript.BuildChunks(w.allClassified)
	state := buildSessionState(w.path, chunks)

	// Track whether we have team tasks so directory watches know
	// whether to trigger rebuilds for new .jsonl files.
	w.hasTeamTasks = state.hasTeamTasks

	// Watch newly discovered team session files for writes so the spinner
	// stays alive while agents work in their own session files.
	if w.fsWatcher != nil {
		for i := range state.allProcs {
			fp := state.allProcs[i].FilePath
			if fp != "" && !w.watchedProcPaths[fp] {
				if err := w.fsWatcher.Add(fp); err == nil {
					w.watchedProcPaths[fp] = true
				}
			}
		}
	}

	// Sync the poll baseline so the ticker doesn't re-signal activity this
	// rebuild already reported.
	w.lastWorkflow = state.workflow

	update := tailUpdateMsg{
		messages:       state.messages,
		teams:          state.teams,
		ongoing:        state.ongoing,
		permissionMode: permissionMode,
		workflow:       state.workflow,
	}

	// Non-blocking send: drop stale update if receiver hasn't consumed yet.
	select {
	case w.sub <- update:
	default:
		// Drain the old value and send the fresh one.
		select {
		case <-w.sub:
		default:
		}
		w.sub <- update
	}
}

// waitForTailUpdate blocks on the subscription channel and wraps the result
// in a tailUpdateMsg for the Bubble Tea runtime. Returns nil when the
// channel is closed (watcher stopped), unblocking the goroutine.
func waitForTailUpdate(sub chan tailUpdateMsg) tea.Cmd {
	return func() tea.Msg {
		u, ok := <-sub
		if !ok {
			return nil
		}
		u.sub = sub // stamp watcher identity so Update can drop stale messages
		return u
	}
}

// waitForWatcherErr blocks on the error channel and wraps the result
// in a watcherErrMsg for the Bubble Tea runtime. Returns nil when the
// channel is closed (watcher stopped), unblocking the goroutine.
func waitForWatcherErr(errc chan error) tea.Cmd {
	return func() tea.Msg {
		err, ok := <-errc
		if !ok {
			return nil
		}
		msg := watcherErrMsg{errc: errc, err: err}
		if fe, isFatal := err.(fatalWatcherErr); isFatal {
			msg.err = fe.err
			msg.fatal = true
		}
		return msg
	}
}
