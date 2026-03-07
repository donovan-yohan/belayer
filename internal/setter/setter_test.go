package setter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/logmgr"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/donovan-yohan/belayer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTmux implements tmux.TmuxManager for tests.
type mockTmux struct {
	sessions map[string]map[string]bool // session -> set of window names
	keys     map[string]string          // target -> last keys sent
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		sessions: make(map[string]map[string]bool),
		keys:     make(map[string]string),
	}
}

func (m *mockTmux) HasSession(name string) bool {
	_, ok := m.sessions[name]
	return ok
}

func (m *mockTmux) NewSession(name string) error {
	m.sessions[name] = make(map[string]bool)
	// tmux creates a default window
	m.sessions[name]["0"] = true
	return nil
}

func (m *mockTmux) KillSession(name string) error {
	delete(m.sessions, name)
	return nil
}

func (m *mockTmux) NewWindow(session, windowName string) error {
	if s, ok := m.sessions[session]; ok {
		s[windowName] = true
	}
	return nil
}

func (m *mockTmux) KillWindow(session, windowName string) error {
	if s, ok := m.sessions[session]; ok {
		delete(s, windowName)
	}
	return nil
}

func (m *mockTmux) SendKeys(session, windowName, keys string) error {
	m.keys[session+":"+windowName] = keys
	return nil
}

func (m *mockTmux) ListWindows(session string) ([]string, error) {
	s, ok := m.sessions[session]
	if !ok {
		return nil, nil
	}
	var names []string
	for name := range s {
		names = append(names, name)
	}
	return names, nil
}

func (m *mockTmux) PipePane(session, windowName, logPath string) error {
	return nil
}

// mockSpawner implements lead.AgentSpawner for tests.
type mockSpawner struct {
	spawned []lead.SpawnOpts
}

func newMockSpawner() *mockSpawner {
	return &mockSpawner{}
}

func (m *mockSpawner) Spawn(_ context.Context, opts lead.SpawnOpts) error {
	m.spawned = append(m.spawned, opts)
	return nil
}

func setupTestEnv(t *testing.T) (*store.Store, *mockTmux, *logmgr.LogManager, *mockSpawner, string) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	s := store.New(db)
	tm := newMockTmux()
	sp := newMockSpawner()
	tmpDir := t.TempDir()
	lm := logmgr.New(filepath.Join(tmpDir, "logs"))
	return s, tm, lm, sp, tmpDir
}

func insertTestTask(t *testing.T, s *store.Store, taskID string, goals []model.Goal) {
	t.Helper()
	goalsJSON, _ := json.Marshal(model.GoalsFile{})
	task := &model.Task{
		ID:         taskID,
		InstanceID: "test-instance",
		Spec:       "test spec",
		GoalsJSON:  string(goalsJSON),
		Status:     model.TaskStatusPending,
	}
	require.NoError(t, s.InsertTask(task, goals))
}

func TestTaskRunner_Init(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Goal{
		{ID: "api-1", TaskID: "task-1", RepoName: "api", Description: "goal 1", DependsOn: []string{}, Status: model.GoalStatusPending},
		{ID: "api-2", TaskID: "task-1", RepoName: "api", Description: "goal 2", DependsOn: []string{"api-1"}, Status: model.GoalStatusPending},
		{ID: "app-1", TaskID: "task-1", RepoName: "app", Description: "goal 3", DependsOn: []string{}, Status: model.GoalStatusPending},
	}
	insertTestTask(t, s, "task-1", goals)

	// Create fake instance dir structure with worktree dirs
	for _, repoName := range []string{"api", "app"} {
		wtDir := filepath.Join(tmpDir, "tasks", "task-1", repoName)
		require.NoError(t, os.MkdirAll(wtDir, 0o755))
	}

	task, err := s.GetTask("task-1")
	require.NoError(t, err)

	// We need a mock that doesn't actually create git worktrees
	runner := &TaskRunner{
		task:        task,
		worktrees:   make(map[string]string),
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		startedAt:   make(map[string]time.Time),
	}

	// Manually set up what Init would do (without real git operations)
	require.NoError(t, s.UpdateTaskStatus("task-1", model.TaskStatusRunning))
	goalsFromDB, err := s.GetGoalsForTask("task-1")
	require.NoError(t, err)
	runner.dag = BuildDAG(goalsFromDB)
	runner.tmuxSession = "belayer-task-task-1"
	require.NoError(t, tm.NewSession(runner.tmuxSession))
	require.NoError(t, lm.EnsureDir("task-1"))
	runner.worktrees["api"] = filepath.Join(tmpDir, "tasks", "task-1", "api")
	runner.worktrees["app"] = filepath.Join(tmpDir, "tasks", "task-1", "app")

	readyGoals := runner.dag.ReadyGoals()

	// api-1 and app-1 should be ready (no deps)
	assert.Len(t, readyGoals, 2)
	readyIDs := make(map[string]bool)
	for _, g := range readyGoals {
		readyIDs[g.ID] = true
	}
	assert.True(t, readyIDs["api-1"])
	assert.True(t, readyIDs["app-1"])
	assert.False(t, readyIDs["api-2"])
}

