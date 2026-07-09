package copilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
)

func mustEvent(t *testing.T, line string) Event {
	t.Helper()
	ev, ok := ParseEvent([]byte(line))
	if !ok {
		t.Fatalf("ParseEvent failed for %s", line)
	}
	return ev
}

func classifyLine(t *testing.T, r *Reader, line string) (transcript.ClassifiedMsg, bool) {
	t.Helper()
	return r.ClassifyEvent(mustEvent(t, line))
}

func TestClassifyDropsAgentIDEvents(t *testing.T) {
	r := NewReader()
	lines := []string{
		`{"type":"assistant.message","data":{"content":"nested"},"id":"1","timestamp":"2026-05-01T10:00:00Z","agentId":"call-1"}`,
		`{"type":"tool.execution_start","data":{"toolCallId":"call-2","toolName":"view","arguments":{}},"id":"2","timestamp":"2026-05-01T10:00:01Z","agentId":"call-1"}`,
		`{"type":"tool.execution_complete","data":{"toolCallId":"call-2","success":true,"result":{"content":"x"}},"id":"3","timestamp":"2026-05-01T10:00:02Z","agentId":"call-1"}`,
		`{"type":"subagent.started","data":{"agentName":"a","toolCallId":"call-1"},"id":"4","timestamp":"2026-05-01T10:00:03Z","agentId":"call-1"}`,
	}
	for _, l := range lines {
		if _, ok := classifyLine(t, r, l); ok {
			t.Errorf("agentId event classified, want drop: %s", l)
		}
	}
	if r.LastEventType != "subagent.started" {
		t.Errorf("LastEventType = %q, want subagent.started", r.LastEventType)
	}
}

func TestClassifySessionStartUpdatesReader(t *testing.T) {
	r := NewReader()
	_, ok := classifyLine(t, r, `{"type":"session.start","data":{"sessionId":"sess-1","selectedModel":"claude-sonnet-4.6","context":{"cwd":"/home/dev/example-project","gitRoot":"/home/dev/example-project","branch":"main"}},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`)
	if ok {
		t.Fatal("session.start classified, want drop")
	}
	if r.SessionID != "sess-1" || r.Model != "claude-sonnet-4.6" {
		t.Errorf("Reader = %+v", r)
	}
	if r.Meta.Cwd != "/home/dev/example-project" || r.Meta.GitBranch != "main" {
		t.Errorf("Meta = %+v", r.Meta)
	}
	if r.Meta.PermissionMode != "" {
		t.Errorf("PermissionMode = %q, want empty", r.Meta.PermissionMode)
	}

	// context_changed overlays; empty fields keep prior values.
	classifyLine(t, r, `{"type":"session.context_changed","data":{"cwd":"/home/dev/other","branch":""},"id":"2","timestamp":"2026-05-01T10:01:00Z"}`)
	if r.Meta.Cwd != "/home/dev/other" || r.Meta.GitBranch != "main" {
		t.Errorf("Meta after context_changed = %+v", r.Meta)
	}

	// model_change updates the model and drops.
	if _, ok := classifyLine(t, r, `{"type":"session.model_change","data":{"newModel":"claude-opus-4.6"},"id":"3","timestamp":"2026-05-01T10:02:00Z"}`); ok {
		t.Error("model_change classified, want drop")
	}
	if r.Model != "claude-opus-4.6" {
		t.Errorf("Model = %q, want claude-opus-4.6", r.Model)
	}
}

