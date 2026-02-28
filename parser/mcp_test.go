package parser_test

import (
	"path/filepath"
	"testing"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

func TestMCPToolResults(t *testing.T) {
	// Fixture has: user question → AI turn with 3 MCP tool calls and results.
	// MCP tool results have toolUseResult as a JSON array (not object),
	// which previously caused ParseEntry to fail silently.
	path := filepath.Join("testdata", "mcp-tools.jsonl")
	chunks, err := parser.ReadSession(path)
	if err != nil {
		t.Fatalf("ReadSession(%q) error: %v", path, err)
	}

	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2 (user + AI)", len(chunks))
	}

	ai := chunks[1]
	if ai.Type != parser.AIChunk {
		t.Fatalf("chunks[1].Type = %d, want AIChunk", ai.Type)
	}

	// Collect tool call items.
	type toolInfo struct {
		name   string
		result string
	}
	var tools []toolInfo
	for _, item := range ai.Items {
		if item.Type == parser.ItemToolCall {
			tools = append(tools, toolInfo{name: item.ToolName, result: item.ToolResult})
		}
	}

	wantTools := []string{
		"mcp__context7__resolve-library-id",
		"ListMcpResourcesTool",
		"mcp__context7__query-docs",
	}
	if len(tools) != len(wantTools) {
		t.Fatalf("tool call count = %d, want %d", len(tools), len(wantTools))
	}

	for i, want := range wantTools {
		if tools[i].name != want {
			t.Errorf("tool[%d].name = %q, want %q", i, tools[i].name, want)
		}
		if tools[i].result == "" {
			t.Errorf("tool %q has empty ToolResult", want)
		}
	}
}
