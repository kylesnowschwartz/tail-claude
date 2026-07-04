package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SubagentProcess holds a parsed subagent and its computed metadata.
// Discovery fills ID, FilePath, Chunks, timing, usage, and Model.
// Linking (LinkSubagents) fills Description, SubagentType, and ParentTaskID.
type SubagentProcess struct {
	ID            string    // agentId from filename (agent-{id}.jsonl)
	FilePath      string    // full path to subagent JSONL file
	FileModTime   time.Time // last modification time of the JSONL file
	Chunks        []Chunk   // parsed via ReadSession pipeline
	StartTime     time.Time // first message timestamp
	EndTime       time.Time // last message timestamp
	DurationMs    int64
	Usage         Usage  // last AI chunk's context-window snapshot (not a sum)
	Model         string // model from first AI chunk (e.g. "claude-opus-4-6")
	Description   string
	SubagentType  string
	ParentTaskID  string // tool_use_id of spawning Task call
	TeamSummary   string // summary attr from first <teammate-message> (team agents only)
	TeammateColor string // color attr from first <teammate-message> (team agents only)
}

// isAgentSessionFile reports whether a .jsonl filename is a subagent file
// rather than a top-level session. Claude Code names subagent files
// agent-{agentId}.jsonl (dash, see DiscoverSubagents); the underscore prefix
// is kept from the picker's original filter as a defensive match, since the
// encoding has varied across Claude Code versions. Shared by session
// discovery and DiscoverTeamSessions so both skip the same files.
func isAgentSessionFile(name string) bool {
	return strings.HasPrefix(name, "agent-") || strings.HasPrefix(name, "agent_")
}

// DiscoverSubagents finds and parses subagent files for a session.
//
// Takes the full path to a session JSONL file (e.g.
// ~/.claude/projects/{projectId}/{sessionUUID}.jsonl) and derives the
// subagents directory: {sessionDir}/{sessionUUID}/subagents/
//
// Filters out:
//   - Empty files
//   - Warmup agents (first user message content is exactly "Warmup")
//   - Compact agents (agentId starts with "acompact")
//
// Returns parsed SubagentProcesses sorted by StartTime.
func DiscoverSubagents(sessionPath string) ([]SubagentProcess, error) {
	projectDir := filepath.Dir(sessionPath)
	base := strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl")
	subagentsDir := filepath.Join(projectDir, base, "subagents")

	entries, err := os.ReadDir(subagentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var procs []SubagentProcess

	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		agentID := strings.TrimPrefix(name, "agent-")
		agentID = strings.TrimSuffix(agentID, ".jsonl")

		// Filter compact agents (context compaction artifacts).
		if strings.HasPrefix(agentID, "acompact") {
			continue
		}

		filePath := filepath.Join(subagentsDir, name)

		// Filter empty files.
		info, err := de.Info()
		if err != nil || info.Size() == 0 {
			continue
		}

		// Filter warmup agents by checking first user message content.
		if isWarmupAgent(filePath) {
			continue
		}

		// Parse through the pipeline with sidechain filtering disabled.
		// Subagent entries all have isSidechain=true (they run in the
		// parent's sidechain context), but within the subagent file
		// they're the main conversation.
		chunks, teamSummary, teamColor, err := readSubagentSession(filePath, projectDir)
		if err != nil || len(chunks) == 0 {
			continue
		}

		startTime, endTime, durationMs := chunkTiming(chunks)
		usage := lastUsageSnapshot(chunks)

		proc := SubagentProcess{
			ID:            agentID,
			FilePath:      filePath,
			FileModTime:   info.ModTime(),
			Chunks:        chunks,
			StartTime:     startTime,
			EndTime:       endTime,
			DurationMs:    durationMs,
			Usage:         usage,
			Model:         extractModel(chunks),
			TeamSummary:   teamSummary,
			TeammateColor: teamColor,
		}

		// Claude Code 2.1.19x writes an agent-{id}.meta.json sidecar with the
		// agent type, description, and spawning tool_use_id. The toolUseId is
		// an exact parent link — it exists from spawn time, so async agents
		// link before their toolUseResult is written.
		applySidecarMeta(&proc, strings.TrimSuffix(filePath, ".jsonl")+".meta.json")

		procs = append(procs, proc)
	}

	sort.Slice(procs, func(i, j int) bool {
		return procs[i].StartTime.Before(procs[j].StartTime)
	})

	return procs, nil
}

