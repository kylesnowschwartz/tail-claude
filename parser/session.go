package parser

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

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
