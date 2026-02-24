package parser

import (
	"sort"
	"sync"
	"time"
)

// SessionCache avoids rescanning unchanged session files on every picker
// refresh. The cache key is (path, modTime) — when a file's modification
// time changes, we rescan it. Files that haven't been touched since the
// last check return cached metadata immediately.
type SessionCache struct {
	mu      sync.Mutex
	entries map[string]cachedSession
}

type cachedSession struct {
	modTime time.Time
	meta    sessionMetadata
}

// NewSessionCache returns an empty cache ready for use.
func NewSessionCache() *SessionCache {
	return &SessionCache{
		entries: make(map[string]cachedSession),
	}
}

// getOrScan returns cached metadata when the file hasn't changed (same modTime),
// otherwise rescans and updates the cache.
func (c *SessionCache) getOrScan(path string, modTime time.Time) sessionMetadata {
	c.mu.Lock()
	defer c.mu.Unlock()

	if cached, ok := c.entries[path]; ok && cached.modTime.Equal(modTime) {
		return cached.meta
	}

	meta := scanSessionMetadata(path)
	c.entries[path] = cachedSession{modTime: modTime, meta: meta}
	return meta
}

// DiscoverProjectSessions finds all session .jsonl files in a project directory,
// using cached metadata for unchanged files. Same logic as the standalone
// DiscoverProjectSessions but avoids redundant file scans across refreshes.
func (c *SessionCache) DiscoverProjectSessions(projectDir string) ([]SessionInfo, error) {
	return discoverSessions(projectDir, c.getOrScan)
}

// DiscoverAllProjectSessions finds sessions across multiple project directories,
// using cached metadata for unchanged files. Same merge-and-sort logic as the
// standalone DiscoverAllProjectSessions.
func (c *SessionCache) DiscoverAllProjectSessions(projectDirs []string) ([]SessionInfo, error) {
	var all []SessionInfo
	for _, dir := range projectDirs {
		sessions, err := c.DiscoverProjectSessions(dir)
		if err != nil {
			continue
		}
		all = append(all, sessions...)
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].ModTime.After(all[j].ModTime)
	})

	return all, nil
}
