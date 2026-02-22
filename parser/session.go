package parser

import (
	"bufio"
	"encoding/json"
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
	TurnCount    int    // conversation turns (user messages + their first AI responses)
	IsOngoing    bool   // AI activity after last ending event
	TotalTokens  int    // sum of all assistant usage tokens
	DurationMs   int64  // last timestamp - first timestamp
	Model        string // model from first real assistant entry
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
//
// If the CWD is inside a git worktree, resolves to the main working tree
// root so we find sessions stored under the original project path.
func CurrentProjectDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// If we're in a git worktree, the CWD differs from the main repo root.
	// Claude stores sessions under the main repo path, so resolve it.
	cwd = resolveGitRoot(cwd)

	// Claude CLI encodes the path by replacing separator with "-".
	encoded := strings.ReplaceAll(cwd, string(filepath.Separator), "-")
	return filepath.Join(home, ".claude", "projects", encoded), nil
}

// resolveGitRoot returns the git toplevel for the given directory. If the
// directory is inside a git worktree, it walks up to find the main working
// tree root. Git worktrees have a .git *file* (not directory) containing
// "gitdir: /path/to/main/.git/worktrees/<name>". We follow that chain to
// find the real repo root whose path Claude uses for session storage.
//
// Falls back to the original path if anything fails (not a git repo, etc).
func resolveGitRoot(dir string) string {
	// Walk up looking for .git entry.
	current := dir
	for {
		gitPath := filepath.Join(current, ".git")
		info, err := os.Lstat(gitPath)
		if err == nil {
			if info.IsDir() {
				// Normal git repo -- this directory is the root.
				return current
			}
			// .git is a file -- this is a worktree (or submodule).
			// Contents: "gitdir: /path/to/main/.git/worktrees/<name>"
			data, err := os.ReadFile(gitPath)
			if err != nil {
				return dir
			}
			content := strings.TrimSpace(string(data))
			if !strings.HasPrefix(content, "gitdir: ") {
				return dir
			}
			gitdir := strings.TrimPrefix(content, "gitdir: ")
			// gitdir points to something like /repo/.git/worktrees/foo
			// The main repo's .git dir is two levels up from there.
			mainGitDir := filepath.Clean(filepath.Join(gitdir, "..", ".."))
			// The main repo root is the parent of .git.
			mainRoot := filepath.Dir(mainGitDir)
			// Sanity check: mainRoot/.git should be a directory.
			if fi, err := os.Stat(filepath.Join(mainRoot, ".git")); err == nil && fi.IsDir() {
				return mainRoot
			}
			return dir
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Hit filesystem root without finding .git.
			return dir
		}
		current = parent
	}
}

