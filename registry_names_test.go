package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kylesnowschwartz/agent-ouija/claude/discover"
	"github.com/kylesnowschwartz/agent-ouija/claude/registry"
)

func TestRegistryNames_SkipsEmptyNames(t *testing.T) {
	names := registryNames([]registry.Live{
		{SessionID: "s1", Name: ""},
		{SessionID: "s2", Name: "picked"},
	})
	if _, ok := names["s1"]; ok {
		t.Errorf("empty name should be skipped")
	}
	if names["s2"] != "picked" {
		t.Errorf("names[s2] = %q, want %q", names["s2"], "picked")
	}
}

func TestRegistryNames_LatestUpdatedWins(t *testing.T) {
	names := registryNames([]registry.Live{
		{SessionID: "s1", Name: "old-name", UpdatedAt: 100},
		{SessionID: "s1", Name: "new-name", UpdatedAt: 200},
		{SessionID: "s2", Name: "kept", UpdatedAt: 300},
		{SessionID: "s2", Name: "stale", UpdatedAt: 50},
	})
	if names["s1"] != "new-name" {
		t.Errorf("names[s1] = %q, want %q (latest UpdatedAt wins)", names["s1"], "new-name")
	}
	if names["s2"] != "kept" {
		t.Errorf("names[s2] = %q, want %q (earlier stale entry must not win)", names["s2"], "kept")
	}
}

func TestApplyRegistryNames(t *testing.T) {
	names := map[string]string{
		"stamped":  "renamed-session",
		"untitled": "proj-3e",
		"custom":   "auto-name-2f",
	}
	sessions := []discover.SessionInfo{
		// Directory-name stamp: registry name supersedes.
		{SessionID: "stamped", Title: "myproj", Cwd: "/Users/kyle/Code/myproj"},
		// No title: registry name supersedes.
		{SessionID: "untitled", Title: "", Cwd: "/Users/kyle/Code/proj"},
		// Genuine custom title: stays.
		{SessionID: "custom", Title: "fix-auth-bug", Cwd: "/Users/kyle/Code/proj"},
		// No registry entry: stays.
		{SessionID: "unknown", Title: "", Cwd: "/Users/kyle/Code/proj"},
	}
	applyRegistryNames(sessions, names)

	want := []string{"renamed-session", "proj-3e", "fix-auth-bug", ""}
	for i, w := range want {
		if sessions[i].Title != w {
			t.Errorf("sessions[%d] (%s) Title = %q, want %q",
				i, sessions[i].SessionID, sessions[i].Title, w)
		}
	}
}

func TestNewerEntry_TieBreaks(t *testing.T) {
	// Equal UpdatedAt (e.g. both zero under format drift) must resolve
	// deterministically: StartedAt, then PID -- never input order.
	a := registry.Live{PID: 100, UpdatedAt: 0, StartedAt: 50}
	b := registry.Live{PID: 200, UpdatedAt: 0, StartedAt: 90}
	if !newerEntry(b, a) || newerEntry(a, b) {
		t.Errorf("StartedAt should break UpdatedAt ties")
	}
	c := registry.Live{PID: 300, UpdatedAt: 0, StartedAt: 50}
	if !newerEntry(c, a) || newerEntry(a, c) {
		t.Errorf("PID should break full-timestamp ties")
	}
}

func TestIsStampedTitle(t *testing.T) {
	// Non-git paths: ResolveGitRoot falls back to the input, so only the
	// cwd basename matches.
	if !isStampedTitle("proj", "/tmp/nowhere/proj") {
		t.Errorf("title equal to cwd basename should count as stamped")
	}
	if isStampedTitle("fix-auth-bug", "/tmp/nowhere/proj") {
		t.Errorf("genuine title must not count as stamped")
	}
	if isStampedTitle("proj", "") {
		t.Errorf("empty cwd should never match")
	}
}

func TestIsStampedTitle_GitSubdir(t *testing.T) {
	// A session running in a repo subdirectory is stamped with the repo
	// name, not the subdir name.
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "internal", "widget")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if !isStampedTitle(filepath.Base(repo), sub) {
		t.Errorf("git-root basename should count as stamped for subdir cwd")
	}
}

func TestMergeTitleRefs_DedupsByPath(t *testing.T) {
	base := []discover.SessionTitleRef{{Path: "/a.jsonl", Title: "one"}}
	extra := []discover.SessionTitleRef{
		{Path: "/a.jsonl", Title: "one-again"},
		{Path: "/b.jsonl", Title: "two"},
	}
	got := mergeTitleRefs(base, extra)
	if len(got) != 2 || got[0].Path != "/a.jsonl" || got[1].Path != "/b.jsonl" {
		t.Errorf("mergeTitleRefs = %+v, want deduped [a, b]", got)
	}
}

func TestPreferExact(t *testing.T) {
	refs := []discover.SessionTitleRef{
		{Path: "/a.jsonl", Title: "dependabot-manager-okr-notes"},
		{Path: "/b.jsonl", Title: "Dependabot-Manager-OKR"},
	}
	got := preferExact(refs, "dependabot-manager-okr")
	if len(got) != 1 || got[0].Path != "/b.jsonl" {
		t.Errorf("preferExact = %+v, want only the case-insensitive exact match", got)
	}
	// No exact match: keep everything.
	if got := preferExact(refs, "dependabot"); len(got) != 2 {
		t.Errorf("preferExact without exact hit should pass refs through, got %+v", got)
	}
}

func TestApplyRegistryNames_NilMapNoOp(t *testing.T) {
	sessions := []discover.SessionInfo{
		{SessionID: "s1", Title: "keep-me", Cwd: "/x/keep-me"},
	}
	applyRegistryNames(sessions, nil)
	if sessions[0].Title != "keep-me" {
		t.Errorf("Title = %q, want unchanged %q", sessions[0].Title, "keep-me")
	}
}