func TestClassifyUserMessage(t *testing.T) {
	r := NewReader()

	msg, ok := classifyLine(t, r, `{"type":"user.message","data":{"content":"add a test","transformedContent":"add a test EXPANDED"},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`)
	if !ok {
		t.Fatal("user.message dropped")
	}
	u, isUser := msg.(transcript.UserMsg)
	if !isUser {
		t.Fatalf("got %T, want UserMsg", msg)
	}
	if u.Text != "add a test" {
		t.Errorf("Text = %q, want raw content (never transformedContent)", u.Text)
	}
	if u.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}

	// Synthetic (source-tagged) messages drop.
	if _, ok := classifyLine(t, r, `{"type":"user.message","data":{"content":"injected","source":"skill-example"},"id":"2","timestamp":"2026-05-01T10:00:01Z"}`); ok {
		t.Error("source-tagged user.message classified, want drop")
	}

	// delivery:"idle" messages keep.
	if _, ok := classifyLine(t, r, `{"type":"user.message","data":{"content":"queued while idle","delivery":"idle"},"id":"3","timestamp":"2026-05-01T10:00:02Z"}`); !ok {
		t.Error("delivery=idle user.message dropped, want keep")
	}

	// Empty content drops.
	if _, ok := classifyLine(t, r, `{"type":"user.message","data":{"content":"  "},"id":"4","timestamp":"2026-05-01T10:00:03Z"}`); ok {
		t.Error("empty user.message classified, want drop")
	}
}

func TestClassifyAssistantMessage(t *testing.T) {
	r := NewReader()
	classifyLine(t, r, `{"type":"session.start","data":{"selectedModel":"claude-sonnet-4.6"},"id":"0","timestamp":"2026-05-01T10:00:00Z"}`)

	msg, ok := classifyLine(t, r, `{"type":"assistant.message","data":{"content":"the answer","reasoningText":"thinking it through","model":"claude-opus-4.6","outputTokens":42,"toolRequests":[{"toolCallId":"tc1","name":"bash","arguments":{"command":"ls"}}]},"id":"1","timestamp":"2026-05-01T10:00:05Z"}`)
	if !ok {
		t.Fatal("assistant.message dropped")
	}
	ai := msg.(transcript.AIMsg)
	if ai.IsMeta {
		t.Error("IsMeta = true, want false")
	}
	if ai.Model != "claude-opus-4.6" {
		t.Errorf("Model = %q", ai.Model)
	}
	if ai.Text != "the answer" {
		t.Errorf("Text = %q", ai.Text)
	}
	if ai.ThinkingCount != 1 {
		t.Errorf("ThinkingCount = %d, want 1", ai.ThinkingCount)
	}
	if ai.Usage.OutputTokens != 42 {
		t.Errorf("OutputTokens = %d, want 42", ai.Usage.OutputTokens)
	}
	// InputTokens/Cache* stay 0 so ContextTokens hides context stats.
	if ai.Usage.ContextTokens() != 0 {
		t.Errorf("ContextTokens = %d, want 0", ai.Usage.ContextTokens())
	}
	// Ordered blocks: thinking then text; toolRequests never emit blocks.
	if len(ai.Blocks) != 2 || ai.Blocks[0].Type != "thinking" || ai.Blocks[1].Type != "text" {
		t.Fatalf("Blocks = %+v", ai.Blocks)
	}
	if ai.Blocks[0].Text != "thinking it through" || ai.Blocks[1].Text != "the answer" {
		t.Errorf("block text = %q / %q", ai.Blocks[0].Text, ai.Blocks[1].Text)
	}
	// assistant.message model updates Reader state.
	if r.Model != "claude-opus-4.6" {
		t.Errorf("r.Model = %q, want claude-opus-4.6", r.Model)
	}

	// reasoningOpaque without reasoningText: counted, no block emitted.
	msg, ok = classifyLine(t, r, `{"type":"assistant.message","data":{"content":"","reasoningOpaque":"ZW5jcnlwdGVk","outputTokens":5},"id":"2","timestamp":"2026-05-01T10:00:06Z"}`)
	if !ok {
		t.Fatal("opaque assistant.message dropped")
	}
	ai = msg.(transcript.AIMsg)
	if ai.ThinkingCount != 1 || len(ai.Blocks) != 0 {
		t.Errorf("opaque: ThinkingCount = %d, Blocks = %+v", ai.ThinkingCount, ai.Blocks)
	}
	// Model falls back to Reader state when the event omits it.
	if ai.Model != "claude-opus-4.6" {
		t.Errorf("fallback Model = %q", ai.Model)
	}

	// parentToolCallId messages drop.
	if _, ok := classifyLine(t, r, `{"type":"assistant.message","data":{"content":"inside a tool","parentToolCallId":"tc9"},"id":"3","timestamp":"2026-05-01T10:00:07Z"}`); ok {
		t.Error("parentToolCallId assistant.message classified, want drop")
	}
}

