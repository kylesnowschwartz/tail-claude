package parser

// ToolCategory classifies tool calls into broad functional groups.
// Used by the TUI to assign per-category icons and colors.
type ToolCategory string

const (
	CategoryRead  ToolCategory = "Read"
	CategoryEdit  ToolCategory = "Edit"
	CategoryWrite ToolCategory = "Write"
	CategoryBash  ToolCategory = "Bash"
	CategoryGrep  ToolCategory = "Grep"
	CategoryGlob  ToolCategory = "Glob"
	CategoryTask  ToolCategory = "Task"
	CategoryTool  ToolCategory = "Tool" // Skill, MCP tools
	CategoryWeb   ToolCategory = "Web"  // WebFetch, WebSearch
	CategoryOther ToolCategory = "Other"
)

// CategorizeToolName maps a raw tool name to a ToolCategory.
// Ported from agentsview's NormalizeToolCategory with an added Web category
// for WebFetch/WebSearch (agentsview puts these in Other).
// Includes multi-agent aliases (Codex, Gemini, OpenCode, Copilot, Cursor)
// for forward compatibility.
func CategorizeToolName(name string) ToolCategory {
	switch name {
	// Claude Code tools
	case "Read":
		return CategoryRead
	case "Edit":
		return CategoryEdit
	case "Write", "NotebookEdit":
		return CategoryWrite
	case "Bash":
		return CategoryBash
	case "Grep":
		return CategoryGrep
	case "Glob":
		return CategoryGlob
	case "Task":
		return CategoryTask
	case "Skill":
		return CategoryTool
	case "WebFetch", "WebSearch":
		return CategoryWeb

	// Codex tools
	case "shell_command", "exec_command",
		"write_stdin", "shell":
		return CategoryBash
	case "apply_patch":
		return CategoryEdit

	// Gemini tools
	case "read_file":
		return CategoryRead
	case "write_file", "edit_file":
		return CategoryWrite
	case "run_command", "execute_command":
		return CategoryBash
	case "search_files", "grep":
		return CategoryGrep

	// OpenCode tools (lowercase variants)
	// Note: "grep" is handled above in the Gemini section.
	case "read":
		return CategoryRead
	case "edit":
		return CategoryEdit
	case "write":
		return CategoryWrite
	case "bash":
		return CategoryBash
	case "glob":
		return CategoryGlob
	case "task":
		return CategoryTask

	// Copilot tools
	// Note: "edit_file" (Write), "shell" (Bash), "grep" (Grep),
	// and "glob" (Glob) are handled in earlier sections.
	case "view":
		return CategoryRead
	case "report_intent":
		return CategoryTool

	// Cursor tools
	case "Shell":
		return CategoryBash
	case "StrReplace":
		return CategoryEdit
	case "LS":
		return CategoryRead

	default:
		return CategoryOther
	}
}
