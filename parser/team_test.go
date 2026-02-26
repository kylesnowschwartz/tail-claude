package parser_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"
)

// makeToolCallItem builds an AI chunk with a single tool call DisplayItem.
func makeToolCallItem(toolName string, input map[string]interface{}) parser.Chunk {
	raw, _ := json.Marshal(input)
	return parser.Chunk{
		Type:      parser.AIChunk,
		Timestamp: time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		Items: []parser.DisplayItem{{
			Type:      parser.ItemToolCall,
			ToolName:  toolName,
			ToolInput: json.RawMessage(raw),
		}},
	}
}

// makeTeamSpawnItem builds an AI chunk with a team Task spawn (ItemSubagent
// with team_name + name in input), matching how BuildChunks classifies them.
func makeTeamSpawnItem(teamName, memberName string) parser.Chunk {
	raw, _ := json.Marshal(map[string]interface{}{
		"subagent_type": "general-purpose",
		"description":   "Do work",
		"team_name":     teamName,
		"name":          memberName,
	})
	return parser.Chunk{
		Type: parser.AIChunk,
		Items: []parser.DisplayItem{{
			Type:      parser.ItemSubagent,
			ToolName:  "Task",
			ToolInput: json.RawMessage(raw),
		}},
	}
}

func TestReconstructTeams_BasicLifecycle(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name":   "my-project",
			"description": "Working on feature X",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject":     "Fix the bug",
			"description": "Something is broken",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject":     "Write tests",
			"description": "Cover the fix",
		}),
	}

	teams := parser.ReconstructTeams(chunks, nil)

	if len(teams) != 1 {
		t.Fatalf("got %d teams, want 1", len(teams))
	}
	team := teams[0]
	if team.Name != "my-project" {
		t.Errorf("Name = %q, want %q", team.Name, "my-project")
	}
	if team.Description != "Working on feature X" {
		t.Errorf("Description = %q, want %q", team.Description, "Working on feature X")
	}
	if len(team.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(team.Tasks))
	}

	// Sequential IDs starting at 1.
	if team.Tasks[0].ID != "1" {
		t.Errorf("Tasks[0].ID = %q, want %q", team.Tasks[0].ID, "1")
	}
	if team.Tasks[1].ID != "2" {
		t.Errorf("Tasks[1].ID = %q, want %q", team.Tasks[1].ID, "2")
	}

	// Subject extraction.
	if team.Tasks[0].Subject != "Fix the bug" {
		t.Errorf("Tasks[0].Subject = %q, want %q", team.Tasks[0].Subject, "Fix the bug")
	}
	if team.Tasks[1].Subject != "Write tests" {
		t.Errorf("Tasks[1].Subject = %q, want %q", team.Tasks[1].Subject, "Write tests")
	}

	// Initial status.
	for i, task := range team.Tasks {
		if task.Status != "pending" {
			t.Errorf("Tasks[%d].Status = %q, want %q", i, task.Status, "pending")
		}
	}
}

func TestReconstructTeams_LeadTaskUpdate(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Task one",
		}),
		makeToolCallItem("TaskUpdate", map[string]interface{}{
			"taskId": "1",
			"status": "in_progress",
			"owner":  "worker-a",
		}),
	}

	teams := parser.ReconstructTeams(chunks, nil)

	if len(teams) == 0 || len(teams[0].Tasks) == 0 {
		t.Fatal("expected 1 team with 1 task")
	}
	task := teams[0].Tasks[0]
	if task.Status != "in_progress" {
		t.Errorf("Status = %q, want %q", task.Status, "in_progress")
	}
	if task.Owner != "worker-a" {
		t.Errorf("Owner = %q, want %q", task.Owner, "worker-a")
	}
}

