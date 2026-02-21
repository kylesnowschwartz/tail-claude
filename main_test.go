package main

import (
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestShortModel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"claude-opus-4-6", "opus4.6"},
		{"claude-sonnet-4-5-20251001", "sonnet4.5"},
		{"unknown", "unknown"},
		{"claude-haiku-4-5", "haiku4.5"},
		{"claude-haiku-4-5-20251201", "haiku4.5"},
		{"", ""},
	}
	for _, tt := range tests {
		got := shortModel(tt.input)
		if got != tt.want {
			t.Errorf("shortModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{500, "500"},
		{999, "999"},
		{1234, "1.2k"},
		{123456, "123.5k"},
		{1234567, "1.2M"},
	}
	for _, tt := range tests {
		got := formatTokens(tt.input)
		if got != tt.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0.0s"},
		{3500, "3.5s"},
		{9999, "10.0s"},
		{15000, "15s"},
		{60000, "1m 0s"},
		{71000, "1m 11s"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.input)
		if got != tt.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatTime(t *testing.T) {
	zero := time.Time{}
	if got := formatTime(zero); got != "" {
		t.Errorf("formatTime(zero) = %q, want empty", got)
	}

	ts := time.Date(2025, 1, 15, 17, 4, 5, 0, time.UTC)
	got := formatTime(ts)
	if got == "" {
		t.Error("formatTime(non-zero) should not be empty")
	}
}

func TestChunksToMessages(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	chunks := []parser.Chunk{
		{
			Type:      parser.UserChunk,
			Timestamp: ts,
			UserText:  "Hello",
		},
		{
			Type:          parser.AIChunk,
			Timestamp:     ts.Add(1 * time.Second),
			Model:         "claude-opus-4-6",
			Text:          "Response here",
			ThinkingCount: 2,
			ToolCalls:     []parser.ToolCall{{ID: "t1", Name: "Bash"}, {ID: "t2", Name: "Read"}},
			Usage:         parser.Usage{InputTokens: 100, OutputTokens: 50},
			DurationMs:    3500,
		},
		{
			Type:      parser.SystemChunk,
			Timestamp: ts.Add(2 * time.Second),
			Output:    "Command output",
		},
	}

	msgs := chunksToMessages(chunks, nil)
	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %d, want 3", len(msgs))
	}

	// User message
	if msgs[0].role != RoleUser {
		t.Errorf("msgs[0].role = %q, want %q", msgs[0].role, RoleUser)
	}
	if msgs[0].content != "Hello" {
		t.Errorf("msgs[0].content = %q, want %q", msgs[0].content, "Hello")
	}

	// AI message
	if msgs[1].role != RoleClaude {
		t.Errorf("msgs[1].role = %q, want %q", msgs[1].role, RoleClaude)
	}
	if msgs[1].model != "opus4.6" {
		t.Errorf("msgs[1].model = %q, want %q", msgs[1].model, "opus4.6")
	}
	if msgs[1].thinkingCount != 2 {
		t.Errorf("msgs[1].thinkingCount = %d, want 2", msgs[1].thinkingCount)
	}
	if msgs[1].toolCallCount != 2 {
		t.Errorf("msgs[1].toolCallCount = %d, want 2", msgs[1].toolCallCount)
	}
	if msgs[1].tokensRaw != 150 {
		t.Errorf("msgs[1].tokensRaw = %d, want 150", msgs[1].tokensRaw)
	}
	if msgs[1].durationMs != 3500 {
		t.Errorf("msgs[1].durationMs = %d, want 3500", msgs[1].durationMs)
	}

	// System message
	if msgs[2].role != RoleSystem {
		t.Errorf("msgs[2].role = %q, want %q", msgs[2].role, RoleSystem)
	}
	if msgs[2].content != "Command output" {
		t.Errorf("msgs[2].content = %q, want %q", msgs[2].content, "Command output")
	}
}

func TestChunksToMessages_EmptyToolCalls(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	chunks := []parser.Chunk{
		{
			Type:      parser.AIChunk,
			Timestamp: ts,
			Model:     "claude-opus-4-6",
			Text:      "No tools used",
			// ToolCalls deliberately nil
		},
	}
	msgs := chunksToMessages(chunks, nil)
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].toolCallCount != 0 {
		t.Errorf("toolCalls = %d, want 0 for nil ToolCalls slice", msgs[0].toolCallCount)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "shorter than max",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exactly at max",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "longer than max truncated with ellipsis",
			input:  "hello world this is long",
			maxLen: 10,
			want:   "hello wor…",
		},
		{
			name:   "newlines collapsed to spaces",
			input:  "line one\nline two\nline three",
			maxLen: 50,
			want:   "line one line two line three",
		},
		{
			name:   "newlines collapsed then truncated",
			input:  "first\nsecond\nthird",
			maxLen: 12,
			want:   "first secon…",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
