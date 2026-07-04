package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionInfo holds metadata about a discovered session file for the picker.
type SessionInfo struct {
	Path           string
	SessionID      string
	ModTime        time.Time
	Title          string // custom or AI-generated session title (custom wins)
	FirstMessage   string // first user message text, truncated
	LastPrompt     string // most recent user input (from type=last-prompt)
	TurnCount      int    // conversation turns (user messages + their first AI responses)
	IsOngoing      bool   // AI activity after last ending event
	ContextTokens  int    // last assistant message's context window usage
	DurationMs     int64  // last timestamp - first timestamp
	Model          string // model from first real assistant entry
	Cwd            string // working directory from session entries
	GitBranch      string // git branch from session entries
	PermissionMode string // last permission mode: "default", "acceptEdits", "bypassPermissions", "plan"
}

// SessionMeta holds session-level metadata extracted from a JSONL file.
// Unlike SessionInfo (which is for the picker), SessionMeta is designed for
// the info bar -- just the metadata fields, no picker-specific data.
type SessionMeta struct {
	Cwd            string
	GitBranch      string
	PermissionMode string
}

// ExtractSessionMeta returns session-level metadata from a JSONL file.
// Reads the full file to capture the last permissionMode (mode can change mid-session).
func ExtractSessionMeta(path string) SessionMeta {
	m := scanSessionMetadata(path)
	return SessionMeta{
		Cwd:            m.cwd,
		GitBranch:      m.gitBranch,
		PermissionMode: m.permissionMode,
	}
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

	lr := newLineReader(f)

	var msgs []ClassifiedMsg

	for {
		line, ok := lr.next()
		if !ok {
			break
		}
		if !lr.LastLineTerminated() {
			// EOF-truncated tail. A JSONL line is one JSON object, so a
			// half-written append can never parse as a complete entry
			// (json.Unmarshal rejects both prefixes and trailing garbage).
			// If the tail parses, the record is complete and the file just
			// lacks a trailing newline -- keep it and consume its bytes.
			// Otherwise it is an append still in progress: skip it and
			// exclude it from the offset (TerminatedBytesRead below) so the
			// next incremental read picks up the completed line intact.
			entry, ok := ParseEntry([]byte(line))
			if !ok {
				break
			}
			if msg, ok := Classify(entry); ok {
				msgs = append(msgs, msg)
			}
			resolvePersistedOutputs(msgs, filepath.Dir(path))
			return msgs, offset + lr.BytesRead(), nil
		}
		entry, ok := ParseEntry([]byte(line))
		if !ok {
			continue
		}
		msg, ok := Classify(entry)
		if !ok {
			continue
		}
		msgs = append(msgs, msg)
	}
	if err := lr.Err(); err != nil {
		return msgs, offset + lr.TerminatedBytesRead(), err
	}

	// Inline externalized tool results ({projectDir}/{session}/tool-results/).
	resolvePersistedOutputs(msgs, filepath.Dir(path))

	return msgs, offset + lr.TerminatedBytesRead(), nil
}

// ProjectDirForPath returns the Claude CLI projects directory for an absolute
// path. Claude Code encodes paths by replacing "/", ".", and "_" with "-",
// then stores sessions under ~/.claude/projects/<encoded>. Example:
//
//	/Users/kyle/Code/proj -> ~/.claude/projects/-Users-kyle-Code-proj
//	/Users/kyle/.config    -> ~/.claude/projects/-Users-kyle--config
//
// Symlinks are resolved so the encoded path matches what Claude Code produces
// (e.g. macOS /tmp -> /private/tmp).
func ProjectDirForPath(absPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}
	encoded := encodePath(absPath)
	return filepath.Join(home, ".claude", "projects", encoded), nil
}

// encodePath encodes an absolute filesystem path into a Claude Code project
// directory name. Three characters are replaced with "-": path separators,
// dots, and underscores. The encoding is lossy (cannot be reversed for paths
// containing literal dashes).
//
// Verified empirically against Claude Code's on-disk output across 273
// project directories including dotfile paths (.claude, .config), worktree
// paths (.claude/worktrees/), and macOS temp paths (containing underscores).
func encodePath(absPath string) string {
	r := strings.NewReplacer(
		string(filepath.Separator), "-",
		".", "-",
		"_", "-",
	)
	return r.Replace(absPath)
}

