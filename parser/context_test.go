package parser_test

import (
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestContextWindow_KnownLargeModels(t *testing.T) {
	cases := map[string]int{
		"claude-opus-4-6":          1_000_000,
		"claude-opus-4-7":          1_000_000,
		"claude-opus-4-7-20260201": 1_000_000, // dated suffix still matches prefix
		"claude-sonnet-4-6":        1_000_000,
		"claude-haiku-4-5":         200_000, // not in 1M set
		"claude-3-5-sonnet":        200_000,
		"":                         200_000,
		"unknown-model":            200_000,
	}
	for model, want := range cases {
		if got := parser.ContextWindow(model); got != want {
			t.Errorf("ContextWindow(%q) = %d, want %d", model, got, want)
		}
	}
}

func TestComputeContextDelta_Growth(t *testing.T) {
	// Three cycles: 50k, 120k, 180k on the default 200k window.
	cycles := []parser.InferenceCycle{
		{Model: "claude-haiku-4-5", Usage: parser.Usage{InputTokens: 50_000}},
		{Model: "claude-haiku-4-5", Usage: parser.Usage{InputTokens: 120_000}},
		{Model: "claude-haiku-4-5", Usage: parser.Usage{InputTokens: 180_000}},
	}
	d := parser.ComputeContextDelta(cycles)
	if d == nil {
		t.Fatal("ComputeContextDelta returned nil, want non-nil")
	}
	if d.FirstInputTokens != 50_000 {
		t.Errorf("FirstInputTokens = %d, want 50000", d.FirstInputTokens)
	}
	if d.LastInputTokens != 180_000 {
		t.Errorf("LastInputTokens = %d, want 180000", d.LastInputTokens)
	}
	if d.DeltaTokens != 130_000 {
		t.Errorf("DeltaTokens = %d, want 130000", d.DeltaTokens)
	}
	if d.WindowSize != 200_000 {
		t.Errorf("WindowSize = %d, want 200000", d.WindowSize)
	}
	if d.LastUsagePct != 90.0 {
		t.Errorf("LastUsagePct = %v, want 90.0", d.LastUsagePct)
	}
}

func TestComputeContextDelta_MillionContext(t *testing.T) {
	cycles := []parser.InferenceCycle{
		{Model: "claude-opus-4-7", Usage: parser.Usage{InputTokens: 50_000}},
		{Model: "claude-opus-4-7", Usage: parser.Usage{InputTokens: 180_000}},
	}
	d := parser.ComputeContextDelta(cycles)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.WindowSize != 1_000_000 {
		t.Errorf("WindowSize = %d, want 1000000", d.WindowSize)
	}
	if d.LastUsagePct != 18.0 {
		t.Errorf("LastUsagePct = %v, want 18.0", d.LastUsagePct)
	}
}

func TestComputeContextDelta_AllZeroReturnsNil(t *testing.T) {
	cycles := []parser.InferenceCycle{
		{Model: "claude-opus-4-7", Usage: parser.Usage{InputTokens: 0}},
		{Model: "claude-opus-4-7", Usage: parser.Usage{InputTokens: 0}},
	}
	if d := parser.ComputeContextDelta(cycles); d != nil {
		t.Errorf("got %+v, want nil for all-zero cycles", d)
	}
}

func TestComputeContextDelta_EmptyReturnsNil(t *testing.T) {
	if d := parser.ComputeContextDelta(nil); d != nil {
		t.Errorf("got %+v, want nil for empty cycles", d)
	}
}

func TestComputeContextDelta_MixedModelTakesFirst(t *testing.T) {
	// First non-zero cycle is sonnet (1M), second is haiku (200k).
	// Window comes from the first non-zero cycle.
	cycles := []parser.InferenceCycle{
		{Model: "claude-sonnet-4-6", Usage: parser.Usage{InputTokens: 100_000}},
		{Model: "claude-haiku-4-5", Usage: parser.Usage{InputTokens: 150_000}},
	}
	d := parser.ComputeContextDelta(cycles)
	if d.WindowSize != 1_000_000 {
		t.Errorf("WindowSize = %d, want 1000000 (from first cycle)", d.WindowSize)
	}
}

func TestComputeContextDelta_NegativeDeltaClampedToZero(t *testing.T) {
	// Context can theoretically shrink (compaction mid-turn). Delta is
	// reported as 0, not a negative number.
	cycles := []parser.InferenceCycle{
		{Model: "claude-opus-4-7", Usage: parser.Usage{InputTokens: 100_000}},
		{Model: "claude-opus-4-7", Usage: parser.Usage{InputTokens: 50_000}},
	}
	d := parser.ComputeContextDelta(cycles)
	if d.DeltaTokens != 0 {
		t.Errorf("DeltaTokens = %d, want 0 (clamped)", d.DeltaTokens)
	}
}

func TestComputeContextDelta_CountsCacheTokensTowardSnapshot(t *testing.T) {
	// Under prompt caching, input_tokens is often small (only the NEW tokens).
	// The real context-window snapshot is input + cache_read + cache_creation.
	cycles := []parser.InferenceCycle{
		{Model: "claude-opus-4-7", Usage: parser.Usage{
			InputTokens:         5,       // tiny: new tokens only
			CacheReadTokens:     100_000, // bulk of context comes from cache
			CacheCreationTokens: 0,
		}},
		{Model: "claude-opus-4-7", Usage: parser.Usage{
			InputTokens:         8,
			CacheReadTokens:     150_000,
			CacheCreationTokens: 50_000,
		}},
	}
	d := parser.ComputeContextDelta(cycles)
	if d == nil {
		t.Fatal("got nil, want non-nil (cache tokens should count)")
	}
	if d.FirstInputTokens != 100_005 {
		t.Errorf("FirstInputTokens = %d, want 100005 (input + cache_read)", d.FirstInputTokens)
	}
	if d.LastInputTokens != 200_008 {
		t.Errorf("LastInputTokens = %d, want 200008 (input + cache_read + cache_creation)", d.LastInputTokens)
	}
	if d.DeltaTokens != 100_003 {
		t.Errorf("DeltaTokens = %d, want 100003", d.DeltaTokens)
	}
}

func TestComputeContextDelta_SkipsZeroCycles(t *testing.T) {
	// Cycles with zero input_tokens (truncated/interrupted) are skipped
	// for both first and last selection.
	cycles := []parser.InferenceCycle{
		{Model: "claude-opus-4-7", Usage: parser.Usage{InputTokens: 0}}, // skipped
		{Model: "claude-opus-4-7", Usage: parser.Usage{InputTokens: 60_000}},
		{Model: "claude-opus-4-7", Usage: parser.Usage{InputTokens: 0}}, // skipped
		{Model: "claude-opus-4-7", Usage: parser.Usage{InputTokens: 90_000}},
	}
	d := parser.ComputeContextDelta(cycles)
	if d.FirstInputTokens != 60_000 {
		t.Errorf("FirstInputTokens = %d, want 60000", d.FirstInputTokens)
	}
	if d.LastInputTokens != 90_000 {
		t.Errorf("LastInputTokens = %d, want 90000", d.LastInputTokens)
	}
}

func TestChunkContextDelta_NonAIChunkReturnsNil(t *testing.T) {
	c := parser.Chunk{Type: parser.UserChunk}
	if d := c.ContextDelta(); d != nil {
		t.Errorf("ContextDelta on UserChunk = %+v, want nil", d)
	}
}
