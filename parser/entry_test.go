package parser_test

import (
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestParseEntry_ValidLine(t *testing.T) {
	line := []byte(`{"uuid":"abc-123","type":"user","timestamp":"2025-01-15T10:00:00Z","isSidechain":false,"isMeta":false,"message":{"role":"user","content":"hello"}}`)
	entry, ok := parser.ParseEntry(line)
	if !ok {
		t.Fatal("expected ParseEntry to succeed")
	}
	if entry.UUID != "abc-123" {
		t.Errorf("UUID = %q, want %q", entry.UUID, "abc-123")
	}
	if entry.Type != "user" {
		t.Errorf("Type = %q, want %q", entry.Type, "user")
	}
	if entry.Timestamp != "2025-01-15T10:00:00Z" {
		t.Errorf("Timestamp = %q, want %q", entry.Timestamp, "2025-01-15T10:00:00Z")
	}
	if entry.IsSidechain {
		t.Error("IsSidechain should be false")
	}
	if entry.IsMeta {
		t.Error("IsMeta should be false")
	}
	if entry.Message.Role != "user" {
		t.Errorf("Message.Role = %q, want %q", entry.Message.Role, "user")
	}
}

func TestParseEntry_InvalidJSON(t *testing.T) {
	line := []byte(`{not valid json`)
	_, ok := parser.ParseEntry(line)
	if ok {
		t.Fatal("expected ParseEntry to fail on invalid JSON")
	}
}

func TestParseEntry_MissingUUID(t *testing.T) {
	line := []byte(`{"type":"user","timestamp":"2025-01-15T10:00:00Z","message":{"role":"user","content":"hello"}}`)
	_, ok := parser.ParseEntry(line)
	if ok {
		t.Fatal("expected ParseEntry to fail when UUID is missing")
	}
}

func TestParseEntry_EmptyLine(t *testing.T) {
	_, ok := parser.ParseEntry([]byte{})
	if ok {
		t.Fatal("expected ParseEntry to fail on empty input")
	}
}
