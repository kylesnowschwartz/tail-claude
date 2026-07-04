package parser

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// TeamTask represents a single task in a team's task board.
type TeamTask struct {
	ID      string // sequential within team: "1", "2", ...
	Subject string
	Status  string // "pending" | "in_progress" | "completed" | "deleted"
	Owner   string // worker name, from TaskUpdate or inferred from worker ID
}

// TeamSnapshot represents the reconstructed state of a team at the
// end of a session (or at the current point during live tailing).
type TeamSnapshot struct {
	Name          string
	Description   string
	Tasks         []TeamTask
	Members       []string          // worker names from Task spawn calls
	MemberColors  map[string]string // member name -> color name (e.g. "blue")
	MemberOngoing map[string]bool   // member name -> true if worker session is ongoing
	Deleted       bool              // true after TeamDelete
}

// taskUpdateEvent is a TaskUpdate call collected for timestamp-ordered
// replay. Team context is resolved at collection time (lead: the active
// team; worker: the team from its "name@team" ID) because the raw input
// carries no team reference.
type taskUpdateEvent struct {
	ts            time.Time
	teamIdx       int
	input         json.RawMessage
	fallbackOwner string // worker name for worker updates; "" for lead updates
}

// ReconstructTeams replays tool call events from lead chunks and linked
// worker processes to build the final task board state for each team.
//
// Phase 1 walks lead chunks chronologically for TeamCreate, TaskCreate,
// TeamDelete, and team Task spawns. Task IDs are assigned sequentially
// per team — Claude Code's task system numbers them from 1. TaskUpdate
// events (lead here, worker in Phase 2) are collected rather than
// applied immediately.
//
// Phase 2 collects worker TaskUpdate events. If a worker update has no
// explicit owner field, the worker's own name (from its ID) is used as
// fallback. Lead and worker updates are then replayed together in
// timestamp order — applying all lead updates before all worker updates
// would let an earlier worker event overwrite a later lead correction.
// Ordering keys on per-item timestamps (see itemEventTime): a merged AI
// chunk's Timestamp is the turn-start time, not the update's.
//
// Phase 3 populates member colors from worker TeammateColor metadata.
func ReconstructTeams(chunks []Chunk, workers []SubagentProcess) []TeamSnapshot {
	var teams []TeamSnapshot
	var updates []taskUpdateEvent
	activeIdx := -1
	taskCounter := 0

	// Phase 1: Lead chunk events.
	for i := range chunks {
		if chunks[i].Type != AIChunk {
			continue
		}
		for j := range chunks[i].Items {
			it := &chunks[i].Items[j]

			switch {
			case it.Type == ItemToolCall && it.ToolName == "TeamCreate":
				teams = append(teams, teamSnapshotFromCreate(it.ToolInput))
				activeIdx = len(teams) - 1
				taskCounter = 0

			case it.Type == ItemToolCall && it.ToolName == "TaskCreate" && activeIdx >= 0:
				taskCounter++
				teams[activeIdx].Tasks = append(teams[activeIdx].Tasks,
					teamTaskFromCreate(it.ToolInput, taskCounter))

			case it.Type == ItemToolCall && it.ToolName == "TaskUpdate" && activeIdx >= 0:
				updates = append(updates, taskUpdateEvent{
					ts:      itemEventTime(it, &chunks[i]),
					teamIdx: activeIdx,
					input:   it.ToolInput,
				})

			case it.Type == ItemToolCall && it.ToolName == "TeamDelete" && activeIdx >= 0:
				teams[activeIdx].Deleted = true
				activeIdx = -1

			case it.Type == ItemSubagent && IsTeamTask(it):
				addTeamSpawnMember(it.ToolInput, teams)
			}
		}
	}

	// Phase 2: Worker TaskUpdate events.
	for i := range workers {
		agentName, teamName := splitWorkerID(workers[i].ID)
		if teamName == "" {
			continue
		}
		teamIdx := findTeamIndex(teams, teamName)
		if teamIdx < 0 {
			continue
		}
		updates = append(updates,
			collectWorkerTaskUpdates(workers[i].Chunks, teamIdx, agentName)...)
	}

	// Replay all TaskUpdates in timestamp order. Stable sort keeps the
	// collection order (lead before worker) for equal or missing timestamps.
	sort.SliceStable(updates, func(a, b int) bool {
		return updates[a].ts.Before(updates[b].ts)
	})
	for _, u := range updates {
		applyTaskUpdate(u.input, &teams[u.teamIdx], u.fallbackOwner)
	}

	// Phase 3: Populate member colors from worker metadata.
	for i := range teams {
		teams[i].MemberColors = make(map[string]string)
		teams[i].MemberOngoing = make(map[string]bool)
	}
	for _, w := range workers {
		agentName, teamName := splitWorkerID(w.ID)
		if teamName == "" || w.TeammateColor == "" {
			continue
		}
		for i := range teams {
			if teams[i].Name == teamName {
				teams[i].MemberColors[agentName] = w.TeammateColor
			}
		}
	}

	// Phase 4: Populate member ongoing state from worker sessions.
	for _, w := range workers {
		agentName, teamName := splitWorkerID(w.ID)
		if teamName == "" {
			continue
		}
		if IsOngoing(w.Chunks) {
			for i := range teams {
				if teams[i].Name == teamName {
					teams[i].MemberOngoing[agentName] = true
				}
			}
		}
	}

	return teams
}