// applySidecarMeta fills a SubagentProcess from its agent-{id}.meta.json
// sidecar ({agentType, description, toolUseId, spawnDepth, isFork}). Missing
// or malformed sidecars are a no-op — LinkSubagents' scan phases still apply.
func applySidecarMeta(proc *SubagentProcess, metaPath string) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return
	}
	var meta struct {
		AgentType   string `json:"agentType"`
		Description string `json:"description"`
		ToolUseID   string `json:"toolUseId"`
	}
	if json.Unmarshal(data, &meta) != nil {
		return
	}
	if meta.AgentType != "" {
		proc.SubagentType = meta.AgentType
	}
	if meta.Description != "" {
		proc.Description = meta.Description
	}
	if meta.ToolUseID != "" {
		proc.ParentTaskID = meta.ToolUseID
	}
}

// isWarmupAgent reads just enough of a subagent file to check if the first
// user message content is exactly "Warmup". Matches claude-devtools behavior:
// the first entry with type=user and string content "Warmup" marks a warmup agent.
func isWarmupAgent(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	// Read just enough to find the first user entry. Subagent files are
	// small-ish and the first entry is almost always the user message,
	// so scanning a few lines is fine.
	lr := newLineReader(f)
	for {
		line, ok := lr.next()
		if !ok {
			break
		}

		var partial struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &partial); err != nil {
			continue
		}
		if partial.Type != "user" {
			continue
		}

		// Extract message.content -- could be a JSON string or array.
		var msg struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(partial.Message, &msg); err != nil {
			return false
		}

		// Only string content "Warmup" counts.
		var content string
		if err := json.Unmarshal(msg.Content, &content); err != nil {
			return false
		}
		return content == "Warmup"
	}
	return false
}

// chunkTiming computes start/end timestamps and duration from a chunk slice.
func chunkTiming(chunks []Chunk) (start, end time.Time, durationMs int64) {
	for _, c := range chunks {
		if c.Timestamp.IsZero() {
			continue
		}
		if start.IsZero() || c.Timestamp.Before(start) {
			start = c.Timestamp
		}
		// A chunk's Timestamp is its FIRST message; DurationMs spans to its
		// last. Using Timestamp alone as the end would drop the entire final
		// AI turn and report ~time-to-first-token instead of real runtime.
		chunkEnd := c.Timestamp.Add(time.Duration(c.DurationMs) * time.Millisecond)
		if end.IsZero() || chunkEnd.After(end) {
			end = chunkEnd
		}
	}
	if !start.IsZero() && !end.IsZero() {
		durationMs = end.Sub(start).Milliseconds()
	}
	return
}

// readSubagentSession reads a subagent JSONL file and returns chunks plus
// team metadata (summary and color). Both are extracted from the raw entry
// content before Classify strips the XML tag attributes.
//
// trustedRoot bounds persisted-output resolution: subagents share the parent
// session's tool-results dir, so callers pass the project directory (the dir
// containing the parent session's .jsonl), which contains both
// {session}/subagents/ and {session}/tool-results/.
//
// Unlike ReadSession, it ignores the isSidechain flag since all entries
// in subagent files are marked isSidechain=true but represent the
// subagent's own main conversation.
func readSubagentSession(path, trustedRoot string) ([]Chunk, string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", "", err
	}
	defer f.Close()

	lr := newLineReader(f)

	var msgs []ClassifiedMsg
	var teamSummary, teamColor string
	extractedTeamMeta := false
	for {
		line, ok := lr.next()
		if !ok {
			break
		}
		entry, ok := ParseEntry([]byte(line))
		if !ok {
			continue
		}

		// Extract team summary and color from the first user entry's
		// <teammate-message> tag before Classify strips the XML attributes.
		if !extractedTeamMeta && entry.Type == "user" {
			var contentStr string
			if json.Unmarshal(entry.Message.Content, &contentStr) == nil {
				if m := teammateSummaryRe.FindStringSubmatch(contentStr); len(m) > 1 {
					teamSummary = m[1]
				}
				if m := teammateColorRe.FindStringSubmatch(contentStr); len(m) > 1 {
					teamColor = m[1]
				}
				extractedTeamMeta = true
			}
		}

		// Clear sidechain flag so Classify doesn't filter these out.
		entry.IsSidechain = false
		msg, ok := Classify(entry)
		if !ok {
			continue
		}
		msgs = append(msgs, msg)
	}
	if err := lr.Err(); err != nil {
		return nil, "", "", err
	}

	resolvePersistedOutputs(msgs, trustedRoot)

	return BuildChunks(msgs), teamSummary, teamColor, nil
}

