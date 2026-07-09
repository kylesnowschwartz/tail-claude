package copilot

import (
	"testing"
	"time"
)

func TestParseEventEnvelope(t *testing.T) {
	line := `{"type":"user.message","data":{"content":"hi"},"id":"abc-123","timestamp":"2026-05-01T10:00:05.250Z","parentId":"def-456","agentId":"call-0001"}`
	ev, ok := ParseEvent([]byte(line))
	if !ok {
		t.Fatal("ParseEvent returned false for valid line")
	}
	if ev.Type != "user.message" {
		t.Errorf("Type = %q, want user.message", ev.Type)
	}
	if ev.ID != "abc-123" {
		t.Errorf("ID = %q, want abc-123", ev.ID)
	}
	if ev.ParentID == nil || *ev.ParentID != "def-456" {
		t.Errorf("ParentID = %v, want def-456", ev.ParentID)
	}
	if ev.AgentID != "call-0001" {
		t.Errorf("AgentID = %q, want call-0001", ev.AgentID)
	}
	want := time.Date(2026, 5, 1, 10, 0, 5, 250_000_000, time.UTC)
	if !ev.Timestamp.Equal(want) {
		t.Errorf("Timestamp = %v, want %v", ev.Timestamp, want)
	}
	if string(ev.Data) != `{"content":"hi"}` {
		t.Errorf("Data = %s", ev.Data)
	}
}

func TestParseEventNullParentID(t *testing.T) {
	ev, ok := ParseEvent([]byte(`{"type":"session.start","data":{},"id":"x","timestamp":"2026-05-01T10:00:00.000Z","parentId":null}`))
	if !ok {
		t.Fatal("ParseEvent returned false")
	}
	if ev.ParentID != nil {
		t.Errorf("ParentID = %v, want nil", ev.ParentID)
	}
	if ev.AgentID != "" {
		t.Errorf("AgentID = %q, want empty", ev.AgentID)
	}
}

func TestParseEventRejects(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"\n",
		"not json",
		`{"data":{}}`,           // missing type
		`{"type":123,"data":1}`, // wrong type shape
		`{"type":"x"` + "\x00",  // truncated garbage
	}
	for _, c := range cases {
		if _, ok := ParseEvent([]byte(c)); ok {
			t.Errorf("ParseEvent(%q) = true, want false", c)
		}
	}
}

func TestParseEventBadTimestamp(t *testing.T) {
	ev, ok := ParseEvent([]byte(`{"type":"abort","data":{},"id":"x","timestamp":"garbage"}`))
	if !ok {
		t.Fatal("ParseEvent returned false; bad timestamps should not reject the event")
	}
	if !ev.Timestamp.IsZero() {
		t.Errorf("Timestamp = %v, want zero", ev.Timestamp)
	}
}
