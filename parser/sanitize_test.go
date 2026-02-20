package parser_test

import (
	"encoding/json"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestSanitizeContent_CommandOutput(t *testing.T) {
	input := "<local-command-stdout>file1.go\nfile2.go</local-command-stdout>"
	got := parser.SanitizeContent(input)
	want := "file1.go\nfile2.go"
	if got != want {
		t.Errorf("SanitizeContent(%q) = %q, want %q", input, got, want)
	}
}

func TestSanitizeContent_CommandMessage(t *testing.T) {
	input := "<command-name>/model</command-name><command-args>opus</command-args>"
	got := parser.SanitizeContent(input)
	want := "/model opus"
	if got != want {
		t.Errorf("SanitizeContent(%q) = %q, want %q", input, got, want)
	}
}

func TestSanitizeContent_CommandMessageNoArgs(t *testing.T) {
	input := "<command-name>/help</command-name>"
	got := parser.SanitizeContent(input)
	want := "/help"
	if got != want {
		t.Errorf("SanitizeContent(%q) = %q, want %q", input, got, want)
	}
}

func TestSanitizeContent_NoiseTagsStripped(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "system-reminder stripped",
			input: "Hello <system-reminder>some noise</system-reminder> world",
			want:  "Hello  world",
		},
		{
			name:  "local-command-caveat stripped",
			input: "Result <local-command-caveat>warning text</local-command-caveat> done",
			want:  "Result  done",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.SanitizeContent(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeContent_PlainTextPassthrough(t *testing.T) {
	input := "Just some plain text, nothing special."
	got := parser.SanitizeContent(input)
	if got != input {
		t.Errorf("SanitizeContent(%q) = %q, want passthrough", input, got)
	}
}

func TestExtractText_JSONString(t *testing.T) {
	raw := json.RawMessage(`"Hello, world!"`)
	got := parser.ExtractText(raw)
	if got != "Hello, world!" {
		t.Errorf("ExtractText(string) = %q, want %q", got, "Hello, world!")
	}
}

func TestExtractText_JSONArray(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"First block"},{"type":"text","text":"Second block"}]`)
	got := parser.ExtractText(raw)
	want := "First block\nSecond block"
	if got != want {
		t.Errorf("ExtractText(array) = %q, want %q", got, want)
	}
}

func TestExtractText_ArraySkipsNonText(t *testing.T) {
	raw := json.RawMessage(`[{"type":"thinking","text":"hmm"},{"type":"text","text":"visible"}]`)
	got := parser.ExtractText(raw)
	if got != "visible" {
		t.Errorf("ExtractText() = %q, want %q", got, "visible")
	}
}

func TestExtractText_EmptyContent(t *testing.T) {
	got := parser.ExtractText(nil)
	if got != "" {
		t.Errorf("ExtractText(nil) = %q, want empty", got)
	}

	got = parser.ExtractText(json.RawMessage{})
	if got != "" {
		t.Errorf("ExtractText(empty) = %q, want empty", got)
	}
}

func TestIsCommandOutput(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"<local-command-stdout>output</local-command-stdout>", true},
		{"<local-command-stderr>error output</local-command-stderr>", true},
		{"regular user message", false},
		{"", false},
	}
	for _, tt := range tests {
		got := parser.IsCommandOutput(tt.input)
		if got != tt.want {
			t.Errorf("IsCommandOutput(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