// teamSnapshotFromCreate extracts team name and description from TeamCreate input.
func teamSnapshotFromCreate(input json.RawMessage) TeamSnapshot {
	fields := parseInputFields(input)
	return TeamSnapshot{
		Name:        getString(fields, "team_name"),
		Description: getString(fields, "description"),
	}
}

// teamTaskFromCreate extracts subject from TaskCreate input and assigns a sequential ID.
func teamTaskFromCreate(input json.RawMessage, seqID int) TeamTask {
	fields := parseInputFields(input)
	return TeamTask{
		ID:      fmt.Sprintf("%d", seqID),
		Subject: getString(fields, "subject"),
		Status:  "pending",
	}
}

// applyTaskUpdate applies a TaskUpdate to the matching task in a team.
// fallbackOwner is set for worker updates without an explicit owner field
// — workers typically claim tasks by setting themselves as owner, but the
// field is optional. Lead updates pass "".
func applyTaskUpdate(input json.RawMessage, team *TeamSnapshot, fallbackOwner string) {
	fields := parseInputFields(input)
	taskID := getString(fields, "taskId")
	if taskID == "" {
		return
	}
	for i := range team.Tasks {
		if team.Tasks[i].ID != taskID {
			continue
		}
		if status := getString(fields, "status"); status != "" {
			team.Tasks[i].Status = status
		}
		if owner := getString(fields, "owner"); owner != "" {
			team.Tasks[i].Owner = owner
		} else if fallbackOwner != "" && team.Tasks[i].Owner == "" {
			team.Tasks[i].Owner = fallbackOwner
		}
		if subject := getString(fields, "subject"); subject != "" {
			team.Tasks[i].Subject = subject
		}
		return
	}
}

// addTeamSpawnMember adds a worker name to the matching team's Members list.
// Deduplicates — a worker spawned twice (e.g. resumed) appears once.
func addTeamSpawnMember(input json.RawMessage, teams []TeamSnapshot) {
	teamName, memberName, _ := teamSpecFromInput(input)
	if teamName == "" || memberName == "" {
		return
	}
	for i := range teams {
		if teams[i].Name != teamName {
			continue
		}
		for _, m := range teams[i].Members {
			if m == memberName {
				return
			}
		}
		teams[i].Members = append(teams[i].Members, memberName)
		return
	}
}

// collectWorkerTaskUpdates gathers a worker's TaskUpdate calls as replay
// events tagged with the worker's team and name.
func collectWorkerTaskUpdates(chunks []Chunk, teamIdx int, workerName string) []taskUpdateEvent {
	var events []taskUpdateEvent
	for i := range chunks {
		if chunks[i].Type != AIChunk {
			continue
		}
		for j := range chunks[i].Items {
			it := &chunks[i].Items[j]
			if it.Type != ItemToolCall || it.ToolName != "TaskUpdate" {
				continue
			}
			events = append(events, taskUpdateEvent{
				ts:            itemEventTime(it, &chunks[i]),
				teamIdx:       teamIdx,
				input:         it.ToolInput,
				fallbackOwner: workerName,
			})
		}
	}
	return events
}

// itemEventTime returns the item's own timestamp — required for correct
// ordering because merged AI chunks stamp Chunk.Timestamp with the FIRST
// buffered message's time — falling back to the chunk timestamp for items
// built without per-message times.
func itemEventTime(it *DisplayItem, chunk *Chunk) time.Time {
	if !it.Timestamp.IsZero() {
		return it.Timestamp
	}
	return chunk.Timestamp
}

// splitWorkerID parses "agentName@teamName" into its parts.
// Returns ("", "") for non-team worker IDs (no "@" separator).
func splitWorkerID(id string) (agentName, teamName string) {
	parts := strings.SplitN(id, "@", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// findTeamIndex returns the index of the named team, or -1.
func findTeamIndex(teams []TeamSnapshot, name string) int {
	for i := range teams {
		if teams[i].Name == name {
			return i
		}
	}
	return -1
}

// parseInputFields unmarshals a JSON tool input into a field map.
// Returns nil on error or empty input.
func parseInputFields(input json.RawMessage) map[string]json.RawMessage {
	if len(input) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil {
		return nil
	}
	return fields
}
