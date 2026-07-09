package copilot

import (
	"os"
	"testing"
	"time"
)

func TestParseWorkspaceYAMLQuirks(t *testing.T) {
	data, err := os.ReadFile("testdata/workspace_quirks.yaml")
	if err != nil {
		t.Fatal(err)
	}
	w := ParseWorkspaceYAML(data)

	if w.ID != "88888888-aaaa-4888-8888-888888888888" {
		t.Errorf("ID = %q (double quotes not stripped?)", w.ID)
	}
	if w.Cwd != "/home/dev/odd path/proj" {
		t.Errorf("Cwd = %q (single quotes not stripped?)", w.Cwd)
	}
	if w.GitRoot != "/home/dev/odd path/proj" {
		t.Errorf("GitRoot = %q", w.GitRoot)
	}
	if w.Branch != "feature/x:y" {
		t.Errorf("Branch = %q (colon-in-value mangled?)", w.Branch)
	}
	if w.Name != "release: cut v2.0" {
		t.Errorf("Name = %q (split must be on FIRST colon-space)", w.Name)
	}
	if !w.UserNamed {
		t.Error("UserNamed = false, want true")
	}
	if w.ClientName != "github/cli" {
		t.Errorf("ClientName = %q", w.ClientName)
	}
	if w.Summary != "plain value with trailing spaces" {
		t.Errorf("Summary = %q", w.Summary)
	}
	if want := time.Date(2026, 5, 2, 8, 30, 0, 0, time.UTC); !w.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", w.CreatedAt, want)
	}
	if want := time.Date(2026, 5, 2, 9, 45, 10, 123_000_000, time.UTC); !w.UpdatedAt.Equal(want) {
		t.Errorf("UpdatedAt = %v, want %v", w.UpdatedAt, want)
	}
}

func TestParseWorkspaceYAMLEdgeCases(t *testing.T) {
	w := ParseWorkspaceYAML([]byte("user_named: false\nunknown_key: whatever\nnot a kv line\n\n# comment\n"))
	if w.UserNamed {
		t.Error("UserNamed = true, want false")
	}
	if w != (Workspace{}) {
		t.Errorf("w = %+v, want zero value", w)
	}
}

func TestReadWorkspace(t *testing.T) {
	w, ok := ReadWorkspace(fixturePath("11111111-aaaa-4111-8111-111111111111"))
	if !ok {
		t.Fatal("ReadWorkspace returned false for existing workspace.yaml")
	}
	if w.Name != "greeting feature" || !w.UserNamed || w.Cwd != "/home/dev/example-project" {
		t.Errorf("w = %+v", w)
	}

	if _, ok := ReadWorkspace(fixturePath("33333333-cccc-4333-8333-333333333333")); ok {
		t.Error("ReadWorkspace returned true for a dir without workspace.yaml")
	}
	if _, ok := ReadWorkspace(t.TempDir()); ok {
		t.Error("ReadWorkspace returned true for empty dir")
	}
}