// extractModel returns the model string from the first AI chunk, or "".
func extractModel(chunks []Chunk) string {
	for _, c := range chunks {
		if c.Type == AIChunk && c.Model != "" {
			return c.Model
		}
	}
	return ""
}

// lastUsageSnapshot returns the last AI chunk's usage snapshot. Each chunk
// already holds the last assistant message's context-window snapshot, so the
// final chunk's snapshot represents the subagent's context state at completion.
func lastUsageSnapshot(chunks []Chunk) Usage {
	for i := len(chunks) - 1; i >= 0; i-- {
		if chunks[i].Type == AIChunk && chunks[i].Usage.TotalTokens() > 0 {
			return chunks[i].Usage
		}
	}
	return Usage{}
}

// LinkSubagents connects discovered subagent processes to their parent Task
// tool calls in the parent session. Mutates processes in place.
//
// Returns toolIDToColor: a map from tool_use_id to team color name, extracted
// from toolUseResult entries in the parent session. Callers use this as a
// fallback color source for Task items that have no linked SubagentProcess
// (e.g. team agents whose JSONL lives outside the subagents/ directory).
//
// Matching strategy (ported from claude-devtools SubagentResolver):
//  1. Result-based: scan parent session entries for toolUseResult containing
//     agentId. Map agentId -> sourceToolUseID -> Task tool call.
//  2. Team member: match the <teammate-message summary="..."> attribute from
//     the subagent's first user message to the Task call's description.
//     Only applies to Task calls with both team_name and name in input.
//  3. Positional fallback: remaining unmatched non-team processes are paired
//     with remaining unmatched non-team Task calls by time order (no wrap-around).
//
// Also populates Description and SubagentType from the parent Task call.
func LinkSubagents(processes []SubagentProcess, parentChunks []Chunk, parentSessionPath string) map[string]string {
	// Always scan for colors, even without processes — team agents don't
	// create subagent files but their toolUseResult entries carry color data.
	links := scanAgentLinks(parentSessionPath)

	if len(processes) == 0 {
		return links.toolIDToColor
	}

	// Collect all Task tool DisplayItems from parent chunks.
	var taskItems []*DisplayItem
	for i := range parentChunks {
		c := &parentChunks[i]
		if c.Type != AIChunk {
			continue
		}
		for j := range c.Items {
			it := &c.Items[j]
			if it.Type != ItemSubagent {
				continue
			}
			taskItems = append(taskItems, it)
		}
	}

	if len(taskItems) == 0 {
		return links.toolIDToColor
	}

	// Build tool_use_id -> DisplayItem for enrichment.
	toolIDToTask := make(map[string]*DisplayItem, len(taskItems))
	for _, it := range taskItems {
		toolIDToTask[it.ToolID] = it
	}

	matchedProcs := make(map[string]bool)
	matchedTools := make(map[string]bool)

	// Phase 0: sidecar meta.json links. Discovery pre-fills ParentTaskID from
	// the agent's meta.json toolUseId; honor it so later phases don't re-pair
	// these processes. Fill description/type from the Task item only where the
	// sidecar left them empty.
	for i := range processes {
		p := &processes[i]
		if p.ParentTaskID == "" || matchedTools[p.ParentTaskID] {
			continue
		}
		it, ok := toolIDToTask[p.ParentTaskID]
		if !ok {
			continue
		}
		if p.Description == "" {
			p.Description = it.SubagentDesc
		}
		if p.SubagentType == "" {
			p.SubagentType = it.SubagentType
		}
		matchedProcs[p.ID] = true
		matchedTools[p.ParentTaskID] = true
	}

	// Phase 1: Result-based matching via structured toolUseResult.agentId.
	for i := range processes {
		if matchedProcs[processes[i].ID] {
			continue
		}
		toolID, ok := links.agentToToolID[processes[i].ID]
		if !ok || matchedTools[toolID] {
			continue
		}
		it, ok := toolIDToTask[toolID]
		if !ok {
			continue
		}
		enrichProcess(&processes[i], it)
		matchedProcs[processes[i].ID] = true
		matchedTools[toolID] = true
	}

	// Phase 2: Team member matching by description -> teammate-message summary.
	// Team Task calls have both team_name and name in input. Their agent_id
	// is "name@team_name" (not a file UUID), so Phase 1 can't match them.
	// Match by comparing the Task call's description to the summary attribute
	// in the subagent's first <teammate-message> tag.
	teamTaskItems := filterTeamTasks(taskItems, matchedTools)
	if len(teamTaskItems) > 0 {
		for _, it := range teamTaskItems {
			var best *SubagentProcess
			for i := range processes {
				if matchedProcs[processes[i].ID] {
					continue
				}
				if processes[i].TeamSummary == "" || processes[i].TeamSummary != it.SubagentDesc {
					continue
				}
				if best == nil || processes[i].StartTime.Before(best.StartTime) {
					best = &processes[i]
				}
			}
			if best != nil {
				enrichProcess(best, it)
				matchedProcs[best.ID] = true
				matchedTools[it.ToolID] = true
			}
		}
	}

	// Phase 3: Positional fallback for non-team tasks (no wrap-around).
	// Explicitly excludes team Task calls AND team processes — pairing an
	// unlinked team worker (e.g. its toolUseResult hasn't arrived yet during
	// live tailing) with a regular Task call would steal that call's metadata
	// and displace the real subagent. Team processes carry "@" in their ID
	// (DiscoverTeamSessions) or a TeamSummary (subagents/ team files).
	var unmatchedProcs []*SubagentProcess
	for i := range processes {
		if matchedProcs[processes[i].ID] {
			continue
		}
		// A process with a sidecar-provided ParentTaskID stays out of the
		// positional fallback even when its Task item wasn't found in the
		// parent chunks — re-pairing it positionally would be a mislink.
		if processes[i].ParentTaskID != "" {
			continue
		}
		if strings.Contains(processes[i].ID, "@") || processes[i].TeamSummary != "" {
			continue
		}
		unmatchedProcs = append(unmatchedProcs, &processes[i])
	}
	var unmatchedTasks []*DisplayItem
	for _, it := range taskItems {
		if !matchedTools[it.ToolID] && !IsTeamTask(it) {
			unmatchedTasks = append(unmatchedTasks, it)
		}
	}

	for i := 0; i < len(unmatchedProcs) && i < len(unmatchedTasks); i++ {
		enrichProcess(unmatchedProcs[i], unmatchedTasks[i])
	}

	// Populate TeamColor from toolUseResult data for any linked process
	// that doesn't already have a color. Team agents' own JSONL files
	// don't carry their color (the first entry is from team-lead), but
	// the teammate_spawned toolUseResult in the parent session does.
	for i := range processes {
		if processes[i].TeammateColor == "" && processes[i].ParentTaskID != "" {
			if color, ok := links.toolIDToColor[processes[i].ParentTaskID]; ok {
				processes[i].TeammateColor = color
			}
		}
	}

	// Remap IDs for team workers discovered via DiscoverSubagents (hex UUID)
	// to the "name@team" format that ReconstructTeams expects. Without this,
	// team workers in subagents/ are invisible to the team task board —
	// phases 2-4 of ReconstructTeams filter on splitWorkerID which requires
	// the "@" separator.
	for i := range processes {
		if processes[i].ParentTaskID == "" {
			continue
		}
		it, found := toolIDToTask[processes[i].ParentTaskID]
		if !found {
			continue
		}
		teamName, agentName, ok := teamSpecFromInput(it.ToolInput)
		if ok && teamName != "" && agentName != "" {
			processes[i].ID = agentName + "@" + teamName
		}
	}

	return links.toolIDToColor
}

