package store

import (
	"testing"

	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInsertAndGetTask(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	task := &model.Task{
		ID:         "task-1",
		InstanceID: "test-instance",
		Spec:       "# My Spec\nDo the thing.",
		GoalsJSON:  `{"repos":{"api":{"goals":[{"id":"api-1","description":"Add endpoint","depends_on":[]}]}}}`,
		JiraRef:    "PROJ-123",
		Status:     model.TaskStatusPending,
	}
	goals := []model.Goal{
		{ID: "api-1", TaskID: "task-1", RepoName: "api", Description: "Add endpoint", DependsOn: []string{}, Status: model.GoalStatusPending},
	}

	err := s.InsertTask(task, goals)
	require.NoError(t, err)

	got, err := s.GetTask("task-1")
	require.NoError(t, err)
	assert.Equal(t, "task-1", got.ID)
	assert.Equal(t, "test-instance", got.InstanceID)
	assert.Equal(t, "# My Spec\nDo the thing.", got.Spec)
	assert.Equal(t, "PROJ-123", got.JiraRef)
	assert.Equal(t, model.TaskStatusPending, got.Status)
}

func TestInsertTaskWithGoals(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	task := &model.Task{
		ID:         "task-2",
		InstanceID: "test-instance",
		Spec:       "spec content",
		GoalsJSON:  "{}",
		Status:     model.TaskStatusPending,
	}
	goals := []model.Goal{
		{ID: "api-1", TaskID: "task-2", RepoName: "api", Description: "First goal", DependsOn: []string{}, Status: model.GoalStatusPending},
		{ID: "api-2", TaskID: "task-2", RepoName: "api", Description: "Second goal", DependsOn: []string{"api-1"}, Status: model.GoalStatusPending},
		{ID: "app-1", TaskID: "task-2", RepoName: "app", Description: "App goal", DependsOn: []string{}, Status: model.GoalStatusPending},
	}

	err := s.InsertTask(task, goals)
	require.NoError(t, err)

	gotGoals, err := s.GetGoalsForTask("task-2")
	require.NoError(t, err)
	assert.Len(t, gotGoals, 3)

	goalMap := make(map[string]model.Goal)
	for _, g := range gotGoals {
		goalMap[g.ID] = g
	}

	assert.Equal(t, "api", goalMap["api-1"].RepoName)
	assert.Equal(t, []string{}, goalMap["api-1"].DependsOn)
	assert.Equal(t, []string{"api-1"}, goalMap["api-2"].DependsOn)
	assert.Equal(t, "app", goalMap["app-1"].RepoName)
}

func TestInsertTaskCreatesEvent(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	task := &model.Task{
		ID:         "task-3",
		InstanceID: "test-instance",
		Spec:       "spec",
		GoalsJSON:  "{}",
		Status:     model.TaskStatusPending,
	}

	err := s.InsertTask(task, nil)
	require.NoError(t, err)

	events, err := s.GetEventsForTask("task-3")
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, model.EventTaskCreated, events[0].Type)
	assert.Equal(t, "task-3", events[0].TaskID)
}

func TestListTasksForInstance(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	for _, id := range []string{"task-a", "task-b"} {
		err := s.InsertTask(&model.Task{
			ID: id, InstanceID: "test-instance", Spec: "s", GoalsJSON: "{}", Status: model.TaskStatusPending,
		}, nil)
		require.NoError(t, err)
	}

	tasks, err := s.ListTasksForInstance("test-instance")
	require.NoError(t, err)
	assert.Len(t, tasks, 2)
}

func TestUpdateTaskStatus(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertTask(&model.Task{
		ID: "task-4", InstanceID: "test-instance", Spec: "s", GoalsJSON: "{}", Status: model.TaskStatusPending,
	}, nil)
	require.NoError(t, err)

	err = s.UpdateTaskStatus("task-4", model.TaskStatusRunning)
	require.NoError(t, err)

	got, err := s.GetTask("task-4")
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusRunning, got.Status)
}

func TestUpdateGoalStatus(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertTask(&model.Task{
		ID: "task-5", InstanceID: "test-instance", Spec: "s", GoalsJSON: "{}", Status: model.TaskStatusPending,
	}, []model.Goal{
		{ID: "g-1", TaskID: "task-5", RepoName: "api", Description: "goal", DependsOn: []string{}, Status: model.GoalStatusPending},
	})
	require.NoError(t, err)

	err = s.UpdateGoalStatus("g-1", model.GoalStatusComplete)
	require.NoError(t, err)

	goals, err := s.GetGoalsForTask("task-5")
	require.NoError(t, err)
	require.Len(t, goals, 1)
	assert.Equal(t, model.GoalStatusComplete, goals[0].Status)
	assert.NotNil(t, goals[0].CompletedAt)
}

