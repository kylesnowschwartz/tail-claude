package main

import (
	"sync"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// debugWatcherDebounce is the delay after the last file-write event before
// triggering a re-read. Debug logs can burst during tool execution.
const debugWatcherDebounce = 300 * time.Millisecond

// debugLogWatcher monitors a debug log file for appended lines and pushes
// rebuilt entry lists through a channel. Simpler than sessionWatcher: no
// chunk building, no subagent discovery, just parse-filter-send.
type debugLogWatcher struct {
	path    string
	offset  int64
	sub     chan debugUpdateMsg
	done    chan struct{}
	signals chan struct{} // debounced rebuild trigger; capacity 1

	mu       sync.Mutex
	debounce *time.Timer
}

func newDebugLogWatcher(path string, initialOffset int64) *debugLogWatcher {
	return &debugLogWatcher{
		path:    path,
		offset:  initialOffset,
		sub:     make(chan debugUpdateMsg, 1),
		done:    make(chan struct{}),
		signals: make(chan struct{}, 1),
	}
}

// stop signals the watcher goroutine to exit and cancels any pending debounce.
func (w *debugLogWatcher) stop() {
	close(w.done)
	w.mu.Lock()
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.mu.Unlock()
}

// sendSignal does a non-blocking send on the signals channel.
func (w *debugLogWatcher) sendSignal() {
	select {
	case w.signals <- struct{}{}:
	default:
	}
}

// run starts the fsnotify watcher loop. Intended to be called as a goroutine.
func (w *debugLogWatcher) run() {
	defer close(w.sub)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()

	if err := watcher.Add(w.path); err != nil {
		return
	}

	for {
		select {
		case <-w.done:
			return

		case <-w.signals:
			w.readAndSend()

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Name == w.path && event.Has(fsnotify.Write) {
				w.mu.Lock()
				if w.debounce != nil {
					w.debounce.Stop()
				}
				w.debounce = time.AfterFunc(debugWatcherDebounce, w.sendSignal)
				w.mu.Unlock()
			}

		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// Non-fatal: ignore transient errors.
		}
	}
}

// readAndSend reads new entries from the debug file and sends the full
// rebuilt list. Only called from run() -- no synchronization needed on
// data fields.
func (w *debugLogWatcher) readAndSend() {
	// Re-read from the beginning to get the complete picture.
	// Debug logs are small enough (typically <2000 lines) that this is fast.
	entries, newOffset, err := parser.ReadDebugLog(w.path)
	if err != nil {
		return
	}
	w.offset = newOffset

	update := debugUpdateMsg{entries: entries}

	// Non-blocking send: drop stale update if receiver hasn't consumed yet.
	select {
	case w.sub <- update:
	default:
		select {
		case <-w.sub:
		default:
		}
		w.sub <- update
	}
}

// waitForDebugUpdate blocks on the subscription channel and wraps the result
// in a debugUpdateMsg for the Bubble Tea runtime. Returns nil when the
// channel is closed (watcher stopped).
func waitForDebugUpdate(sub chan debugUpdateMsg) tea.Cmd {
	return func() tea.Msg {
		u, ok := <-sub
		if !ok {
			return nil
		}
		return u
	}
}
