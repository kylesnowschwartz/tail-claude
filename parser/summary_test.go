package parser_test

import (
	"encoding/json"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestToolSummary_Read(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "file_path only",
			input: `{"file_path":"/Users/kyle/Code/project/main.go"}`,
			want:  "main.go",
		},
		{
			name:  "with limit and offset",
			input: `{"file_path":"/Users/kyle/Code/project/main.go","limit":50,"offset":10}`,
			want:  "main.go - lines 10-59",
		},
		{
			name:  "with limit no offset",
			input: `{"file_path":"/Users/kyle/Code/project/main.go","limit":100}`,
			want:  "main.go - lines 1-100",
		},
		{
			name:  "no file_path",
			input: `{}`,
			want:  "Read",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("Read", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(Read, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_Write(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "with content",
			input: `{"file_path":"/tmp/foo.txt","content":"line1\nline2\nline3"}`,
			want:  "foo.txt - 3 lines",
		},
		{
			name:  "file_path only",
			input: `{"file_path":"/tmp/foo.txt"}`,
			want:  "foo.txt",
		},
		{
			name:  "no file_path",
			input: `{}`,
			want:  "Write",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("Write", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(Write, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_Edit(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "with old and new strings same line count",
			input: `{"file_path":"/tmp/foo.go","old_string":"a\nb","new_string":"c\nd"}`,
			want:  "foo.go - 2 lines",
		},
		{
			name:  "with old and new strings different line count",
			input: `{"file_path":"/tmp/foo.go","old_string":"a","new_string":"b\nc\nd"}`,
			want:  "foo.go - 1 -> 3 lines",
		},
		{
			name:  "file_path only",
			input: `{"file_path":"/tmp/foo.go"}`,
			want:  "foo.go",
		},
		{
			name:  "no file_path",
			input: `{}`,
			want:  "Edit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("Edit", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(Edit, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_Bash(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "description preferred over command",
			input: `{"command":"go test ./...","description":"Run all tests"}`,
			want:  "Run all tests",
		},
		{
			name:  "command fallback",
			input: `{"command":"go test ./..."}`,
			want:  "go test ./...",
		},
		{
			name:  "long command truncated",
			input: `{"command":"this is a very long command that should be truncated at fifty characters exactly here"}`,
			want:  "this is a very long command that should be truncat...",
		},
		{
			name:  "empty",
			input: `{}`,
			want:  "Bash",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("Bash", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(Bash, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_Grep(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "pattern only",
			input: `{"pattern":"TODO"}`,
			want:  `"TODO"`,
		},
		{
			name:  "pattern with glob",
			input: `{"pattern":"TODO","glob":"*.go"}`,
			want:  `"TODO" in *.go`,
		},
		{
			name:  "pattern with path",
			input: `{"pattern":"TODO","path":"/Users/kyle/Code/project/src"}`,
			want:  `"TODO" in src`,
		},
		{
			name:  "glob preferred over path",
			input: `{"pattern":"TODO","glob":"*.go","path":"/some/dir"}`,
			want:  `"TODO" in *.go`,
		},
		{
			name:  "no pattern",
			input: `{}`,
			want:  "Grep",
		},
		{
			name:  "long pattern truncated",
			input: `{"pattern":"this is a very long pattern that exceeds thirty chars"}`,
			want:  `"this is a very long pattern th..."`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("Grep", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(Grep, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_Glob(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "pattern only",
			input: `{"pattern":"**/*.go"}`,
			want:  `"**/*.go"`,
		},
		{
			name:  "pattern with path",
			input: `{"pattern":"**/*.go","path":"/Users/kyle/Code/project"}`,
			want:  `"**/*.go" in project`,
		},
		{
			name:  "no pattern",
			input: `{}`,
			want:  "Glob",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("Glob", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(Glob, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_Task(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "description with subagent type",
			input: `{"description":"Search for patterns","subagentType":"research"}`,
			want:  "research - Search for patterns",
		},
		{
			name:  "prompt fallback",
			input: `{"prompt":"Find all TODO comments"}`,
			want:  "Find all TODO comments",
		},
		{
			name:  "subagent type only",
			input: `{"subagentType":"research"}`,
			want:  "research",
		},
		{
			name:  "empty",
			input: `{}`,
			want:  "Task",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("Task", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(Task, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_WebFetch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "with url",
			input: `{"url":"https://docs.example.com/api/reference"}`,
			want:  "docs.example.com/api/reference",
		},
		{
			name:  "invalid url fallback",
			input: `{"url":"not-a-url"}`,
			want:  "not-a-url",
		},
		{
			name:  "no url",
			input: `{}`,
			want:  "WebFetch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("WebFetch", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(WebFetch, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_WebSearch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "with query",
			input: `{"query":"golang testing patterns"}`,
			want:  `"golang testing patterns"`,
		},
		{
			name:  "no query",
			input: `{}`,
			want:  "WebSearch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("WebSearch", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(WebSearch, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_SendMessage(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "shutdown request",
			input: `{"type":"shutdown_request","recipient":"worker-1"}`,
			want:  "Shutdown worker-1",
		},
		{
			name:  "shutdown response",
			input: `{"type":"shutdown_response"}`,
			want:  "Shutdown response",
		},
		{
			name:  "broadcast",
			input: `{"type":"broadcast","summary":"All tasks complete"}`,
			want:  "Broadcast: All tasks complete",
		},
		{
			name:  "to recipient",
			input: `{"recipient":"worker-2","summary":"Please review PR"}`,
			want:  "To worker-2: Please review PR",
		},
		{
			name:  "empty",
			input: `{}`,
			want:  "Send message",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("SendMessage", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(SendMessage, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_TaskCreate(t *testing.T) {
	got := parser.ToolSummary("TaskCreate", json.RawMessage(`{"subject":"Fix the build"}`))
	if got != "Fix the build" {
		t.Errorf("got %q, want %q", got, "Fix the build")
	}

	got = parser.ToolSummary("TaskCreate", json.RawMessage(`{}`))
	if got != "Create task" {
		t.Errorf("got %q, want %q", got, "Create task")
	}
}

func TestToolSummary_TaskUpdate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "full",
			input: `{"taskId":"42","status":"done","owner":"kyle"}`,
			want:  "#42 done -> kyle",
		},
		{
			name:  "id and status",
			input: `{"taskId":"42","status":"in_progress"}`,
			want:  "#42 in_progress",
		},
		{
			name:  "empty",
			input: `{}`,
			want:  "Update task",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("TaskUpdate", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(TaskUpdate, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_NilInput(t *testing.T) {
	got := parser.ToolSummary("Read", nil)
	if got != "Read" {
		t.Errorf("ToolSummary with nil input = %q, want %q", got, "Read")
	}
}

func TestToolSummary_EmptyInput(t *testing.T) {
	got := parser.ToolSummary("Read", json.RawMessage(``))
	if got != "Read" {
		t.Errorf("ToolSummary with empty input = %q, want %q", got, "Read")
	}
}

func TestToolSummary_MalformedJSON(t *testing.T) {
	got := parser.ToolSummary("Bash", json.RawMessage(`{not json}`))
	if got != "Bash" {
		t.Errorf("ToolSummary with malformed JSON = %q, want %q", got, "Bash")
	}
}

func TestToolSummary_DefaultWithCommonFields(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input string
		want  string
	}{
		{
			name:  "name field",
			tool:  "CustomTool",
			input: `{"name":"my-resource"}`,
			want:  "my-resource",
		},
		{
			name:  "path field",
			tool:  "CustomTool",
			input: `{"path":"/some/path"}`,
			want:  "/some/path",
		},
		{
			name:  "query field",
			tool:  "CustomTool",
			input: `{"query":"search term"}`,
			want:  "search term",
		},
		{
			name:  "command field",
			tool:  "CustomTool",
			input: `{"command":"ls -la"}`,
			want:  "ls -la",
		},
		{
			name:  "no common fields",
			tool:  "CustomTool",
			input: `{"foo":"bar"}`,
			want:  "bar",
		},
		{
			name:  "empty object",
			tool:  "CustomTool",
			input: `{}`,
			want:  "CustomTool",
		},
		{
			name:  "non-string first value",
			tool:  "CustomTool",
			input: `{"count":42}`,
			want:  "CustomTool",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary(tt.tool, json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(%s, %s) = %q, want %q", tt.tool, tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_Truncation(t *testing.T) {
	// Bash truncates at 50 chars
	longCmd := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaXXX" // 53 chars
	got := parser.ToolSummary("Bash", json.RawMessage(`{"command":"`+longCmd+`"}`))
	if len(got) > 53 { // 50 + "..."
		t.Errorf("truncation failed: len=%d, got %q", len(got), got)
	}
	if got != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa..." {
		t.Errorf("got %q", got)
	}
}

func TestToolSummary_NewlinesCollapsed(t *testing.T) {
	// Summaries must be single-line for item row rendering.
	got := parser.ToolSummary("Bash", json.RawMessage(`{"command":"echo hello\necho world"}`))
	if got != "echo hello echo world" {
		t.Errorf("newlines not collapsed: got %q", got)
	}

	// Description with newlines.
	got = parser.ToolSummary("Bash", json.RawMessage(`{"description":"Build and\nrun tests"}`))
	if got != "Build and run tests" {
		t.Errorf("newlines not collapsed: got %q", got)
	}
}

func TestToolSummary_NotebookEdit(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "with edit mode",
			input: `{"notebook_path":"/tmp/analysis.ipynb","edit_mode":"replace"}`,
			want:  "replace - analysis.ipynb",
		},
		{
			name:  "path only",
			input: `{"notebook_path":"/tmp/analysis.ipynb"}`,
			want:  "analysis.ipynb",
		},
		{
			name:  "empty",
			input: `{}`,
			want:  "NotebookEdit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("NotebookEdit", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(NotebookEdit, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_TodoWrite(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "multiple items",
			input: `{"todos":["a","b","c"]}`,
			want:  "3 items",
		},
		{
			name:  "single item",
			input: `{"todos":["a"]}`,
			want:  "1 item",
		},
		{
			name:  "empty",
			input: `{}`,
			want:  "TodoWrite",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("TodoWrite", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(TodoWrite, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestToolSummary_LSP(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "operation with file",
			input: `{"operation":"hover","filePath":"/tmp/main.go"}`,
			want:  "hover - main.go",
		},
		{
			name:  "operation only",
			input: `{"operation":"references"}`,
			want:  "references",
		},
		{
			name:  "empty",
			input: `{}`,
			want:  "LSP",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.ToolSummary("LSP", json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("ToolSummary(LSP, %s) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