// filterTeamTasks returns unmatched Task items whose input contains both
// team_name and name keys, identifying them as team member spawns.
func filterTeamTasks(items []*DisplayItem, matched map[string]bool) []*DisplayItem {
	var out []*DisplayItem
	for _, it := range items {
		if matched[it.ToolID] {
			continue
		}
		if IsTeamTask(it) {
			out = append(out, it)
		}
	}
	return out
}

// IsTeamTask checks whether a Task DisplayItem's input contains both
// team_name and name keys, marking it as a team member spawn.
func IsTeamTask(it *DisplayItem) bool {
	_, _, ok := teamSpecFromInput(it.ToolInput)
	return ok
}

// teamSpecFromInput extracts team_name and name from a Task tool input.
// Single home for the "is this a team spawn?" definition. ok reports key
// PRESENCE of both fields (matching IsTeamTask's historical semantics), not
// value validity — malformed values yield "" with ok=true, so callers that
// need usable names must still check the strings are non-empty.
func teamSpecFromInput(input json.RawMessage) (teamName, agentName string, ok bool) {
	fields := parseInputFields(input)
	if fields == nil {
		return "", "", false
	}
	_, hasTeamName := fields["team_name"]
	_, hasName := fields["name"]
	if !hasTeamName || !hasName {
		return "", "", false
	}
	return getString(fields, "team_name"), getString(fields, "name"), true
}