func TestGetTaskNotFound(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	_, err := s.GetTask("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetTasksByStatus(t *testing.T) {
	db := testutil.SetupTestDB(t)
	s := New(db)

	err := s.InsertTask(&model.Task{
		ID: "task-p", InstanceID: "test-instance", Spec: "s", GoalsJSON: "{}", Status: model.TaskStatusPending,
	}, nil)
	require.NoError(t, err)

	err = s.InsertTask(&model.Task{
		ID: "task-r", InstanceID: "test-instance", Spec: "s", GoalsJSON: "{}", Status: model.TaskStatusPending,
	}, nil)
	require.NoError(t, err)
	err = s.UpdateTaskStatus("task-r", model.TaskStatusRunning)
	require.NoError(t, err)

	pending, err := s.GetTasksByStatus(model.TaskStatusPending)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
	assert.Equal(t, "task-p", pending[0].ID)

	running, err := s.GetTasksByStatus(model.TaskStatusRunning)
	require.NoError(t, err)
	assert.Len(t, running, 1)
	assert.Equal(t, "task-r", running[0].ID)
}

func TestValidateGoalsFile(t *testing.T) {
	tests := []struct {
		name    string
		gf      model.GoalsFile
		wantErr string
	}{
		{
			name: "valid single repo",
			gf: model.GoalsFile{
				Repos: map[string]model.RepoGoals{
					"api": {Goals: []model.GoalSpec{
						{ID: "api-1", Description: "do thing", DependsOn: []string{}},
					}},
				},
			},
		},
		{
			name: "valid with dependencies",
			gf: model.GoalsFile{
				Repos: map[string]model.RepoGoals{
					"api": {Goals: []model.GoalSpec{
						{ID: "api-1", Description: "first", DependsOn: []string{}},
						{ID: "api-2", Description: "second", DependsOn: []string{"api-1"}},
					}},
				},
			},
		},
		{
			name: "duplicate goal ID",
			gf: model.GoalsFile{
				Repos: map[string]model.RepoGoals{
					"api": {Goals: []model.GoalSpec{{ID: "g-1", Description: "a"}}},
					"app": {Goals: []model.GoalSpec{{ID: "g-1", Description: "b"}}},
				},
			},
			wantErr: "duplicate goal ID",
		},
		{
			name: "empty goal ID",
			gf: model.GoalsFile{
				Repos: map[string]model.RepoGoals{
					"api": {Goals: []model.GoalSpec{{ID: "", Description: "a"}}},
				},
			},
			wantErr: "empty ID",
		},
		{
			name: "empty description",
			gf: model.GoalsFile{
				Repos: map[string]model.RepoGoals{
					"api": {Goals: []model.GoalSpec{{ID: "api-1", Description: ""}}},
				},
			},
			wantErr: "empty description",
		},
		{
			name: "depends on nonexistent goal",
			gf: model.GoalsFile{
				Repos: map[string]model.RepoGoals{
					"api": {Goals: []model.GoalSpec{
						{ID: "api-1", Description: "a", DependsOn: []string{"nope"}},
					}},
				},
			},
			wantErr: "does not exist",
		},
		{
			name: "cross-repo dependency",
			gf: model.GoalsFile{
				Repos: map[string]model.RepoGoals{
					"api": {Goals: []model.GoalSpec{{ID: "api-1", Description: "a"}}},
					"app": {Goals: []model.GoalSpec{{ID: "app-1", Description: "b", DependsOn: []string{"api-1"}}}},
				},
			},
			wantErr: "different repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGoalsFile(&tt.gf)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateGoalsRepos(t *testing.T) {
	gf := &model.GoalsFile{
		Repos: map[string]model.RepoGoals{
			"api":     {Goals: []model.GoalSpec{{ID: "api-1", Description: "a"}}},
			"unknown": {Goals: []model.GoalSpec{{ID: "u-1", Description: "b"}}},
		},
	}

	err := ValidateGoalsRepos(gf, []string{"api", "app"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")

	err = ValidateGoalsRepos(gf, []string{"api", "unknown"})
	require.NoError(t, err)
}

func TestGoalsFromFile(t *testing.T) {
	gf := &model.GoalsFile{
		Repos: map[string]model.RepoGoals{
			"api": {Goals: []model.GoalSpec{
				{ID: "api-1", Description: "first", DependsOn: []string{}},
				{ID: "api-2", Description: "second", DependsOn: []string{"api-1"}},
			}},
			"app": {Goals: []model.GoalSpec{
				{ID: "app-1", Description: "app goal"},
			}},
		},
	}

	goals := GoalsFromFile("task-99", gf)
	assert.Len(t, goals, 3)

	goalMap := make(map[string]model.Goal)
	for _, g := range goals {
		goalMap[g.ID] = g
	}

	assert.Equal(t, "task-99", goalMap["api-1"].TaskID)
	assert.Equal(t, "api", goalMap["api-1"].RepoName)
	assert.Equal(t, []string{}, goalMap["api-1"].DependsOn)
	assert.Equal(t, []string{"api-1"}, goalMap["api-2"].DependsOn)
	assert.Equal(t, []string{}, goalMap["app-1"].DependsOn) // nil converted to empty
	assert.Equal(t, model.GoalStatusPending, goalMap["app-1"].Status)
}
