package main

import (
	"path/filepath"
	"strings"
	"testing"

	zone "github.com/lrstanley/bubblezone/v2"
)

// copilotFixture is the multi-tool synthetic session from the copilot
// package's testdata (bash + grep + edit + MCP tool in one turn).
func copilotFixture(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("copilot", "testdata", "sessions",
		"22222222-bbbb-4222-8222-222222222222", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSessionDispatchesCopilot(t *testing.T) {
	result, err := loadSession(copilotFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.copilotReader == nil {
		t.Fatal("loadSession must route copilot paths through loadCopilotSession")
	}
	if len(result.messages) == 0 {
		t.Fatal("no messages loaded")
	}
	if result.messages[0].role != RoleUser {
		t.Errorf("first message role = %q, want user", result.messages[0].role)
	}
	if result.messages[0].content != "refactor the config loader" {
		t.Errorf("first message content = %q", result.messages[0].content)
	}
	if result.meta.Cwd != "/home/dev/sample-api" {
		t.Errorf("meta.Cwd = %q, want /home/dev/sample-api", result.meta.Cwd)
	}
	if result.meta.GitBranch != "main" {
		t.Errorf("meta.GitBranch = %q, want main", result.meta.GitBranch)
	}
	if len(result.teams) != 0 || result.hasTeamTasks {
		t.Error("copilot sessions must not report teams")
	}
	if result.ongoing {
		t.Error("shutdown-terminated fixture must not be ongoing")
	}
}

// TestCopilotDumpStable renders the fixture the way --dump --expand does and
// asserts the output is populated and deterministic across renders.
func TestCopilotDumpStable(t *testing.T) {
	result, err := loadSession(copilotFixture(t))
	if err != nil {
		t.Fatal(err)
	}

	initZone.Do(zone.NewGlobal)
	render := func() string {
		m := initialModel(result.messages, true)
		m.width = 160
		m.height = 1_000_000
		m.applySessionMeta(result.meta)
		for i := range m.messages {
			m.expanded[i] = true
		}
		m.layoutList()
		return m.viewList()
	}

	out := render()
	for _, want := range []string{
		"refactor the config loader",
		"go test ./...", // bash tool summary from the copilot summary rewrite
		"Refactored the config loader to accept options.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dump output missing %q", want)
		}
	}

	if out2 := render(); out != out2 {
		t.Error("dump output is not deterministic across renders")
	}
}
