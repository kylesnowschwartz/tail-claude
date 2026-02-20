package parser

// LastOutputType discriminates last output categories.
type LastOutputType int

const (
	LastOutputText       LastOutputType = iota
	LastOutputToolResult
)

// LastOutput represents the final visible output from an AI turn.
// Used by the TUI to show "the answer" in collapsed message view.
type LastOutput struct {
	Type       LastOutputType
	Text       string // LastOutputText: the output text
	ToolName   string // LastOutputToolResult: which tool
	ToolResult string // LastOutputToolResult: result content
	IsError    bool   // LastOutputToolResult: was it an error
}

// FindLastOutput scans display items in reverse to find the last meaningful output.
// Priority order (matching claude-devtools lastOutputDetector.ts):
//  1. Last ItemOutput with non-empty Text
//  2. Last ItemToolCall with non-empty ToolResult
//  3. nil (no output found)
func FindLastOutput(items []DisplayItem) *LastOutput {
	// First pass: look for last output text
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.Type == ItemOutput && item.Text != "" {
			return &LastOutput{
				Type: LastOutputText,
				Text: item.Text,
			}
		}
	}

	// Second pass: look for last tool result
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.Type == ItemToolCall && item.ToolResult != "" {
			return &LastOutput{
				Type:       LastOutputToolResult,
				ToolName:   item.ToolName,
				ToolResult: item.ToolResult,
				IsError:    item.ToolError,
			}
		}
	}

	return nil
}
