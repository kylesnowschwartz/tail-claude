package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

// --- search left-pane scrolling ----------------------------------------------

// searchScrollModel builds a picker model in committed search mode with n
// filtered session results, sized so the result list overflows the viewport.
func searchScrollModel(n int) model {
	m := pickerModel()
	m.height = 20 // small viewport to force overflow

	now := time.Now()
	sessions := make([]parser.SessionInfo, n)
	for i := range sessions {
		sessions[i] = parser.SessionInfo{
			Path:         fmt.Sprintf("/tmp/result-%02d.jsonl", i),
			FirstMessage: fmt.Sprintf("needle result %02d", i),
			ModTime:      now.Add(-time.Duration(i) * time.Minute),
		}
	}

	m.pickerSearchMode = true
	m.pickerSearchTyping = false
	m.pickerSearchQuery = "needle"
	m.pickerSearchResults = rebuildPickerItems(sessions)
	for i, item := range m.pickerSearchResults {
		if item.typ == pickerItemSession {
			m.pickerCursor = i
			break
		}
	}
	return m
}

func TestViewPickerSearchScrollShowsLaterResults(t *testing.T) {
	m := searchScrollModel(20)

	// Sanity: an unscrolled view shows early results but not late ones.
	unscrolled := m.viewPickerSearch()
	if !strings.Contains(unscrolled, "result 00") {
		t.Fatal("unscrolled view missing first result")
	}
	if strings.Contains(unscrolled, "result 19") {
		t.Fatal("unscrolled view unexpectedly shows the last result; viewport too tall for this test")
	}

	// Regression: padLines used to truncate BEFORE the scroll slice, so any
	// scrolled view showed only blank padding past the first screenful.
	m.pickerScroll = m.searchPickerTotalLines() - m.contentHeight(1, 0)
	scrolled := m.viewPickerSearch()
	if !strings.Contains(scrolled, "result 19") {
		t.Error("scrolled view does not show the last result")
	}
	if strings.Contains(scrolled, "result 00") {
		t.Error("scrolled view still shows the first result; scroll not applied")
	}
}

func TestPickerSearchNavKeepsCursorVisible(t *testing.T) {
	t.Run("G scrolls to keep the last result visible", func(t *testing.T) {
		m := searchScrollModel(20)

		result, _ := m.updatePickerSearchNav(key("G"))
		got := result.(model)

		if got.pickerScroll == 0 {
			t.Fatal("G did not adjust pickerScroll")
		}
		if !strings.Contains(got.viewPickerSearch(), "result 19") {
			t.Error("cursor row not visible after G")
		}
	})

	t.Run("j past the viewport edge scrolls down", func(t *testing.T) {
		m := searchScrollModel(20)

		var got model = m
		for i := 0; i < 19; i++ {
			result, _ := got.updatePickerSearchNav(key("j"))
			got = result.(model)
		}

		if got.pickerScroll == 0 {
			t.Fatal("j navigation never adjusted pickerScroll")
		}
		if !strings.Contains(got.viewPickerSearch(), "result 19") {
			t.Error("cursor row not visible after navigating to the last result")
		}
	})

	t.Run("k back to the top scrolls up and g resets scroll", func(t *testing.T) {
		m := searchScrollModel(20)

		result, _ := m.updatePickerSearchNav(key("G"))
		got := result.(model)
		for i := 0; i < 19; i++ {
			r, _ := got.updatePickerSearchNav(key("k"))
			got = r.(model)
		}
		// Like the main picker, k only guarantees the cursor row is visible
		// (the group header above it may stay scrolled off).
		if !strings.Contains(got.viewPickerSearch(), "result 00") {
			t.Errorf("first result not visible after k back to top (pickerScroll = %d)", got.pickerScroll)
		}

		r, _ := got.updatePickerSearchNav(key("G"))
		got = r.(model)
		r, _ = got.updatePickerSearchNav(key("g"))
		got = r.(model)
		if got.pickerScroll != 0 {
			t.Errorf("pickerScroll = %d after g, want 0", got.pickerScroll)
		}
	})
}

func TestPickerSearchMouseWheelClampsToFilteredList(t *testing.T) {
	// The unfiltered pickerItems list is long, but the search results are
	// short; the wheel clamp must use the filtered list or it scrolls into
	// blank padding.
	m := searchScrollModel(2)
	now := time.Now()
	var unfiltered []parser.SessionInfo
	for i := 0; i < 50; i++ {
		unfiltered = append(unfiltered, parser.SessionInfo{
			Path:         fmt.Sprintf("/tmp/all-%02d.jsonl", i),
			FirstMessage: fmt.Sprintf("haystack %02d", i),
			ModTime:      now.Add(-time.Duration(i) * time.Minute),
		})
	}
	m.pickerItems = rebuildPickerItems(unfiltered)

	result, _ := m.updatePickerMouse(mouseScroll(tea.MouseWheelDown))
	got := result.(model)

	if got.pickerScroll != 0 {
		t.Errorf("pickerScroll = %d, want 0 (2 results fit on screen; nothing to scroll)", got.pickerScroll)
	}
}

func TestHighlightMatchesRuneSafe(t *testing.T) {
	wrap := func(s string) string { return "(" + s + ")" }
	plain := lipgloss.NewStyle() // attribute-free style renders text unchanged

	tests := []struct {
		name, text, query, want string
	}{
		{"empty query", "abc", "", "(abc)"},
		{"no match", "abc", "z", "(abc)"},
		{"ascii case-insensitive", "Hello World", "world", "(Hello )World"},
		{"adjacent matches", "aAa", "a", "aAa"},
		// U+0130 İ is 2 bytes but lowers to 1-byte i: byte offsets found in
		// the lowered string used to drift past the match (mojibake).
		{"shrinking fold before match", "İZMİR", "r", "(İZMİ)R"},
		// U+023A Ⱥ is 2 bytes but lowers to 3-byte U+2C65: byte offsets used
		// to overshoot the original string (slice-bounds panic).
		{"growing fold before match", "ȺȺx", "x", "(ȺȺ)x"},
		// The matched region keeps its original casing even when its byte
		// length differs from the query's.
		{"fold inside match", "İzmir", "i", "İ(zm)i(r)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := highlightMatches(tt.text, tt.query, wrap, plain)
			if got != tt.want {
				t.Errorf("highlightMatches(%q, %q) = %q, want %q", tt.text, tt.query, got, tt.want)
			}
		})
	}
}

func TestHighlightQueryUnicodeRoundTrip(t *testing.T) {
	// Regression: byte-offset slicing panicked or emitted mid-rune garbage on
	// case folds that change byte length. Stripping the highlight ANSI must
	// recover the original text exactly.
	for _, text := range []string{"ȺȺx", "İZMİR", "plain ascii"} {
		if got := ansi.Strip(highlightQuery(text, "x")); got != text {
			t.Errorf("ansi.Strip(highlightQuery(%q, \"x\")) = %q, want original text", text, got)
		}
	}
}
