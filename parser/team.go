package parser

import (
	"encoding/json"
	"fmt"
	"strings"
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

// ReconstructTeams replays tool call events from lead chunks and linked
// worker processes to build the final task board state for each team.
//
// Phase 1 walks lead chunks chronologically for TeamCreate, TaskCreate,
// TaskUpdate, TeamDelete, and team Task spawns. Task IDs are assigned
// sequentially per team — Claude Code's task system numbers them from 1.
//
// Phase 2 walks worker chunks for TaskUpdate events that modify task
// status and ownership. If a worker update has no explicit owner field,
// the worker's own name (from its ID) is used as fallback.
//
// Phase 3 populates member colors from worker TeammateColor metadata.
func ReconstructTeams(chunks []Chunk, workers []SubagentProcess) []TeamSnapshot {
	var teams []TeamSnapshot
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
				applyTeamTaskUpdate(it.ToolInput, &teams[activeIdx])

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
		team := findTeamByName(teams, teamName)
		if team == nil {
			continue
		}
		applyWorkerTaskUpdates(workers[i].Chunks, team, agentName)
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

// applyTeamTaskUpdate applies a TaskUpdate to the matching task in a team.
func applyTeamTaskUpdate(input json.RawMessage, team *TeamSnapshot) {
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
	fields := parseInputFields(input)
	teamName := getString(fields, "team_name")
	memberName := getString(fields, "name")
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

// applyWorkerTaskUpdates scans a worker's chunks for TaskUpdate calls and
// applies them to the team's tasks. If the update has no explicit owner
// field, the worker's own name is used as fallback — workers typically
// claim tasks by setting themselves as owner, but the field is optional.
func applyWorkerTaskUpdates(chunks []Chunk, team *TeamSnapshot, workerName string) {
	for i := range chunks {
		if chunks[i].Type != AIChunk {
			continue
		}
		for j := range chunks[i].Items {
			it := &chunks[i].Items[j]
			if it.Type != ItemToolCall || it.ToolName != "TaskUpdate" {
				continue
			}
			fields := parseInputFields(it.ToolInput)
			taskID := getString(fields, "taskId")
			if taskID == "" {
				continue
			}
			for k := range team.Tasks {
				if team.Tasks[k].ID != taskID {
					continue
				}
				if status := getString(fields, "status"); status != "" {
					team.Tasks[k].Status = status
				}
				if owner := getString(fields, "owner"); owner != "" {
					team.Tasks[k].Owner = owner
				} else if team.Tasks[k].Owner == "" {
					team.Tasks[k].Owner = workerName
				}
				if subject := getString(fields, "subject"); subject != "" {
					team.Tasks[k].Subject = subject
				}
			}
		}
	}
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

// findTeamByName returns a pointer to the named team, or nil.
func findTeamByName(teams []TeamSnapshot, name string) *TeamSnapshot {
	for i := range teams {
		if teams[i].Name == name {
			return &teams[i]
		}
	}
	return nil
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
