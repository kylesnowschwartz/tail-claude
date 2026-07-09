package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Synthetic Copilot events for watcher tests (schema mirrors the copilot
// package's fixtures; all content invented).
const (
	cwEvStart = `{"type":"session.start","data":{"sessionId":"99999999-aaaa-4999-8999-999999999999","selectedModel":"claude-sonnet-4.6","context":{"cwd":"/home/dev/example-project","branch":"main"}},"id":"e1","timestamp":"2026-07-01T10:00:00.000Z","parentId":null}` + "\n"
	cwEvUser1 = `{"type":"user.message","data":{"content":"add a greeting function"},"id":"e2","timestamp":"2026-07-01T10:00:01.000Z","parentId":"e1"}` + "\n"
	cwEvAsst1 = `{"type":"assistant.message","data":{"messageId":"m1","content":"Added a greeting function.","model":"claude-sonnet-4.6","outputTokens":10},"id":"e3","timestamp":"2026-07-01T10:00:05.000Z","parentId":"e2"}` + "\n"
	cwEvUser2 = `{"type":"user.message","data":{"content":"now add tests"},"id":"e4","timestamp":"2026-07-01T10:01:00.000Z","parentId":"e3"}` + "\n"
	cwEvAsst2 = `{"type":"assistant.message","data":{"messageId":"m2","content":"Tests added.","model":"claude-sonnet-4.6","outputTokens":8},"id":"e5","timestamp":"2026-07-01T10:01:05.000Z","parentId":"e4"}` + "\n"
)

// writeCopilotEvents writes content to a temp events.jsonl and returns its path.
func writeCopilotEvents(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// receiveUpdate reads one tailUpdateMsg from sub with a timeout.
func receiveUpdate(t *testing.T, sub chan tailUpdateMsg) tailUpdateMsg {
	t.Helper()
	select {
	case u, ok := <-sub:
		if !ok {
			t.Fatal("sub closed before an update arrived")
		}
		return u
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tail update")
	}
	return tailUpdateMsg{}
}

func TestCopilotWatcherIncrementalAppend(t *testing.T) {
	path := writeCopilotEvents(t, cwEvStart+cwEvUser1+cwEvAsst1)

	result, err := loadCopilotSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.messages) != 2 {
		t.Fatalf("initial load: %d messages, want 2 (user + ai)", len(result.messages))
	}
	if result.copilotReader == nil {
		t.Fatal("loadCopilotSession must return the reader for watcher handoff")
	}

	w := newCopilotWatcher(path, result.classified, result.offset, result.copilotReader)

	// Append a new turn and rebuild directly (no fsnotify in this test).
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(cwEvUser2 + cwEvAsst2); err != nil {
		t.Fatal(err)
	}
	f.Close()

	w.readAndRebuild()
	u := receiveUpdate(t, w.sub)
	if len(u.messages) != 4 {
		t.Fatalf("after append: %d messages, want 4", len(u.messages))
	}
	if len(u.teams) != 0 || u.permissionMode != "" {
		t.Error("copilot updates must carry zero teams/permissionMode")
	}

	// A second rebuild with no new data must not duplicate messages.
	w.readAndRebuild()
	u = receiveUpdate(t, w.sub)
	if len(u.messages) != 4 {
		t.Fatalf("idempotent rebuild: %d messages, want 4", len(u.messages))
	}
}

func TestCopilotWatcherTruncationResetsReader(t *testing.T) {
	path := writeCopilotEvents(t, cwEvStart+cwEvUser1+cwEvAsst1+cwEvUser2+cwEvAsst2)

	result, err := loadCopilotSession(path)
	if err != nil {
		t.Fatal(err)
	}
	w := newCopilotWatcher(path, result.classified, result.offset, result.copilotReader)
	oldReader := w.reader

	// Rewrite the file shorter than the watcher's offset (in-place rewrite).
	if err := os.WriteFile(path, []byte(cwEvStart+cwEvUser1+cwEvAsst1), 0o644); err != nil {
		t.Fatal(err)
	}

	w.readAndRebuild()
	u := receiveUpdate(t, w.sub)

	if w.reader == oldReader {
		t.Error("truncation must swap in a fresh Reader (classification state restarts with the file)")
	}
	if len(u.messages) != 2 {
		t.Fatalf("after truncation: %d messages, want 2 (no duplicates, no stale entries)", len(u.messages))
	}

	// Cross-check against a fresh full read.
	fresh, err := loadCopilotSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.messages) != len(u.messages) {
		t.Errorf("rebuild after truncation (%d msgs) diverges from fresh read (%d msgs)",
			len(u.messages), len(fresh.messages))
	}
	if w.offset != fresh.offset {
		t.Errorf("offset after truncation rebuild = %d, want %d", w.offset, fresh.offset)
	}
}

func TestCopilotWatcherStopClosesChannels(t *testing.T) {
	path := writeCopilotEvents(t, cwEvStart+cwEvUser1+cwEvAsst1)

	result, err := loadCopilotSession(path)
	if err != nil {
		t.Fatal(err)
	}
	w := newCopilotWatcher(path, result.classified, result.offset, result.copilotReader)
	go w.run()

	// Trigger a write so a debounce timer may be pending when stop hits.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(cwEvUser2)
	f.Close()
	time.Sleep(50 * time.Millisecond)

	w.stop()

	// sub and errc must close so waitFor* Cmds unblock (drain any queued update first).
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-w.sub:
			if !ok {
				return // closed — success
			}
		case <-deadline:
			t.Fatal("sub not closed after stop()")
		}
	}
}