// agentLinkData holds the results of scanning a parent session for agent links.
type agentLinkData struct {
	agentToToolID map[string]string // agentId -> tool_use_id
	toolIDToColor map[string]string // tool_use_id -> team color name
}

// scanAgentLinks reads a parent session JSONL file and builds maps from
// agentId -> toolUseID (for Phase 1 linking) and toolUseID -> color
// (for populating TeamColor after any linking phase).
//
// Matching strategy (ported from claude-devtools SubagentResolver):
//
//	toolUseResult.agentId (or agent_id) -> sourceToolUseID
//
// Fallback when sourceToolUseID is missing: extract the first tool_result
// block's tool_use_id from the message content (matches devtools:
// msg.sourceToolUseID ?? msg.toolResults[0]?.toolUseId).
//
// Color extraction: teammate_spawned toolUseResult entries carry a color
// field. The tool_use_id links back to the spawning Task call.
func scanAgentLinks(sessionPath string) agentLinkData {
	data := agentLinkData{
		agentToToolID: make(map[string]string),
		toolIDToColor: make(map[string]string),
	}

	f, err := os.Open(sessionPath)
	if err != nil {
		return data
	}
	defer f.Close()

	lr := newLineReader(f)

	for {
		line, ok := lr.next()
		if !ok {
			break
		}
		entry, ok := ParseEntry([]byte(line))
		if !ok {
			continue
		}
		resultMap := entry.ToolUseResultMap()
		if resultMap == nil {
			continue
		}

		// Check both camelCase and snake_case field names, matching
		// claude-devtools: result.agentId ?? result.agent_id
		agentID := getString(resultMap, "agentId")
		if agentID == "" {
			agentID = getString(resultMap, "agent_id")
		}
		if agentID == "" {
			continue
		}

		// Primary: top-level sourceToolUseID field.
		toolUseID := entry.SourceToolUseID

		// Fallback: first tool_result block's tool_use_id from message content.
		// Many entries lack sourceToolUseID but the link is in the content.
		if toolUseID == "" {
			toolUseID = extractFirstToolResultID(entry)
		}
		if toolUseID == "" {
			continue
		}

		data.agentToToolID[agentID] = toolUseID

		// Extract team color from teammate_spawned results.
		if color := getString(resultMap, "color"); color != "" {
			data.toolIDToColor[toolUseID] = color
		}
	}

	return data
}

