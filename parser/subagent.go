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

// SubagentProcess holds a parsed subagent and its computed metadata.
// Discovery fills ID, FilePath, Chunks, timing, and usage.
// Linking (Phase 5B) fills Description, SubagentType, and ParentTaskID.
type SubagentProcess struct {
	ID           string    // agentId from filename (agent-{id}.jsonl)
	FilePath     string    // full path to subagent JSONL file
	Chunks       []Chunk   // parsed via ReadSession pipeline
	StartTime    time.Time // first message timestamp
	EndTime      time.Time // last message timestamp
	DurationMs   int64
	Usage        Usage // aggregated from all AI chunks
	Description  string
	SubagentType string
	ParentTaskID string // tool_use_id of spawning Task call
	AgentName    string // team member name, empty for regular subagents
	TeamName     string // team name, empty for regular subagents
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
	dir := filepath.Dir(sessionPath)
	base := strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl")
	subagentsDir := filepath.Join(dir, base, "subagents")

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
		chunks, err := readSubagentSession(filePath)
		if err != nil || len(chunks) == 0 {
			continue
		}

		startTime, endTime, durationMs := chunkTiming(chunks)
		usage := aggregateUsage(chunks)

		procs = append(procs, SubagentProcess{
			ID:         agentID,
			FilePath:   filePath,
			Chunks:     chunks,
			StartTime:  startTime,
			EndTime:    endTime,
			DurationMs: durationMs,
			Usage:      usage,
		})
	}

	sort.Slice(procs, func(i, j int) bool {
		return procs[i].StartTime.Before(procs[j].StartTime)
	})

	return procs, nil
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
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var partial struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &partial); err != nil {
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
		if end.IsZero() || c.Timestamp.After(end) {
			end = c.Timestamp
		}
	}
	if !start.IsZero() && !end.IsZero() {
		durationMs = end.Sub(start).Milliseconds()
	}
	return
}

