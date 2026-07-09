package main

import (
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"

	"github.com/kylesnowschwartz/tail-claude/copilot"
)

// copilotWatcher monitors a Copilot events.jsonl file for appended events and
// pushes rebuilt message lists through the same tailUpdateMsg protocol as
// sessionWatcher: channel identity for stale-update dropping, sub/errc closed
// on exit so waitForTailUpdate/waitForWatcherErr unblock, capacity-1
// drain-and-resend sends. It watches ONLY the events.jsonl file — never the
// session dir (checkpoints/, files/, rewind-snapshots/ would each cost a
// kqueue fd per file; see CLAUDE.md's fd-budget rule).
//
// All data processing (offset, allClassified, reader, rebuilds) happens on
// the single run() goroutine. Timer callbacks send signals instead of calling
// methods directly, avoiding data races.
type copilotWatcher struct {
	path          string
	offset        int64
	allClassified []transcript.ClassifiedMsg
	reader        *copilot.Reader
	sub           chan tailUpdateMsg
	errc          chan error
	done          chan struct{}
	signals       chan struct{} // debounced rebuild trigger; capacity 1

	// Guards the debounce timer so stop() can cancel it safely.
	// Does NOT guard data fields — those are only touched by run().
	mu       sync.Mutex
	debounce *time.Timer
}

// newCopilotWatcher continues from the initial load's classification state:
// reader must be the Reader that produced initialClassified so incremental
// reads pick up model/meta state mid-stream. A nil reader gets a fresh one
// (only correct with offset 0).
func newCopilotWatcher(path string, initialClassified []transcript.ClassifiedMsg, initialOffset int64, reader *copilot.Reader) *copilotWatcher {
	if reader == nil {
		reader = copilot.NewReader()
	}
	return &copilotWatcher{
		path:          path,
		offset:        initialOffset,
		allClassified: initialClassified,
		reader:        reader,
		sub:           make(chan tailUpdateMsg, 1),
		errc:          make(chan error, 1),
		done:          make(chan struct{}),
		signals:       make(chan struct{}, 1),
	}
}

// stop signals the watcher goroutine to exit and cancels any pending debounce.
func (w *copilotWatcher) stop() {
	close(w.done)
	w.mu.Lock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.mu.Unlock()
}

// sendSignal does a non-blocking send on the signals channel. If a signal is
// already pending, this is a no-op (the pending signal triggers a full
// rebuild anyway).
func (w *copilotWatcher) sendSignal() {
	select {
	case w.signals <- struct{}{}:
	default:
	}
}

// run starts the fsnotify watcher loop. Intended to be called as a goroutine.
// Closes sub and errc on exit so blocked waitForTailUpdate/waitForWatcherErr
// Cmds unblock and return nil instead of leaking goroutines.
func (w *copilotWatcher) run() {
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

	for {
		select {
		case <-w.done:
			return

		case <-w.signals:
			w.readAndRebuild()

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Name == w.path && event.Has(fsnotify.Write) {
				w.mu.Lock()
				if w.debounce != nil {
					w.debounce.Stop()
				}
				w.debounce = time.AfterFunc(watcherDebounce, w.sendSignal)
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

// readAndRebuild reads any new events, rebuilds chunks from all classified
// messages, and sends the full update (never a diff — chunks merge
// retroactively). Only called from run() — no synchronization needed on
// data fields.
func (w *copilotWatcher) readAndRebuild() {
	// Truncation guard: a file shorter than our offset was rewritten in
	// place. Classification state must restart with the file, so the Reader
	// is replaced along with the offset/classified reset — reusing the old
	// Reader would double-count or misattribute model/meta state.
	if info, err := os.Stat(w.path); err == nil && info.Size() < w.offset {
		w.offset = 0
		w.allClassified = nil
		w.reader = copilot.NewReader()
	}

	newMsgs, newOffset, err := w.reader.ReadIncremental(w.path, w.offset)
	if err != nil {
		select {
		case w.errc <- err:
		default:
		}
		return
	}

	if len(newMsgs) > 0 || newOffset != w.offset {
		w.offset = newOffset
		w.allClassified = append(w.allClassified, newMsgs...)
	}

	chunks := copilot.BuildChunks(w.allClassified)

	ongoing := false
	if info, err := os.Stat(w.path); err == nil {
		ongoing = copilot.IsOngoing(w.reader.LastEventType, info.ModTime())
	}

	// teams/workflow/permissionMode stay zero-valued: those concepts don't
	// exist for Copilot sessions and the Update handler treats zero as
	// "unchanged"/"none".
	update := tailUpdateMsg{
		messages: chunksToMessages(chunks, nil, nil),
		ongoing:  ongoing,
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