// CurrentProjectDir returns the Claude CLI projects directory for the current
// working directory. If the CWD is inside a git worktree, resolves to the
// main working tree root so we find sessions stored under the original
// project path.
func CurrentProjectDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// If we're in a git worktree, the CWD differs from the main repo root.
	// Claude stores sessions under the main repo path, so resolve it.
	cwd = ResolveGitRoot(cwd)

	return ProjectDirForPath(cwd)
}

// ResolveGitRoot returns the git toplevel for the given directory. If the
// directory is inside a git worktree, it resolves to the main working tree
// root via the .git file's gitdir reference and commondir.
//
// Falls back to the original path if anything fails (not a git repo, etc).
func ResolveGitRoot(dir string) string {
	if root := findGitRepoRoot(dir); root != "" {
		return root
	}
	return dir
}

// DiscoverProjectSessions finds all session .jsonl files in a project directory,
// scans each for metadata, and returns them sorted by modification time (newest first).
// Subagent files (see isAgentSessionFile) are excluded.
func DiscoverProjectSessions(projectDir string) ([]SessionInfo, error) {
	return discoverSessions(projectDir, func(path string, _ time.Time) sessionMetadata {
		return scanSessionMetadata(path)
	})
}

// ListAllProjectDirs returns every Claude Code project directory under
// ~/.claude/projects. Used for name-based session lookup that spans projects;
// name resolution inside a single project should prefer CurrentProjectDir.
func ListAllProjectDirs() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, ".claude", "projects")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirs = append(dirs, filepath.Join(root, e.Name()))
	}
	return dirs, nil
}

// SessionTitleRef is a lightweight session reference for name-based lookup.
// It carries only the fields needed to open or display the session; full
// metadata requires DiscoverProjectSessions.
type SessionTitleRef struct {
	Path      string
	SessionID string
	Title     string
	ModTime   time.Time
}

