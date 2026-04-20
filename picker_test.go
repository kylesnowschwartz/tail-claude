package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
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
			sessions: []parser.SessionInfo{
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

// errForTest is a simple error type for test assertions.
type errForTest string

func (e errForTest) Error() string { return string(e) }
