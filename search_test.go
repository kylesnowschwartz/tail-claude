package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Realistic current-format lines: the type field sits ~100+ bytes in,
// after parentUuid, isSidechain, cwd, sessionId, version, gitBranch.
const (
	modernUserLine      = `{"parentUuid":null,"isSidechain":false,"userType":"external","cwd":"/Users/kyle/Code/proj","sessionId":"11111111-2222-3333-4444-555555555555","version":"2.1.200","gitBranch":"main","type":"user","message":{"role":"user","content":"hello flibbertigibbet world"},"uuid":"aaaa","timestamp":"2026-07-04T00:00:00Z"}`
	modernAssistantLine = `{"parentUuid":"aaaa","isSidechain":false,"userType":"external","cwd":"/Users/kyle/Code/proj","sessionId":"11111111-2222-3333-4444-555555555555","version":"2.1.200","gitBranch":"main","message":{"role":"assistant","content":[{"type":"text","text":"quixotic reply"}]},"type":"assistant","uuid":"bbbb","timestamp":"2026-07-04T00:00:01Z"}`
	oldUserLine         = `{"uuid":"cccc","type":"user","timestamp":"2025-01-01T00:00:00Z","message":{"role":"user","content":"legacy format"}}`
	summaryLine         = `{"type":"summary","summary":"sesquipedalian recap","leafUuid":"dddd"}`
	snapshotLine        = `{"type":"file-history-snapshot","messageId":"eeee","snapshot":{}}`
)

func TestIsConversationLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"modern user line with deep type field", modernUserLine, true},
		{"modern assistant line with deep type field", modernAssistantLine, true},
		{"older uuid-first user line", oldUserLine, true},
		{"prefix user line", `{"type":"user","message":{"role":"user","content":"hi"}}`, true},
		{"summary entry", summaryLine, false},
		{"file-history-snapshot entry", snapshotLine, false},
		{"last-prompt index entry", `{"type":"last-prompt","prompt":"do the thing"}`, false},
		{"empty line", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConversationLine(tt.line); got != tt.want {
				t.Errorf("isConversationLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestMatchesSessionContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := summaryLine + "\n" + modernUserLine + "\n" + modernAssistantLine + "\n" + snapshotLine + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{"matches user content deep in modern line", "flibbertigibbet", true},
		{"matches assistant content", "quixotic", true},
		{"case-insensitive via lowered query", "quixotic", true},
		{"no match for absent text", "zanzibar", false},
		{"summary-only text ignored (not a conversation line)", "sesquipedalian", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesSessionContent(path, tt.query); got != tt.want {
				t.Errorf("matchesSessionContent(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}

	t.Run("missing file returns false", func(t *testing.T) {
		if matchesSessionContent(filepath.Join(dir, "nope.jsonl"), "x") {
			t.Error("expected false for missing file")
		}
	})
}