func TestReconstructTeams_WorkerUpdates(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Task one",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Task two",
		}),
	}

	// Worker completes task 1.
	updateInput, _ := json.Marshal(map[string]interface{}{
		"taskId": "1",
		"status": "completed",
	})
	workers := []parser.SubagentProcess{{
		ID: "fixer@proj",
		Chunks: []parser.Chunk{{
			Type: parser.AIChunk,
			Items: []parser.DisplayItem{{
				Type:      parser.ItemToolCall,
				ToolName:  "TaskUpdate",
				ToolInput: json.RawMessage(updateInput),
			}},
		}},
	}}

	teams := parser.ReconstructTeams(chunks, workers)

	if len(teams) == 0 || len(teams[0].Tasks) < 2 {
		t.Fatal("expected 1 team with 2 tasks")
	}

	// Task 1: completed, owner inferred from worker name.
	task1 := teams[0].Tasks[0]
	if task1.Status != "completed" {
		t.Errorf("Task 1 Status = %q, want %q", task1.Status, "completed")
	}
	if task1.Owner != "fixer" {
		t.Errorf("Task 1 Owner = %q, want %q (inferred from worker ID)", task1.Owner, "fixer")
	}

	// Task 2: untouched.
	if teams[0].Tasks[1].Status != "pending" {
		t.Errorf("Task 2 Status = %q, want %q", teams[0].Tasks[1].Status, "pending")
	}
}

func TestReconstructTeams_WorkerExplicitOwner(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Task 1",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Task 2",
		}),
	}

	update1, _ := json.Marshal(map[string]interface{}{
		"taskId": "1",
		"status": "in_progress",
		"owner":  "explicit-owner",
	})
	update2, _ := json.Marshal(map[string]interface{}{
		"taskId": "2",
		"status": "in_progress",
	})
	workers := []parser.SubagentProcess{{
		ID: "my-worker@proj",
		Chunks: []parser.Chunk{{
			Type: parser.AIChunk,
			Items: []parser.DisplayItem{
				{Type: parser.ItemToolCall, ToolName: "TaskUpdate", ToolInput: json.RawMessage(update1)},
				{Type: parser.ItemToolCall, ToolName: "TaskUpdate", ToolInput: json.RawMessage(update2)},
			},
		}},
	}}

	teams := parser.ReconstructTeams(chunks, workers)

	if teams[0].Tasks[0].Owner != "explicit-owner" {
		t.Errorf("Task 1 Owner = %q, want %q", teams[0].Tasks[0].Owner, "explicit-owner")
	}
	if teams[0].Tasks[1].Owner != "my-worker" {
		t.Errorf("Task 2 Owner = %q, want %q (inferred)", teams[0].Tasks[1].Owner, "my-worker")
	}
}

func TestReconstructTeams_TeamDelete(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "temp",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Only task",
		}),
		makeToolCallItem("TeamDelete", map[string]interface{}{}),
	}

	teams := parser.ReconstructTeams(chunks, nil)

	if len(teams) != 1 {
		t.Fatalf("got %d teams, want 1", len(teams))
	}
	if !teams[0].Deleted {
		t.Error("team should be marked deleted")
	}
	// Tasks survive deletion — they're historical data.
	if len(teams[0].Tasks) != 1 {
		t.Errorf("got %d tasks after delete, want 1", len(teams[0].Tasks))
	}
}

func TestReconstructTeams_TaskCreateIgnoredWithoutTeam(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Orphan task",
		}),
	}

	teams := parser.ReconstructTeams(chunks, nil)

	if len(teams) != 0 {
		t.Errorf("got %d teams, want 0 (no TeamCreate)", len(teams))
	}
}

func TestReconstructTeams_MultipleTeams(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "team-a",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "A task",
		}),
		makeToolCallItem("TeamDelete", map[string]interface{}{}),
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "team-b",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "B task 1",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "B task 2",
		}),
	}

	teams := parser.ReconstructTeams(chunks, nil)

	if len(teams) != 2 {
		t.Fatalf("got %d teams, want 2", len(teams))
	}
	if teams[0].Name != "team-a" {
		t.Errorf("teams[0].Name = %q, want %q", teams[0].Name, "team-a")
	}
	if !teams[0].Deleted {
		t.Error("team-a should be deleted")
	}
	if len(teams[0].Tasks) != 1 {
		t.Errorf("team-a tasks = %d, want 1", len(teams[0].Tasks))
	}

	if teams[1].Name != "team-b" {
		t.Errorf("teams[1].Name = %q, want %q", teams[1].Name, "team-b")
	}
	if teams[1].Deleted {
		t.Error("team-b should not be deleted")
	}
	if len(teams[1].Tasks) != 2 {
		t.Errorf("team-b tasks = %d, want 2", len(teams[1].Tasks))
	}

	// IDs reset for the second team.
	if teams[1].Tasks[0].ID != "1" {
		t.Errorf("team-b Tasks[0].ID = %q, want %q", teams[1].Tasks[0].ID, "1")
	}
	if teams[1].Tasks[1].ID != "2" {
		t.Errorf("team-b Tasks[1].ID = %q, want %q", teams[1].Tasks[1].ID, "2")
	}
}