func TestClassifyAssistantReasoning(t *testing.T) {
	r := NewReader()
	r.Model = "gpt-5.5"
	msg, ok := classifyLine(t, r, `{"type":"assistant.reasoning","data":{"content":"standalone reasoning","reasoningId":"r1"},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`)
	if !ok {
		t.Fatal("assistant.reasoning dropped")
	}
	ai := msg.(transcript.AIMsg)
	if ai.ThinkingCount != 1 || ai.Model != "gpt-5.5" {
		t.Errorf("msg = %+v", ai)
	}
	if len(ai.Blocks) != 1 || ai.Blocks[0].Type != "thinking" || ai.Blocks[0].Text != "standalone reasoning" {
		t.Errorf("Blocks = %+v", ai.Blocks)
	}
}

func TestClassifyToolEvents(t *testing.T) {
	r := NewReader()
	r.Model = "claude-sonnet-4.6"

	msg, ok := classifyLine(t, r, `{"type":"tool.execution_start","data":{"toolCallId":"tc1","toolName":"view","arguments":{"path":"/home/dev/x/main.go"}},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`)
	if !ok {
		t.Fatal("tool.execution_start dropped")
	}
	ai := msg.(transcript.AIMsg)
	if ai.IsMeta {
		t.Error("start IsMeta = true, want false")
	}
	if len(ai.Blocks) != 1 || ai.Blocks[0].Type != "tool_use" || ai.Blocks[0].ToolID != "tc1" || ai.Blocks[0].ToolName != "view" {
		t.Fatalf("Blocks = %+v", ai.Blocks)
	}
	if string(ai.Blocks[0].ToolInput) != `{"path":"/home/dev/x/main.go"}` {
		t.Errorf("ToolInput = %s", ai.Blocks[0].ToolInput)
	}

	// Completion with result.content.
	msg, ok = classifyLine(t, r, `{"type":"tool.execution_complete","data":{"toolCallId":"tc1","success":true,"result":{"content":"file body","detailedContent":"full body"}},"id":"2","timestamp":"2026-05-01T10:00:02Z"}`)
	if !ok {
		t.Fatal("tool.execution_complete dropped")
	}
	ai = msg.(transcript.AIMsg)
	if !ai.IsMeta {
		t.Error("complete IsMeta = false, want true")
	}
	b := ai.Blocks[0]
	if b.Type != "tool_result" || b.ToolID != "tc1" || b.Content != "file body" || b.IsError {
		t.Errorf("block = %+v", b)
	}

	// detailedContent fallback.
	msg, _ = classifyLine(t, r, `{"type":"tool.execution_complete","data":{"toolCallId":"tc2","success":true,"result":{"detailedContent":"only detailed"}},"id":"3","timestamp":"2026-05-01T10:00:03Z"}`)
	if got := msg.(transcript.AIMsg).Blocks[0].Content; got != "only detailed" {
		t.Errorf("detailedContent fallback = %q", got)
	}

	// Error case: result absent, error present.
	msg, _ = classifyLine(t, r, `{"type":"tool.execution_complete","data":{"toolCallId":"tc3","success":false,"error":{"code":"denied","message":"User denied permission"}},"id":"4","timestamp":"2026-05-01T10:00:04Z"}`)
	b = msg.(transcript.AIMsg).Blocks[0]
	if !b.IsError || b.Content != "[denied] User denied permission" {
		t.Errorf("error block = %+v", b)
	}

	// Nested tool events drop.
	if _, ok := classifyLine(t, r, `{"type":"tool.execution_start","data":{"toolCallId":"tc4","toolName":"bash","parentToolCallId":"tc1","arguments":{}},"id":"5","timestamp":"2026-05-01T10:00:05Z"}`); ok {
		t.Error("nested tool start classified, want drop")
	}
	if _, ok := classifyLine(t, r, `{"type":"tool.execution_complete","data":{"toolCallId":"tc4","success":true,"parentToolCallId":"tc1","result":{"content":"x"}},"id":"6","timestamp":"2026-05-01T10:00:06Z"}`); ok {
		t.Error("nested tool complete classified, want drop")
	}
}

