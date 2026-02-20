package parser

import (
	"path/filepath"
	"testing"
)

func TestScanSessionMetadata_OngoingToolUse(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "ongoing_tooluse.jsonl"))
	if !meta.isOngoing {
		t.Error("expected isOngoing=true for session ending with tool_use (no result)")
	}
}

func TestScanSessionMetadata_OngoingToolResult(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "ongoing_toolresult.jsonl"))
	if !meta.isOngoing {
		t.Error("expected isOngoing=true for session ending with tool_result (no text output)")
	}
}

func TestScanSessionMetadata_NotOngoingText(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_text.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session ending with text output")
	}
}

func TestScanSessionMetadata_NotOngoingExitPlan(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_exitplan.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session ending with ExitPlanMode")
	}
}

func TestScanSessionMetadata_NotOngoingShutdown(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_shutdown.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session ending with shutdown_response")
	}
}

func TestScanSessionMetadata_NotOngoingRejected(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_rejected.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session with rejected tool use")
	}
}

func TestScanSessionMetadata_NotOngoingInterrupted(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_interrupted.jsonl"))
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session ending with user interruption")
	}
}

func TestScanSessionMetadata_MultiTurn(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "multi_turn.jsonl"))

	// 3 user messages + 3 first-AI-after-user = 6 turns.
	// a3 is a continuation after tool_result, not a new turn (awaitingAIGroup already false after a2).
	if meta.turnCount != 6 {
		t.Errorf("turnCount = %d, want 6", meta.turnCount)
	}

	// Tokens: sum of all assistant usage fields (non-sidechain, non-synthetic).
	// a1: 100+50+10+5 = 165
	// a2: 200+80+0+0 = 280
	// a3: 300+100+0+0 = 400
	// a4: 400+120+20+0 = 540
	// Total: 1385
	wantTokens := 165 + 280 + 400 + 540
	if meta.totalTokens != wantTokens {
		t.Errorf("totalTokens = %d, want %d", meta.totalTokens, wantTokens)
	}

	// Duration: last timestamp - first timestamp.
	// u1: 10:00:00, a4: 10:02:30 -> 150 seconds = 150000 ms
	if meta.durationMs != 150000 {
		t.Errorf("durationMs = %d, want 150000", meta.durationMs)
	}

	// Model: from first real assistant entry.
	if meta.model != "claude-opus-4-6" {
		t.Errorf("model = %q, want %q", meta.model, "claude-opus-4-6")
	}

	// Preview: first user message.
	if meta.firstMsg != "First question" {
		t.Errorf("firstMsg = %q, want %q", meta.firstMsg, "First question")
	}

	// Ongoing: ends with text output -> not ongoing.
	if meta.isOngoing {
		t.Error("expected isOngoing=false for session ending with text output")
	}
}

func TestScanSessionMetadata_ModelExtraction(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "ongoing_tooluse.jsonl"))
	if meta.model != "claude-sonnet-4-5-20250514" {
		t.Errorf("model = %q, want %q", meta.model, "claude-sonnet-4-5-20250514")
	}
}

func TestScanSessionMetadata_TokenAccumulation(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_text.jsonl"))
	// a1: 500+200+100+0 = 800
	if meta.totalTokens != 800 {
		t.Errorf("totalTokens = %d, want 800", meta.totalTokens)
	}
}

func TestScanSessionMetadata_Duration(t *testing.T) {
	meta := scanSessionMetadata(filepath.Join("testdata", "not_ongoing_text.jsonl"))
	// u1: 10:00:00, a1: 10:00:05 -> 5000 ms
	if meta.durationMs != 5000 {
		t.Errorf("durationMs = %d, want 5000", meta.durationMs)
	}
}
