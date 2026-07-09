package copilot

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
)

func TestReadIncrementalOffsetContinuity(t *testing.T) {
	full, err := os.ReadFile(fixturePath("22222222-bbbb-4222-8222-222222222222", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(full), "\n") // last element is "" after the final \n
	half := len(lines) / 2

	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines[:half], "")), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewReader()
	first, off1, err := r.ReadIncremental(path, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Append the rest and continue with the SAME reader from the offset.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(strings.Join(lines[half:], "")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	rest, off2, err := r.ReadIncremental(path, off1)
	if err != nil {
		t.Fatal(err)
	}
	if off2 != int64(len(full)) {
		t.Errorf("final offset = %d, want %d", off2, len(full))
	}

	split := append(append([]transcript.ClassifiedMsg{}, first...), rest...)

	whole, offFull, err := NewReader().ReadIncremental(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if offFull != off2 {
		t.Errorf("full-read offset = %d, want %d", offFull, off2)
	}
	if !reflect.DeepEqual(split, whole) {
		t.Errorf("split read != full read\nsplit: %+v\nwhole: %+v", split, whole)
	}
}

func TestReadIncrementalPartialTrailingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	complete := `{"type":"user.message","data":{"content":"first prompt"},"id":"1","timestamp":"2026-05-01T10:00:00Z"}` + "\n"
	partial := `{"type":"user.message","data":{"content":"second pro` // mid-append, unparseable
	if err := os.WriteFile(path, []byte(complete+partial), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewReader()
	msgs, off, err := r.ReadIncremental(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d msgs, want 1 (partial line skipped)", len(msgs))
	}
	if off != int64(len(complete)) {
		t.Errorf("offset = %d, want %d (partial line excluded)", off, len(complete))
	}

	// Complete the line; the next incremental read picks it up intact.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`mpt"},"id":"2","timestamp":"2026-05-01T10:00:05Z"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	msgs, _, err = r.ReadIncremental(path, off)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d msgs after completion, want 1", len(msgs))
	}
	if u := msgs[0].(transcript.UserMsg); u.Text != "second prompt" {
		t.Errorf("Text = %q", u.Text)
	}
}

func TestReadIncrementalKeepsParseableUnterminatedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	// No trailing newline, but the line is complete JSON: it must be KEPT
	// and its bytes consumed (a watcher may get no further event for it).
	line := `{"type":"user.message","data":{"content":"final prompt"},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs, off, err := NewReader().ReadIncremental(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d msgs, want 1", len(msgs))
	}
	if off != int64(len(line)) {
		t.Errorf("offset = %d, want %d", off, len(line))
	}
}

func TestReadIncrementalMissingFile(t *testing.T) {
	_, off, err := NewReader().ReadIncremental(filepath.Join(t.TempDir(), "nope.jsonl"), 7)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if off != 7 {
		t.Errorf("offset = %d, want unchanged 7", off)
	}
}

func TestIsConversationLine(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{`{"type":"user.message","data":{"content":"x"}}`, true},
		{`{"type":"assistant.message","data":{"content":"y"}}`, true},
		{`{"type":"tool.execution_start","data":{}}`, false},
		{`{"type":"session.start","data":{}}`, false},
		{``, false},
	}
	for _, c := range cases {
		if got := IsConversationLine(c.line); got != c.want {
			t.Errorf("IsConversationLine(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}
