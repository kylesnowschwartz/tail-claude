package copilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kylesnowschwartz/agent-ouija/claude/discover"
	"github.com/kylesnowschwartz/agent-ouija/jsonl"
)

// DefaultRoot returns the Copilot CLI session-state directory
// (~/.copilot/session-state).
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".copilot", "session-state"), nil
}

// Cache avoids rescanning unchanged events.jsonl files on every picker
// refresh. The key is the events file path; an entry is valid while both
// mtime and size are unchanged. Changed files resume scanning from the
// previous byte offset (events.jsonl is append-only), so a live session
// being polled every few seconds costs one read of the new tail rather
// than a whole-file re-parse; truncation forces a full rescan.
type Cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	modTime time.Time
	size    int64
	offset  int64 // byte offset the scan consumed up to (resume point)
	scan    sessionScan
}

// NewCache returns an empty cache ready for use.
func NewCache() *Cache {
	return &Cache{entries: make(map[string]cacheEntry)}
}

// getOrScan returns cached scan results when the file hasn't changed,
// otherwise scans (resuming from the cached offset when the file only
// grew) and updates the cache. A nil Cache always scans from the start.
func (c *Cache) getOrScan(path string, modTime time.Time, size int64) sessionScan {
	if c == nil {
		scan, _ := scanSessionEvents(path, sessionScan{}, 0)
		return scan
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cached, ok := c.entries[path]
	if ok && cached.modTime.Equal(modTime) && cached.size == size {
		return cached.scan
	}
	prev, offset := sessionScan{}, int64(0)
	if ok && size >= cached.offset {
		// Append-only growth: fold only the new tail into the cached scan.
		prev, offset = cached.scan, cached.offset
	}
	scan, newOffset := scanSessionEvents(path, prev, offset)
	c.entries[path] = cacheEntry{modTime: modTime, size: size, offset: newOffset, scan: scan}
	return scan
}

// SessionFiles lists every Copilot session events file under root: both
// {root}/{dir}/events.jsonl and flat {root}/{uuid}.jsonl files. One stat
// per directory entry -- also used by the picker watcher's poll signature.
func SessionFiles(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			p := filepath.Join(root, e.Name(), "events.jsonl")
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				files = append(files, p)
			}
		} else if strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(root, e.Name()))
		}
	}
	return files
}

// DiscoverSessions finds all Copilot sessions under root and returns picker
// metadata, newest first. Sessions with no user messages (scaffolding/ghost
// dirs) are skipped. cache may be nil (every file is scanned).
func DiscoverSessions(root string, cache *Cache) ([]discover.SessionInfo, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}

	var sessions []discover.SessionInfo
	for _, path := range SessionFiles(root) {
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		scan := cache.getOrScan(path, fi.ModTime(), fi.Size())
		if scan.TurnCount == 0 {
			continue // ghost/scaffolding session
		}

		info := discover.SessionInfo{
			Path:         path,
			SessionID:    scan.SessionID,
			ModTime:      fi.ModTime(),
			FirstMessage: scan.FirstMessage,
			LastPrompt:   scan.LastPrompt,
			TurnCount:    scan.TurnCount,
			IsOngoing:    IsOngoing(scan.LastEventType, fi.ModTime()),
			DurationMs:   scan.DurationMs,
			Model:        scan.Model,
			Cwd:          scan.Cwd,
			GitBranch:    scan.GitBranch,
		}
		if info.SessionID == "" {
			info.SessionID = fallbackSessionID(path)
		}

		// workspace.yaml overlay (dir-based sessions only; flat files have
		// no session dir of their own).
		if filepath.Base(path) == "events.jsonl" {
			if ws, ok := ReadWorkspace(filepath.Dir(path)); ok {
				if ws.Name != "" {
					info.Title = ws.Name
				}
				if info.Cwd == "" {
					info.Cwd = ws.Cwd
				}
				if info.GitBranch == "" {
					info.GitBranch = ws.Branch
				}
			}
		}

		sessions = append(sessions, info)
	}

	sort.Slice(sessions, func(i, j int) bool {
		if !sessions[i].ModTime.Equal(sessions[j].ModTime) {
			return sessions[i].ModTime.After(sessions[j].ModTime)
		}
		return sessions[i].Path < sessions[j].Path // deterministic tiebreak
	})
	return sessions, nil
}

