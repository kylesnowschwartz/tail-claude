package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kylesnowschwartz/agent-ouija/claude/discover"
	"github.com/kylesnowschwartz/agent-ouija/claude/registry"
	"github.com/kylesnowschwartz/agent-ouija/gitroot"
)

// Title arbitration for the picker's first line, best name first:
//
//	custom title > AI title > registry session name > no title
//
// The first two are resolved library-side into SessionInfo.Title. The
// registry name (~/.claude/sessions/{pid}.json) fills two gaps the
// transcript can't: auto-named sessions carry no title records at all,
// and Claude Code re-stamps the transcript's custom-title with the
// launcher/agent name on every flush, so a /rename survives only in the
// registry. A stored title that merely repeats the project directory
// name is that stamp, not a user choice -- rank it below the registry
// name.

// discoverSessionsWithNames is the single entry point for building the
// picker's session list: project discovery plus the registry-name
// overlay. The initial load and watcher rescans both route through here
// so the overlay cannot regress on one path and not the other.
func discoverSessionsWithNames(projectDirs []string, cache *discover.SessionCache) ([]discover.SessionInfo, error) {
	var sessions []discover.SessionInfo
	var err error
	if cache != nil {
		sessions, err = cache.DiscoverAllProjectSessions(projectDirs)
	} else {
		sessions, err = discover.DiscoverAllProjectSessions(projectDirs)
	}
	applyRegistryNames(sessions, sessionRegistryNames())
	return sessions, err
}

// registryNames maps sessionID to display name from registry entries.
// Registry files linger after exit and a resumed session appears under a
// new pid; when several entries claim the same session, newerEntry picks
// the winner.
func registryNames(entries []registry.Live) map[string]string {
	winners := make(map[string]registry.Live, len(entries))
	for _, e := range entries {
		if e.Name == "" {
			continue
		}
		if cur, seen := winners[e.SessionID]; !seen || newerEntry(e, cur) {
			winners[e.SessionID] = e
		}
	}
	names := make(map[string]string, len(winners))
	for id, e := range winners {
		names[id] = e.Name
	}
	return names
}

// newerEntry orders same-session registry entries: freshest UpdatedAt
// wins, then StartedAt, then PID -- deterministic even when format drift
// decodes the timestamps to zero.
func newerEntry(a, b registry.Live) bool {
	if a.UpdatedAt != b.UpdatedAt {
		return a.UpdatedAt > b.UpdatedAt
	}
	if a.StartedAt != b.StartedAt {
		return a.StartedAt > b.StartedAt
	}
	return a.PID > b.PID
}

// applyRegistryNames overlays registry names onto discovered sessions in
// place. Genuine custom/AI titles stay untouched; only untitled sessions
// and directory-name stamps are replaced.
func applyRegistryNames(sessions []discover.SessionInfo, names map[string]string) {
	if len(names) == 0 {
		return
	}
	for i := range sessions {
		name := names[sessions[i].SessionID]
		if name == "" {
			continue
		}
		if sessions[i].Title == "" || isStampedTitle(sessions[i].Title, sessions[i].Cwd) {
			sessions[i].Title = name
		}
	}
}

// isStampedTitle reports whether a stored title merely repeats the
// session's directory name -- the launcher stamp, not a user choice. The
// stamp carries the project's name while the session may run in a
// subdirectory or worktree, so the git main-worktree root's name counts
// too (one .git lookup, only on the rare non-matching path).
func isStampedTitle(title, cwd string) bool {
	if cwd == "" {
		return false
	}
	return title == filepath.Base(cwd) || title == filepath.Base(gitroot.ResolveGitRoot(cwd))
}

// sessionRegistryNames reads the session registry at the IO edge. Returns
// nil when the Claude root is unavailable; the overlay then no-ops.
// Lingering entries from exited sessions are wanted here -- they are the
// only surviving record of a /rename once the transcript flush re-stamps
// the title -- so do NOT filter with Live.Alive.
func sessionRegistryNames() map[string]string {
	if claudeRootErr != nil {
		return nil
	}
	return registryNames(registry.Read(claudeRoot.SessionsDir()))
}

// registryNameMatches resolves a CLI name argument against registry
// session names, mirroring FindTitleMatches semantics (case-insensitive,
// exact matches beat substring matches). The picker displays registry
// names for stamped and auto-named sessions, so they must resolve as
// `tail-claude <name>` arguments too.
func registryNameMatches(name string) []discover.SessionTitleRef {
	name = strings.TrimSpace(name)
	if name == "" || claudeRootErr != nil {
		return nil
	}
	lower := strings.ToLower(name)
	var exact, partial []discover.SessionTitleRef
	for _, e := range registry.Read(claudeRoot.SessionsDir()) {
		if e.Name == "" || e.Cwd == "" {
			continue
		}
		t := strings.ToLower(e.Name)
		isExact := t == lower
		if !isExact && !strings.Contains(t, lower) {
			continue
		}
		path := claudeRoot.SessionTranscriptPath(e.Cwd, e.SessionID)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		ref := discover.SessionTitleRef{Path: path, SessionID: e.SessionID, Title: e.Name, ModTime: info.ModTime()}
		if isExact {
			exact = append(exact, ref)
		} else {
			partial = append(partial, ref)
		}
	}
	if len(exact) > 0 {
		return mergeTitleRefs(nil, exact)
	}
	return mergeTitleRefs(nil, partial)
}

// mergeTitleRefs appends extra onto base, dropping refs whose Path is
// already present (a session can match by both transcript title and
// registry name, or via duplicate registry entries).
func mergeTitleRefs(base, extra []discover.SessionTitleRef) []discover.SessionTitleRef {
	seen := make(map[string]struct{}, len(base))
	for _, r := range base {
		seen[r.Path] = struct{}{}
	}
	for _, r := range extra {
		if _, dup := seen[r.Path]; dup {
			continue
		}
		seen[r.Path] = struct{}{}
		base = append(base, r)
	}
	return base
}

// preferExact narrows merged matches to exact title matches when any
// exist, restoring FindTitleMatches' exact-beats-substring rule across
// the two match sources.
func preferExact(refs []discover.SessionTitleRef, name string) []discover.SessionTitleRef {
	var exact []discover.SessionTitleRef
	for _, r := range refs {
		if strings.EqualFold(r.Title, name) {
			exact = append(exact, r)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return refs
}
