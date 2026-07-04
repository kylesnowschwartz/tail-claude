package main

import (
	"fmt"
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

// --- renderPreviewPane ------------------------------------------------------

// previewPaneModel builds a model with n plain user messages loaded as the
// search preview, matching how pickerPreviewLoadedMsg populates the state.
func previewPaneModel(n int) model {
	msgs := make([]message, n)
	for i := range msgs {
		msgs[i] = message{role: RoleUser, content: fmt.Sprintf("preview message %d", i)}
	}
	return model{
		width:                 120,
		height:                40,
		md:                    newMdRenderer(true),
		jsonHL:                newJSONHL(true),
		pickerPreviewMessages: msgs,
		pickerPreviewPath:     "/tmp/session-a.jsonl",
		pickerPreviewRender:   &previewRenderCache{},
	}
}

func TestRenderPreviewPaneStopsAtMaxLines(t *testing.T) {
	full := previewPaneModel(50)
	all := full.renderPreviewPane(60, 1<<30)

	m := previewPaneModel(50)
	truncated := m.renderPreviewPane(60, 10)

	if len(truncated) < 10 {
		t.Fatalf("truncated render has %d lines, want >= 10", len(truncated))
	}
	if len(truncated) >= len(all) {
		t.Errorf("truncated render has %d lines, want fewer than full render (%d)",
			len(truncated), len(all))
	}
	if m.pickerPreviewRender.complete {
		t.Error("cache entry marked complete despite early stop")
	}
}

func TestRenderPreviewPaneCache(t *testing.T) {
	m := previewPaneModel(5)
	m.renderPreviewPane(60, 100)

	c := m.pickerPreviewRender
	if c.path != m.pickerPreviewPath || c.width != 60 {
		t.Fatalf("cache key = (%q, %d), want (%q, 60)", c.path, c.width, m.pickerPreviewPath)
	}
	if !c.complete {
		t.Fatal("full render not marked complete")
	}

	// Plant a sentinel to prove the next same-key call is a cache hit.
	c.lines = []string{"SENTINEL"}
	got := m.renderPreviewPane(60, 100)
	if len(got) != 1 || got[0] != "SENTINEL" {
		t.Error("same (path, width) did not hit the cache")
	}

	// A different width must miss the cache — glamour wrapping depends on it.
	got = m.renderPreviewPane(40, 100)
	if len(got) == 1 && got[0] == "SENTINEL" {
		t.Error("width change reused stale cache entry")
	}
	if c.width != 40 {
		t.Errorf("cache width = %d, want 40 after re-render", c.width)
	}

	// A different path must miss the cache.
	c.lines = []string{"SENTINEL"}
	m.pickerPreviewPath = "/tmp/session-b.jsonl"
	got = m.renderPreviewPane(40, 100)
	if len(got) == 1 && got[0] == "SENTINEL" {
		t.Error("path change reused stale cache entry")
	}
}

func TestRenderPreviewPaneTruncatedCacheRerendersForTallerPane(t *testing.T) {
	m := previewPaneModel(50)
	m.renderPreviewPane(60, 10)
	if m.pickerPreviewRender.complete {
		t.Fatal("expected truncated cache entry")
	}

	short := len(m.pickerPreviewRender.lines)
	taller := m.renderPreviewPane(60, short+20)
	if len(taller) <= short {
		t.Errorf("taller pane got %d lines, want more than cached %d", len(taller), short)
	}
}

func TestRenderPreviewPaneNilCache(t *testing.T) {
	// Models built outside initialModel may lack the cache pointer; rendering
	// must still work without it.
	m := previewPaneModel(3)
	m.pickerPreviewRender = nil
	if lines := m.renderPreviewPane(60, 100); len(lines) == 0 {
		t.Error("nil cache produced no output")
	}
}