func TestTaskRunner_SpawnGoal(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Goal{
		{ID: "api-1", TaskID: "task-1", RepoName: "api", Description: "test goal", DependsOn: []string{}, Status: model.GoalStatusPending},
	}
	insertTestTask(t, s, "task-1", goals)

	task, _ := s.GetTask("task-1")
	runner := &TaskRunner{
		task:        task,
		worktrees:   map[string]string{"api": filepath.Join(tmpDir, "api")},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		tmuxSession: "belayer-task-task-1",
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "api"), 0o755))

	goalsFromDB, _ := s.GetGoalsForTask("task-1")
	runner.dag = BuildDAG(goalsFromDB)
	require.NoError(t, tm.NewSession("belayer-task-task-1"))
	require.NoError(t, lm.EnsureDir("task-1"))

	err := runner.SpawnGoal(goals[0])
	require.NoError(t, err)

	// Check window was created
	windows, _ := tm.ListWindows("belayer-task-task-1")
	assert.Contains(t, windows, "api-api-1")

	// Check goal is now running in DAG
	assert.Equal(t, model.GoalStatusRunning, runner.dag.Get("api-1").Status)

	// Check event was inserted
	events, _ := s.GetEventsForTask("task-1")
	foundStarted := false
	for _, e := range events {
		if e.Type == model.EventGoalStarted && e.GoalID == "api-1" {
			foundStarted = true
		}
	}
	assert.True(t, foundStarted)

	// Check spawner was called with correct opts
	require.Len(t, sp.spawned, 1)
	assert.Equal(t, "belayer-task-task-1", sp.spawned[0].TmuxSession)
	assert.Equal(t, "api-api-1", sp.spawned[0].WindowName)
	assert.Equal(t, filepath.Join(tmpDir, "api"), sp.spawned[0].WorkDir)
	assert.Contains(t, sp.spawned[0].Prompt, "test goal")
	assert.Contains(t, sp.spawned[0].Prompt, "test spec")
	assert.Contains(t, sp.spawned[0].Prompt, "DONE.json")
}

func TestTaskRunner_CheckCompletions(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Goal{
		{ID: "api-1", TaskID: "task-2", RepoName: "api", Description: "first", DependsOn: []string{}, Status: model.GoalStatusPending},
		{ID: "api-2", TaskID: "task-2", RepoName: "api", Description: "second", DependsOn: []string{"api-1"}, Status: model.GoalStatusPending},
	}
	insertTestTask(t, s, "task-2", goals)

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-2", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	task, _ := s.GetTask("task-2")
	runner := &TaskRunner{
		task:        task,
		worktrees:   map[string]string{"api": worktreeDir},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		tmuxSession: "belayer-task-task-2",
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession("belayer-task-task-2"))
	require.NoError(t, lm.EnsureDir("task-2"))

	goalsFromDB, _ := s.GetGoalsForTask("task-2")
	runner.dag = BuildDAG(goalsFromDB)

	// Spawn api-1
	require.NoError(t, runner.SpawnGoal(goals[0]))

	// Write DONE.json for api-1
	doneJSON := DoneJSON{
		Status:       "complete",
		Summary:      "Did the thing",
		FilesChanged: []string{"api/main.go"},
	}
	data, _ := json.Marshal(doneJSON)
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "DONE.json"), data, 0o644))

	// Check completions — should find api-1 complete and api-2 ready
	newlyReady, err := runner.CheckCompletions()
	require.NoError(t, err)

	assert.Equal(t, model.GoalStatusComplete, runner.dag.Get("api-1").Status)
	require.Len(t, newlyReady, 1)
	assert.Equal(t, "api-2", newlyReady[0].Goal.ID)
}

