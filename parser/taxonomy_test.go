package parser

import "testing"

func TestCategorizeToolName(t *testing.T) {
	tests := []struct {
		toolName string
		want     ToolCategory
	}{
		// Claude Code tools
		{"Read", CategoryRead},
		{"Edit", CategoryEdit},
		{"Write", CategoryWrite},
		{"NotebookEdit", CategoryWrite},
		{"Bash", CategoryBash},
		{"Grep", CategoryGrep},
		{"Glob", CategoryGlob},
		{"Task", CategoryTask},
		{"Skill", CategoryTool},
		{"WebFetch", CategoryWeb},
		{"WebSearch", CategoryWeb},

		// Codex tools
		{"shell_command", CategoryBash},
		{"exec_command", CategoryBash},
		{"apply_patch", CategoryEdit},
		{"write_stdin", CategoryBash},
		{"shell", CategoryBash},

		// Gemini tools
		{"read_file", CategoryRead},
		{"write_file", CategoryWrite},
		{"edit_file", CategoryWrite},
		{"run_command", CategoryBash},
		{"execute_command", CategoryBash},
		{"search_files", CategoryGrep},
		{"grep", CategoryGrep},

		// OpenCode tools (lowercase)
		// "grep" already tested above in Gemini section.
		{"read", CategoryRead},
		{"edit", CategoryEdit},
		{"write", CategoryWrite},
		{"bash", CategoryBash},
		{"glob", CategoryGlob},
		{"task", CategoryTask},

		// Copilot tools
		{"view", CategoryRead},
		{"report_intent", CategoryTool},

		// Cursor tools
		{"Shell", CategoryBash},
		{"StrReplace", CategoryEdit},
		{"LS", CategoryRead},

		// Unknown -> Other
		{"view_image", CategoryOther},
		{"update_plan", CategoryOther},
		{"list_mcp_resources", CategoryOther},
		{"AskUserQuestion", CategoryOther},
		{"EnterPlanMode", CategoryOther},
		{"ExitPlanMode", CategoryOther},
		{"", CategoryOther},
		{"some_random_tool", CategoryOther},
	}

	for _, tt := range tests {
		testName := tt.toolName
		if testName == "" {
			testName = "empty_string"
		}
		t.Run(testName, func(t *testing.T) {
			got := CategorizeToolName(tt.toolName)
			if got != tt.want {
				t.Errorf(
					"CategorizeToolName(%q) = %q, want %q",
					tt.toolName, got, tt.want,
				)
			}
		})
	}
}
