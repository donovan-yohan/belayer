package coordinator

import (
	"fmt"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	require.NoError(t, database.Migrate())

	// Insert a test instance for foreign key constraints.
	_, err = database.Conn().Exec(
		`INSERT INTO instances (id, name, path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"test-instance", "test", "/tmp/test", time.Now().UTC(), time.Now().UTC(),
	)
	require.NoError(t, err)

	return NewStore(database.Conn())
}

func makeTask(id string) *model.Task {
	return &model.Task{
		ID:          id,
		InstanceID:  "test-instance",
		Description: "implement feature X",
		Source:      "text",
		SourceRef:   "",
		Status:      model.TaskStatusPending,
	}
}

func TestInsertAndGetTask(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-1001")
	require.NoError(t, store.InsertTask(task))

	got, err := store.GetTask("task-1001")
	require.NoError(t, err)

	assert.Equal(t, "task-1001", got.ID)
	assert.Equal(t, "test-instance", got.InstanceID)
	assert.Equal(t, "implement feature X", got.Description)
	assert.Equal(t, "text", got.Source)
	assert.Equal(t, model.TaskStatusPending, got.Status)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestGetTask_NotFound(t *testing.T) {
	store := setupTestDB(t)

	_, err := store.GetTask("nonexistent")
	require.Error(t, err)
}

func TestUpdateTaskStatus(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-2001")
	require.NoError(t, store.InsertTask(task))

	require.NoError(t, store.UpdateTaskStatus("task-2001", model.TaskStatusRunning))

	got, err := store.GetTask("task-2001")
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusRunning, got.Status)
}

func TestUpdateTaskStatus_Progression(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-2002")
	require.NoError(t, store.InsertTask(task))

	transitions := []model.TaskStatus{
		model.TaskStatusDecomposing,
		model.TaskStatusRunning,
		model.TaskStatusAligning,
		model.TaskStatusComplete,
	}

	for _, status := range transitions {
		require.NoError(t, store.UpdateTaskStatus("task-2002", status))
		got, err := store.GetTask("task-2002")
		require.NoError(t, err)
		assert.Equal(t, status, got.Status)
	}
}

func TestGetTasksByStatus(t *testing.T) {
	store := setupTestDB(t)

	// Insert tasks with different statuses.
	pending1 := makeTask("task-3001")
	pending2 := makeTask("task-3002")
	running := makeTask("task-3003")
	running.Status = model.TaskStatusRunning

	require.NoError(t, store.InsertTask(pending1))
	require.NoError(t, store.InsertTask(pending2))
	require.NoError(t, store.InsertTask(running))

	pendingTasks, err := store.GetTasksByStatus(model.TaskStatusPending)
	require.NoError(t, err)
	assert.Len(t, pendingTasks, 2)

	runningTasks, err := store.GetTasksByStatus(model.TaskStatusRunning)
	require.NoError(t, err)
	assert.Len(t, runningTasks, 1)
	assert.Equal(t, "task-3003", runningTasks[0].ID)
}

func TestGetTasksByStatus_Empty(t *testing.T) {
	store := setupTestDB(t)

	tasks, err := store.GetTasksByStatus(model.TaskStatusFailed)
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestInsertAndGetTaskRepos(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-4001")
	require.NoError(t, store.InsertTask(task))

	repos := []*model.TaskRepo{
		{ID: "tr-4001", TaskID: "task-4001", RepoName: "frontend", Spec: "build login page", WorktreePath: "/tmp/wt/frontend"},
		{ID: "tr-4002", TaskID: "task-4001", RepoName: "backend", Spec: "add auth endpoint", WorktreePath: "/tmp/wt/backend"},
	}
	for _, r := range repos {
		require.NoError(t, store.InsertTaskRepo(r))
	}

	got, err := store.GetTaskReposForTask("task-4001")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// Verify data roundtrips correctly.
	repoByName := make(map[string]model.TaskRepo)
	for _, r := range got {
		repoByName[r.RepoName] = r
	}

	fe := repoByName["frontend"]
	assert.Equal(t, "tr-4001", fe.ID)
	assert.Equal(t, "task-4001", fe.TaskID)
	assert.Equal(t, "build login page", fe.Spec)
	assert.Equal(t, "/tmp/wt/frontend", fe.WorktreePath)
	assert.False(t, fe.CreatedAt.IsZero())

	be := repoByName["backend"]
	assert.Equal(t, "tr-4002", be.ID)
	assert.Equal(t, "add auth endpoint", be.Spec)
}

func TestGetTaskReposForTask_Empty(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-4010")
	require.NoError(t, store.InsertTask(task))

	repos, err := store.GetTaskReposForTask("task-4010")
	require.NoError(t, err)
	assert.Empty(t, repos)
}

func TestInsertAndGetLeadsForTask(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-5001")
	require.NoError(t, store.InsertTask(task))

	tr := &model.TaskRepo{ID: "tr-5001", TaskID: "task-5001", RepoName: "api", Spec: "implement endpoints"}
	require.NoError(t, store.InsertTaskRepo(tr))

	leads := []*model.Lead{
		{ID: "lead-5001", TaskRepoID: "tr-5001", Status: model.LeadStatusPending, Attempt: 1, Output: ""},
		{ID: "lead-5002", TaskRepoID: "tr-5001", Status: model.LeadStatusRunning, Attempt: 1, Output: "in progress"},
	}
	for _, l := range leads {
		require.NoError(t, store.InsertLead(l))
	}

	got, err := store.GetLeadsForTask("task-5001")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	leadByID := make(map[string]model.Lead)
	for _, l := range got {
		leadByID[l.ID] = l
	}

	l1 := leadByID["lead-5001"]
	assert.Equal(t, model.LeadStatusPending, l1.Status)
	assert.Equal(t, 1, l1.Attempt)
	assert.Nil(t, l1.StartedAt)
	assert.Nil(t, l1.FinishedAt)
	assert.False(t, l1.CreatedAt.IsZero())

	l2 := leadByID["lead-5002"]
	assert.Equal(t, model.LeadStatusRunning, l2.Status)
	assert.Equal(t, "in progress", l2.Output)
}

func TestGetLeadsForTask_MultipleRepos(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-5010")
	require.NoError(t, store.InsertTask(task))

	tr1 := &model.TaskRepo{ID: "tr-5010", TaskID: "task-5010", RepoName: "svc-a", Spec: "spec A"}
	tr2 := &model.TaskRepo{ID: "tr-5011", TaskID: "task-5010", RepoName: "svc-b", Spec: "spec B"}
	require.NoError(t, store.InsertTaskRepo(tr1))
	require.NoError(t, store.InsertTaskRepo(tr2))

	require.NoError(t, store.InsertLead(&model.Lead{ID: "lead-5010", TaskRepoID: "tr-5010", Status: model.LeadStatusPending, Attempt: 1}))
	require.NoError(t, store.InsertLead(&model.Lead{ID: "lead-5011", TaskRepoID: "tr-5011", Status: model.LeadStatusPending, Attempt: 1}))

	got, err := store.GetLeadsForTask("task-5010")
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestGetLeadsForTask_Empty(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-5020")
	require.NoError(t, store.InsertTask(task))

	tr := &model.TaskRepo{ID: "tr-5020", TaskID: "task-5020", RepoName: "repo", Spec: "spec"}
	require.NoError(t, store.InsertTaskRepo(tr))

	leads, err := store.GetLeadsForTask("task-5020")
	require.NoError(t, err)
	assert.Empty(t, leads)
}

func TestInsertAndGetAgenticDecisions(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-6001")
	require.NoError(t, store.InsertTask(task))

	decisions := []*model.AgenticDecision{
		{
			ID:         "ad-6001",
			TaskID:     "task-6001",
			NodeType:   model.AgenticSufficiency,
			Input:      `{"description":"implement feature X"}`,
			Output:     `{"sufficient":true}`,
			Model:      "claude-sonnet-4-20250514",
			DurationMs: 1200,
		},
		{
			ID:         "ad-6002",
			TaskID:     "task-6001",
			NodeType:   model.AgenticDecomposition,
			Input:      `{"description":"implement feature X","repos":["frontend","backend"]}`,
			Output:     `{"repos":[{"name":"frontend","spec":"build UI"},{"name":"backend","spec":"add API"}]}`,
			Model:      "claude-sonnet-4-20250514",
			DurationMs: 3400,
		},
	}
	for _, d := range decisions {
		require.NoError(t, store.InsertAgenticDecision(d))
	}

	got, err := store.GetAgenticDecisionsForTask("task-6001")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	decByID := make(map[string]model.AgenticDecision)
	for _, d := range got {
		decByID[d.ID] = d
	}

	d1 := decByID["ad-6001"]
	assert.Equal(t, model.AgenticSufficiency, d1.NodeType)
	assert.Equal(t, `{"description":"implement feature X"}`, d1.Input)
	assert.Equal(t, `{"sufficient":true}`, d1.Output)
	assert.Equal(t, "claude-sonnet-4-20250514", d1.Model)
	assert.Equal(t, int64(1200), d1.DurationMs)
	assert.False(t, d1.CreatedAt.IsZero())

	d2 := decByID["ad-6002"]
	assert.Equal(t, model.AgenticDecomposition, d2.NodeType)
	assert.Equal(t, int64(3400), d2.DurationMs)
}

func TestGetAgenticDecisionsForTask_Empty(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-6010")
	require.NoError(t, store.InsertTask(task))

	decisions, err := store.GetAgenticDecisionsForTask("task-6010")
	require.NoError(t, err)
	assert.Empty(t, decisions)
}

func TestInsertTask_DuplicateID(t *testing.T) {
	store := setupTestDB(t)

	task := makeTask("task-7001")
	require.NoError(t, store.InsertTask(task))

	err := store.InsertTask(task)
	require.Error(t, err)
}

func TestInsertTaskRepo_ForeignKeyConstraint(t *testing.T) {
	store := setupTestDB(t)

	// Task does not exist; foreign key should reject this.
	tr := &model.TaskRepo{ID: "tr-8001", TaskID: "nonexistent-task", RepoName: "repo", Spec: "spec"}
	err := store.InsertTaskRepo(tr)
	require.Error(t, err)
}

func TestInsertLead_ForeignKeyConstraint(t *testing.T) {
	store := setupTestDB(t)

	// task_repo does not exist; foreign key should reject this.
	lead := &model.Lead{ID: "lead-8001", TaskRepoID: "nonexistent-tr", Status: model.LeadStatusPending, Attempt: 1}
	err := store.InsertLead(lead)
	require.Error(t, err)
}

func TestInsertAgenticDecision_ForeignKeyConstraint(t *testing.T) {
	store := setupTestDB(t)

	d := &model.AgenticDecision{
		ID:         "ad-8001",
		TaskID:     "nonexistent-task",
		NodeType:   model.AgenticSufficiency,
		Input:      "test",
		Output:     "test",
		Model:      "test",
		DurationMs: 100,
	}
	err := store.InsertAgenticDecision(d)
	require.Error(t, err)
}

func TestGetTasksByStatus_OnlyMatchingStatus(t *testing.T) {
	store := setupTestDB(t)

	// Insert tasks with every status to confirm filtering is exact.
	statuses := []model.TaskStatus{
		model.TaskStatusPending,
		model.TaskStatusDecomposing,
		model.TaskStatusRunning,
		model.TaskStatusAligning,
		model.TaskStatusComplete,
		model.TaskStatusFailed,
	}
	for i, status := range statuses {
		task := makeTask(fmt.Sprintf("task-9%03d", i))
		task.Status = status
		require.NoError(t, store.InsertTask(task))
	}

	for _, status := range statuses {
		tasks, err := store.GetTasksByStatus(status)
		require.NoError(t, err)
		assert.Len(t, tasks, 1, "expected exactly 1 task with status %s", status)
		assert.Equal(t, status, tasks[0].Status)
	}
}
