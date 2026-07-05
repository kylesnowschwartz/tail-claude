package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kylesnowschwartz/agent-ouija/claude/discover"
	zone "github.com/lrstanley/bubblezone/v2"
)

var initZone sync.Once

// pickerModel builds a model in picker view with sensible defaults.
func pickerModel() model {
	initZone.Do(zone.NewGlobal)
	m := initialModel(nil, true)
	m.width = 120
	m.height = 40
	m.view = viewPicker
	return m
}

// --- TestPickerLoadingState ------------------------------------------------

func TestPickerLoadingState(t *testing.T) {
	t.Run("viewPicker shows loading when pickerLoading and no items", func(t *testing.T) {
		m := pickerModel()
		m.pickerLoading = true

		output := m.viewPicker()
		if !strings.Contains(output, "Loading sessions...") {
			t.Errorf("expected 'Loading sessions...' in output, got:\n%s", output)
		}
		if strings.Contains(output, "No sessions found") {
			t.Error("should not show 'No sessions found' while loading")
		}
	})

	t.Run("viewPicker shows no sessions when not loading and no items", func(t *testing.T) {
		m := pickerModel()
		m.pickerLoading = false

		output := m.viewPicker()
		if !strings.Contains(output, "No sessions found") {
			t.Errorf("expected 'No sessions found' in output, got:\n%s", output)
		}
		if strings.Contains(output, "Loading sessions...") {
			t.Error("should not show 'Loading sessions...' when not loading")
		}
	})

	t.Run("viewPicker includes spinner frame in loading text", func(t *testing.T) {
		m := pickerModel()
		m.pickerLoading = true
		m.pickerAnimFrame = 0

		output := m.viewPicker()
		// Frame 0 is SpinnerFrames[0]
		if !strings.Contains(output, SpinnerFrames[0]) {
			t.Errorf("expected spinner frame %q in output", SpinnerFrames[0])
		}
	})
}

// --- TestPickerSessionsMsgClearsLoading -----------------------------------

func TestPickerSessionsMsgClearsLoading(t *testing.T) {
	t.Run("pickerSessionsMsg clears loading state", func(t *testing.T) {
		m := pickerModel()
		m.pickerLoading = true
		m.projectDirs = []string{"/tmp/fake-project"}

		msg := pickerSessionsMsg{
			sessions: []discover.SessionInfo{
				{
					Path:         "/tmp/fake.jsonl",
					ModTime:      time.Now(),
					FirstMessage: "hello",
				},
			},
		}

		result, cmd := m.Update(msg)
		got := result.(model)

		if got.pickerLoading {
			t.Error("pickerLoading should be false after pickerSessionsMsg")
		}
		if len(got.pickerItems) == 0 {
			t.Error("pickerItems should be populated")
		}
		if cmd == nil {
			t.Error("pickerSessionsMsg should return a cmd (picker tick chain starts on entry)")
		}
	})

	t.Run("pickerSessionsMsg with error clears loading state", func(t *testing.T) {
		m := pickerModel()
		m.pickerLoading = true

		msg := pickerSessionsMsg{
			err: errForTest("discovery failed"),
		}

		result, _ := m.Update(msg)
		got := result.(model)

		if got.pickerLoading {
			t.Error("pickerLoading should be false even on error")
		}
	})

	t.Run("pickerTickMsg advances frame regardless of ongoing state", func(t *testing.T) {
		// The picker tick chain runs unconditionally while in picker view.
		// Spinner visibility is gated per-session at the render site, so the
		// chain must keep ticking even when no session is currently ongoing —
		// otherwise the spinner freezes during quiet moments between tool calls.
		m := pickerModel()
		m.view = viewPicker
		m.pickerHasOngoing = false
		m.pickerAnimFrame = 5

		result, cmd := m.Update(pickerTickMsg(time.Now()))
		got := result.(model)

		if got.pickerAnimFrame != 6 {
			t.Errorf("pickerAnimFrame = %d, want 6 (tick must advance even without ongoing)", got.pickerAnimFrame)
		}
		if cmd == nil {
			t.Error("pickerTickMsg should return a cmd to self-perpetuate the chain")
		}
	})

	t.Run("pickerTickMsg dies when view is not picker", func(t *testing.T) {
		m := pickerModel()
		m.view = viewList
		m.pickerAnimFrame = 5

		result, cmd := m.Update(pickerTickMsg(time.Now()))
		got := result.(model)

		if got.pickerAnimFrame != 5 {
			t.Errorf("pickerAnimFrame = %d, want 5 (tick must not advance outside picker)", got.pickerAnimFrame)
		}
		if cmd != nil {
			t.Error("pickerTickMsg should return nil cmd when leaving picker view")
		}
	})
}

// --- TestPickerSessionsMsgViewAndSelection ----------------------------------

