package parser_test

// Tests for the Claude Code 2.1.18x-2.1.200 session-format changes:
// compact_boundary system entries, empty (redacted) thinking blocks,
// usage.iterations, and the unknown-entry-type fallback gate.
// Background: .agent-history/INVESTIGATION-2026-07-04-format-drift.md

import (
	"encoding/json"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestClassify_CompactBoundary(t *testing.T) {
	e := makeEntry("system", "s1", "2026-07-01T10:00:00.000Z",
		json.RawMessage(`"Conversation compacted"`))
	e.Subtype = "compact_boundary"
	e.CompactMetadata.Trigger = "auto"
	e.CompactMetadata.PreTokens = 165000

	msg, ok := parser.Classify(e)
	if !ok {
		t.Fatal("expected compact_boundary system entry to classify")
	}
	c, isCompact := msg.(parser.CompactMsg)
	if !isCompact {
		t.Fatalf("expected CompactMsg, got %T", msg)
	}
	if c.Text != "Conversation compacted (auto, 165k tokens)" {
		t.Errorf("Text = %q, want %q", c.Text, "Conversation compacted (auto, 165k tokens)")
	}
}

func TestClassify_SystemSubtypesRemainNoise(t *testing.T) {
	for _, subtype := range []string{"turn_duration", "stop_hook_summary", "away_summary", "local_command"} {
		e := makeEntry("system", "s1", "2026-07-01T10:00:00.000Z",
			json.RawMessage(`"whatever"`))
		e.Subtype = subtype
		if _, ok := parser.Classify(e); ok {
			t.Errorf("system subtype %q should be dropped as noise", subtype)
		}
	}
}

func TestClassify_UnknownEntryTypesDropped(t *testing.T) {
	// last-prompt carries a leafUuid so it survives ParseEntry; other
	// metadata types and hypothetical future types must not become phantom
	// meta AIMsgs either.
	for _, typ := range []string{"last-prompt", "custom-title", "agent-name", "mode", "permission-mode", "some-future-type"} {
		e := makeEntry(typ, "u1", "2026-07-01T10:00:00.000Z", nil)
		if _, ok := parser.Classify(e); ok {
			t.Errorf("entry type %q should be dropped, not classified", typ)
		}
	}
}

func TestClassify_EmptyThinkingCountedButNotEmitted(t *testing.T) {
	// Opus 4.7+/Claude 5 transcripts persist thinking as
	// {"thinking":"","signature":"..."} — the count stays truthful but no
	// dead block should reach the display pipeline.
	content := json.RawMessage(`[
		{"type":"thinking","thinking":"","signature":"EqQBCkgIBBABGAI="},
		{"type":"thinking","thinking":"","signature":"EqQBCkgIBBABGAI="},
		{"type":"text","text":"Answer."}
	]`)
	e := makeEntry("assistant", "a1", "2026-07-01T10:00:00.000Z", content,
		withModel("claude-fable-5"))

	msg, ok := parser.Classify(e)
	if !ok {
		t.Fatal("expected assistant message to classify")
	}
	ai := msg.(parser.AIMsg)
	if ai.ThinkingCount != 2 {
		t.Errorf("ThinkingCount = %d, want 2", ai.ThinkingCount)
	}
	for _, b := range ai.Blocks {
		if b.Type == "thinking" {
			t.Errorf("empty thinking block should not be emitted, got %+v", b)
		}
	}
	if len(ai.Blocks) != 1 || ai.Blocks[0].Type != "text" {
		t.Errorf("Blocks = %+v, want single text block", ai.Blocks)
	}
}

func TestClassify_NonEmptyThinkingStillEmitted(t *testing.T) {
	content := json.RawMessage(`[{"type":"thinking","thinking":"real thoughts","signature":"sig"}]`)
	e := makeEntry("assistant", "a1", "2026-07-01T10:00:00.000Z", content,
		withModel("claude-haiku-4-5"))

	msg, _ := parser.Classify(e)
	ai := msg.(parser.AIMsg)
	if ai.ThinkingCount != 1 {
		t.Errorf("ThinkingCount = %d, want 1", ai.ThinkingCount)
	}
	if len(ai.Blocks) != 1 || ai.Blocks[0].Type != "thinking" || ai.Blocks[0].Text != "real thoughts" {
		t.Errorf("Blocks = %+v, want one thinking block with text", ai.Blocks)
	}
}

func TestClassify_UsageIterationsLastWins(t *testing.T) {
	// Multi-iteration usage: the live context window is the LAST iteration's
	// snapshot; the top-level input/cache counts are a merge across cycles.
	// OutputTokens keeps the top-level total.
	e := makeEntry("assistant", "a1", "2026-07-01T10:00:00.000Z",
		json.RawMessage(`[{"type":"text","text":"hi"}]`),
		withModel("claude-fable-5"))
	e.Message.Usage = parser.EntryUsage{
		InputTokens:              900, // merged across iterations — must not win
		OutputTokens:             70,
		CacheReadInputTokens:     5000,
		CacheCreationInputTokens: 400,
		Iterations: []parser.EntryUsage{
			{InputTokens: 500, OutputTokens: 30, CacheReadInputTokens: 2000, CacheCreationInputTokens: 300},
			{InputTokens: 400, OutputTokens: 40, CacheReadInputTokens: 3000, CacheCreationInputTokens: 100},
		},
	}

	msg, _ := parser.Classify(e)
	ai := msg.(parser.AIMsg)
	if ai.Usage.InputTokens != 400 {
		t.Errorf("InputTokens = %d, want 400 (last iteration)", ai.Usage.InputTokens)
	}
	if ai.Usage.CacheReadTokens != 3000 {
		t.Errorf("CacheReadTokens = %d, want 3000 (last iteration)", ai.Usage.CacheReadTokens)
	}
	if ai.Usage.CacheCreationTokens != 100 {
		t.Errorf("CacheCreationTokens = %d, want 100 (last iteration)", ai.Usage.CacheCreationTokens)
	}
	if ai.Usage.OutputTokens != 70 {
		t.Errorf("OutputTokens = %d, want 70 (top-level total)", ai.Usage.OutputTokens)
	}
}

func TestIsOngoing_ThinkingOnlyChunk(t *testing.T) {
	// A trailing AI chunk whose only content was redacted thinking has zero
	// items but a positive ThinkingCount — Claude is mid-thought, so the
	// session is ongoing. Regression test: the empty-thinking skip in
	// Classify must not make thinking-phase sessions look idle.
	textOut := parser.Chunk{Type: parser.AIChunk, Items: []parser.DisplayItem{
		{Type: parser.ItemOutput, Text: "done with the last request"},
	}}
	userPrompt := parser.Chunk{Type: parser.UserChunk, UserText: "next task"}
	thinkingOnly := parser.Chunk{Type: parser.AIChunk, ThinkingCount: 1}

	if !parser.IsOngoing([]parser.Chunk{textOut, userPrompt, thinkingOnly}) {
		t.Error("thinking-only trailing chunk should read as ongoing")
	}
	if parser.IsOngoing([]parser.Chunk{userPrompt, textOut}) {
		t.Error("trailing text output should read as ended")
	}
}

func TestClassify_UsageWithoutIterationsUnchanged(t *testing.T) {
	e := makeEntry("assistant", "a1", "2026-07-01T10:00:00.000Z",
		json.RawMessage(`[{"type":"text","text":"hi"}]`),
		withModel("claude-fable-5"))
	e.Message.Usage = parser.EntryUsage{
		InputTokens: 100, OutputTokens: 50, CacheReadInputTokens: 25, CacheCreationInputTokens: 10,
	}

	msg, _ := parser.Classify(e)
	ai := msg.(parser.AIMsg)
	if ai.Usage.TotalTokens() != 185 {
		t.Errorf("TotalTokens = %d, want 185", ai.Usage.TotalTokens())
	}
}
