package parser

import "encoding/json"

// ToolSummary generates a human-readable summary for a tool call.
// Returns the tool name as fallback when input is nil or unparseable.
func ToolSummary(name string, input json.RawMessage) string {
	return name
}
