package parser

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionInfo holds metadata about a discovered session file for the picker.
type SessionInfo struct {
	Path         string
	SessionID    string
	ModTime      time.Time
	Size         int64
	FirstMessage string // first user message text, truncated
	MessageCount int    // number of classified messages
}

// ReadSession reads a JSONL session file and returns the fully processed chunk list.
// This is the only function in the package that performs IO.
func ReadSession(path string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []ClassifiedMsg

	scanner := bufio.NewScanner(f)
	// Default token size is 64KB. Session files can have very long lines
	// (tool outputs, base64 images), so bump to 4MB.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		entry, ok := ParseEntry(line)
		if !ok {
			continue
		}
		msg, ok := Classify(entry)
		if !ok {
			continue
		}
		msgs = append(msgs, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return BuildChunks(msgs), nil
}

// DiscoverLatestSession finds the most recently modified .jsonl file under
// ~/.claude/projects/. Subagent files inside session UUID subdirectories
// are excluded.
func DiscoverLatestSession() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	root := filepath.Join(home, ".claude", "projects")

	var bestPath string
	var bestTime int64

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip directories we can't read.
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".jsonl") {
			return nil
		}

		// Exclude subagent files: they live inside a subdirectory named after
		// the parent session UUID (e.g. {session_uuid}/agent_{id}.jsonl) or
		// at project root with an agent_ prefix (legacy structure).
		// We want top-level session files: {project}/{session_uuid}.jsonl.
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		parts := strings.Split(rel, string(filepath.Separator))
		// Expected: project-name/session.jsonl (2 parts).
		// Subagent new structure: project-name/session-uuid/agent_xxx.jsonl (3+ parts).
		if len(parts) > 2 {
			return nil
		}
		// Legacy subagent: project-name/agent_xxx.jsonl.
		if strings.HasPrefix(info.Name(), "agent_") {
			return nil
		}

		modTime := info.ModTime().UnixNano()
		if modTime > bestTime {
			bestTime = modTime
			bestPath = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if bestPath == "" {
		return "", os.ErrNotExist
	}
	return bestPath, nil
}

// CurrentProjectDir returns the Claude CLI projects directory for the current
// working directory. The encoding scheme replaces "/" with "-" and prepends
// to ~/.claude/projects/. Example:
//
//	/Users/kyle/Code/proj -> ~/.claude/projects/-Users-kyle-Code-proj
func CurrentProjectDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Claude CLI encodes the path by replacing separator with "-".
	encoded := strings.ReplaceAll(cwd, string(filepath.Separator), "-")
	return filepath.Join(home, ".claude", "projects", encoded), nil
}

// DiscoverProjectSessions finds all session .jsonl files in a project directory,
// scans each for a preview, and returns them sorted by modification time (newest first).
// Subagent files (agent_*) are excluded.
func DiscoverProjectSessions(projectDir string) ([]SessionInfo, error) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}

	var sessions []SessionInfo
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if strings.HasPrefix(name, "agent_") {
			continue
		}

		info, err := de.Info()
		if err != nil {
			continue
		}

		path := filepath.Join(projectDir, name)
		firstMsg, msgCount := scanSessionPreview(path)

		sessions = append(sessions, SessionInfo{
			Path:         path,
			SessionID:    strings.TrimSuffix(name, ".jsonl"),
			ModTime:      info.ModTime(),
			Size:         info.Size(),
			FirstMessage: firstMsg,
			MessageCount: msgCount,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	return sessions, nil
}

// scanSessionPreview does a quick scan of a session file, counting classified
// messages and capturing the first user message text. The preview is truncated
// to 120 characters.
func scanSessionPreview(path string) (firstMsg string, msgCount int) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		entry, ok := ParseEntry(line)
		if !ok {
			continue
		}
		msg, ok := Classify(entry)
		if !ok {
			continue
		}
		msgCount++

		if firstMsg == "" {
			if u, ok := msg.(UserMsg); ok && u.Text != "" {
				firstMsg = u.Text
			}
		}
	}

	// Collapse newlines and truncate for display.
	if firstMsg != "" {
		firstMsg = strings.ReplaceAll(firstMsg, "\n", " ")
		if len(firstMsg) > 120 {
			firstMsg = firstMsg[:119] + "…"
		}
	}

	return firstMsg, msgCount
}
