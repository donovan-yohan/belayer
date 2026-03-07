package lead

import (
	"testing"

	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/model"
)

func setupTestDB(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.Migrate(); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	// Insert prerequisite records
	conn := database.Conn()

	_, err = conn.Exec("INSERT INTO instances (id, name, path) VALUES (?, ?, ?)", "test-inst", "test-inst", "/tmp/test")
	if err != nil {
		t.Fatalf("inserting instance: %v", err)
	}

	_, err = conn.Exec("INSERT INTO tasks (id, instance_id, description) VALUES (?, ?, ?)", "task-1", "test-inst", "test task")
	if err != nil {
		t.Fatalf("inserting task: %v", err)
	}

	_, err = conn.Exec("INSERT INTO task_repos (id, task_id, repo_name) VALUES (?, ?, ?)", "tr-1", "task-1", "test-repo")
	if err != nil {
		t.Fatalf("inserting task_repo: %v", err)
	}

	_, err = conn.Exec("INSERT INTO leads (id, task_repo_id, status, attempt) VALUES (?, ?, ?, ?)", "lead-1", "tr-1", "pending", 1)
	if err != nil {
		t.Fatalf("inserting lead: %v", err)
	}

	_, _ = conn.Exec("INSERT INTO leads (id, task_repo_id, status, attempt) VALUES (?, ?, ?, ?)", "lead-2", "tr-1", "pending", 1)

	return NewStore(conn)
}

func TestSetLeadStarted(t *testing.T) {
	store := setupTestDB(t)

	if err := store.SetLeadStarted("lead-1"); err != nil {
		t.Fatalf("SetLeadStarted: %v", err)
	}

	lead, err := store.GetLead("lead-1")
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}

	if lead.Status != model.LeadStatusRunning {
		t.Errorf("expected status %q, got %q", model.LeadStatusRunning, lead.Status)
	}
	if lead.StartedAt == nil {
		t.Error("expected started_at to be set")
	}
}

func TestSetLeadFinished(t *testing.T) {
	store := setupTestDB(t)

	if err := store.SetLeadFinished("lead-1", model.LeadStatusComplete, "all done"); err != nil {
		t.Fatalf("SetLeadFinished: %v", err)
	}

	lead, err := store.GetLead("lead-1")
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}

	if lead.Status != model.LeadStatusComplete {
		t.Errorf("expected status %q, got %q", model.LeadStatusComplete, lead.Status)
	}
	if lead.Output != "all done" {
		t.Errorf("expected output %q, got %q", "all done", lead.Output)
	}
	if lead.FinishedAt == nil {
		t.Error("expected finished_at to be set")
	}
}

func TestUpdateLeadAttempt(t *testing.T) {
	store := setupTestDB(t)

	if err := store.UpdateLeadAttempt("lead-1", 3); err != nil {
		t.Fatalf("UpdateLeadAttempt: %v", err)
	}

	lead, err := store.GetLead("lead-1")
	if err != nil {
		t.Fatalf("GetLead: %v", err)
	}

	if lead.Attempt != 3 {
		t.Errorf("expected attempt 3, got %d", lead.Attempt)
	}
}

func TestInsertAndGetLeadGoals(t *testing.T) {
	store := setupTestDB(t)

	goals := []*model.LeadGoal{
		{ID: "goal-0", LeadID: "lead-1", GoalIndex: 0, Description: "first goal", Status: model.LeadGoalPending},
		{ID: "goal-1", LeadID: "lead-1", GoalIndex: 1, Description: "second goal", Status: model.LeadGoalPending},
	}

	for _, g := range goals {
		if err := store.InsertLeadGoal(g); err != nil {
			t.Fatalf("InsertLeadGoal: %v", err)
		}
	}

	retrieved, err := store.GetLeadGoals("lead-1")
	if err != nil {
		t.Fatalf("GetLeadGoals: %v", err)
	}

	if len(retrieved) != 2 {
		t.Fatalf("expected 2 goals, got %d", len(retrieved))
	}

	if retrieved[0].Description != "first goal" {
		t.Errorf("expected description %q, got %q", "first goal", retrieved[0].Description)
	}
	if retrieved[1].GoalIndex != 1 {
		t.Errorf("expected goal_index 1, got %d", retrieved[1].GoalIndex)
	}
}

func TestUpdateLeadGoalStatus(t *testing.T) {
	store := setupTestDB(t)

	goal := &model.LeadGoal{ID: "goal-0", LeadID: "lead-1", GoalIndex: 0, Description: "test", Status: model.LeadGoalPending}
	if err := store.InsertLeadGoal(goal); err != nil {
		t.Fatalf("InsertLeadGoal: %v", err)
	}

	verdictJSON := `{"pass":true,"summary":"looks good"}`
	if err := store.UpdateLeadGoalStatus("goal-0", model.LeadGoalRunning, 2, "some output", verdictJSON); err != nil {
		t.Fatalf("UpdateLeadGoalStatus: %v", err)
	}

	goals, err := store.GetLeadGoals("lead-1")
	if err != nil {
		t.Fatalf("GetLeadGoals: %v", err)
	}

	if goals[0].Status != model.LeadGoalRunning {
		t.Errorf("expected status %q, got %q", model.LeadGoalRunning, goals[0].Status)
	}
	if goals[0].Attempt != 2 {
		t.Errorf("expected attempt 2, got %d", goals[0].Attempt)
	}
	if goals[0].VerdictJSON != verdictJSON {
		t.Errorf("expected verdict %q, got %q", verdictJSON, goals[0].VerdictJSON)
	}
}

func TestSetLeadGoalFinished(t *testing.T) {
	store := setupTestDB(t)

	goal := &model.LeadGoal{ID: "goal-0", LeadID: "lead-1", GoalIndex: 0, Description: "test", Status: model.LeadGoalPending}
	if err := store.InsertLeadGoal(goal); err != nil {
		t.Fatalf("InsertLeadGoal: %v", err)
	}

	if err := store.SetLeadGoalFinished("goal-0", model.LeadGoalComplete, `{"pass":true}`); err != nil {
		t.Fatalf("SetLeadGoalFinished: %v", err)
	}

	goals, err := store.GetLeadGoals("lead-1")
	if err != nil {
		t.Fatalf("GetLeadGoals: %v", err)
	}

	if goals[0].Status != model.LeadGoalComplete {
		t.Errorf("expected status %q, got %q", model.LeadGoalComplete, goals[0].Status)
	}
	if goals[0].FinishedAt == nil {
		t.Error("expected finished_at to be set")
	}
}

func TestInsertEvent(t *testing.T) {
	store := setupTestDB(t)

	if err := store.InsertEvent("task-1", "lead-1", model.EventLeadStarted, `{"test":true}`); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	// Verify event was inserted (query directly since we don't have a GetEvents method yet)
	var count int
	err := store.db.QueryRow("SELECT COUNT(*) FROM events WHERE task_id = ? AND lead_id = ?", "task-1", "lead-1").Scan(&count)
	if err != nil {
		t.Fatalf("counting events: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 event, got %d", count)
	}
}

func TestGetLeadGoalsEmpty(t *testing.T) {
	store := setupTestDB(t)

	goals, err := store.GetLeadGoals("lead-2")
	if err != nil {
		t.Fatalf("GetLeadGoals: %v", err)
	}

	if len(goals) != 0 {
		t.Errorf("expected 0 goals, got %d", len(goals))
	}
}
