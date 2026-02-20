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
	if ai.ThinkingCount != 1 {
		t.Errorf("Thinking = %d, want 1", ai.ThinkingCount)
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

// --- ContentBlock tests ---

func TestClassify_AssistantBlocks_ThinkingTextToolUse(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"thinking","thinking":"Let me think about this..."},
		{"type":"text","text":"Here is the answer."},
		{"type":"tool_use","id":"call_1","name":"Read","input":{"file_path":"/tmp/foo.go"}}
	]`)

	e := makeEntry("assistant", "a1", "2025-01-15T10:00:00Z", content,
		withModel("claude-opus-4-6"), withStopReason("end_turn"))

	msg, ok := parser.Classify(e)
	if !ok {
		t.Fatal("expected Classify to succeed")
	}
	ai := msg.(parser.AIMsg)

	if len(ai.Blocks) != 3 {
		t.Fatalf("len(Blocks) = %d, want 3", len(ai.Blocks))
	}

	// Block 0: thinking
	if ai.Blocks[0].Type != "thinking" {
		t.Errorf("Blocks[0].Type = %q, want thinking", ai.Blocks[0].Type)
	}
	if ai.Blocks[0].Text != "Let me think about this..." {
		t.Errorf("Blocks[0].Text = %q, want thinking text", ai.Blocks[0].Text)
	}

	// Block 1: text
	if ai.Blocks[1].Type != "text" {
		t.Errorf("Blocks[1].Type = %q, want text", ai.Blocks[1].Type)
	}
	if ai.Blocks[1].Text != "Here is the answer." {
		t.Errorf("Blocks[1].Text = %q, want text content", ai.Blocks[1].Text)
	}

	// Block 2: tool_use
	if ai.Blocks[2].Type != "tool_use" {
		t.Errorf("Blocks[2].Type = %q, want tool_use", ai.Blocks[2].Type)
	}
	if ai.Blocks[2].ToolID != "call_1" {
		t.Errorf("Blocks[2].ToolID = %q, want call_1", ai.Blocks[2].ToolID)
	}
	if ai.Blocks[2].ToolName != "Read" {
		t.Errorf("Blocks[2].ToolName = %q, want Read", ai.Blocks[2].ToolName)
	}
	if string(ai.Blocks[2].ToolInput) != `{"file_path":"/tmp/foo.go"}` {
		t.Errorf("Blocks[2].ToolInput = %s, want file_path JSON", string(ai.Blocks[2].ToolInput))
	}
}

func TestClassify_AssistantBlocks_ThinkingTextCaptured(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"thinking","thinking":"Deep thoughts here"},
		{"type":"thinking","thinking":"More deep thoughts"},
		{"type":"text","text":"Output"}
	]`)
	e := makeEntry("assistant", "a1", "2025-01-15T10:00:00Z", content, withModel("claude-opus-4-6"))

	msg, _ := parser.Classify(e)
	ai := msg.(parser.AIMsg)

	// Thinking count still correct for backward compat
	if ai.ThinkingCount != 2 {
		t.Errorf("Thinking count = %d, want 2", ai.ThinkingCount)
	}

	// But blocks capture the actual text
	if ai.Blocks[0].Text != "Deep thoughts here" {
		t.Errorf("Blocks[0].Text = %q, want first thinking text", ai.Blocks[0].Text)
	}
	if ai.Blocks[1].Text != "More deep thoughts" {
		t.Errorf("Blocks[1].Text = %q, want second thinking text", ai.Blocks[1].Text)
	}
}

func TestClassify_AssistantBlocks_ToolInputCaptured(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./...","description":"Run tests"}}
	]`)
	e := makeEntry("assistant", "a1", "2025-01-15T10:00:00Z", content, withModel("claude-opus-4-6"))

	msg, _ := parser.Classify(e)
	ai := msg.(parser.AIMsg)

	if len(ai.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(ai.Blocks))
	}

	// Verify the raw JSON is captured
	var parsed map[string]string
	if err := json.Unmarshal(ai.Blocks[0].ToolInput, &parsed); err != nil {
		t.Fatalf("failed to parse ToolInput: %v", err)
	}
	if parsed["command"] != "go test ./..." {
		t.Errorf("ToolInput.command = %q, want 'go test ./...'", parsed["command"])
	}
}

func TestClassify_AssistantBlocks_OrderMatchesRawArray(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"text","text":"first"},
		{"type":"thinking","thinking":"middle"},
		{"type":"tool_use","id":"t1","name":"Bash","input":{}},
		{"type":"text","text":"last"}
	]`)
	e := makeEntry("assistant", "a1", "2025-01-15T10:00:00Z", content, withModel("claude-opus-4-6"))

	msg, _ := parser.Classify(e)
	ai := msg.(parser.AIMsg)

	if len(ai.Blocks) != 4 {
		t.Fatalf("len(Blocks) = %d, want 4", len(ai.Blocks))
	}

	wantTypes := []string{"text", "thinking", "tool_use", "text"}
	for i, want := range wantTypes {
		if ai.Blocks[i].Type != want {
			t.Errorf("Blocks[%d].Type = %q, want %q", i, ai.Blocks[i].Type, want)
		}
	}
}