func TestReconstructTeams_MemberDiscovery(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeTeamSpawnItem("proj", "worker-1"),
		makeTeamSpawnItem("proj", "worker-2"),
	}

	teams := parser.ReconstructTeams(chunks, nil)

	if len(teams) == 0 {
		t.Fatal("expected 1 team")
	}
	if len(teams[0].Members) != 2 {
		t.Fatalf("Members = %d, want 2", len(teams[0].Members))
	}
	if teams[0].Members[0] != "worker-1" || teams[0].Members[1] != "worker-2" {
		t.Errorf("Members = %v, want [worker-1 worker-2]", teams[0].Members)
	}
}

func TestReconstructTeams_NoDuplicateMembers(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeTeamSpawnItem("proj", "worker-1"),
		makeTeamSpawnItem("proj", "worker-1"), // duplicate
	}

	teams := parser.ReconstructTeams(chunks, nil)

	if len(teams) == 0 {
		t.Fatal("expected 1 team")
	}
	if len(teams[0].Members) != 1 {
		t.Errorf("Members = %d, want 1 (no duplicates)", len(teams[0].Members))
	}
}

func TestReconstructTeams_MemberColors(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeTeamSpawnItem("proj", "worker-1"),
	}

	workers := []parser.SubagentProcess{{
		ID:            "worker-1@proj",
		TeammateColor: "blue",
		Chunks:        []parser.Chunk{{Type: parser.AIChunk}},
	}}

	teams := parser.ReconstructTeams(chunks, workers)

	if len(teams) == 0 {
		t.Fatal("expected 1 team")
	}
	if teams[0].MemberColors["worker-1"] != "blue" {
		t.Errorf("MemberColors[worker-1] = %q, want %q", teams[0].MemberColors["worker-1"], "blue")
	}
}

func TestReconstructTeams_MemberOngoing(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeTeamSpawnItem("proj", "active-worker"),
		makeTeamSpawnItem("proj", "done-worker"),
	}

	// Active worker: tool_use with no tool_result -> IsOngoing == true.
	activeInput, _ := json.Marshal(map[string]interface{}{"command": "npm test"})
	activeWorker := parser.SubagentProcess{
		ID: "active-worker@proj",
		Chunks: []parser.Chunk{{
			Type: parser.AIChunk,
			Items: []parser.DisplayItem{{
				Type:      parser.ItemToolCall,
				ToolName:  "Bash",
				ToolID:    "tool_1",
				ToolInput: json.RawMessage(activeInput),
				// No ToolResult -> pending tool call -> ongoing.
			}},
		}},
	}

	// Done worker: tool_use with a tool_result -> IsOngoing == false.
	doneInput, _ := json.Marshal(map[string]interface{}{"command": "echo done"})
	doneWorker := parser.SubagentProcess{
		ID: "done-worker@proj",
		Chunks: []parser.Chunk{{
			Type: parser.AIChunk,
			Items: []parser.DisplayItem{
				{
					Type:       parser.ItemToolCall,
					ToolName:   "Bash",
					ToolID:     "tool_2",
					ToolInput:  json.RawMessage(doneInput),
					ToolResult: "done",
				},
				{
					Type: parser.ItemOutput,
					Text: "All finished.",
				},
			},
		}},
	}

	workers := []parser.SubagentProcess{activeWorker, doneWorker}
	teams := parser.ReconstructTeams(chunks, workers)

	if len(teams) == 0 {
		t.Fatal("expected 1 team")
	}

	if !teams[0].MemberOngoing["active-worker"] {
		t.Error("active-worker should be ongoing (pending tool call)")
	}
	if teams[0].MemberOngoing["done-worker"] {
		t.Error("done-worker should not be ongoing (tool call completed)")
	}
}

