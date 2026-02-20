package parser

import "testing"

func TestFindLastOutput(t *testing.T) {
	tests := []struct {
		name     string
		items    []DisplayItem
		wantNil  bool
		wantType LastOutputType
		wantText string
		wantTool string
		wantErr  bool
	}{
		{
			name:    "empty items returns nil",
			items:   nil,
			wantNil: true,
		},
		{
			name: "only thinking items returns nil",
			items: []DisplayItem{
				{Type: ItemThinking, Text: "pondering..."},
				{Type: ItemThinking, Text: "still thinking"},
			},
			wantNil: true,
		},
		{
			name: "trailing text output returns text",
			items: []DisplayItem{
				{Type: ItemThinking, Text: "let me think"},
				{Type: ItemToolCall, ToolName: "Bash", ToolResult: "ok"},
				{Type: ItemOutput, Text: "Here is the answer."},
			},
			wantType: LastOutputText,
			wantText: "Here is the answer.",
		},
		{
			name: "trailing tool result with no output returns tool result",
			items: []DisplayItem{
				{Type: ItemThinking, Text: "thinking"},
				{Type: ItemToolCall, ToolName: "Read", ToolResult: "file contents"},
			},
			wantType: LastOutputToolResult,
			wantTool: "Read",
			wantText: "file contents",
		},
		{
			name: "text output after tool results returns text (text takes priority)",
			items: []DisplayItem{
				{Type: ItemToolCall, ToolName: "Bash", ToolResult: "compiled"},
				{Type: ItemToolCall, ToolName: "Read", ToolResult: "file data"},
				{Type: ItemOutput, Text: "Done! Everything compiled."},
			},
			wantType: LastOutputText,
			wantText: "Done! Everything compiled.",
		},
		{
			name: "tool call with error returns IsError true",
			items: []DisplayItem{
				{Type: ItemToolCall, ToolName: "Bash", ToolResult: "exit code 1", ToolError: true},
			},
			wantType: LastOutputToolResult,
			wantTool: "Bash",
			wantText: "exit code 1",
			wantErr:  true,
		},
		{
			name: "multiple outputs returns the last one",
			items: []DisplayItem{
				{Type: ItemOutput, Text: "first answer"},
				{Type: ItemOutput, Text: "revised answer"},
				{Type: ItemOutput, Text: "final answer"},
			},
			wantType: LastOutputText,
			wantText: "final answer",
		},
		{
			name: "tool call with empty ToolResult is skipped",
			items: []DisplayItem{
				{Type: ItemToolCall, ToolName: "Write", ToolResult: ""},
			},
			wantNil: true,
		},
		{
			name: "output with empty text is skipped",
			items: []DisplayItem{
				{Type: ItemOutput, Text: ""},
				{Type: ItemToolCall, ToolName: "Bash", ToolResult: "ok"},
			},
			wantType: LastOutputToolResult,
			wantTool: "Bash",
			wantText: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindLastOutput(tt.items)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil, got nil")
			}
			if got.Type != tt.wantType {
				t.Errorf("Type = %d, want %d", got.Type, tt.wantType)
			}

			switch tt.wantType {
			case LastOutputText:
				if got.Text != tt.wantText {
					t.Errorf("Text = %q, want %q", got.Text, tt.wantText)
				}
			case LastOutputToolResult:
				if got.ToolName != tt.wantTool {
					t.Errorf("ToolName = %q, want %q", got.ToolName, tt.wantTool)
				}
				if got.ToolResult != tt.wantText {
					t.Errorf("ToolResult = %q, want %q", got.ToolResult, tt.wantText)
				}
				if got.IsError != tt.wantErr {
					t.Errorf("IsError = %v, want %v", got.IsError, tt.wantErr)
				}
			}
		})
	}
}
