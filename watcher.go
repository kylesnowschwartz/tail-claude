package main

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// watcherDebounce is the delay after the last file-write event before
// triggering a rebuild. 500ms coalesces rapid writes (e.g. tool call
// round-trips) into a single re-read, reducing visual churn.
const watcherDebounce = 500 * time.Millisecond

// tailUpdateMsg carries the full rebuilt message list after an incremental read.
// We send the complete list (not a diff) because BuildChunks merges consecutive
// AI messages -- the last chunk can grow as new tool calls or text arrive.
type tailUpdateMsg struct {
	messages       []message
	ongoing        bool   // whether the session appears to still be in progress
	permissionMode string // last-seen permissionMode from new entries; empty if unchanged
}

// watcherErrMsg reports errors from the file watcher goroutine.
type watcherErrMsg struct {
	err error
}

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
	allClassified []parser.ClassifiedMsg
	sub           chan tailUpdateMsg
	errc          chan error
	done          chan struct{}
	signals       chan struct{} // debounced rebuild trigger; capacity 1

	// Guards debounce timers so stop() can cancel them safely.
	// Does NOT guard data fields — those are only touched by run().
	mu           sync.Mutex
	debounce     *time.Timer
	dirDebounce  *time.Timer
	hasTeamTasks bool // true when parent chunks contain team Task items
}

func newSessionWatcher(path string, initialClassified []parser.ClassifiedMsg, initialOffset int64) *sessionWatcher {
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
		w.errc <- err
		return
	}
	defer watcher.Close()

	if err := watcher.Add(w.path); err != nil {
		w.errc <- err
		return
	}

	// Watch the project directory for new team session files.
	// Non-fatal if this fails (directory watch is an optimization).
	projectDir := filepath.Dir(w.path)
	_ = watcher.Add(projectDir)

	for {
		select {
		case <-w.done:
			return

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

// readAndRebuild reads any new parent data, rebuilds chunks from all
// classified messages, discovers subagents, and sends the update.
// Only called from run() — no synchronization needed on data fields.
func (w *sessionWatcher) readAndRebuild() {
	newMsgs, newOffset, err := parser.ReadSessionIncremental(w.path, w.offset)
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
			if u, ok := newMsgs[i].(parser.UserMsg); ok && u.PermissionMode != "" {
				permissionMode = u.PermissionMode
				break
			}
		}
	}

	chunks := parser.BuildChunks(w.allClassified)

	subagents, _ := parser.DiscoverSubagents(w.path)
	colorMap := parser.LinkSubagents(subagents, chunks, w.path)

	// Track whether we have team tasks so directory watches know
	// whether to trigger rebuilds for new .jsonl files.
	w.hasTeamTasks = hasTeamTaskItems(chunks)

	update := tailUpdateMsg{
		messages:       chunksToMessages(chunks, subagents, colorMap),
		ongoing:        parser.IsOngoing(chunks),
		permissionMode: permissionMode,
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
		return watcherErrMsg{err: err}
	}
}