// extractFirstToolResultID returns the tool_use_id from the first tool_result
// content block in the entry's message, or "" if none found.
func extractFirstToolResultID(entry Entry) string {
	var blocks []struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
	}
	if err := json.Unmarshal(entry.Message.Content, &blocks); err != nil {
		return "" // content is a string, not an array — skip
	}
	for _, b := range blocks {
		if b.Type == "tool_result" && b.ToolUseID != "" {
			return b.ToolUseID
		}
	}
	return ""
}

// enrichProcess fills a SubagentProcess with metadata from its parent Task call.
func enrichProcess(proc *SubagentProcess, item *DisplayItem) {
	proc.ParentTaskID = item.ToolID
	proc.Description = item.SubagentDesc
	proc.SubagentType = item.SubagentType
}

// ReadTeamSessionMeta scans the head of a JSONL file for the teamName and
// agentName top-level fields. Returns ("", "") for non-team sessions or on
// any error. Cheap: no full parse.
//
// Team fields live on the first conversation entry, but Claude Code 2.1.19x
// re-appends uuid-less session-metadata records (last-prompt, mode,
// custom-title, ...) that commonly lead the file — so scan past them instead
// of trusting line 1. The scan stops at the first conversation entry (it
// either carries the team fields or the file isn't a team session) or after
// teamMetaScanCap lines.
func ReadTeamSessionMeta(path string) (teamName, agentName string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	const teamMetaScanCap = 25

	lr := newLineReader(f)
	for i := 0; i < teamMetaScanCap; i++ {
		line, ok := lr.next()
		if !ok {
			return "", ""
		}

		var meta struct {
			Type      string `json:"type"`
			UUID      string `json:"uuid"`
			TeamName  string `json:"teamName"`
			AgentName string `json:"agentName"`
		}
		if err := json.Unmarshal([]byte(line), &meta); err != nil {
			continue
		}
		// Both fields must be present: a lone agentName also appears on
		// uuid-less type=agent-name metadata records, which say nothing
		// about team membership.
		if meta.TeamName != "" && meta.AgentName != "" {
			return meta.TeamName, meta.AgentName
		}
		// Stop at the first conversation entry — team fields live on it or
		// not at all. Other uuid-bearing types (attachment, system) can
		// precede it, so the type check matters.
		if meta.UUID != "" && (meta.Type == "user" || meta.Type == "assistant") {
			return "", ""
		}
	}
	return "", ""
}

// teamSpec identifies a team agent spawn from the parent session.
type teamSpec struct {
	teamName  string
	agentName string
}

// extractTeamSpecs collects {teamName, agentName} pairs from Task items
// in the parent chunks where IsTeamTask returns true.
func extractTeamSpecs(chunks []Chunk) []teamSpec {
	var specs []teamSpec
	for i := range chunks {
		if chunks[i].Type != AIChunk {
			continue
		}
		for j := range chunks[i].Items {
			it := &chunks[i].Items[j]
			if it.Type != ItemSubagent {
				continue
			}
			tn, an, ok := teamSpecFromInput(it.ToolInput)
			if ok && tn != "" && an != "" {
				specs = append(specs, teamSpec{teamName: tn, agentName: an})
			}
		}
	}
	return specs
}