func TestTaskRunner_CheckStaleGoals(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Goal{
		{ID: "api-1", TaskID: "task-3", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.GoalStatusPending},
	}
	insertTestTask(t, s, "task-3", goals)

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-3", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	task, _ := s.GetTask("task-3")
	runner := &TaskRunner{
		task:        task,
		worktrees:   map[string]string{"api": worktreeDir},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		tmuxSession: "belayer-task-task-3",
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession("belayer-task-task-3"))
	require.NoError(t, lm.EnsureDir("task-3"))

	goalsFromDB, _ := s.GetGoalsForTask("task-3")
	runner.dag = BuildDAG(goalsFromDB)
	require.NoError(t, runner.SpawnGoal(goals[0]))

	// Kill the window to simulate crash
	tm.KillWindow("belayer-task-task-3", "api-api-1")

	// Check stale goals
	retryGoals, err := runner.CheckStaleGoals(30 * time.Minute)
	require.NoError(t, err)

	// Goal should be retried (attempt 0 -> 1)
	require.Len(t, retryGoals, 1)
	assert.Equal(t, "api-1", retryGoals[0].Goal.ID)

	// Check goal is pending in DAG now (reset for retry)
	assert.Equal(t, model.GoalStatusPending, runner.dag.Get("api-1").Status)
}

func TestTaskRunner_StaleTimeout(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Goal{
		{ID: "api-1", TaskID: "task-4", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.GoalStatusPending},
	}
	insertTestTask(t, s, "task-4", goals)

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-4", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	task, _ := s.GetTask("task-4")
	runner := &TaskRunner{
		task:        task,
		worktrees:   map[string]string{"api": worktreeDir},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		tmuxSession: "belayer-task-task-4",
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession("belayer-task-task-4"))
	require.NoError(t, lm.EnsureDir("task-4"))

	goalsFromDB, _ := s.GetGoalsForTask("task-4")
	runner.dag = BuildDAG(goalsFromDB)
	require.NoError(t, runner.SpawnGoal(goals[0]))

	// Backdate the start time to simulate timeout
	runner.startedAt["api-1"] = time.Now().Add(-1 * time.Hour)

	// Window is still alive, but goal timed out
	retryGoals, err := runner.CheckStaleGoals(30 * time.Minute)
	require.NoError(t, err)

	require.Len(t, retryGoals, 1)
	assert.Equal(t, "api-1", retryGoals[0].Goal.ID)
}

func TestTaskRunner_HasStuckGoals(t *testing.T) {
	s, tm, lm, sp, _ := setupTestEnv(t)

	goals := []model.Goal{
		{ID: "api-1", TaskID: "task-5", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.GoalStatusPending},
	}
	insertTestTask(t, s, "task-5", goals)

	task, _ := s.GetTask("task-5")
	runner := &TaskRunner{
		task:      task,
		store:     s,
		tmux:      tm,
		logMgr:    lm,
		spawner:   sp,
		startedAt: make(map[string]time.Time),
	}

	goalsFromDB, _ := s.GetGoalsForTask("task-5")
	runner.dag = BuildDAG(goalsFromDB)

	// Simulate goal failing at max attempts
	runner.dag.Get("api-1").Status = model.GoalStatusFailed
	runner.dag.Get("api-1").Attempt = 3

	assert.True(t, runner.HasStuckGoals())

	// Reset to under max
	runner.dag.Get("api-1").Attempt = 2
	assert.False(t, runner.HasStuckGoals())
}