// scanSessionTitle reads a session file and returns its effective title
// (custom-title wins over ai-title; last occurrence of each wins). It
// avoids the full scanSessionMetadata pipeline — no preview extraction,
// no ongoing detection, no turn counting, no JSON parsing of content
// lines. Lines over titleLineCap bytes or lacking the "title" substring
// are rejected before unmarshaling, so content-bearing entries cost only
// a length check and a byte scan.
func scanSessionTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	const titleLineCap = 512 // title entries are tiny; real content is KB+

	lr := newLineReader(f)
	var custom, ai string
	for {
		line, ok := lr.next()
		if !ok {
			break
		}
		if len(line) > titleLineCap {
			continue
		}
		if !strings.Contains(line, `"custom-title"`) && !strings.Contains(line, `"ai-title"`) {
			continue
		}
		var raw struct {
			Type        string `json:"type"`
			CustomTitle string `json:"customTitle"`
			AITitle     string `json:"aiTitle"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		switch raw.Type {
		case "custom-title":
			if raw.CustomTitle != "" {
				custom = raw.CustomTitle
			}
		case "ai-title":
			if raw.AITitle != "" {
				ai = raw.AITitle
			}
		}
	}
	if custom != "" {
		return custom
	}
	return ai
}

// discoverSessionTitles lists every titled session in a project directory.
// Untitled sessions are omitted — they can't match a name lookup. Much
// cheaper than DiscoverProjectSessions because it uses scanSessionTitle
// instead of scanSessionMetadata.
func discoverSessionTitles(projectDir string) ([]SessionTitleRef, error) {
	files, err := listSessionFiles(projectDir)
	if err != nil {
		return nil, err
	}
	var refs []SessionTitleRef
	for _, sf := range files {
		title := scanSessionTitle(sf.path)
		if title == "" {
			continue
		}
		refs = append(refs, SessionTitleRef{
			Path:      sf.path,
			SessionID: strings.TrimSuffix(sf.name, ".jsonl"),
			Title:     title,
			ModTime:   sf.modTime,
		})
	}
	return refs, nil
}

// FindTitleMatches searches the given project directories for titled sessions
// whose Title (custom-title or ai-title) matches the query case-insensitively.
// Exact matches win over substring matches; within a tier, newest-first order.
//
// This function reads only the title metadata from each session — it does not
// scan conversation content — so cost scales with the number of session files,
// not their total size.
func FindTitleMatches(query string, projectDirs []string) ([]SessionTitleRef, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	var all []SessionTitleRef
	for _, d := range projectDirs {
		refs, err := discoverSessionTitles(d)
		if err != nil {
			continue // missing dir or permission error — skip
		}
		all = append(all, refs...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].ModTime.After(all[j].ModTime)
	})
	lower := strings.ToLower(query)
	var exact, partial []SessionTitleRef
	for _, r := range all {
		t := strings.ToLower(r.Title)
		switch {
		case t == lower:
			exact = append(exact, r)
		case strings.Contains(t, lower):
			partial = append(partial, r)
		}
	}
	if len(exact) > 0 {
		return exact, nil
	}
	return partial, nil
}

// DiscoverAllProjectSessions finds sessions across multiple project directories
// (main + worktree dirs). Calls DiscoverProjectSessions on each, merges results,
// and sorts by ModTime descending. Missing directories are silently skipped.
func DiscoverAllProjectSessions(projectDirs []string) ([]SessionInfo, error) {
	return discoverAllSessions(projectDirs, DiscoverProjectSessions)
}

// discoverAllSessions is the shared merge-and-sort for DiscoverAllProjectSessions
// and its cached variant. The discover function determines how each directory is
// scanned (direct vs cache-backed). Missing directories are silently skipped.
func discoverAllSessions(projectDirs []string, discover func(string) ([]SessionInfo, error)) ([]SessionInfo, error) {
	var all []SessionInfo
	for _, dir := range projectDirs {
		sessions, err := discover(dir)
		if err != nil {
			continue // missing dir or permission error -- skip
		}
		all = append(all, sessions...)
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].ModTime.After(all[j].ModTime)
	})

	return all, nil
}

// sessionFile is a candidate session file returned by listSessionFiles.
type sessionFile struct {
	path    string
	name    string
	modTime time.Time
}

// listSessionFiles returns the session .jsonl files in a project directory,
// skipping subdirectories, non-.jsonl files, and agent-prefixed subagent files.
// Single home for the walk filtering shared by discoverSessions and
// discoverSessionTitles, so a filtering fix lands in both.
func listSessionFiles(projectDir string) ([]sessionFile, error) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}
	var files []sessionFile
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if isAgentSessionFile(name) {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		files = append(files, sessionFile{
			path:    filepath.Join(projectDir, name),
			name:    name,
			modTime: info.ModTime(),
		})
	}
	return files, nil
}

// scanFn returns session metadata for a given file path and modTime.
type scanFn func(path string, modTime time.Time) sessionMetadata

// discoverSessions is the shared directory-walk logic for DiscoverProjectSessions
// and its cached variant. The scan function determines how metadata is obtained
// (direct scan vs cache lookup).
func discoverSessions(projectDir string, scan scanFn) ([]SessionInfo, error) {
	files, err := listSessionFiles(projectDir)
	if err != nil {
		return nil, err
	}

	var sessions []SessionInfo
	for _, sf := range files {
		meta := scan(sf.path, sf.modTime)

		// Skip ghost sessions (e.g. only file-history-snapshot entries).
		if meta.turnCount == 0 {
			continue
		}

		isOngoing := meta.isOngoing
		if isOngoing && time.Since(sf.modTime) > OngoingStalenessThreshold {
			isOngoing = false
		}
		// A background Workflow run keeps working while the parent file goes
		// silent — the parent-derived signal above misses it in both
		// directions (stale parent, or a turn that "ended" with the launch).
		if !isOngoing && ScanWorkflowActivity(sf.path).Active(OngoingStalenessThreshold) {
			isOngoing = true
		}

		// Resolve title: custom (user rename) wins over AI-generated.
		title := meta.customTitle
		if title == "" {
			title = meta.aiTitle
		}

		sessions = append(sessions, SessionInfo{
			Path:           sf.path,
			SessionID:      strings.TrimSuffix(sf.name, ".jsonl"),
			ModTime:        sf.modTime,
			Title:          title,
			FirstMessage:   meta.firstMsg,
			LastPrompt:     meta.lastPrompt,
			TurnCount:      meta.turnCount,
			IsOngoing:      isOngoing,
			ContextTokens:  meta.contextTokens,
			DurationMs:     meta.durationMs,
			Model:          meta.model,
			Cwd:            meta.cwd,
			GitBranch:      meta.gitBranch,
			PermissionMode: meta.permissionMode,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	return sessions, nil
}

// sessionMetadata holds all metadata extracted from a single-pass file scan.
type sessionMetadata struct {
	firstMsg       string
	lastPrompt     string // from type=last-prompt entries (most recent user input)
	customTitle    string // from type=custom-title entries (user rename via /rename or --name)
	aiTitle        string // from type=ai-title entries (auto-generated by Claude)
	turnCount      int
	isOngoing      bool
	contextTokens  int
	durationMs     int64
	model          string
	cwd            string // first non-empty cwd from any entry
	gitBranch      string // first non-empty gitBranch from any entry
	permissionMode string // last non-empty permissionMode (mode can change mid-session)
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

	lr := newLineReader(f)

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

	// Context tokens: we want the last assistant message's context snapshot.
	// Streaming entries share a requestId with incrementally larger counts,
	// but since we always overwrite, last-entry-wins naturally.

	// Ongoing detection state (one-pass, ported from jsonl.ts:437-499).
	var activityIndex int
	lastEndingIndex := -1
	hasAnyOngoingActivity := false
	hasActivityAfterLastEnding := false
	shutdownToolIDs := make(map[string]bool)
	pendingToolIDs := make(map[string]bool) // tool_use IDs awaiting tool_result

	// Duration tracking.
	var firstTS, lastTS time.Time

	for {
		line, ok := lr.next()
		if !ok {
			break
		}
		linesRead++

		// Parse the entry with a lightweight struct that captures toolUseResult
		// as raw JSON for the ongoing detection edge case.
		var raw metadataScanEntry
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		// Metadata entries (no UUID) carry session-level state. Extract
		// before the UUID guard. Last value wins for all of these.
		switch raw.Type {
		case "custom-title":
			if raw.CustomTitle != "" {
				meta.customTitle = raw.CustomTitle
			}
			continue
		case "ai-title":
			if raw.AITitle != "" {
				meta.aiTitle = raw.AITitle
			}
			continue
		case "permission-mode":
			if raw.PermissionMode != "" {
				meta.permissionMode = raw.PermissionMode
			}
			continue
		case "last-prompt":
			if raw.LastPrompt != "" {
				meta.lastPrompt = raw.LastPrompt
			}
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

		// --- Session-level metadata (cwd, branch: first seen; mode: last seen) ---
		if meta.cwd == "" && raw.Cwd != "" {
			meta.cwd = raw.Cwd
		}
		if meta.gitBranch == "" && raw.GitBranch != "" {
			meta.gitBranch = raw.GitBranch
		}
		// Fallback: older sessions lack type=permission-mode entries and
		// only carry the field on conversational entries.
		if raw.PermissionMode != "" {
			meta.permissionMode = raw.PermissionMode
		}

		// --- Turn counting (matches isParsedUserChunkMessage + AI pairing) ---
		if isUserChunkForTurnCount(&raw) {
			meta.turnCount++
			awaitingAIGroup = true
		} else if awaitingAIGroup && raw.Type == "assistant" && raw.Message.Model != "<synthetic>" && !raw.IsSidechain {
			meta.turnCount++
			awaitingAIGroup = false
		}

		// --- Context token tracking (last assistant message's window snapshot) ---
		if raw.Type == "assistant" && !raw.IsSidechain && raw.Message.Model != "<synthetic>" {
			// Iterations-aware fields (ContextUsage takes the last iteration's
			// snapshot), routed through the one canonical window formula.
			u := raw.Message.Usage.ContextUsage()
			meta.contextTokens = Usage{
				InputTokens:         u.InputTokens,
				CacheReadTokens:     u.CacheReadInputTokens,
				CacheCreationTokens: u.CacheCreationInputTokens,
			}.ContextTokens()
		}

		// --- Model extraction (first real assistant entry) ---
		if meta.model == "" && raw.Type == "assistant" && !raw.IsSidechain && raw.Message.Model != "" && raw.Message.Model != "<synthetic>" {
			meta.model = raw.Message.Model
		}

		// --- Ongoing detection (ported from jsonl.ts:437-499) ---
		if raw.Type == "assistant" && !raw.IsSidechain {
			scanOngoingAssistant(&raw, &activityIndex, &lastEndingIndex,
				&hasAnyOngoingActivity, &hasActivityAfterLastEnding, shutdownToolIDs, pendingToolIDs)
		} else if raw.Type == "user" {
			scanOngoingUser(&raw, &activityIndex, &lastEndingIndex,
				&hasAnyOngoingActivity, &hasActivityAfterLastEnding, shutdownToolIDs, pendingToolIDs)
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
		// Cut on rune boundaries: a byte slice can split a multi-byte rune
		// and corrupt every downstream preview of this message.
		if r := []rune(sanitized); len(r) > 500 {
			sanitized = string(r[:500])
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

	// Default permissionMode when absent. Some Claude Code sessions omit the
	// field entirely (inconsistent serialization). "default" is the correct
	// label -- the session ran under the user's default permission mode.
	if meta.permissionMode == "" {
		meta.permissionMode = "default"
	}

	// Finalize ongoing detection.
	// Activity-based: is there AI activity after the last ending event?
	if lastEndingIndex == -1 {
		meta.isOngoing = hasAnyOngoingActivity
	} else {
		meta.isOngoing = hasActivityAfterLastEnding
	}
	// Pending tool calls override: a tool_use without a matching tool_result
	// means work is still in progress, even if text output appeared after it.
	if !meta.isOngoing && len(pendingToolIDs) > 0 {
		meta.isOngoing = true
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
	UUID           string          `json:"uuid"`
	Type           string          `json:"type"`
	Timestamp      string          `json:"timestamp"`
	IsSidechain    bool            `json:"isSidechain"`
	IsMeta         bool            `json:"isMeta"`
	Cwd            string          `json:"cwd"`
	GitBranch      string          `json:"gitBranch"`
	PermissionMode string          `json:"permissionMode"`
	ToolResult     json.RawMessage `json:"toolUseResult"`
	CustomTitle    string          `json:"customTitle"`
	AITitle        string          `json:"aiTitle"`
	LastPrompt     string          `json:"lastPrompt"`
	Message        struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Model   string          `json:"model"`
		Usage   EntryUsage      `json:"usage"`
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
	lastEndingIndex *int, hasAny, hasAfter *bool, shutdownIDs, pendingToolIDs map[string]bool) {

	var blocks []ongoingBlock
	if err := json.Unmarshal(e.Message.Content, &blocks); err != nil {
		return
	}

	for _, b := range blocks {
		switch b.Type {
		case "thinking":
			// Counts even when the text is empty: Opus 4.7+/Claude 5 models
			// persist redacted thinking blocks (signature only), and a
			// thinking block of either kind means Claude is working.
			*hasAny = true
			if *lastEndingIndex >= 0 {
				*hasAfter = true
			}
			*activityIndex++
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
				pendingToolIDs[b.ID] = true
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
	lastEndingIndex *int, hasAny, hasAfter *bool, shutdownIDs, pendingToolIDs map[string]bool) {

	// Check for user-rejected tool use at the entry level.
	isRejection := isToolUseRejection(e.ToolResult)

	// String-content user entries (e.g. "[Request interrupted by user...]") fail
	// array unmarshal. Check them before attempting block parsing.
	var text string
	if err := json.Unmarshal(e.Message.Content, &text); err == nil {
		if strings.HasPrefix(text, "[Request interrupted by user") {
			// Interruption clears all pending tool calls — the process was killed.
			for id := range pendingToolIDs {
				delete(pendingToolIDs, id)
			}
			*lastEndingIndex = *activityIndex
			*hasAfter = false
			*activityIndex++
		}
		return
	}

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
			delete(pendingToolIDs, b.ToolUseID)
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
				// Interruption clears all pending tool calls.
				for id := range pendingToolIDs {
					delete(pendingToolIDs, id)
				}
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
