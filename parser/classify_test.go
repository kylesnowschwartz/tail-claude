package parser_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

// helper to build an Entry quickly.
func makeEntry(typ, uuid, ts string, content json.RawMessage, opts ...func(*parser.Entry)) parser.Entry {
	e := parser.Entry{
		Type:      typ,
		UUID:      uuid,
		Timestamp: ts,
	}
	e.Message.Role = typ // default role = type
	e.Message.Content = content
	for _, fn := range opts {
		fn(&e)
	}
	return e
}

func withModel(m string) func(*parser.Entry) {
	return func(e *parser.Entry) { e.Message.Model = m }
}

func withSidechain() func(*parser.Entry) {
	return func(e *parser.Entry) { e.IsSidechain = true }
}

func withMeta() func(*parser.Entry) {
	return func(e *parser.Entry) { e.IsMeta = true }
}

func withStopReason(r string) func(*parser.Entry) {
	return func(e *parser.Entry) {
		e.Message.StopReason = &r
	}
}

func withRole(r string) func(*parser.Entry) {
	return func(e *parser.Entry) { e.Message.Role = r }
}

// --- Classify tests ---

func TestClassify_UserMessage(t *testing.T) {
	e := makeEntry("user", "u1", "2025-01-15T10:00:00.000Z",
		json.RawMessage(`"Can you help me with this?"`))

	msg, ok := parser.Classify(e)
	if !ok {
		t.Fatal("expected Classify to succeed for user message")
	}
	u, isUser := msg.(parser.UserMsg)
	if !isUser {
		t.Fatalf("expected UserMsg, got %T", msg)
	}
	if u.Text != "Can you help me with this?" {
		t.Errorf("Text = %q, want %q", u.Text, "Can you help me with this?")
	}
	if u.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestClassify_AssistantMessage(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"thinking","thinking":"Let me consider..."},
		{"type":"text","text":"Here is my answer."},
		{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}},
		{"type":"tool_use","id":"t2","name":"Read","input":{"path":"foo.go"}}
	]`)

	e := makeEntry("assistant", "a1", "2025-01-15T10:00:05.500Z", content,
		withModel("claude-opus-4-6"),
		withStopReason("end_turn"),
	)
	e.Message.Usage.InputTokens = 100
	e.Message.Usage.OutputTokens = 50
	e.Message.Usage.CacheReadInputTokens = 25
	e.Message.Usage.CacheCreationInputTokens = 10

	msg, ok := parser.Classify(e)
	if !ok {
		t.Fatal("expected Classify to succeed for assistant message")
	}
	ai, isAI := msg.(parser.AIMsg)
	if !isAI {
		t.Fatalf("expected AIMsg, got %T", msg)
	}
	if ai.Model != "claude-opus-4-6" {
		t.Errorf("Model = %q, want %q", ai.Model, "claude-opus-4-6")
	}
	if ai.Text != "Here is my answer." {
		t.Errorf("Text = %q, want %q", ai.Text, "Here is my answer.")
	}
	if ai.Thinking != 1 {
		t.Errorf("Thinking = %d, want 1", ai.Thinking)
	}
	if len(ai.ToolCalls) != 2 {
		t.Errorf("len(ToolCalls) = %d, want 2", len(ai.ToolCalls))
	}
	if ai.Usage.TotalTokens() != 185 {
		t.Errorf("TotalTokens = %d, want 185", ai.Usage.TotalTokens())
	}
	if ai.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", ai.StopReason, "end_turn")
	}
}

func TestClassify_SystemMessage(t *testing.T) {
	content := json.RawMessage(`"<local-command-stdout>Hello from command</local-command-stdout>"`)
	e := makeEntry("user", "s1", "2025-01-15T10:00:06.000Z", content)

	msg, ok := parser.Classify(e)
	if !ok {
		t.Fatal("expected Classify to succeed for system message")
	}
	sys, isSys := msg.(parser.SystemMsg)
	if !isSys {
		t.Fatalf("expected SystemMsg, got %T", msg)
	}
	if sys.Output != "Hello from command" {
		t.Errorf("Output = %q, want %q", sys.Output, "Hello from command")
	}
}

func TestClassify_SidechainFiltered(t *testing.T) {
	e := makeEntry("assistant", "sc1", "2025-01-15T10:00:00Z",
		json.RawMessage(`[{"type":"text","text":"sidechain"}]`),
		withSidechain(), withModel("claude-opus-4-6"),
	)
	_, ok := parser.Classify(e)
	if ok {
		t.Fatal("sidechain messages should be filtered out")
	}
}

func TestClassify_HardNoise(t *testing.T) {
	tests := []struct {
		name    string
		typ     string
		content json.RawMessage
		opts    []func(*parser.Entry)
	}{
		{
			name:    "system type",
			typ:     "system",
			content: json.RawMessage(`"system prompt"`),
		},
		{
			name:    "summary type",
			typ:     "summary",
			content: json.RawMessage(`"conversation summary"`),
		},
		{
			name:    "system-reminder wrapped",
			typ:     "user",
			content: json.RawMessage(`"<system-reminder>Remember this</system-reminder>"`),
		},
		{
			name:    "synthetic assistant",
			typ:     "assistant",
			content: json.RawMessage(`"synthetic content"`),
			opts:    []func(*parser.Entry){withModel("<synthetic>")},
		},
		{
			name:    "empty stdout",
			typ:     "user",
			content: json.RawMessage(`"<local-command-stdout></local-command-stdout>"`),
		},
		{
			name:    "empty stderr",
			typ:     "user",
			content: json.RawMessage(`"<local-command-stderr></local-command-stderr>"`),
		},
		{
			name:    "interruption string",
			typ:     "user",
			content: json.RawMessage(`"[Request interrupted by user at 2025-01-15T10:00:00Z]"`),
		},
		{
			name:    "interruption array",
			typ:     "user",
			content: json.RawMessage(`[{"type":"text","text":"[Request interrupted by user at 2025-01-15T10:00:00Z]"}]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := makeEntry(tt.typ, "noise", "2025-01-15T10:00:00Z", tt.content, tt.opts...)
			_, ok := parser.Classify(e)
			if ok {
				t.Errorf("expected noise entry %q to be filtered out", tt.name)
			}
		})
	}
}