func TestClassifyCompaction(t *testing.T) {
	r := NewReader()

	msg, ok := classifyLine(t, r, `{"type":"session.compaction_complete","data":{"success":true,"summaryContent":"summary text"},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`)
	if !ok {
		t.Fatal("compaction_complete dropped")
	}
	if c, isCompact := msg.(transcript.CompactMsg); !isCompact || c.Text != "summary text" {
		t.Errorf("msg = %#v", msg)
	}

	// Empty summary falls back.
	msg, _ = classifyLine(t, r, `{"type":"session.compaction_complete","data":{"success":true},"id":"2","timestamp":"2026-05-01T10:00:01Z"}`)
	if c := msg.(transcript.CompactMsg); c.Text != "context compacted" {
		t.Errorf("fallback Text = %q", c.Text)
	}

	// Failure becomes an error SystemMsg.
	msg, ok = classifyLine(t, r, `{"type":"session.compaction_complete","data":{"success":false,"error":"model timeout"},"id":"3","timestamp":"2026-05-01T10:00:02Z"}`)
	if !ok {
		t.Fatal("failed compaction dropped")
	}
	s, isSys := msg.(transcript.SystemMsg)
	if !isSys || !s.IsError || !strings.Contains(s.Output, "model timeout") {
		t.Errorf("msg = %#v", msg)
	}
}

func TestClassifySystemEvents(t *testing.T) {
	r := NewReader()

	msg, ok := classifyLine(t, r, `{"type":"session.error","data":{"errorType":"quota","message":"quota exceeded"},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`)
	if !ok {
		t.Fatal("session.error dropped")
	}
	if s := msg.(transcript.SystemMsg); !s.IsError || s.Output != "[quota] quota exceeded" {
		t.Errorf("msg = %#v", s)
	}

	for _, reason := range []string{"user initiated", "user_initiated"} {
		msg, ok = classifyLine(t, r, `{"type":"abort","data":{"reason":"`+reason+`"},"id":"2","timestamp":"2026-05-01T10:00:01Z"}`)
		if !ok {
			t.Fatalf("abort (%s) dropped", reason)
		}
		if s := msg.(transcript.SystemMsg); s.IsError || s.Output != "aborted by user" {
			t.Errorf("abort msg = %#v", s)
		}
	}

	msg, ok = classifyLine(t, r, `{"type":"session.info","data":{"infoType":"model","message":"Model set to x"},"id":"3","timestamp":"2026-05-01T10:00:02Z"}`)
	if !ok {
		t.Fatal("session.info dropped")
	}
	if s := msg.(transcript.SystemMsg); s.IsError || s.Output != "Model set to x" {
		t.Errorf("info msg = %#v", s)
	}
}

