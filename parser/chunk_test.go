package parser_test

import (
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestBuildChunks_SingleUser(t *testing.T) {
	msgs := []parser.ClassifiedMsg{
		parser.UserMsg{
			Timestamp: time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			Text:      "Hello",
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].Type != parser.UserChunk {
		t.Errorf("Type = %d, want UserChunk", chunks[0].Type)
	}
	if chunks[0].UserText != "Hello" {
		t.Errorf("UserText = %q, want %q", chunks[0].UserText, "Hello")
	}
}

func TestBuildChunks_UserAIUser(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.UserMsg{Timestamp: t0, Text: "Question"},
		parser.AIMsg{Timestamp: t0.Add(1 * time.Second), Text: "Answer", Model: "claude-opus-4-6"},
		parser.UserMsg{Timestamp: t0.Add(5 * time.Second), Text: "Follow-up"},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 3 {
		t.Fatalf("len(chunks) = %d, want 3", len(chunks))
	}
	if chunks[0].Type != parser.UserChunk {
		t.Errorf("chunks[0].Type = %d, want UserChunk", chunks[0].Type)
	}
	if chunks[1].Type != parser.AIChunk {
		t.Errorf("chunks[1].Type = %d, want AIChunk", chunks[1].Type)
	}
	if chunks[2].Type != parser.UserChunk {
		t.Errorf("chunks[2].Type = %d, want UserChunk", chunks[2].Type)
	}
}

func TestBuildChunks_ConsecutiveAIMerged(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{
			Timestamp: t0,
			Text:      "First response",
			Model:     "claude-opus-4-6",
			Thinking:  1,
			ToolCalls: []parser.ToolCall{{ID: "t1", Name: "Bash"}},
			Usage:     parser.Usage{InputTokens: 100, OutputTokens: 50},
		},
		parser.AIMsg{
			Timestamp: t0.Add(3 * time.Second),
			Text:      "Continued response",
			IsMeta:    true,
			Thinking:  0,
			ToolCalls: []parser.ToolCall{{ID: "t2", Name: "Read"}},
			Usage:     parser.Usage{InputTokens: 200, OutputTokens: 75},
		},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1 (merged AI)", len(chunks))
	}

	c := chunks[0]
	if c.Type != parser.AIChunk {
		t.Errorf("Type = %d, want AIChunk", c.Type)
	}
	if c.Text != "First response\nContinued response" {
		t.Errorf("Text = %q, want merged text", c.Text)
	}
	if c.Thinking != 1 {
		t.Errorf("Thinking = %d, want 1", c.Thinking)
	}
	if len(c.ToolCalls) != 2 {
		t.Errorf("len(ToolCalls) = %d, want 2", len(c.ToolCalls))
	}
	// Tokens summed.
	if c.Usage.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", c.Usage.InputTokens)
	}
	if c.Usage.OutputTokens != 125 {
		t.Errorf("OutputTokens = %d, want 125", c.Usage.OutputTokens)
	}
}

func TestBuildChunks_AIDuration(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{Timestamp: t0, Text: "start", Model: "claude-opus-4-6"},
		parser.AIMsg{Timestamp: t0.Add(5 * time.Second), Text: "end"},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want 5000", chunks[0].DurationMs)
	}
}

func TestBuildChunks_AIModelFromFirstNonMeta(t *testing.T) {
	t0 := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	msgs := []parser.ClassifiedMsg{
		parser.AIMsg{Timestamp: t0, Text: "meta result", IsMeta: true},
		parser.AIMsg{Timestamp: t0.Add(1 * time.Second), Text: "real response", Model: "claude-opus-4-6"},
	}
	chunks := parser.BuildChunks(msgs)
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	if chunks[0].Model != "claude-opus-4-6" {
		t.Errorf("Model = %q, want %q", chunks[0].Model, "claude-opus-4-6")
	}
}

func TestBuildChunks_Empty(t *testing.T) {
	chunks := parser.BuildChunks(nil)
	if len(chunks) != 0 {
		t.Errorf("len(chunks) = %d, want 0", len(chunks))
	}
}