func TestClassify_MetaUserMessage(t *testing.T) {
	e := makeEntry("user", "m1", "2025-01-15T10:00:03.500Z",
		json.RawMessage(`"Tool result: success"`),
		withMeta(),
	)
	msg, ok := parser.Classify(e)
	if !ok {
		t.Fatal("expected Classify to succeed for meta user message")
	}
	ai, isAI := msg.(parser.AIMsg)
	if !isAI {
		t.Fatalf("expected AIMsg for meta user message, got %T", msg)
	}
	if !ai.IsMeta {
		t.Error("IsMeta should be true")
	}
}

// --- parseTimestamp tests ---

func TestParseTimestamp_RFC3339Nano(t *testing.T) {
	ts := parser.ParseTimestamp("2025-01-15T10:00:05.500Z")
	if ts.IsZero() {
		t.Fatal("expected non-zero time")
	}
	if ts.Year() != 2025 || ts.Month() != time.January || ts.Day() != 15 {
		t.Errorf("date = %v, want 2025-01-15", ts)
	}
}

func TestParseTimestamp_RFC3339(t *testing.T) {
	ts := parser.ParseTimestamp("2025-01-15T10:00:05Z")
	if ts.IsZero() {
		t.Fatal("expected non-zero time")
	}
	if ts.Second() != 5 {
		t.Errorf("Second = %d, want 5", ts.Second())
	}
}

func TestParseTimestamp_NoTimezone(t *testing.T) {
	ts := parser.ParseTimestamp("2025-01-15T10:00:05.500")
	if ts.IsZero() {
		t.Fatal("expected non-zero time for format without timezone")
	}
}

func TestParseTimestamp_Invalid(t *testing.T) {
	ts := parser.ParseTimestamp("not-a-timestamp")
	if !ts.IsZero() {
		t.Errorf("expected zero time for invalid input, got %v", ts)
	}
}

// --- detectSlash tests ---

func TestDetectSlash_WithCommandTag(t *testing.T) {
	content := `<command-name>/model</command-name><command-args>opus</command-args>`
	isSlash, name := parser.DetectSlash(content)
	if !isSlash {
		t.Fatal("expected slash command to be detected")
	}
	if name != "model" {
		t.Errorf("name = %q, want %q", name, "model")
	}
}

func TestDetectSlash_WithoutCommandTag(t *testing.T) {
	isSlash, name := parser.DetectSlash("Just a regular message")
	if isSlash {
		t.Fatal("expected no slash command")
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
}