// DiscoverTeamSessions finds team agent session files that live as top-level
// .jsonl files in the project directory (not in subagents/). These are created
// when Task is called with team_name + name parameters.
//
// Discovery: scan the project directory for .jsonl files whose first entry has
// teamName + agentName matching a team Task call in the parent chunks.
// Candidates whose entry timestamps fall outside the parent's run (with a
// small grace window) are rejected, and duplicate IDs keep only the latest
// session — team/agent names are reused across runs, so name matching alone
// would link files from a different parent session.
// Each match is parsed via readSubagentSession and returned with
// ID = "agentName@teamName" so Phase 1 of LinkSubagents can match it
// against the parent's toolUseResult agent_id field.
func DiscoverTeamSessions(sessionPath string, parentChunks []Chunk) ([]SubagentProcess, error) {
	specs := extractTeamSpecs(parentChunks)
	if len(specs) == 0 {
		return nil, nil
	}

	// Build a lookup set for quick matching.
	type specKey struct{ team, agent string }
	wanted := make(map[specKey]bool, len(specs))
	for _, s := range specs {
		wanted[specKey{s.teamName, s.agentName}] = true
	}

	// Team/agent names are commonly reused across runs ("planner@analysis"
	// yesterday and today), so name matching alone can pick up files from a
	// different parent session. The parent's own time range disambiguates.
	parentStart, parentEnd, _ := chunkTiming(parentChunks)

	projectDir := filepath.Dir(sessionPath)
	parentBase := filepath.Base(sessionPath)

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}

	var procs []SubagentProcess
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		// Skip the parent session itself.
		if name == parentBase {
			continue
		}
		// Skip agent-prefixed files (handled by DiscoverSubagents).
		if isAgentSessionFile(name) {
			continue
		}

		filePath := filepath.Join(projectDir, name)

		// Skip empty files.
		info, err := de.Info()
		if err != nil || info.Size() == 0 {
			continue
		}

		teamName, agentName := ReadTeamSessionMeta(filePath)
		if teamName == "" || agentName == "" {
			continue
		}
		if !wanted[specKey{teamName, agentName}] {
			continue
		}

		chunks, _, teamColor, err := readSubagentSession(filePath, projectDir)
		if err != nil || len(chunks) == 0 {
			continue
		}

		startTime, endTime, durationMs := chunkTiming(chunks)
		usage := lastUsageSnapshot(chunks)

		// Reject sessions from a different run: bounding BOTH ends means a
		// stale file from an earlier run and a reused name from a future run
		// both fail, regardless of which parent session is being viewed.
		if !withinParentRun(startTime, endTime, parentStart, parentEnd) {
			continue
		}

		procs = append(procs, SubagentProcess{
			ID:            agentName + "@" + teamName,
			FilePath:      filePath,
			FileModTime:   info.ModTime(),
			Chunks:        chunks,
			StartTime:     startTime,
			EndTime:       endTime,
			DurationMs:    durationMs,
			Usage:         usage,
			Model:         extractModel(chunks),
			TeammateColor: teamColor,
		})
	}

	// An ID can still collide when the same teammate is respawned within one
	// run. scanAgentLinks keeps only the last spawn's tool_use_id, so keep
	// the latest session per ID to match what Phase 1 will link against.
	latest := make(map[string]int, len(procs))
	for i := range procs {
		if j, ok := latest[procs[i].ID]; !ok || procs[i].StartTime.After(procs[j].StartTime) {
			latest[procs[i].ID] = i
		}
	}
	if len(latest) < len(procs) {
		deduped := make([]SubagentProcess, 0, len(latest))
		for i := range procs {
			if latest[procs[i].ID] == i {
				deduped = append(deduped, procs[i])
			}
		}
		procs = deduped
	}

	sort.Slice(procs, func(i, j int) bool {
		return procs[i].StartTime.Before(procs[j].StartTime)
	})

	return procs, nil
}

// teamSpawnGrace pads the parent's time range when validating candidate team
// session files. A teammate's first entry lands moments after the spawning
// Task call, which can be the parent's very last entry — without the pad a
// legitimate session starting seconds after the parent's final timestamp
// would be rejected.
const teamSpawnGrace = 2 * time.Minute

// withinParentRun reports whether a candidate team session's [start, end]
// overlaps the parent run's [parentStart, parentEnd] padded by teamSpawnGrace
// on both sides. Zero timestamps on either side make the comparison
// meaningless, so those pass — better to over-link than to hide a session
// with missing timestamps.
func withinParentRun(start, end, parentStart, parentEnd time.Time) bool {
	if start.IsZero() || end.IsZero() || parentStart.IsZero() || parentEnd.IsZero() {
		return true
	}
	return !start.After(parentEnd.Add(teamSpawnGrace)) && !end.Before(parentStart.Add(-teamSpawnGrace))
}