// fallbackSessionID derives a session ID from the file path when the
// events file carries no session.start: the session dir name for
// dir-based sessions, the file basename for flat files.
func fallbackSessionID(path string) string {
	if filepath.Base(path) == "events.jsonl" {
		return filepath.Base(filepath.Dir(path))
	}
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

// sessionScan holds metadata extracted from a streaming pass over an
// events.jsonl file. It is fold-forward: firstTS/lastTS are carried so a
// later pass can resume from a byte offset and keep DurationMs correct.
type sessionScan struct {
	SessionID     string
	Model         string
	Cwd           string
	GitBranch     string
	FirstMessage  string
	LastPrompt    string
	TurnCount     int
	DurationMs    int64
	LastEventType string

	firstTS time.Time
	lastTS  time.Time
}

// scanSessionEvents streams an events.jsonl file from the given byte
// offset, folding events into prev, and returns the updated scan plus the
// offset consumed. Follows ReadIncremental's partial-last-line rule: an
// unterminated final line is kept (and its bytes consumed) when it already
// parses as complete JSON; an unparseable tail is a mid-append line and is
// excluded from the returned offset so the next pass re-reads it whole.
// Subagent-internal events (top-level agentId) contribute only to the
// last-event-type and timestamps, never to previews or turn counts.
func scanSessionEvents(path string, prev sessionScan, offset int64) (sessionScan, int64) {
	s := prev

	f, err := os.Open(path)
	if err != nil {
		return finishScan(s), offset
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, 0); err != nil {
			return finishScan(s), offset
		}
	}

	lr := jsonl.NewReader(f)
	for {
		line, ok := lr.Next()
		if !ok {
			break
		}
		if !lr.LastLineTerminated() {
			if ev, ok := ParseEvent([]byte(line)); ok {
				foldScanEvent(&s, ev)
				return finishScan(s), offset + lr.BytesRead()
			}
			break
		}
		if ev, ok := ParseEvent([]byte(line)); ok {
			foldScanEvent(&s, ev)
		}
	}
	return finishScan(s), offset + lr.TerminatedBytesRead()
}

// foldScanEvent folds one event into the scan state.
func foldScanEvent(s *sessionScan, ev Event) {
	s.LastEventType = ev.Type
	if !ev.Timestamp.IsZero() {
		if s.firstTS.IsZero() {
			s.firstTS = ev.Timestamp
		}
		s.lastTS = ev.Timestamp
	}
	if ev.AgentID != "" {
		return
	}

	switch ev.Type {
	case "session.start", "session.resume":
		var d SessionStartData
		if json.Unmarshal(ev.Data, &d) == nil {
			if d.SessionID != "" {
				s.SessionID = d.SessionID
			}
			if d.SelectedModel != "" {
				s.Model = d.SelectedModel
			}
			applyContext(s, d.Context)
		}
	case "session.context_changed":
		var d ContextData
		if json.Unmarshal(ev.Data, &d) == nil {
			applyContext(s, d)
		}
	case "session.model_change":
		var d ModelChangeData
		if json.Unmarshal(ev.Data, &d) == nil && d.NewModel != "" {
			s.Model = d.NewModel
		}
	case "assistant.message":
		var d AssistantMessageData
		if json.Unmarshal(ev.Data, &d) == nil && d.Model != "" && d.ParentToolCallID == "" {
			s.Model = d.Model
		}
	case "user.message":
		var d UserMessageData
		if json.Unmarshal(ev.Data, &d) != nil {
			return
		}
		if d.Source != "" || strings.TrimSpace(d.Content) == "" {
			return // synthetic or empty -- not a conversation turn
		}
		s.TurnCount++
		preview := previewText(d.Content)
		if s.FirstMessage == "" {
			s.FirstMessage = preview
		}
		s.LastPrompt = preview
	}
}

// finishScan derives DurationMs from the carried timestamps.
func finishScan(s sessionScan) sessionScan {
	if !s.firstTS.IsZero() && !s.lastTS.IsZero() {
		s.DurationMs = s.lastTS.Sub(s.firstTS).Milliseconds()
	}
	return s
}

// applyContext folds a context snapshot into the scan, keeping prior values
// when the snapshot omits a field.
func applyContext(s *sessionScan, ctx ContextData) {
	if ctx.Cwd != "" {
		s.Cwd = ctx.Cwd
	}
	if ctx.Branch != "" {
		s.GitBranch = ctx.Branch
	}
}

// previewText normalizes a user message for single-line picker display:
// newlines collapsed, truncated to 500 runes (matching Claude conventions).
func previewText(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if r := []rune(s); len(r) > 500 {
		s = string(r[:500])
	}
	return s
}