// readSubagentSession reads a subagent JSONL file and returns chunks.
// Unlike ReadSession, it ignores the isSidechain flag since all entries
// in subagent files are marked isSidechain=true but represent the
// subagent's own main conversation.
func readSubagentSession(path string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var msgs []ClassifiedMsg
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		entry, ok := ParseEntry(line)
		if !ok {
			continue
		}
		// Clear sidechain flag so Classify doesn't filter these out.
		entry.IsSidechain = false
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

// aggregateUsage sums token usage across all AI chunks.
func aggregateUsage(chunks []Chunk) Usage {
	var u Usage
	for _, c := range chunks {
		if c.Type != AIChunk {
			continue
		}
		u.InputTokens += c.Usage.InputTokens
		u.OutputTokens += c.Usage.OutputTokens
		u.CacheReadTokens += c.Usage.CacheReadTokens
		u.CacheCreationTokens += c.Usage.CacheCreationTokens
	}
	return u
}

// LinkSubagents connects discovered subagent processes to their parent Task
// tool calls in the parent session. Mutates processes in place.
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
func LinkSubagents(processes []SubagentProcess, parentChunks []Chunk, parentSessionPath string) {
	if len(processes) == 0 {
		return
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
		return
	}

	// Build agentId -> sourceToolUseID map from structured Entry fields.
	agentToToolID := scanAgentLinks(parentSessionPath)

	// Build tool_use_id -> DisplayItem for enrichment.
	toolIDToTask := make(map[string]*DisplayItem, len(taskItems))
	for _, it := range taskItems {
		toolIDToTask[it.ToolID] = it
	}

	matchedProcs := make(map[string]bool)
	matchedTools := make(map[string]bool)

	// Phase 1: Result-based matching via structured toolUseResult.agentId.
	for i := range processes {
		toolID, ok := agentToToolID[processes[i].ID]
		if !ok {
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

	// Phase 2: Team member matching by agentName + teamName.
	// Team Task calls have both team_name and name in input. Their agent_id
	// is "name@team_name" (not a file UUID), so Phase 1 can't match them.
	// Match by comparing the Task call's name/team_name fields to the
	// AgentName/TeamName extracted from the subagent's session file.
	teamTaskItems := filterTeamTasks(taskItems, matchedTools)
	for _, it := range teamTaskItems {
		name, team := extractTeamTaskFields(it)
		if name == "" || team == "" {
			continue
		}
		var best *SubagentProcess
		for i := range processes {
			if matchedProcs[processes[i].ID] {
				continue
			}
			if processes[i].AgentName != name || processes[i].TeamName != team {
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

	// Phase 3: Positional fallback for non-team tasks (no wrap-around).
	// Explicitly excludes team Task calls — they either matched in Phase 2
	// or remain unmatched.
	var unmatchedProcs []*SubagentProcess
	for i := range processes {
		if !matchedProcs[processes[i].ID] {
			unmatchedProcs = append(unmatchedProcs, &processes[i])
		}
	}
	var unmatchedTasks []*DisplayItem
	for _, it := range taskItems {
		if !matchedTools[it.ToolID] && !isTeamTask(it) {
			unmatchedTasks = append(unmatchedTasks, it)
		}
	}

	for i := 0; i < len(unmatchedProcs) && i < len(unmatchedTasks); i++ {
		enrichProcess(unmatchedProcs[i], unmatchedTasks[i])
	}
}

// filterTeamTasks returns unmatched Task items whose input contains both
// team_name and name keys, identifying them as team member spawns.
func filterTeamTasks(items []*DisplayItem, matched map[string]bool) []*DisplayItem {
	var out []*DisplayItem
	for _, it := range items {
		if matched[it.ToolID] {
			continue
		}
		if isTeamTask(it) {
			out = append(out, it)
		}
	}
	return out
}

// isTeamTask checks whether a Task DisplayItem's input contains both
// team_name and name keys, marking it as a team member spawn.
func isTeamTask(it *DisplayItem) bool {
	if len(it.ToolInput) == 0 {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(it.ToolInput, &fields); err != nil {
		return false
	}
	_, hasTeamName := fields["team_name"]
	_, hasName := fields["name"]
	return hasTeamName && hasName
}

// extractTeamTaskFields parses a team Task item's ToolInput for the
// name and team_name fields used to identify the team member.
func extractTeamTaskFields(it *DisplayItem) (name, teamName string) {
	if len(it.ToolInput) == 0 {
		return "", ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(it.ToolInput, &fields); err != nil {
		return "", ""
	}
	var n, t string
	if raw, ok := fields["name"]; ok {
		json.Unmarshal(raw, &n)
	}
	if raw, ok := fields["team_name"]; ok {
		json.Unmarshal(raw, &t)
	}
	return n, t
}

// scanAgentLinks reads a parent session JSONL file and builds a map from
// agentId -> toolUseID by extracting structured toolUseResult data.
//
// Matching strategy (ported from claude-devtools SubagentResolver):
//
//	toolUseResult.agentId (or agent_id) -> sourceToolUseID
//
// Fallback when sourceToolUseID is missing: extract the first tool_result
// block's tool_use_id from the message content (matches devtools:
// msg.sourceToolUseID ?? msg.toolResults[0]?.toolUseId).
func scanAgentLinks(sessionPath string) map[string]string {
	result := make(map[string]string)

	f, err := os.Open(sessionPath)
	if err != nil {
		return result
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
		if len(entry.ToolUseResult) == 0 {
			continue
		}

		// Check both camelCase and snake_case field names, matching
		// claude-devtools: result.agentId ?? result.agent_id
		agentID := extractStringField(entry.ToolUseResult, "agentId")
		if agentID == "" {
			agentID = extractStringField(entry.ToolUseResult, "agent_id")
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

		result[agentID] = toolUseID
	}

	return result
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

// extractStringField extracts a string value from a map of json.RawMessage.
func extractStringField(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// enrichProcess fills a SubagentProcess with metadata from its parent Task call.
func enrichProcess(proc *SubagentProcess, item *DisplayItem) {
	proc.ParentTaskID = item.ToolID
	proc.Description = item.SubagentDesc
	proc.SubagentType = item.SubagentType
}

// teamMember identifies an expected team member from the parent's Task calls.
type teamMember struct {
	Name     string
	TeamName string
}

// DiscoverTeamSessions finds and parses team member session files.
//
// Team-spawned agents (via TeamCreate + Task with team_name/name) create
// independent session files at the project root ({projectDir}/{uuid}.jsonl),
// not under {session}/subagents/. Their entries carry agentName and teamName
// fields that identify which team member they belong to.
//
// Returns parsed SubagentProcesses with AgentName and TeamName set,
// sorted by StartTime.
func DiscoverTeamSessions(parentSessionPath string, parentChunks []Chunk) ([]SubagentProcess, error) {
	expected := scanTeamInfo(parentChunks)
	if len(expected) == 0 {
		return nil, nil
	}

	dir := filepath.Dir(parentSessionPath)
	parentBase := filepath.Base(parentSessionPath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var procs []SubagentProcess
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		// Skip non-JSONL files.
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		// Skip the parent session itself.
		if name == parentBase {
			continue
		}
		// Skip regular subagent files (agent-* prefix).
		if strings.HasPrefix(name, "agent-") {
			continue
		}

		filePath := filepath.Join(dir, name)

		// Skip empty files.
		info, err := de.Info()
		if err != nil || info.Size() == 0 {
			continue
		}

		agentName, teamName, ok := matchTeamSession(filePath, expected)
		if !ok {
			continue
		}

		chunks, err := readSubagentSession(filePath)
		if err != nil || len(chunks) == 0 {
			continue
		}

		startTime, endTime, durationMs := chunkTiming(chunks)
		usage := aggregateUsage(chunks)

		// Use the filename (minus .jsonl) as the ID for team sessions.
		id := strings.TrimSuffix(name, ".jsonl")

		procs = append(procs, SubagentProcess{
			ID:         id,
			FilePath:   filePath,
			Chunks:     chunks,
			StartTime:  startTime,
			EndTime:    endTime,
			DurationMs: durationMs,
			Usage:      usage,
			AgentName:  agentName,
			TeamName:   teamName,
		})
	}

	sort.Slice(procs, func(i, j int) bool {
		return procs[i].StartTime.Before(procs[j].StartTime)
	})

	return procs, nil
}

// scanTeamInfo collects expected team member identities from parent chunks.
// Looks for ItemSubagent items with both team_name and name in ToolInput.
func scanTeamInfo(chunks []Chunk) []teamMember {
	var members []teamMember
	seen := make(map[teamMember]bool)

	for i := range chunks {
		c := &chunks[i]
		if c.Type != AIChunk {
			continue
		}
		for j := range c.Items {
			it := &c.Items[j]
			if it.Type != ItemSubagent || !isTeamTask(it) {
				continue
			}
			name, team := extractTeamTaskFields(it)
			if name == "" || team == "" {
				continue
			}
			m := teamMember{Name: name, TeamName: team}
			if !seen[m] {
				seen[m] = true
				members = append(members, m)
			}
		}
	}
	return members
}

// matchTeamSession reads the first JSONL line of a candidate file and checks
// whether its agentName and teamName fields match any expected team member.
// Returns the matched name/team and true, or empty strings and false.
func matchTeamSession(path string, expected []teamMember) (agentName, teamName string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	if !scanner.Scan() {
		return "", "", false
	}
	line := scanner.Bytes()
	if len(line) == 0 {
		return "", "", false
	}

	var partial struct {
		AgentName string `json:"agentName"`
		TeamName  string `json:"teamName"`
	}
	if err := json.Unmarshal(line, &partial); err != nil {
		return "", "", false
	}
	if partial.AgentName == "" || partial.TeamName == "" {
		return "", "", false
	}

	for _, m := range expected {
		if partial.AgentName == m.Name && partial.TeamName == m.TeamName {
			return partial.AgentName, partial.TeamName, true
		}
	}
	return "", "", false
}