func TestReconstructTeams_EmptyChunks(t *testing.T) {
	teams := parser.ReconstructTeams(nil, nil)
	if len(teams) != 0 {
		t.Errorf("got %d teams, want 0", len(teams))
	}
}

func TestReconstructTeams_TaskSubjectUpdate(t *testing.T) {
	// TaskUpdate can change a task's subject.
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Original name",
		}),
		makeToolCallItem("TaskUpdate", map[string]interface{}{
			"taskId":  "1",
			"subject": "Renamed task",
		}),
	}

	teams := parser.ReconstructTeams(chunks, nil)

	if teams[0].Tasks[0].Subject != "Renamed task" {
		t.Errorf("Subject = %q, want %q", teams[0].Tasks[0].Subject, "Renamed task")
	}
}

func TestReconstructTeams_TaskDeletedStatus(t *testing.T) {
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Will be deleted",
		}),
		makeToolCallItem("TaskUpdate", map[string]interface{}{
			"taskId": "1",
			"status": "deleted",
		}),
	}

	teams := parser.ReconstructTeams(chunks, nil)

	// The task should have status "deleted" — rendering can filter it.
	if teams[0].Tasks[0].Status != "deleted" {
		t.Errorf("Status = %q, want %q", teams[0].Tasks[0].Status, "deleted")
	}
}

func TestReconstructTeams_TaskUpdateAfterDelete(t *testing.T) {
	// TaskUpdate after TeamDelete should be ignored (no active team).
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Task one",
		}),
		makeToolCallItem("TeamDelete", map[string]interface{}{}),
		makeToolCallItem("TaskUpdate", map[string]interface{}{
			"taskId": "1",
			"status": "completed",
		}),
	}

	teams := parser.ReconstructTeams(chunks, nil)

	// Task should remain pending — the update happened after TeamDelete.
	if teams[0].Tasks[0].Status != "pending" {
		t.Errorf("Status = %q, want %q (update after delete should be ignored)",
			teams[0].Tasks[0].Status, "pending")
	}
}

func TestReconstructTeams_WorkerMismatchedTeam(t *testing.T) {
	// Worker belongs to a different team — updates should not apply.
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj-a",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Task A",
		}),
	}

	updateInput, _ := json.Marshal(map[string]interface{}{
		"taskId": "1",
		"status": "completed",
	})
	workers := []parser.SubagentProcess{{
		ID: "worker@proj-b", // different team
		Chunks: []parser.Chunk{{
			Type: parser.AIChunk,
			Items: []parser.DisplayItem{{
				Type:      parser.ItemToolCall,
				ToolName:  "TaskUpdate",
				ToolInput: json.RawMessage(updateInput),
			}},
		}},
	}}

	teams := parser.ReconstructTeams(chunks, workers)

	// Task should remain pending — worker is on a different team.
	if teams[0].Tasks[0].Status != "pending" {
		t.Errorf("Status = %q, want %q (wrong team)", teams[0].Tasks[0].Status, "pending")
	}
}

func TestReconstructTeams_NonTeamWorkerIgnored(t *testing.T) {
	// Worker without "@" in ID is not a team worker.
	chunks := []parser.Chunk{
		makeToolCallItem("TeamCreate", map[string]interface{}{
			"team_name": "proj",
		}),
		makeToolCallItem("TaskCreate", map[string]interface{}{
			"subject": "Task",
		}),
	}

	updateInput, _ := json.Marshal(map[string]interface{}{
		"taskId": "1",
		"status": "completed",
	})
	workers := []parser.SubagentProcess{{
		ID: "abc123def", // regular subagent, not team
		Chunks: []parser.Chunk{{
			Type: parser.AIChunk,
			Items: []parser.DisplayItem{{
				Type:      parser.ItemToolCall,
				ToolName:  "TaskUpdate",
				ToolInput: json.RawMessage(updateInput),
			}},
		}},
	}}

	teams := parser.ReconstructTeams(chunks, workers)

	if teams[0].Tasks[0].Status != "pending" {
		t.Errorf("Status = %q, want %q (non-team worker)", teams[0].Tasks[0].Status, "pending")
	}
}
