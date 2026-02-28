package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// pickerRefreshMsg delivers an updated session list from the directory watcher.
type pickerRefreshMsg struct {
	sessions []parser.SessionInfo
}

// pickerWatcher watches project directories for .jsonl file changes and
// pushes refreshed session lists through a channel. Watches all related
// project directories (main + worktree dirs) so worktree sessions appear
// in the picker as soon as they're created.
type pickerWatcher struct {
	projectDirs []string
	cache       *parser.SessionCache
	sub         chan []parser.SessionInfo
	done        chan struct{}
}

func newPickerWatcher(projectDirs []string, cache *parser.SessionCache) *pickerWatcher {
	return &pickerWatcher{
		projectDirs: projectDirs,
		cache:       cache,
		sub:         make(chan []parser.SessionInfo, 1),
		done:        make(chan struct{}),
	}
}

// run watches all project directories for .jsonl changes. Debounces 500ms
// before rescanning. Blocks until stop() is called.
func (pw *pickerWatcher) run() {
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
			debounce = time.AfterFunc(500*time.Millisecond, func() {
				var sessions []parser.SessionInfo
				var err error
				if pw.cache != nil {
					sessions, err = pw.cache.DiscoverAllProjectSessions(pw.projectDirs)
				} else {
					sessions, err = parser.DiscoverAllProjectSessions(pw.projectDirs)
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
			})

		case _, ok := <-w.Errors:
			if !ok {
				return
			}
			// Swallow watch errors -- they're transient.
		}
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
func waitForPickerRefresh(sub chan []parser.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		sessions, ok := <-sub
		if !ok {
			return nil
		}
		return pickerRefreshMsg{sessions: sessions}
	}
}
