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
	FirstMessage string // first user message text, truncated
	MessageCount int    // number of classified messages
}

// ReadSession reads a JSONL session file and returns the fully processed chunk list.
func ReadSession(path string) ([]Chunk, error) {
	msgs, _, err := ReadSessionIncremental(path, 0)
	if err != nil {
		return nil, err
	}
	return BuildChunks(msgs), nil
}

// ReadSessionIncremental reads new lines from a session file starting at the
// given byte offset. Returns newly classified messages, the updated offset,
// and any error. This is the building block for live tailing -- the caller
// accumulates classified messages and re-runs BuildChunks after each call.
func ReadSessionIncremental(path string, offset int64) ([]ClassifiedMsg, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	if _, err := f.Seek(offset, 0); err != nil {
		return nil, offset, err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var msgs []ClassifiedMsg
	bytesRead := offset

	for scanner.Scan() {
		line := scanner.Bytes()
		// +1 for the \n delimiter stripped by scanner. This assumes Unix line
		// endings, which is correct -- Claude Code only runs on macOS/Linux.
		bytesRead += int64(len(line)) + 1

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
		return msgs, bytesRead, err
	}

	return msgs, bytesRead, nil
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

		// Skip ghost sessions (e.g. only file-history-snapshot entries).
		if msgCount == 0 {
			continue
		}

		sessions = append(sessions, SessionInfo{
			Path:         path,
			SessionID:    strings.TrimSuffix(name, ".jsonl"),
			ModTime:      info.ModTime(),
			FirstMessage: firstMsg,
			MessageCount: msgCount,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	return sessions, nil
}

// scanSessionPreview extracts a preview string and classified message count
// from a session file. Ported from claude-devtools' extractFirstUserMessagePreview
// in metadataExtraction.ts -- processes ALL type=user entries without filtering
// isMeta or sidechain, skips only command output and interruptions, and uses
// SanitizeContent on everything else.
//
// Fallback chain: real user text > slash command > "".
// Preview search covers the first 200 raw lines; message counting covers the
// entire file.
func scanSessionPreview(path string) (firstMsg string, msgCount int) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var commandFallback string
	previewFound := false
	linesRead := 0
	const maxPreviewLines = 200

	for scanner.Scan() {
		line := scanner.Bytes()
		// Count ALL lines toward the limit (matching claude-devtools behavior).
		linesRead++

		if len(line) == 0 {
			continue
		}
		entry, ok := ParseEntry(line)
		if !ok {
			continue
		}

		// Count classified messages for the badge (full file scan).
		if _, ok := Classify(entry); ok {
			msgCount++
		}

		// Preview: only within line limit, only type=user entries.
		// No isMeta or sidechain filtering -- matches claude-devtools.
		if previewFound || linesRead > maxPreviewLines || entry.Type != "user" {
			continue
		}

		text := ExtractText(entry.Message.Content)
		if text == "" {
			continue
		}

		// Skip command output and interruptions.
		if IsCommandOutput(text) || strings.HasPrefix(text, "[Request interrupted by user") {
			continue
		}

		// Commands: extract name as fallback, keep searching for real text.
		if strings.HasPrefix(text, "<command-name>") {
			if commandFallback == "" {
				if m := reCommandName.FindStringSubmatch(text); m != nil {
					commandFallback = "/" + strings.TrimSpace(m[1])
				} else {
					commandFallback = "/command"
				}
			}
			continue
		}

		// Everything else: sanitize and use. This handles teammate messages,
		// meta entries, and anything else -- matching claude-devtools which
		// runs sanitizeDisplayContent on all non-skipped user entries.
		sanitized := strings.TrimSpace(SanitizeContent(text))
		if sanitized == "" {
			continue
		}
		if len(sanitized) > 500 {
			sanitized = sanitized[:500]
		}
		firstMsg = sanitized
		previewFound = true
	}

	if firstMsg == "" {
		firstMsg = commandFallback
	}

	// Collapse newlines for single-line display.
	if firstMsg != "" {
		firstMsg = strings.ReplaceAll(firstMsg, "\n", " ")
		if len(firstMsg) > 120 {
			firstMsg = firstMsg[:119] + "…"
		}
	}

	return firstMsg, msgCount
}