func TestClassifyDropsLifecycleAndUnknown(t *testing.T) {
	r := NewReader()
	drops := []string{
		`{"type":"assistant.turn_start","data":{"turnId":"t1"},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`,
		`{"type":"assistant.turn_end","data":{"turnId":"t1"},"id":"2","timestamp":"2026-05-01T10:00:01Z"}`,
		`{"type":"session.shutdown","data":{"shutdownType":"routine"},"id":"3","timestamp":"2026-05-01T10:00:02Z"}`,
		`{"type":"session.mode_changed","data":{"previousMode":"interactive","newMode":"plan"},"id":"4","timestamp":"2026-05-01T10:00:03Z"}`,
		`{"type":"session.truncation","data":{"performedBy":"BasicTruncator"},"id":"5","timestamp":"2026-05-01T10:00:04Z"}`,
		`{"type":"session.compaction_start","data":{},"id":"6","timestamp":"2026-05-01T10:00:05Z"}`,
		`{"type":"session.warning","data":{"warningType":"mcp","message":"w"},"id":"7","timestamp":"2026-05-01T10:00:06Z"}`,
		`{"type":"session.task_complete","data":{"success":true,"summary":"s"},"id":"8","timestamp":"2026-05-01T10:00:07Z"}`,
		`{"type":"permission.requested","data":{"requestId":"r1"},"id":"9","timestamp":"2026-05-01T10:00:08Z"}`,
		`{"type":"permission.completed","data":{"requestId":"r1"},"id":"10","timestamp":"2026-05-01T10:00:09Z"}`,
		`{"type":"hook.start","data":{"hookInvocationId":"h1"},"id":"11","timestamp":"2026-05-01T10:00:10Z"}`,
		`{"type":"hook.end","data":{"hookInvocationId":"h1"},"id":"12","timestamp":"2026-05-01T10:00:11Z"}`,
		`{"type":"system.message","data":{"role":"system","content":"You are..."},"id":"13","timestamp":"2026-05-01T10:00:12Z"}`,
		`{"type":"system.notification","data":{"content":"done"},"id":"14","timestamp":"2026-05-01T10:00:13Z"}`,
		`{"type":"skill.invoked","data":{"name":"x"},"id":"15","timestamp":"2026-05-01T10:00:14Z"}`,
		`{"type":"subagent.selected","data":{"agentName":"a"},"id":"16","timestamp":"2026-05-01T10:00:15Z"}`,
		`{"type":"some.future_event","data":{},"id":"17","timestamp":"2026-05-01T10:00:16Z"}`,
	}
	for _, l := range drops {
		if msg, ok := classifyLine(t, r, l); ok {
			t.Errorf("classified %s as %#v, want drop", l, msg)
		}
	}
	if r.LastEventType != "some.future_event" {
		t.Errorf("LastEventType = %q", r.LastEventType)
	}
}

// --- End-to-end: fixture -> ReadSession -> BuildChunks ---

func fixturePath(parts ...string) string {
	return filepath.Join(append([]string{"testdata", "sessions"}, parts...)...)
}