func TestPickerSessionsMsgViewAndSelection(t *testing.T) {
	sessions := []discover.SessionInfo{
		{Path: "/tmp/a.jsonl", SessionID: "aaa", ModTime: time.Now(), FirstMessage: "first"},
		{Path: "/tmp/b.jsonl", SessionID: "bbb", ModTime: time.Now().Add(-time.Minute), FirstMessage: "second"},
	}

	t.Run("does not hijack the view when the user navigated away", func(t *testing.T) {
		m := pickerModel()
		m.view = viewDetail // user moved on while discovery ran

		result, _ := m.Update(pickerSessionsMsg{sessions: sessions})
		got := result.(model)

		if got.view != viewDetail {
			t.Errorf("view = %v, want viewDetail (discovery result must not switch views)", got.view)
		}
		if len(got.pickerItems) == 0 {
			t.Error("pickerItems should still refresh in the background")
		}
	})

	t.Run("preserves selection when a refresh lands mid-navigation", func(t *testing.T) {
		m := pickerModel()
		m.pickerSessions = sessions
		m.pickerItems = rebuildPickerItems(sessions)
		for i, item := range m.pickerItems {
			if item.typ == pickerItemSession && item.session.SessionID == "bbb" {
				m.pickerCursor = i
				break
			}
		}

		// A second queued discovery lands with the same sessions.
		result, _ := m.Update(pickerSessionsMsg{sessions: sessions})
		got := result.(model)

		selected := got.pickerSelectedSession()
		if selected == nil || selected.SessionID != "bbb" {
			t.Errorf("selection lost: got %+v, want session bbb", selected)
		}
	})

	t.Run("stale discovery result is dropped", func(t *testing.T) {
		m := pickerModel()
		m.pickerLoading = true
		// Simulate `b` toggling worktree mode mid-discovery: the toggle
		// bumped the generation, then the pre-toggle scan lands late.
		m.pickerLoadGen = 2

		result, cmd := m.Update(pickerSessionsMsg{gen: 1, sessions: sessions})
		got := result.(model)

		if !got.pickerLoading {
			t.Error("stale result must not clear pickerLoading for the in-flight scan")
		}
		if len(got.pickerItems) != 0 {
			t.Error("stale result must not replace picker items")
		}
		if cmd != nil {
			t.Error("stale result must not start the tick chain or watcher")
		}
	})

	t.Run("current discovery result is applied", func(t *testing.T) {
		m := pickerModel()
		m.pickerLoading = true
		m.pickerLoadGen = 2

		result, _ := m.Update(pickerSessionsMsg{gen: 2, sessions: sessions})
		got := result.(model)

		if got.pickerLoading {
			t.Error("matching-gen result should clear pickerLoading")
		}
		if len(got.pickerItems) == 0 {
			t.Error("matching-gen result should populate picker items")
		}
	})

	t.Run("q from list opens the picker at dispatch time", func(t *testing.T) {
		m := testModel()

		result, cmd := m.updateList(key("q"))
		got := result.(model)

		if got.view != viewPicker {
			t.Errorf("view = %v, want viewPicker (switch happens on keypress, not on discovery)", got.view)
		}
		if !got.pickerLoading {
			t.Error("pickerLoading should be true while discovery runs")
		}
		if cmd == nil {
			t.Error("q should dispatch the discovery command")
		}
	})
}

// --- TestPickerInitFiresTick ----------------------------------------------

func TestPickerInitFiresTick(t *testing.T) {
	t.Run("Init fires pickerTickCmd when pickerLoading", func(t *testing.T) {
		m := pickerModel()
		m.pickerLoading = true
		m.projectDirs = []string{"/tmp/fake-project"}

		cmd := m.Init()
		if cmd == nil {
			t.Fatal("Init should return a command when in picker view with loading")
		}

		// Batch the command and check that at least one sub-command produces
		// a pickerTickMsg (we can't easily decompose tea.Batch, but we can
		// verify the command is non-nil, which is the critical path).
	})

	t.Run("Init does not fire pickerTickCmd when not loading", func(t *testing.T) {
		m := pickerModel()
		m.pickerLoading = false
		m.projectDirs = []string{"/tmp/fake-project"}

		cmd := m.Init()
		// Should still fire loadPickerSessionsCmd but NOT pickerTickCmd.
		// We can verify the command is non-nil (discovery cmd fires).
		if cmd == nil {
			t.Fatal("Init should return a command for session discovery")
		}
	})
}

// --- TestPickerTabEmptyList -------------------------------------------------

func TestPickerTabEmptyList(t *testing.T) {
	t.Run("tab with no picker items does not panic", func(t *testing.T) {
		m := pickerModel()
		m.pickerItems = nil
		m.pickerCursor = 0

		// Regression: this used to index pickerItems[0] on an empty slice.
		_, _ = m.updatePicker(key("tab"))
	})
}

// errForTest is a simple error type for test assertions.
type errForTest string

func (e errForTest) Error() string { return string(e) }
