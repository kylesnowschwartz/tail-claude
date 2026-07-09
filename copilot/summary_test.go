package copilot

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolSummary(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"view", `{"path":"/home/dev/example-project/main.go"}`, "example-project/main.go"},
		{"view", `{"path":"/home/dev/example-project/main.go","view_range":[10,40]}`, "example-project/main.go - lines 10-40"},
		{"view", `{"path":"/home/dev/x/a.go","view_range":"bogus"}`, "x/a.go"},
		{"view", `{}`, "view"},
		{"edit", `{"path":"/home/dev/x/config/loader.go","old_str":"a","new_str":"b"}`, "config/loader.go"},
		{"create", `{"path":"/home/dev/x/new.go","file_text":"package x"}`, "x/new.go"},
		{"bash", `{"command":"go test ./...","description":"run tests"}`, "go test ./... - run tests"},
		{"bash", `{"command":"ls -la"}`, "ls -la"},
		{"read_bash", `{"shellId":"shell-1"}`, "read_bash shell-1"},
		{"stop_bash", `{}`, "stop_bash"},
		{"grep", `{"pattern":"LoadConfig","paths":["/x"]}`, `"LoadConfig"`},
		{"rg", `{"pattern":"TODO"}`, `"TODO"`},
		{"glob", `{"pattern":"**/*.go"}`, "**/*.go"},
		{"task", `{"agent_type":"analyzer","name":"analyzer","description":"analyze test coverage","prompt":"..."}`, "analyzer: analyze test coverage"},
		{"task", `{"agent_type":"explorer","prompt":"find all handlers"}`, "explorer: find all handlers"},
		{"web_fetch", `{"url":"https://example.com/docs"}`, "https://example.com/docs"},
		{"web_search", `{"query":"go generics"}`, `"go generics"`},
		{"report_intent", `{"intent":"Reading configuration files"}`, "Reading configuration files"},
		{"ask_user", `{"question":"Which branch should I use?","choices":["main"]}`, "Which branch should I use?"},
		{"task_complete", `{"summary":"All tests pass"}`, "All tests pass"},
		{"exit_plan_mode", `{"summary":"Plan drafted"}`, "Plan drafted"},
		{"skill", `{"skill":"code-review"}`, "code-review"},
	}
	for _, c := range cases {
		if got := toolSummary(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("toolSummary(%s, %s) = %q, want %q", c.name, c.input, got, c.want)
		}
	}
}

func TestToolSummaryTruncatesLongValues(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := toolSummary("bash", json.RawMessage(`{"command":"`+long+`"}`))
	if len(got) > 90 {
		t.Errorf("long command not truncated: %d chars", len(got))
	}
}

func TestToolSummaryFallbacks(t *testing.T) {
	// Unknown / MCP names defer to ouija's generic summary: must be non-empty.
	if got := toolSummary("github-mcp-server-get_file_contents", json.RawMessage(`{"owner":"example","repo":"sample"}`)); got == "" {
		t.Error("MCP summary is empty")
	}
	// Nil / unparseable input falls back to the tool name.
	if got := toolSummary("view", nil); got != "view" {
		t.Errorf("nil input = %q, want view", got)
	}
	if got := toolSummary("bash", json.RawMessage(`not json`)); got != "bash" {
		t.Errorf("garbage input = %q, want bash", got)
	}
}