func TestEndToEndNormalSession(t *testing.T) {
	sess, err := ReadSession(fixturePath("11111111-aaaa-4111-8111-111111111111", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if sess.Offset == 0 {
		t.Error("Offset = 0")
	}
	if sess.Reader.LastEventType != "session.shutdown" {
		t.Errorf("LastEventType = %q", sess.Reader.LastEventType)
	}
	if sess.Reader.Meta.Cwd != "/home/dev/example-project" || sess.Reader.Meta.GitBranch != "main" {
		t.Errorf("Meta = %+v", sess.Reader.Meta)
	}

	chunks := BuildChunks(sess.Msgs)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 (user, ai)", len(chunks))
	}
	if chunks[0].Type != transcript.UserChunk || chunks[0].UserText != "add a greeting function" {
		t.Errorf("chunk 0 = %+v", chunks[0])
	}

	ai := chunks[1]
	if ai.Type != transcript.AIChunk {
		t.Fatalf("chunk 1 type = %v", ai.Type)
	}
	if ai.Model != "claude-sonnet-4.6" {
		t.Errorf("Model = %q", ai.Model)
	}
	if ai.ThinkingCount != 1 {
		t.Errorf("ThinkingCount = %d", ai.ThinkingCount)
	}
	if ai.Usage.OutputTokens != 30 { // last assistant message
		t.Errorf("OutputTokens = %d, want 30", ai.Usage.OutputTokens)
	}
	if ai.Usage.ContextTokens() != 0 {
		t.Errorf("ContextTokens = %d, want 0 (never synthesized)", ai.Usage.ContextTokens())
	}
	if ai.DurationMs <= 0 {
		t.Errorf("DurationMs = %d, want > 0", ai.DurationMs)
	}

	// Items: thinking, output, tool call (view, paired result), output.
	if len(ai.Items) != 4 {
		t.Fatalf("got %d items: %+v", len(ai.Items), ai.Items)
	}
	if ai.Items[0].Type != transcript.ItemThinking {
		t.Errorf("item 0 = %+v", ai.Items[0])
	}
	if ai.Items[1].Type != transcript.ItemOutput || ai.Items[1].Text != "I'll look at the main file first." {
		t.Errorf("item 1 = %+v", ai.Items[1])
	}
	tool := ai.Items[2]
	if tool.Type != transcript.ItemToolCall || tool.ToolName != "view" {
		t.Fatalf("item 2 = %+v", tool)
	}
	if tool.ToolSummary != "example-project/main.go" {
		t.Errorf("ToolSummary = %q", tool.ToolSummary)
	}
	if tool.ToolResult != "package main\n\nfunc main() {}\n" || tool.ToolError {
		t.Errorf("tool result = %q err=%v", tool.ToolResult, tool.ToolError)
	}
	if tool.DurationMs != 2000 {
		t.Errorf("tool DurationMs = %d, want 2000", tool.DurationMs)
	}
	if ai.Items[3].Type != transcript.ItemOutput || ai.Items[3].Text != "Added a greet function to main.go." {
		t.Errorf("item 3 = %+v", ai.Items[3])
	}
}

func TestEndToEndMultiToolSession(t *testing.T) {
	sess, err := ReadSession(fixturePath("22222222-bbbb-4222-8222-222222222222", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	chunks := BuildChunks(sess.Msgs)
	// user, AI turn, session.info system chunk
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks: %+v", len(chunks), chunks)
	}
	ai := chunks[1]
	if ai.ThinkingCount != 1 { // reasoningOpaque counted
		t.Errorf("ThinkingCount = %d, want 1", ai.ThinkingCount)
	}
	var toolItems []transcript.DisplayItem
	for _, it := range ai.Items {
		if it.Type == transcript.ItemToolCall {
			toolItems = append(toolItems, it)
		}
	}
	if len(toolItems) != 4 {
		t.Fatalf("got %d tool items", len(toolItems))
	}
	if toolItems[0].ToolSummary != "go test ./... - run tests" {
		t.Errorf("bash summary = %q", toolItems[0].ToolSummary)
	}
	if toolItems[1].ToolSummary != `"LoadConfig"` {
		t.Errorf("grep summary = %q", toolItems[1].ToolSummary)
	}
	// detailedContent-only result.
	if !strings.Contains(toolItems[1].ToolResult, "func LoadConfig") {
		t.Errorf("grep result = %q", toolItems[1].ToolResult)
	}
	if toolItems[2].ToolSummary != "config/loader.go" {
		t.Errorf("edit summary = %q", toolItems[2].ToolSummary)
	}
	// MCP name falls back to the generic ouija summary (never empty).
	if toolItems[3].ToolSummary == "" {
		t.Error("mcp summary is empty")
	}
	if chunks[2].Type != transcript.SystemChunk || chunks[2].Output != "Model set to claude-sonnet-4.6" {
		t.Errorf("chunk 2 = %+v", chunks[2])
	}
}

func TestEndToEndFailedAndDeniedTools(t *testing.T) {
	sess, err := ReadSession(fixturePath("33333333-cccc-4333-8333-333333333333", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	chunks := BuildChunks(sess.Msgs)
	// user, AI, session.error system chunk
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks", len(chunks))
	}
	ai := chunks[1]
	var tools []transcript.DisplayItem
	for _, it := range ai.Items {
		if it.Type == transcript.ItemToolCall {
			tools = append(tools, it)
		}
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tool items", len(tools))
	}
	if !tools[0].ToolError || tools[0].ToolResult != "[failure] exit status 1" {
		t.Errorf("failed tool = %+v", tools[0])
	}
	if !tools[1].ToolError || tools[1].ToolResult != "[denied] User denied permission" {
		t.Errorf("denied tool = %+v", tools[1])
	}
	if chunks[2].Type != transcript.SystemChunk || !chunks[2].IsError {
		t.Errorf("chunk 2 = %+v", chunks[2])
	}
}

func TestEndToEndResumedSession(t *testing.T) {
	sess, err := ReadSession(fixturePath("44444444-dddd-4444-8444-444444444444", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if sess.Reader.Model != "claude-opus-4.6" {
		t.Errorf("Model = %q, want claude-opus-4.6 after model_change", sess.Reader.Model)
	}
	if sess.Reader.Meta.GitBranch != "feature/next" {
		t.Errorf("GitBranch = %q, want feature/next after resume", sess.Reader.Meta.GitBranch)
	}
	chunks := BuildChunks(sess.Msgs)
	// user, AI, user, compact, AI
	if len(chunks) != 5 {
		t.Fatalf("got %d chunks: %+v", len(chunks), chunks)
	}
	if chunks[3].Type != transcript.CompactChunk || !strings.Contains(chunks[3].Output, "Earlier discussion summarized") {
		t.Errorf("chunk 3 = %+v", chunks[3])
	}
	// The post-compaction assistant.message has no model field; it inherits
	// the model_change value via Reader state.
	if chunks[4].Model != "claude-opus-4.6" {
		t.Errorf("chunk 4 Model = %q", chunks[4].Model)
	}
}

func TestEndToEndSubagentFiltering(t *testing.T) {
	sess, err := ReadSession(fixturePath("55555555-eeee-4555-8555-555555555555", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	chunks := BuildChunks(sess.Msgs)
	// user, AI (task tool), user "stop", system "aborted by user"
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks: %+v", len(chunks), chunks)
	}
	ai := chunks[1]
	var taskItem *transcript.DisplayItem
	for i := range ai.Items {
		if ai.Items[i].Type == transcript.ItemToolCall && ai.Items[i].ToolName == "task" {
			taskItem = &ai.Items[i]
		}
	}
	if taskItem == nil {
		t.Fatalf("no task tool item in %+v", ai.Items)
	}
	if taskItem.ToolResult != "coverage: 82% of statements" {
		t.Errorf("task result = %q", taskItem.ToolResult)
	}
	if taskItem.ToolSummary != "analyzer: analyze test coverage" {
		t.Errorf("task summary = %q", taskItem.ToolSummary)
	}
	// The nested (agentId-tagged) transcript never surfaces.
	for _, c := range chunks {
		if strings.Contains(c.Text, "Nested analysis output") {
			t.Error("subagent-internal assistant text surfaced")
		}
		for _, it := range c.Items {
			if it.ToolID == "call-0502" {
				t.Error("subagent-internal tool call surfaced")
			}
		}
	}
	if chunks[3].Type != transcript.SystemChunk || chunks[3].Output != "aborted by user" {
		t.Errorf("chunk 3 = %+v", chunks[3])
	}
}

func TestEndToEndEmptySession(t *testing.T) {
	sess, err := ReadSession(fixturePath("66666666-ffff-4666-8666-666666666666", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.Msgs) != 0 {
		t.Errorf("got %d msgs, want 0: %+v", len(sess.Msgs), sess.Msgs)
	}
}

func TestTransformedContentNeverSurfaces(t *testing.T) {
	sess, err := ReadSession(fixturePath("11111111-aaaa-4111-8111-111111111111", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(BuildChunks(sess.Msgs))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "synthetic-expansion-must-not-render") {
		t.Error("transformedContent leaked into chunks")
	}
}

// Copilot auto-compacts mid-turn, including between a top-level
// tool.execution_start and its tool.execution_complete (observed in real
// sessions). The CompactMsg must be deferred until the pair closes so the
// tool_use keeps its result and the result never renders as bare output.
func TestMidTurnCompactionDefersUntilToolCompletes(t *testing.T) {
	lines := []string{
		`{"type":"session.start","data":{"sessionId":"sess-1","selectedModel":"claude-opus-4.7"},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`,
		`{"type":"user.message","data":{"content":"run the tests"},"id":"2","timestamp":"2026-05-01T10:00:01Z"}`,
		`{"type":"tool.execution_start","data":{"toolCallId":"call-1","toolName":"bash","arguments":{"command":"go test ./..."}},"id":"3","timestamp":"2026-05-01T10:00:02Z"}`,
		`{"type":"session.compaction_complete","data":{"success":true,"summaryContent":"mid-turn summary"},"id":"4","timestamp":"2026-05-01T10:00:03Z"}`,
		`{"type":"tool.execution_complete","data":{"toolCallId":"call-1","success":true,"result":{"content":"ok"}},"id":"5","timestamp":"2026-05-01T10:00:04Z"}`,
		`{"type":"assistant.message","data":{"content":"tests pass"},"id":"6","timestamp":"2026-05-01T10:00:05Z"}`,
	}

	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, err := ReadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	chunks := BuildChunks(sess.Msgs)
	// user, AI (tool pair intact), compact, AI ("tests pass")
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks: %+v", len(chunks), chunks)
	}
	ai := chunks[1]
	if ai.Type != transcript.AIChunk || ai.Model != "claude-opus-4.7" {
		t.Fatalf("chunk 1 = %+v", ai)
	}
	var tool *transcript.DisplayItem
	for i := range ai.Items {
		if ai.Items[i].Type == transcript.ItemToolCall {
			tool = &ai.Items[i]
		}
	}
	if tool == nil {
		t.Fatalf("no tool item in chunk 1: %+v", ai.Items)
	}
	if tool.ToolResult != "ok" || tool.ToolError {
		t.Errorf("tool result = %q err=%v, want paired result 'ok'", tool.ToolResult, tool.ToolError)
	}
	if chunks[2].Type != transcript.CompactChunk || !strings.Contains(chunks[2].Output, "mid-turn summary") {
		t.Errorf("chunk 2 = %+v, want compact after the closed tool pair", chunks[2])
	}
	if chunks[3].Type != transcript.AIChunk || chunks[3].Text != "tests pass" {
		t.Errorf("chunk 3 = %+v", chunks[3])
	}

	// Incremental invariant: splitting the file mid-deferral (after the
	// compaction event, before the completion) must yield the same messages.
	half := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(half, []byte(strings.Join(lines[:4], "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewReader()
	first, off, err := r.ReadIncremental(half, 0)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(half, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(strings.Join(lines[4:], "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	rest, _, err := r.ReadIncremental(half, off)
	if err != nil {
		t.Fatal(err)
	}
	split := append(append([]transcript.ClassifiedMsg{}, first...), rest...)
	if !reflect.DeepEqual(split, sess.Msgs) {
		t.Errorf("split read != full read\nsplit: %+v\nwhole: %+v", split, sess.Msgs)
	}
}

// If a tool call never completes (crash/abandoned turn), a deferred
// compaction must not be held forever: the next user turn releases it.
func TestMidTurnCompactionReleasedByNextUserTurn(t *testing.T) {
	lines := []string{
		`{"type":"session.start","data":{"sessionId":"sess-1","selectedModel":"claude-opus-4.7"},"id":"1","timestamp":"2026-05-01T10:00:00Z"}`,
		`{"type":"tool.execution_start","data":{"toolCallId":"call-1","toolName":"bash","arguments":{}},"id":"2","timestamp":"2026-05-01T10:00:01Z"}`,
		`{"type":"session.compaction_complete","data":{"success":true,"summaryContent":"orphaned summary"},"id":"3","timestamp":"2026-05-01T10:00:02Z"}`,
		`{"type":"user.message","data":{"content":"next prompt"},"id":"4","timestamp":"2026-05-01T10:00:03Z"}`,
	}
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, err := ReadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range sess.Msgs {
		if c, ok := m.(transcript.CompactMsg); ok && strings.Contains(c.Text, "orphaned summary") {
			found = true
		}
	}
	if !found {
		t.Errorf("deferred compaction lost after new user turn: %+v", sess.Msgs)
	}
}