// DiscoverProjectSessions finds all session .jsonl files in a project directory,
// scans each for metadata, and returns them sorted by modification time (newest first).
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
		meta := scanSessionMetadata(path)

		// Skip ghost sessions (e.g. only file-history-snapshot entries).
		if meta.turnCount == 0 {
			continue
		}

		isOngoing := meta.isOngoing
		if isOngoing && time.Since(info.ModTime()) > OngoingStalenessThreshold {
			isOngoing = false
		}

		sessions = append(sessions, SessionInfo{
			Path:         path,
			SessionID:    strings.TrimSuffix(name, ".jsonl"),
			ModTime:      info.ModTime(),
			FirstMessage: meta.firstMsg,
			TurnCount:    meta.turnCount,
			IsOngoing:    isOngoing,
			TotalTokens:  meta.totalTokens,
			DurationMs:   meta.durationMs,
			Model:        meta.model,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	return sessions, nil
}

// sessionMetadata holds all metadata extracted from a single-pass file scan.
type sessionMetadata struct {
	firstMsg    string
	turnCount   int
	isOngoing   bool
	totalTokens int
	durationMs  int64
	model       string
}

// scanSessionMetadata extracts all session metadata in a single streaming pass.
// Replaces the old scanSessionPreview -- same preview extraction logic plus
// ongoing detection, token accumulation, duration, model, and turn counting.
//
// Preview extraction ported from claude-devtools' extractFirstUserMessagePreview.
// Ongoing detection ported from claude-devtools' analyzeSessionFileMetadata (jsonl.ts:437-499).
// Turn counting ported from claude-devtools' analyzeSessionFileMetadata (jsonl.ts:374-385).
func scanSessionMetadata(path string) sessionMetadata {
	f, err := os.Open(path)
	if err != nil {
		return sessionMetadata{}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var meta sessionMetadata
	var commandFallback string
	previewFound := false
	linesRead := 0
	// maxPreviewLines caps how many raw JSONL lines we scan for the session preview.
	// 200 is generous enough to find the first real user message even in sessions
	// that start with many system/meta entries, without scanning enormous files.
	// Ported from claude-devtools' extractFirstUserMessagePreview.
	const maxPreviewLines = 200

	// Turn counting: user message increments, then first qualifying AI response increments.
	awaitingAIGroup := false

	// Ongoing detection state (one-pass, ported from jsonl.ts:437-499).
	var activityIndex int
	lastEndingIndex := -1
	hasAnyOngoingActivity := false
	hasActivityAfterLastEnding := false
	shutdownToolIDs := make(map[string]bool)

	// Duration tracking.
	var firstTS, lastTS time.Time

	for scanner.Scan() {
		line := scanner.Bytes()
		linesRead++

		if len(line) == 0 {
			continue
		}

		// Parse the entry with a lightweight struct that captures toolUseResult
		// as raw JSON for the ongoing detection edge case.
		var raw metadataScanEntry
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if raw.UUID == "" {
			continue
		}

		// Track timestamps for duration.
		if ts := parseTimestamp(raw.Timestamp); !ts.IsZero() {
			if firstTS.IsZero() {
				firstTS = ts
			}
			lastTS = ts
		}

		// --- Turn counting (matches isParsedUserChunkMessage + AI pairing) ---
		if isUserChunkForTurnCount(&raw) {
			meta.turnCount++
			awaitingAIGroup = true
		} else if awaitingAIGroup && raw.Type == "assistant" && raw.Message.Model != "<synthetic>" && !raw.IsSidechain {
			meta.turnCount++
			awaitingAIGroup = false
		}

		// --- Token accumulation ---
		if raw.Type == "assistant" && !raw.IsSidechain && raw.Message.Model != "<synthetic>" {
			u := raw.Message.Usage
			meta.totalTokens += u.InputTokens + u.OutputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
		}

		// --- Model extraction (first real assistant entry) ---
		if meta.model == "" && raw.Type == "assistant" && !raw.IsSidechain && raw.Message.Model != "" && raw.Message.Model != "<synthetic>" {
			meta.model = raw.Message.Model
		}

		// --- Ongoing detection (ported from jsonl.ts:437-499) ---
		if raw.Type == "assistant" && !raw.IsSidechain {
			scanOngoingAssistant(&raw, &activityIndex, &lastEndingIndex,
				&hasAnyOngoingActivity, &hasActivityAfterLastEnding, shutdownToolIDs)
		} else if raw.Type == "user" {
			scanOngoingUser(&raw, &activityIndex, &lastEndingIndex,
				&hasAnyOngoingActivity, &hasActivityAfterLastEnding, shutdownToolIDs)
		}

		// --- Preview extraction (unchanged from scanSessionPreview) ---
		if previewFound || linesRead > maxPreviewLines || raw.Type != "user" {
			continue
		}

		text := ExtractText(raw.Message.Content)
		if text == "" {
			continue
		}

		if IsCommandOutput(text) || strings.HasPrefix(text, "[Request interrupted by user") {
			continue
		}

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

		sanitized := strings.TrimSpace(SanitizeContent(text))
		if sanitized == "" {
			continue
		}
		if len(sanitized) > 500 {
			sanitized = sanitized[:500]
		}
		meta.firstMsg = sanitized
		previewFound = true
	}

	if meta.firstMsg == "" {
		meta.firstMsg = commandFallback
	}

	// Collapse newlines for single-line display.
	if meta.firstMsg != "" {
		meta.firstMsg = strings.ReplaceAll(meta.firstMsg, "\n", " ")
	}

	// Finalize ongoing detection.
	if lastEndingIndex == -1 {
		meta.isOngoing = hasAnyOngoingActivity
	} else {
		meta.isOngoing = hasActivityAfterLastEnding
	}

	// Finalize duration.
	if !firstTS.IsZero() && !lastTS.IsZero() {
		meta.durationMs = lastTS.Sub(firstTS).Milliseconds()
	}

	return meta
}

// metadataScanEntry is a lightweight struct for the metadata scan pass.
// It captures toolUseResult as raw JSON because the field can be either a
// string or an object, and we need the raw value for rejection detection.
type metadataScanEntry struct {
	UUID        string          `json:"uuid"`
	Type        string          `json:"type"`
	Timestamp   string          `json:"timestamp"`
	IsSidechain bool            `json:"isSidechain"`
	IsMeta      bool            `json:"isMeta"`
	ToolResult  json.RawMessage `json:"toolUseResult"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Model   string          `json:"model"`
		Usage   struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// isUserChunkForTurnCount mirrors claude-devtools' isParsedUserChunkMessage:
// type=user, isMeta=false, not teammate, not sidechain, has real user content,
// and doesn't start with system output tags.
func isUserChunkForTurnCount(e *metadataScanEntry) bool {
	if e.Type != "user" || e.IsMeta || e.IsSidechain {
		return false
	}

	text := ExtractText(e.Message.Content)
	trimmed := strings.TrimSpace(text)

	// Teammate messages.
	if teammateMessageRe.MatchString(trimmed) {
		return false
	}

	// System output tags.
	for _, tag := range systemOutputTags {
		if strings.HasPrefix(trimmed, tag) {
			return false
		}
	}

	// Must have actual content (text or image blocks for array content).
	return hasUserContent(e.Message.Content, text)
}

// scanOngoingAssistant processes an assistant entry for ongoing detection.
// Ported from jsonl.ts:438-470.
func scanOngoingAssistant(e *metadataScanEntry, activityIndex *int,
	lastEndingIndex *int, hasAny, hasAfter *bool, shutdownIDs map[string]bool) {

	var blocks []ongoingBlock
	if err := json.Unmarshal(e.Message.Content, &blocks); err != nil {
		return
	}

	for _, b := range blocks {
		switch b.Type {
		case "thinking":
			if strings.TrimSpace(b.Thinking) != "" {
				*hasAny = true
				if *lastEndingIndex >= 0 {
					*hasAfter = true
				}
				*activityIndex++
			}
		case "tool_use":
			if b.ID == "" {
				continue
			}
			if b.Name == "ExitPlanMode" {
				*lastEndingIndex = *activityIndex
				*hasAfter = false
				*activityIndex++
			} else if isShutdownApproval(b.Name, b.Input) {
				shutdownIDs[b.ID] = true
				*lastEndingIndex = *activityIndex
				*hasAfter = false
				*activityIndex++
			} else {
				*hasAny = true
				if *lastEndingIndex >= 0 {
					*hasAfter = true
				}
				*activityIndex++
			}
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				*lastEndingIndex = *activityIndex
				*hasAfter = false
				*activityIndex++
			}
		}
	}
}

// scanOngoingUser processes a user entry for ongoing detection.
// Ported from jsonl.ts:471-499.
func scanOngoingUser(e *metadataScanEntry, activityIndex *int,
	lastEndingIndex *int, hasAny, hasAfter *bool, shutdownIDs map[string]bool) {

	// Check for user-rejected tool use at the entry level.
	isRejection := isToolUseRejection(e.ToolResult)

	var blocks []ongoingUserBlock
	if err := json.Unmarshal(e.Message.Content, &blocks); err != nil {
		return
	}

	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			if b.ToolUseID == "" {
				continue
			}
			if shutdownIDs[b.ToolUseID] || isRejection {
				// Ending event.
				*lastEndingIndex = *activityIndex
				*hasAfter = false
				*activityIndex++
			} else {
				// Ongoing activity.
				*hasAny = true
				if *lastEndingIndex >= 0 {
					*hasAfter = true
				}
				*activityIndex++
			}
		case "text":
			if strings.HasPrefix(b.Text, "[Request interrupted by user") {
				*lastEndingIndex = *activityIndex
				*hasAfter = false
				*activityIndex++
			}
		}
	}
}

// ongoingBlock is the minimal struct for parsing assistant content blocks
// during ongoing detection. Only captures fields needed for activity classification.
type ongoingBlock struct {
	Type     string          `json:"type"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Input    json.RawMessage `json:"input"`
}

// ongoingUserBlock is the minimal struct for parsing user content blocks
// during ongoing detection.
type ongoingUserBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Text      string `json:"text"`
}

// toolUseRejectedMsg is the exact string Claude Code writes to toolUseResult
// when a user rejects a tool invocation.
const toolUseRejectedMsg = "User rejected tool use"

// isToolUseRejection checks if a raw toolUseResult value equals the rejection string.
func isToolUseRejection(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	return s == toolUseRejectedMsg
}