func TestSetter_MaxLeadsCap(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	// Create a task with 3 independent goals
	goals := []model.Goal{
		{ID: "g-1", TaskID: "task-6", RepoName: "api", Description: "one", DependsOn: []string{}, Status: model.GoalStatusPending},
		{ID: "g-2", TaskID: "task-6", RepoName: "api", Description: "two", DependsOn: []string{}, Status: model.GoalStatusPending},
		{ID: "g-3", TaskID: "task-6", RepoName: "api", Description: "three", DependsOn: []string{}, Status: model.GoalStatusPending},
	}
	insertTestTask(t, s, "task-6", goals)

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-6", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	task, _ := s.GetTask("task-6")
	runner := &TaskRunner{
		task:        task,
		worktrees:   map[string]string{"api": worktreeDir},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		tmuxSession: "belayer-task-task-6",
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession("belayer-task-task-6"))
	require.NoError(t, lm.EnsureDir("task-6"))

	goalsFromDB, _ := s.GetGoalsForTask("task-6")
	runner.dag = BuildDAG(goalsFromDB)

	// Create setter with maxLeads=2
	setter := &Setter{
		config: Config{
			MaxLeads:     2,
			InstanceName: "test-instance",
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		tasks:   map[string]*TaskRunner{"task-6": runner},
	}

	// Queue all 3 goals
	readyGoals := runner.dag.ReadyGoals()
	for _, g := range readyGoals {
		setter.leadQueue = append(setter.leadQueue, QueuedGoal{Goal: g, TaskID: "task-6"})
	}

	// Process queue — should only spawn 2 (maxLeads cap)
	setter.processLeadQueue()

	assert.Equal(t, 2, setter.activeLeads)
	assert.Len(t, setter.leadQueue, 1) // 1 still queued
}

func TestSetter_CrashRecovery(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Goal{
		{ID: "api-1", TaskID: "task-7", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.GoalStatusPending},
		{ID: "api-2", TaskID: "task-7", RepoName: "api", Description: "dep on 1", DependsOn: []string{"api-1"}, Status: model.GoalStatusPending},
	}
	insertTestTask(t, s, "task-7", goals)

	// Simulate task was running when setter crashed
	require.NoError(t, s.UpdateTaskStatus("task-7", model.TaskStatusRunning))
	require.NoError(t, s.UpdateGoalStatus("api-1", model.GoalStatusRunning))

	// Create worktree dir
	worktreeDir := filepath.Join(tmpDir, "tasks", "task-7", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	// Write DONE.json that was written while setter was down
	doneJSON := DoneJSON{Status: "complete", Summary: "done while crashed"}
	data, _ := json.Marshal(doneJSON)
	require.NoError(t, os.WriteFile(filepath.Join(worktreeDir, "DONE.json"), data, 0o644))

	setter := &Setter{
		config: Config{
			InstanceName: "test-instance",
			InstanceDir:  tmpDir,
			MaxLeads:     8,
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		tasks:   make(map[string]*TaskRunner),
	}

	// Run recovery
	err := setter.recover()
	require.NoError(t, err)

	// Task should have been recovered
	require.Contains(t, setter.tasks, "task-7")

	// api-1 should be marked complete (from DONE.json)
	runner := setter.tasks["task-7"]
	assert.Equal(t, model.GoalStatusComplete, runner.dag.Get("api-1").Status)

	// api-2 should be queued as ready
	assert.True(t, len(setter.leadQueue) > 0)
	foundApi2 := false
	for _, q := range setter.leadQueue {
		if q.Goal.ID == "api-2" {
			foundApi2 = true
		}
	}
	assert.True(t, foundApi2)
}

func TestSetter_RunTickCycle(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	setter := &Setter{
		config: Config{
			InstanceName: "test-instance",
			InstanceDir:  tmpDir,
			MaxLeads:     8,
			PollInterval: 100 * time.Millisecond,
			StaleTimeout: 30 * time.Minute,
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		tasks:   make(map[string]*TaskRunner),
	}

	// Run one tick with no tasks — should not error
	err := setter.tick()
	require.NoError(t, err)

	// Run with context cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = setter.Run(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
