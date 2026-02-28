package main

import (
	"encoding/json"
	"testing"
	"time"

	"image/color"

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

func TestModelColor(t *testing.T) {
	tests := []struct {
		model string
		want  color.Color
	}{
		{"opus4.6", ColorModelOpus},
		{"claude-opus-4-6", ColorModelOpus},
		{"sonnet4.5", ColorModelSonnet},
		{"claude-sonnet-4-5", ColorModelSonnet},
		{"haiku4.5", ColorModelHaiku},
		{"claude-haiku-4-5", ColorModelHaiku},
		{"unknown-model", ColorTextSecondary},
		{"", ColorTextSecondary},
	}
	for _, tt := range tests {
		got := modelColor(tt.model)
		if got != tt.want {
			t.Errorf("modelColor(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestCountOutputItems(t *testing.T) {
	tests := []struct {
		name  string
		items []parser.DisplayItem
		want  int
	}{
		{
			name:  "empty slice",
			items: nil,
			want:  0,
		},
		{
			name: "only output items",
			items: []parser.DisplayItem{
				{Type: parser.ItemOutput},
				{Type: parser.ItemOutput},
			},
			want: 2,
		},
		{
			name: "mixed types — only ItemOutput counted",
			items: []parser.DisplayItem{
				{Type: parser.ItemThinking},
				{Type: parser.ItemOutput},
				{Type: parser.ItemToolCall},
				{Type: parser.ItemOutput},
				{Type: parser.ItemSubagent},
			},
			want: 2,
		},
		{
			name: "no output items",
			items: []parser.DisplayItem{
				{Type: parser.ItemThinking},
				{Type: parser.ItemToolCall},
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countOutputItems(tt.items)
			if got != tt.want {
				t.Errorf("countOutputItems() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIsTeamTaskItem(t *testing.T) {
	withInput := func(raw string) *parser.DisplayItem {
		return &parser.DisplayItem{ToolInput: json.RawMessage(raw)}
	}

	tests := []struct {
		name string
		item *parser.DisplayItem
		want bool
	}{
		{
			name: "valid team task — has team_name and name",
			item: withInput(`{"team_name":"my-team","name":"researcher","prompt":"do work"}`),
			want: true,
		},
		{
			name: "missing name field",
			item: withInput(`{"team_name":"my-team","prompt":"do work"}`),
			want: false,
		},
		{
			name: "missing team_name field",
			item: withInput(`{"name":"researcher","prompt":"do work"}`),
			want: false,
		},
		{
			name: "empty ToolInput",
			item: &parser.DisplayItem{},
			want: false,
		},
		{
			name: "malformed JSON",
			item: withInput(`{not valid json`),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.IsTeamTask(tt.item)
			if got != tt.want {
				t.Errorf("parser.IsTeamTask() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasTeamTaskItems(t *testing.T) {
	teamInput := json.RawMessage(`{"team_name":"my-team","name":"researcher"}`)
	regularInput := json.RawMessage(`{"command":"ls"}`)

	tests := []struct {
		name   string
		chunks []parser.Chunk
		want   bool
	}{
		{
			name:   "empty chunks",
			chunks: nil,
			want:   false,
		},
		{
			name: "no team task items",
			chunks: []parser.Chunk{
				{
					Type: parser.AIChunk,
					Items: []parser.DisplayItem{
						{Type: parser.ItemToolCall, ToolInput: regularInput},
					},
				},
			},
			want: false,
		},
		{
			name: "has team task item",
			chunks: []parser.Chunk{
				{
					Type: parser.AIChunk,
					Items: []parser.DisplayItem{
						{Type: parser.ItemSubagent, ToolInput: teamInput},
					},
				},
			},
			want: true,
		},
		{
			name: "team task item not a subagent type — not counted",
			chunks: []parser.Chunk{
				{
					Type: parser.AIChunk,
					Items: []parser.DisplayItem{
						// team_name+name present but type is ToolCall, not Subagent
						{Type: parser.ItemToolCall, ToolInput: teamInput},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasTeamTaskItems(tt.chunks)
			if got != tt.want {
				t.Errorf("hasTeamTaskItems() = %v, want %v", got, tt.want)
			}
		})
	}
}
