package main

import (
	"sync"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// tailUpdateMsg carries the full rebuilt message list after an incremental read.
// We send the complete list (not a diff) because BuildChunks merges consecutive
// AI messages -- the last chunk can grow as new tool calls or text arrive.
type tailUpdateMsg struct {
	messages []message
}

// watcherErrMsg reports errors from the file watcher goroutine.
type watcherErrMsg struct {
	err error
}

// sessionWatcher monitors a JSONL session file for appended lines and pushes
// rebuilt message lists through a channel.
type sessionWatcher struct {
	path          string
	offset        int64
	allClassified []parser.ClassifiedMsg
	sub           chan []message
	errc          chan error
	done          chan struct{}

	// Guards the debounce timer so stop() can cancel it safely.
	mu       sync.Mutex
	debounce *time.Timer
}

func newSessionWatcher(path string, initialClassified []parser.ClassifiedMsg, initialOffset int64) *sessionWatcher {
	return &sessionWatcher{
		path:          path,
		offset:        initialOffset,
		allClassified: initialClassified,
		sub:           make(chan []message, 1),
		errc:          make(chan error, 1),
		done:          make(chan struct{}),
	}
}

// stop signals the watcher goroutine to exit and cancels any pending debounce.
func (w *sessionWatcher) stop() {
	close(w.done)
	w.mu.Lock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.mu.Unlock()
}

// run starts the fsnotify watcher loop. Intended to be called as a goroutine.
// Debounces write events with a 100ms timer so rapid appends coalesce into
// a single incremental read.
func (w *sessionWatcher) run() {
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

	for {
		select {
		case <-w.done:
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !event.Has(fsnotify.Write) {
				continue
			}
			// Reset or start the debounce timer.
			w.mu.Lock()
			if w.debounce != nil {
				w.debounce.Stop()
			}
			w.debounce = time.AfterFunc(100*time.Millisecond, func() {
				w.readIncremental()
			})
			w.mu.Unlock()

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

// readIncremental reads new lines from the session file, rebuilds chunks,
// and sends the full message list on w.sub.
func (w *sessionWatcher) readIncremental() {
	newMsgs, newOffset, err := parser.ReadSessionIncremental(w.path, w.offset)
	if err != nil {
		select {
		case w.errc <- err:
		default:
		}
		return
	}

	if len(newMsgs) == 0 && newOffset == w.offset {
		return // nothing new
	}

	w.offset = newOffset
	w.allClassified = append(w.allClassified, newMsgs...)

	chunks := parser.BuildChunks(w.allClassified)
	messages := chunksToMessages(chunks)

	// Non-blocking send: drop stale update if receiver hasn't consumed yet.
	select {
	case w.sub <- messages:
	default:
		// Drain the old value and send the fresh one.
		select {
		case <-w.sub:
		default:
		}
		w.sub <- messages
	}
}

// waitForTailUpdate blocks on the subscription channel and wraps the result
// in a tailUpdateMsg for the Bubble Tea runtime.
func waitForTailUpdate(sub chan []message) tea.Cmd {
	return func() tea.Msg {
		msgs, ok := <-sub
		if !ok {
			return nil
		}
		return tailUpdateMsg{messages: msgs}
	}
}

// waitForWatcherErr blocks on the error channel and wraps the result
// in a watcherErrMsg for the Bubble Tea runtime.
func waitForWatcherErr(errc chan error) tea.Cmd {
	return func() tea.Msg {
		err, ok := <-errc
		if !ok {
			return nil
		}
		return watcherErrMsg{err: err}
	}
}
