package copilot

import (
	"encoding/json"
	"fmt"

	"github.com/kylesnowschwartz/agent-ouija/claude/tools"
	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
)

// rewriteToolSummaries replaces each tool item's ToolSummary with one built
// from Copilot tool-name conventions, mutating Items in place. ToolCategory
// is left as computed by BuildChunks (tools.CategorizeToolName already maps
// view/edit/shell/grep/glob/task/report_intent correctly).
func rewriteToolSummaries(chunks []transcript.Chunk) {
	for ci := range chunks {
		for ii := range chunks[ci].Items {
			it := &chunks[ci].Items[ii]
			if it.Type != transcript.ItemToolCall && it.Type != transcript.ItemSubagent {
				continue
			}
			it.ToolSummary = toolSummary(it.ToolName, it.ToolInput)
		}
	}
}

// toolSummary builds a one-line summary for a Copilot tool call.
// Unknown names (including {server}-{tool} MCP names) fall back to
// ouija's generic ToolSummary.
func toolSummary(name string, input json.RawMessage) string {
	fields := tools.ParseInputFields(input)
	if fields == nil {
		return tools.ToolSummary(name, input)
	}

	switch name {
	case "view":
		p := tools.GetString(fields, "path")
		if p == "" {
			return name
		}
		short := tools.ShortPath(p, 2)
		if start, end, ok := viewRange(fields); ok {
			return fmt.Sprintf("%s - lines %d-%d", short, start, end)
		}
		return short

	case "edit", "create":
		if p := tools.GetString(fields, "path"); p != "" {
			return tools.ShortPath(p, 2)
		}
		return name

	case "bash", "read_bash", "write_bash", "stop_bash":
		cmd := tools.GetString(fields, "command")
		desc := tools.GetString(fields, "description")
		switch {
		case cmd != "" && desc != "":
			return tools.Truncate(cmd, 60) + " - " + tools.Truncate(desc, 40)
		case cmd != "":
			return tools.Truncate(cmd, 80)
		case desc != "":
			return tools.Truncate(desc, 80)
		}
		if id := tools.GetString(fields, "shellId"); id != "" {
			return name + " " + id
		}
		return name

	case "grep", "rg":
		if p := tools.GetString(fields, "pattern"); p != "" {
			return `"` + tools.Truncate(p, 60) + `"`
		}
		return name

	case "glob":
		if p := tools.GetString(fields, "pattern"); p != "" {
			return p
		}
		return name

	case "task":
		agent := tools.GetString(fields, "name")
		if agent == "" {
			agent = tools.GetString(fields, "agent_type")
		}
		desc := tools.GetString(fields, "description")
		if desc == "" {
			desc = tools.Truncate(tools.GetString(fields, "prompt"), 80)
		}
		switch {
		case agent != "" && desc != "":
			return agent + ": " + desc
		case agent != "":
			return agent
		case desc != "":
			return desc
		}
		return name

	case "web_fetch":
		if u := tools.GetString(fields, "url"); u != "" {
			return tools.Truncate(u, 80)
		}
		return name

	case "web_search":
		if q := tools.GetString(fields, "query"); q != "" {
			return `"` + tools.Truncate(q, 60) + `"`
		}
		return name

	case "report_intent":
		return firstOf(fields, name, "intent")
	case "ask_user":
		return firstOf(fields, name, "question")
	case "task_complete", "exit_plan_mode":
		return firstOf(fields, name, "summary")
	case "skill":
		return firstOf(fields, name, "skill")
	}

	return tools.ToolSummary(name, input)
}

// firstOf returns the first non-empty string field among keys, truncated
// for one-line display; fallback otherwise.
func firstOf(fields map[string]json.RawMessage, fallback string, keys ...string) string {
	for _, k := range keys {
		if v := tools.GetString(fields, k); v != "" {
			return tools.Truncate(v, 80)
		}
	}
	return fallback
}

// viewRange extracts the [start, end] line range from a view tool's
// view_range argument. Returns false when absent or malformed.
func viewRange(fields map[string]json.RawMessage) (int, int, bool) {
	raw, ok := fields["view_range"]
	if !ok {
		return 0, 0, false
	}
	var r []int
	if err := json.Unmarshal(raw, &r); err != nil || len(r) != 2 {
		return 0, 0, false
	}
	return r[0], r[1], true
}
