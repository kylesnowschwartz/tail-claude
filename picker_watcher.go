package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
	"github.com/kylesnowschwartz/agent-ouija/claude/discover"
)

// pickerRefreshMsg delivers an updated session list from the directory watcher.
type pickerRefreshMsg struct {
	sessions []discover.SessionInfo
}

// pickerWatcher watches project directories for .jsonl file changes and
// pushes refreshed session lists through a channel. Watches all related
// project directories (main + worktree dirs) so worktree sessions appear
// in the picker as soon as they're created.
type pickerWatcher struct {
	projectDirs []string
	cache       *discover.SessionCache
	sub         chan []discover.SessionInfo
	done        chan struct{}
	signals     chan struct{} // debounced rescan trigger; capacity 1, never closed
}

func newPickerWatcher(projectDirs []string, cache *discover.SessionCache) *pickerWatcher {
	return &pickerWatcher{
		projectDirs: projectDirs,
		cache:       cache,
		sub:         make(chan []discover.SessionInfo, 1),
		done:        make(chan struct{}),
		signals:     make(chan struct{}, 1),
	}
}

// run watches all project directories for .jsonl changes. Debounces 500ms
// before rescanning. Blocks until stop() is called.
//
// Closes sub on exit so blocked waitForPickerRefresh Cmds unblock and return
// nil instead of leaking goroutines. run is the only sender on sub: debounce
// timer callbacks route through the signals channel (never closed) so a
// late-firing timer can never send on a closed channel.
func (pw *pickerWatcher) run() {
	defer close(pw.sub)

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer w.Close()

	// Watch all existing project directories. Missing dirs are silently
	// skipped -- they may not exist yet if no worktree session has been created.
	for _, dir := range pw.projectDirs {
		if _, err := os.Stat(dir); err == nil {
			_ = w.Add(dir)
		}
	}

	var debounce *time.Timer

	for {
		select {
		case <-pw.done:
			if debounce != nil {
				debounce.Stop()
			}
			return

		case <-pw.signals:
			// Debounced rescan trigger. Scan and send here on the run
			// goroutine so closing sub on exit can never race a send.
			pw.rescan()

		case event, ok := <-w.Events:
			if !ok {
				return
			}
			// Only care about .jsonl files (not agent_ files).
			name := filepath.Base(event.Name)
			if !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			if strings.HasPrefix(name, "agent_") {
				continue
			}

			// Debounce: reset the timer on each qualifying event.
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(500*time.Millisecond, pw.sendSignal)

		case _, ok := <-w.Errors:
			if !ok {
				return
			}
			// Swallow watch errors -- they're transient.
		}
	}
}

// rescan re-discovers all project sessions and sends the fresh list on sub.
// Only called from run() -- run owns all sends on sub.
func (pw *pickerWatcher) rescan() {
	var sessions []discover.SessionInfo
	var err error
	if pw.cache != nil {
		sessions, err = pw.cache.DiscoverAllProjectSessions(pw.projectDirs)
	} else {
		sessions, err = discover.DiscoverAllProjectSessions(pw.projectDirs)
	}
	if err != nil {
		return
	}
	// Non-blocking send: drop stale refresh if channel is full.
	select {
	case pw.sub <- sessions:
	default:
		// Drain and resend with fresh data.
		select {
		case <-pw.sub:
		default:
		}
		pw.sub <- sessions
	}
}

// sendSignal does a non-blocking send on the signals channel. If a signal
// is already pending, the pending one will trigger a full rescan anyway.
func (pw *pickerWatcher) sendSignal() {
	select {
	case pw.signals <- struct{}{}:
	default:
	}
}

// stop signals the watcher to exit.
func (pw *pickerWatcher) stop() {
	select {
	case <-pw.done:
		// Already closed.
	default:
		close(pw.done)
	}
}

// waitForPickerRefresh returns a Cmd that waits for the next session refresh.
func waitForPickerRefresh(sub chan []discover.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		sessions, ok := <-sub
		if !ok {
			return nil
		}
		return pickerRefreshMsg{sessions: sessions}
	}
}