func TestClassify_AssistantBlocks_BackwardCompat(t *testing.T) {
	// Verify flat fields are still populated correctly alongside Blocks
	content := json.RawMessage(`[
		{"type":"thinking","thinking":"hmm"},
		{"type":"text","text":"answer"},
		{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"x.go"}}
	]`)

	e := makeEntry("assistant", "a1", "2025-01-15T10:00:00Z", content,
		withModel("claude-opus-4-6"), withStopReason("tool_use"))
	e.Message.Usage.InputTokens = 50
	e.Message.Usage.OutputTokens = 30

	msg, _ := parser.Classify(e)
	ai := msg.(parser.AIMsg)

	// Flat fields
	if ai.Text != "answer" {
		t.Errorf("Text = %q, want 'answer'", ai.Text)
	}
	if ai.ThinkingCount != 1 {
		t.Errorf("Thinking = %d, want 1", ai.ThinkingCount)
	}
	if len(ai.ToolCalls) != 1 || ai.ToolCalls[0].Name != "Read" {
		t.Errorf("ToolCalls = %v, want [{t1 Read}]", ai.ToolCalls)
	}
	if ai.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want 'tool_use'", ai.StopReason)
	}
	if ai.Usage.InputTokens != 50 || ai.Usage.OutputTokens != 30 {
		t.Errorf("Usage = %+v, want {50 30 0 0}", ai.Usage)
	}

	// Blocks also populated
	if len(ai.Blocks) != 3 {
		t.Errorf("len(Blocks) = %d, want 3", len(ai.Blocks))
	}
}

func TestClassify_MetaUser_ArrayWithToolResult(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"tool_result","tool_use_id":"call_1","content":"file contents here","is_error":false},
		{"type":"tool_result","tool_use_id":"call_2","content":"error: not found","is_error":true}
	]`)
	e := makeEntry("user", "m1", "2025-01-15T10:00:01Z", content, withMeta())

	msg, ok := parser.Classify(e)
	if !ok {
		t.Fatal("expected meta user to classify")
	}
	ai := msg.(parser.AIMsg)
	if !ai.IsMeta {
		t.Error("IsMeta should be true")
	}

	if len(ai.Blocks) != 2 {
		t.Fatalf("len(Blocks) = %d, want 2", len(ai.Blocks))
	}

	// Block 0
	if ai.Blocks[0].Type != "tool_result" {
		t.Errorf("Blocks[0].Type = %q, want tool_result", ai.Blocks[0].Type)
	}
	if ai.Blocks[0].ToolID != "call_1" {
		t.Errorf("Blocks[0].ToolID = %q, want call_1", ai.Blocks[0].ToolID)
	}
	if ai.Blocks[0].Content != "file contents here" {
		t.Errorf("Blocks[0].Content = %q, want 'file contents here'", ai.Blocks[0].Content)
	}
	if ai.Blocks[0].IsError {
		t.Error("Blocks[0].IsError should be false")
	}

	// Block 1
	if ai.Blocks[1].ToolID != "call_2" {
		t.Errorf("Blocks[1].ToolID = %q, want call_2", ai.Blocks[1].ToolID)
	}
	if !ai.Blocks[1].IsError {
		t.Error("Blocks[1].IsError should be true")
	}
}

func TestClassify_MetaUser_StringContent(t *testing.T) {
	e := makeEntry("user", "m1", "2025-01-15T10:00:01Z",
		json.RawMessage(`"plain text tool result"`), withMeta())

	msg, _ := parser.Classify(e)
	ai := msg.(parser.AIMsg)

	if len(ai.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(ai.Blocks))
	}
	if ai.Blocks[0].Type != "text" {
		t.Errorf("Blocks[0].Type = %q, want text", ai.Blocks[0].Type)
	}
	if ai.Blocks[0].Text != "plain text tool result" {
		t.Errorf("Blocks[0].Text = %q, want original text", ai.Blocks[0].Text)
	}
}

func TestClassify_MetaUser_ToolResultWithArrayContent(t *testing.T) {
	// tool_result blocks can have structured content (array of text blocks)
	content := json.RawMessage(`[
		{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"structured result"}],"is_error":false}
	]`)
	e := makeEntry("user", "m1", "2025-01-15T10:00:01Z", content, withMeta())

	msg, _ := parser.Classify(e)
	ai := msg.(parser.AIMsg)

	if len(ai.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(ai.Blocks))
	}
	if ai.Blocks[0].Type != "tool_result" {
		t.Errorf("Type = %q, want tool_result", ai.Blocks[0].Type)
	}
	// Content should be stringified
	if ai.Blocks[0].Content == "" {
		t.Error("Content should not be empty for array content")
	}
}

// --- Teammate message classification tests ---

func jsonStr(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func TestClassify_TeammateMessageProducesTeammateMsg(t *testing.T) {
	content := `<teammate-message teammate_id="researcher">Task #1 is done</teammate-message>`
	e := makeEntry("user", "u1", "2025-01-15T10:00:00Z", jsonStr(content))

	msg, ok := parser.Classify(e)
	if !ok {
		t.Fatal("teammate message should not be filtered")
	}

	tm, is := msg.(parser.TeammateMsg)
	if !is {
		t.Fatalf("got %T, want TeammateMsg", msg)
	}
	if tm.TeammateID != "researcher" {
		t.Errorf("TeammateID = %q, want researcher", tm.TeammateID)
	}
	if tm.Text != "Task #1 is done" {
		t.Errorf("Text = %q, want 'Task #1 is done'", tm.Text)
	}
}

func TestClassify_TeammateMessageExtractsContent(t *testing.T) {
	content := "<teammate-message teammate_id=\"lead\">You are working on task #1.\nPlease commit when done.</teammate-message>"
	e := makeEntry("user", "u1", "2025-01-15T10:00:00Z", jsonStr(content))

	msg, ok := parser.Classify(e)
	if !ok {
		t.Fatal("teammate message should not be filtered")
	}

	tm, is := msg.(parser.TeammateMsg)
	if !is {
		t.Fatalf("got %T, want TeammateMsg", msg)
	}
	if tm.TeammateID != "lead" {
		t.Errorf("TeammateID = %q, want lead", tm.TeammateID)
	}
	if tm.Text == "" {
		t.Error("Text should not be empty")
	}
}
