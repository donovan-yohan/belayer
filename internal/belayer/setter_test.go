package belayer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/anchor"
	"github.com/donovan-yohan/belayer/internal/climbctx"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/logmgr"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/spotter"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/donovan-yohan/belayer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTmux implements tmux.TmuxManager for tests.
type mockTmux struct {
	sessions       map[string]map[string]bool // session -> set of window names
	keys           map[string]string          // target -> last keys sent
	remainOnExit   map[string]bool            // target -> enabled
	envVars        map[string]string          // "session:key" -> value
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		sessions:     make(map[string]map[string]bool),
		keys:         make(map[string]string),
		remainOnExit: make(map[string]bool),
		envVars:      make(map[string]string),
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

func (m *mockTmux) SetRemainOnExit(session, windowName string, enabled bool) error {
	m.remainOnExit[session+":"+windowName] = enabled
	return nil
}

func (m *mockTmux) IsPaneDead(session, windowName string) (bool, error) {
	return false, nil
}

func (m *mockTmux) CapturePaneContent(session, windowName string, lines int) (string, error) {
	return "", nil
}

func (m *mockTmux) SetEnvironment(session, key, value string) error {
	m.envVars[session+":"+key] = value
	return nil
}

func (m *mockTmux) SendKeysLiteral(target, text string) error {
	return nil
}

func (m *mockTmux) SendKeysRaw(target, key string) error {
	return nil
}

func (m *mockTmux) GetPanePID(session, windowName string) (int, error) {
	return 0, nil
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

// mockGitRunner returns canned git output for tests.
type mockGitRunner struct {
	responses map[string]string // key "<workdir>:<args>" -> output
}

func newMockGitRunner() *mockGitRunner {
	return &mockGitRunner{responses: make(map[string]string)}
}

func (m *mockGitRunner) Run(workdir string, args ...string) (string, error) {
	key := workdir + ":" + args[0]
	if resp, ok := m.responses[key]; ok {
		return resp, nil
	}
	return "", nil
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

func insertTestTask(t *testing.T, s *store.Store, taskID string, goals []model.Climb) {
	t.Helper()
	goalsJSON, _ := json.Marshal(model.ClimbsFile{})
	task := &model.Problem{
		ID:         taskID,
		InstanceID: "test-instance",
		Spec:       "test spec",
		ClimbsJSON: string(goalsJSON),
		Status:     model.ProblemStatusPending,
	}
	require.NoError(t, s.InsertProblem(task, goals))
}

func TestProblemRunner_Init(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-1", RepoName: "api", Description: "goal 1", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "api-2", ProblemID: "task-1", RepoName: "api", Description: "goal 2", DependsOn: []string{"api-1"}, Status: model.ClimbStatusPending},
		{ID: "app-1", ProblemID: "task-1", RepoName: "app", Description: "goal 3", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	insertTestTask(t, s, "task-1", goals)

	// Create fake instance dir structure with worktree dirs
	for _, repoName := range []string{"api", "app"} {
		wtDir := filepath.Join(tmpDir, "tasks", "task-1", repoName)
		require.NoError(t, os.MkdirAll(wtDir, 0o755))
	}

	task, err := s.GetProblem("task-1")
	require.NoError(t, err)

	// We need a mock that doesn't actually create git worktrees
	runner := &ProblemRunner{
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
	require.NoError(t, s.UpdateProblemStatus("task-1", model.ProblemStatusRunning))
	goalsFromDB, err := s.GetClimbsForProblem("task-1")
	require.NoError(t, err)
	runner.dag = BuildDAG(goalsFromDB)
	runner.tmuxSession = "belayer-problem-task-1"
	require.NoError(t, tm.NewSession(runner.tmuxSession))
	require.NoError(t, lm.EnsureDir("task-1"))
	runner.worktrees["api"] = filepath.Join(tmpDir, "tasks", "task-1", "api")
	runner.worktrees["app"] = filepath.Join(tmpDir, "tasks", "task-1", "app")

	readyGoals := runner.dag.ReadyClimbs()

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

func TestProblemRunner_SpawnClimb(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-1", RepoName: "api", Description: "test goal", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	insertTestTask(t, s, "task-1", goals)

	task, _ := s.GetProblem("task-1")
	runner := &ProblemRunner{
		task:        task,
		worktrees:   map[string]string{"api": filepath.Join(tmpDir, "api")},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		tmuxSession: "belayer-problem-task-1",
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "api"), 0o755))

	goalsFromDB, _ := s.GetClimbsForProblem("task-1")
	runner.dag = BuildDAG(goalsFromDB)
	require.NoError(t, tm.NewSession("belayer-problem-task-1"))
	require.NoError(t, lm.EnsureDir("task-1"))

	err := runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-1"})
	require.NoError(t, err)

	// Check window was created
	windows, _ := tm.ListWindows("belayer-problem-task-1")
	assert.Contains(t, windows, "api-api-1")

	// Check goal is now running in DAG
	assert.Equal(t, model.ClimbStatusRunning, runner.dag.Get("api-1").Status)

	// Check event was inserted
	events, _ := s.GetEventsForProblem("task-1")
	foundStarted := false
	for _, e := range events {
		if e.Type == model.EventClimbStarted && e.ClimbID == "api-1" {
			foundStarted = true
		}
	}
	assert.True(t, foundStarted)

	// Check spawner was called with correct opts
	require.Len(t, sp.spawned, 1)
	assert.Equal(t, "belayer-problem-task-1", sp.spawned[0].TmuxSession)
	assert.Equal(t, "api-api-1", sp.spawned[0].WindowName)
	assert.Equal(t, filepath.Join(tmpDir, "api"), sp.spawned[0].WorkDir)

	// Verify GOAL.json was written to goal-scoped path
	goalJSON, err := os.ReadFile(filepath.Join(tmpDir, "api", ".lead", "api-1", "GOAL.json"))
	require.NoError(t, err)
	assert.Contains(t, string(goalJSON), "test goal")
	assert.Contains(t, string(goalJSON), "test spec")

	// Verify AppendSystemPrompt is set
	assert.NotEmpty(t, sp.spawned[0].AppendSystemPrompt)
}

func TestProblemRunner_SpawnClimb_SetsMailAddress(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-1", RepoName: "api", Description: "test goal", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	insertTestTask(t, s, "task-1", goals)

	task, _ := s.GetProblem("task-1")
	runner := &ProblemRunner{
		task:        task,
		worktrees:   map[string]string{"api": filepath.Join(tmpDir, "api")},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		tmuxSession: "belayer-problem-task-1",
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "api"), 0o755))

	goalsFromDB, _ := s.GetClimbsForProblem("task-1")
	runner.dag = BuildDAG(goalsFromDB)
	require.NoError(t, tm.NewSession("belayer-problem-task-1"))
	require.NoError(t, lm.EnsureDir("task-1"))

	err := runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-1"})
	require.NoError(t, err)

	// Verify BELAYER_MAIL_ADDRESS was passed via Env in SpawnOpts
	require.NotEmpty(t, sp.spawned, "should have spawned at least one agent")
	lastSpawn := sp.spawned[len(sp.spawned)-1]
	assert.Equal(t, "problem/task-1/lead/api/api-1", lastSpawn.Env["BELAYER_MAIL_ADDRESS"])
}

func TestProblemRunner_CheckCompletions_ValidationDisabled(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-2", RepoName: "api", Description: "first", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "api-2", ProblemID: "task-2", RepoName: "api", Description: "second", DependsOn: []string{"api-1"}, Status: model.ClimbStatusPending},
	}
	insertTestTask(t, s, "task-2", goals)

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-2", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	task, _ := s.GetProblem("task-2")
	runner := &ProblemRunner{
		task:              task,
		worktrees:         map[string]string{"api": worktreeDir},
		instanceDir:       tmpDir,
		store:             s,
		tmux:              tm,
		logMgr:            lm,
		spawner:           sp,
		tmuxSession:       "belayer-problem-task-2",
		startedAt:         make(map[string]time.Time),
		validationEnabled: false, // direct completion
	}
	require.NoError(t, tm.NewSession("belayer-problem-task-2"))
	require.NoError(t, lm.EnsureDir("task-2"))

	goalsFromDB, _ := s.GetClimbsForProblem("task-2")
	runner.dag = BuildDAG(goalsFromDB)

	// Spawn api-1
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-2"}))

	// Write TOP.json for api-1
	doneJSON := TopJSON{
		Status:       "complete",
		Summary:      "Did the thing",
		FilesChanged: []string{"api/main.go"},
	}
	data, _ := json.Marshal(doneJSON)
	goalDoneDir := filepath.Join(worktreeDir, ".lead", "api-1")
	require.NoError(t, os.MkdirAll(goalDoneDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(goalDoneDir, "TOP.json"), data, 0o644))

	// Check completions — should find api-1 complete and api-2 ready
	newlyReady, completedCount, err := runner.CheckCompletions()
	require.NoError(t, err)
	assert.Equal(t, 1, completedCount)

	assert.Equal(t, model.ClimbStatusComplete, runner.dag.Get("api-1").Status)
	require.Len(t, newlyReady, 1)
	assert.Equal(t, "api-2", newlyReady[0].Goal.ID)
}

func TestProblemRunner_CheckCompletions_ValidationEnabled(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-2v", RepoName: "api", Description: "first", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "api-2", ProblemID: "task-2v", RepoName: "api", Description: "second", DependsOn: []string{"api-1"}, Status: model.ClimbStatusPending},
	}
	insertTestTask(t, s, "task-2v", goals)

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-2v", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	task, _ := s.GetProblem("task-2v")
	runner := &ProblemRunner{
		task:              task,
		worktrees:         map[string]string{"api": worktreeDir},
		instanceDir:       tmpDir,
		store:             s,
		tmux:              tm,
		logMgr:            lm,
		spawner:           sp,
		tmuxSession:       "belayer-problem-task-2v",
		startedAt:         make(map[string]time.Time),
		validationEnabled: true,
	}
	require.NoError(t, tm.NewSession("belayer-problem-task-2v"))
	require.NoError(t, lm.EnsureDir("task-2v"))

	goalsFromDB, _ := s.GetClimbsForProblem("task-2v")
	runner.dag = BuildDAG(goalsFromDB)

	// Spawn api-1
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-2v"}))

	// Write TOP.json for api-1 to goal-scoped path
	doneJSON := TopJSON{
		Status:       "complete",
		Summary:      "Did the thing",
		FilesChanged: []string{"api/main.go"},
	}
	data, _ := json.Marshal(doneJSON)
	goalDoneDir := filepath.Join(worktreeDir, ".lead", "api-1")
	require.NoError(t, os.MkdirAll(goalDoneDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(goalDoneDir, "TOP.json"), data, 0o644))

	// Check completions — with validation enabled, goal should be spotting, not complete
	newlyReady, completedCount, err := runner.CheckCompletions()
	require.NoError(t, err)
	assert.Equal(t, 0, completedCount) // not counted as complete
	assert.Len(t, newlyReady, 0)       // no newly unblocked goals

	// Goal should be in spotting status
	assert.Equal(t, model.ClimbStatusSpotting, runner.dag.Get("api-1").Status)

	// api-2 should NOT be ready (api-1 is spotting, not complete)
	assert.False(t, runner.AllClimbsComplete())
}

func TestProblemRunner_CheckStaleClimbs(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-3", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	insertTestTask(t, s, "task-3", goals)

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-3", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	task, _ := s.GetProblem("task-3")
	runner := &ProblemRunner{
		task:        task,
		worktrees:   map[string]string{"api": worktreeDir},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		tmuxSession: "belayer-problem-task-3",
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession("belayer-problem-task-3"))
	require.NoError(t, lm.EnsureDir("task-3"))

	goalsFromDB, _ := s.GetClimbsForProblem("task-3")
	runner.dag = BuildDAG(goalsFromDB)
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-3"}))

	// Kill the window to simulate crash
	tm.KillWindow("belayer-problem-task-3", "api-api-1")

	// Check stale goals
	retryGoals, err := runner.CheckStaleClimbs(30 * time.Minute)
	require.NoError(t, err)

	// Goal should be retried (attempt 0 -> 1)
	require.Len(t, retryGoals, 1)
	assert.Equal(t, "api-1", retryGoals[0].Goal.ID)

	// Check goal is pending in DAG now (reset for retry)
	assert.Equal(t, model.ClimbStatusPending, runner.dag.Get("api-1").Status)
}

func TestProblemRunner_StaleTimeout(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-4", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	insertTestTask(t, s, "task-4", goals)

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-4", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	task, _ := s.GetProblem("task-4")
	runner := &ProblemRunner{
		task:        task,
		worktrees:   map[string]string{"api": worktreeDir},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		tmuxSession: "belayer-problem-task-4",
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession("belayer-problem-task-4"))
	require.NoError(t, lm.EnsureDir("task-4"))

	goalsFromDB, _ := s.GetClimbsForProblem("task-4")
	runner.dag = BuildDAG(goalsFromDB)
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-4"}))

	// Backdate the start time to simulate timeout
	runner.startedAt["api-1"] = time.Now().Add(-1 * time.Hour)

	// Window is still alive, but goal timed out
	retryGoals, err := runner.CheckStaleClimbs(30 * time.Minute)
	require.NoError(t, err)

	require.Len(t, retryGoals, 1)
	assert.Equal(t, "api-1", retryGoals[0].Goal.ID)
}

func TestProblemRunner_HasStuckClimbs(t *testing.T) {
	s, tm, lm, sp, _ := setupTestEnv(t)

	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-5", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	insertTestTask(t, s, "task-5", goals)

	task, _ := s.GetProblem("task-5")
	runner := &ProblemRunner{
		task:      task,
		store:     s,
		tmux:      tm,
		logMgr:    lm,
		spawner:   sp,
		startedAt: make(map[string]time.Time),
	}

	goalsFromDB, _ := s.GetClimbsForProblem("task-5")
	runner.dag = BuildDAG(goalsFromDB)

	// Simulate goal failing at max attempts
	runner.dag.Get("api-1").Status = model.ClimbStatusFailed
	runner.dag.Get("api-1").Attempt = 3

	assert.True(t, runner.HasStuckClimbs())

	// Reset to under max
	runner.dag.Get("api-1").Attempt = 2
	assert.False(t, runner.HasStuckClimbs())
}

func TestSetter_MaxLeadsCap(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	// Create a task with 3 independent goals
	goals := []model.Climb{
		{ID: "g-1", ProblemID: "task-6", RepoName: "api", Description: "one", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "g-2", ProblemID: "task-6", RepoName: "api", Description: "two", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "g-3", ProblemID: "task-6", RepoName: "api", Description: "three", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	insertTestTask(t, s, "task-6", goals)

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-6", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	task, _ := s.GetProblem("task-6")
	runner := &ProblemRunner{
		task:        task,
		worktrees:   map[string]string{"api": worktreeDir},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		tmuxSession: "belayer-problem-task-6",
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession("belayer-problem-task-6"))
	require.NoError(t, lm.EnsureDir("task-6"))

	goalsFromDB, _ := s.GetClimbsForProblem("task-6")
	runner.dag = BuildDAG(goalsFromDB)

	// Create setter with maxLeads=2
	setter := &Belayer{
		config: Config{
			MaxLeads:     2,
			InstanceName: "test-instance",
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		problems: map[string]*ProblemRunner{"task-6": runner},
	}

	// Queue all 3 goals
	readyGoals := runner.dag.ReadyClimbs()
	for _, g := range readyGoals {
		setter.leadQueue = append(setter.leadQueue, QueuedClimb{Goal: g, TaskID: "task-6"})
	}

	// Process queue — should only spawn 2 (maxLeads cap)
	setter.processLeadQueue()

	assert.Equal(t, 2, setter.activeLeads)
	assert.Len(t, setter.leadQueue, 1) // 1 still queued
}

func TestSetter_CrashRecovery(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-7", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "api-2", ProblemID: "task-7", RepoName: "api", Description: "dep on 1", DependsOn: []string{"api-1"}, Status: model.ClimbStatusPending},
	}
	insertTestTask(t, s, "task-7", goals)

	// Simulate task was running when setter crashed
	require.NoError(t, s.UpdateProblemStatus("task-7", model.ProblemStatusRunning))
	require.NoError(t, s.UpdateClimbStatus("api-1", model.ClimbStatusRunning))

	// Create worktree dir
	worktreeDir := filepath.Join(tmpDir, "tasks", "task-7", "api")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	// Write TOP.json that was written while setter was down (goal-scoped path)
	doneJSON := TopJSON{Status: "complete", Summary: "done while crashed"}
	data, _ := json.Marshal(doneJSON)
	goalDoneDir := filepath.Join(worktreeDir, ".lead", "api-1")
	require.NoError(t, os.MkdirAll(goalDoneDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(goalDoneDir, "TOP.json"), data, 0o644))

	setter := &Belayer{
		config: Config{
			InstanceName: "test-instance",
			InstanceDir:  tmpDir,
			MaxLeads:     8,
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		problems: make(map[string]*ProblemRunner),
	}

	// Run recovery
	err := setter.recover()
	require.NoError(t, err)

	// Task should have been recovered
	require.Contains(t, setter.problems, "task-7")

	// With validation enabled (default), api-1 should be in spotting status
	// (TOP.json found during recovery triggers spotter, not direct completion)
	runner := setter.problems["task-7"]
	assert.Equal(t, model.ClimbStatusSpotting, runner.dag.Get("api-1").Status)

	// api-2 should NOT be ready yet (api-1 is spotting, not complete)
	foundApi2 := false
	for _, q := range setter.leadQueue {
		if q.Goal.ID == "api-2" {
			foundApi2 = true
		}
	}
	assert.False(t, foundApi2)
}

func TestSetter_RunTickCycle(t *testing.T) {
	s, tm, lm, sp, tmpDir := setupTestEnv(t)

	setter := &Belayer{
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
		problems: make(map[string]*ProblemRunner),
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

func TestDAG_AddClimbs(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "p1", RepoName: "api", Description: "first", DependsOn: []string{}, Status: model.ClimbStatusComplete},
	}
	dag := BuildDAG(goals)
	assert.True(t, dag.AllComplete())

	// Add correction goals
	corrGoals := []model.Climb{
		{ID: "api-corr-1-1", ProblemID: "p1", RepoName: "api", Description: "fix response", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "api-corr-1-2", ProblemID: "p1", RepoName: "api", Description: "fix tests", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	dag.AddClimbs(corrGoals)

	assert.False(t, dag.AllComplete())
	assert.NotNil(t, dag.Get("api-corr-1-1"))
	assert.NotNil(t, dag.Get("api-corr-1-2"))

	ready := dag.ReadyClimbs()
	assert.Len(t, ready, 2) // both correction goals should be ready
}

func newTestRunner(t *testing.T, taskID string, goals []model.Climb) (*ProblemRunner, *store.Store, *mockTmux, *mockSpawner, *mockGitRunner, string) {
	t.Helper()
	s, tm, lm, sp, tmpDir := setupTestEnv(t)
	mg := newMockGitRunner()
	insertTestTask(t, s, taskID, goals)

	task, err := s.GetProblem(taskID)
	require.NoError(t, err)

	// Set up worktrees and task dir
	repos := make(map[string]string)
	for _, g := range goals {
		if _, ok := repos[g.RepoName]; !ok {
			wtDir := filepath.Join(tmpDir, "tasks", taskID, g.RepoName)
			require.NoError(t, os.MkdirAll(wtDir, 0o755))
			repos[g.RepoName] = wtDir
		}
	}

	taskDir := filepath.Join(tmpDir, "tasks", taskID)
	require.NoError(t, os.MkdirAll(taskDir, 0o755))

	require.NoError(t, s.UpdateProblemStatus(taskID, model.ProblemStatusRunning))
	task.Status = model.ProblemStatusRunning

	runner := &ProblemRunner{
		task:        task,
		worktrees:   repos,
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		git:         mg,
		tmuxSession: "belayer-problem-" + taskID,
		problemDir:  taskDir,
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession(runner.tmuxSession))
	require.NoError(t, lm.EnsureDir(taskID))

	goalsFromDB, err := s.GetClimbsForProblem(taskID)
	require.NoError(t, err)
	runner.dag = BuildDAG(goalsFromDB)

	return runner, s, tm, sp, mg, tmpDir
}

func TestProblemRunner_SpawnAnchor(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-s1", RepoName: "api", Description: "add endpoint", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, _, tm, sp, mg, _ := newTestRunner(t, "task-s1", goals)

	// Mark goal as complete with TOP.json (goal-scoped path)
	runner.dag.MarkComplete("api-1")
	doneData, _ := json.Marshal(TopJSON{Status: "complete", Summary: "Added endpoint"})
	goalDoneDir := filepath.Join(runner.worktrees["api"], ".lead", "api-1")
	os.MkdirAll(goalDoneDir, 0o755)
	os.WriteFile(filepath.Join(goalDoneDir, "TOP.json"), doneData, 0o644)

	// Set mock git responses
	mg.responses[runner.worktrees["api"]+":diff"] = "+func NewEndpoint() {}"
	mg.responses[runner.worktrees["api"]+":log"] = "abc123 Added endpoint"

	err := runner.SpawnAnchor()
	require.NoError(t, err)

	// Verify anchor state
	assert.True(t, runner.AnchorRunning())
	assert.Equal(t, 1, runner.AnchorAttempt())

	// Verify tmux window was created
	windows, _ := tm.ListWindows(runner.tmuxSession)
	assert.Contains(t, windows, "anchor")

	// Verify agent was spawned with correct opts
	require.Len(t, sp.spawned, 1)
	assert.Equal(t, runner.tmuxSession, sp.spawned[0].TmuxSession)
	assert.Equal(t, "anchor", sp.spawned[0].WindowName)
	assert.Equal(t, runner.problemDir, sp.spawned[0].WorkDir)

	// Verify GOAL.json was written to goal-scoped path (.lead/anchor/)
	goalJSON, err := os.ReadFile(filepath.Join(runner.problemDir, ".lead", "anchor", "GOAL.json"))
	require.NoError(t, err)
	assert.Contains(t, string(goalJSON), "test spec")
	assert.Contains(t, string(goalJSON), "anchor")

	// Verify AppendSystemPrompt is set
	assert.NotEmpty(t, sp.spawned[0].AppendSystemPrompt)
}

func TestProblemRunner_CheckAnchorVerdict_Approve(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-s2", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, s, _, _, _, _ := newTestRunner(t, "task-s2", goals)
	runner.anchorAttempt = 1
	runner.anchorRunning = true

	// Write VERDICT.json
	verdict := anchor.VerdictJSON{
		Verdict: "approve",
		Repos: map[string]anchor.RepoVerdict{
			"api": {Status: "pass", Goals: []string{}},
		},
	}
	data, _ := json.Marshal(verdict)
	require.NoError(t, os.WriteFile(filepath.Join(runner.problemDir, "VERDICT.json"), data, 0o644))

	v, found, err := runner.CheckAnchorVerdict()
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "approve", v.Verdict)
	assert.False(t, runner.AnchorRunning())

	// VERDICT.json should be removed
	_, statErr := os.Stat(filepath.Join(runner.problemDir, "VERDICT.json"))
	assert.True(t, os.IsNotExist(statErr))

	// Review should be recorded in SQLite
	reviews, _ := s.GetAnchorReviewsForProblem("task-s2")
	require.Len(t, reviews, 1)
	assert.Equal(t, "approve", reviews[0].Verdict)
	assert.Equal(t, 1, reviews[0].Attempt)
}

func TestProblemRunner_CheckAnchorVerdict_NotFound(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-s3", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, _, _, _, _, _ := newTestRunner(t, "task-s3", goals)
	runner.anchorAttempt = 1
	runner.anchorRunning = true

	// No VERDICT.json exists
	v, found, err := runner.CheckAnchorVerdict()
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, v)
	assert.True(t, runner.AnchorRunning()) // still running
}

func TestProblemRunner_HandleRejection(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-s4", RepoName: "api", Description: "add endpoint", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "app-1", ProblemID: "task-s4", RepoName: "app", Description: "add UI", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, s, _, _, _, _ := newTestRunner(t, "task-s4", goals)
	runner.anchorAttempt = 1

	// Mark both goals as complete with TOP.json (goal-scoped paths)
	runner.dag.MarkComplete("api-1")
	runner.dag.MarkComplete("app-1")
	doneData, _ := json.Marshal(TopJSON{Status: "complete", Summary: "done"})
	apiGoalDir := filepath.Join(runner.worktrees["api"], ".lead", "api-1")
	os.MkdirAll(apiGoalDir, 0o755)
	os.WriteFile(filepath.Join(apiGoalDir, "TOP.json"), doneData, 0o644)
	appGoalDir := filepath.Join(runner.worktrees["app"], ".lead", "app-1")
	os.MkdirAll(appGoalDir, 0o755)
	os.WriteFile(filepath.Join(appGoalDir, "TOP.json"), doneData, 0o644)

	verdict := &anchor.VerdictJSON{
		Verdict: "reject",
		Repos: map[string]anchor.RepoVerdict{
			"api": {Status: "fail", Goals: []string{"Fix response schema", "Add error handling"}},
			"app": {Status: "pass", Goals: []string{}},
		},
	}

	correctionGoals, err := runner.HandleRejection(verdict)
	require.NoError(t, err)

	// Should have 2 correction goals for the failing api repo
	require.Len(t, correctionGoals, 2)
	assert.Equal(t, "api-corr-1-1", correctionGoals[0].Goal.ID)
	assert.Equal(t, "api-corr-1-2", correctionGoals[1].Goal.ID)
	assert.Equal(t, "Fix response schema", correctionGoals[0].Goal.Description)
	assert.Equal(t, "Add error handling", correctionGoals[1].Goal.Description)

	// TOP.json should be removed from failing repo only (goal-scoped paths)
	_, apiDoneErr := os.Stat(filepath.Join(runner.worktrees["api"], ".lead", "api-1", "TOP.json"))
	assert.True(t, os.IsNotExist(apiDoneErr))
	_, appDoneErr := os.Stat(filepath.Join(runner.worktrees["app"], ".lead", "app-1", "TOP.json"))
	assert.False(t, os.IsNotExist(appDoneErr)) // app's TOP.json should remain

	// Correction goals should be in the DAG
	assert.NotNil(t, runner.dag.Get("api-corr-1-1"))
	assert.NotNil(t, runner.dag.Get("api-corr-1-2"))

	// Correction goals should be in SQLite
	dbGoals, _ := s.GetClimbsForProblem("task-s4")
	goalIDs := make(map[string]bool)
	for _, g := range dbGoals {
		goalIDs[g.ID] = true
	}
	assert.True(t, goalIDs["api-corr-1-1"])
	assert.True(t, goalIDs["api-corr-1-2"])
}

func TestSetter_SingleRepoSkipsAnchor(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-s5a", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	s, tm, lm, sp, tmpDir := setupTestEnv(t)
	mg := newMockGitRunner()
	insertTestTask(t, s, "task-s5a", goals)

	task, _ := s.GetProblem("task-s5a")
	require.NoError(t, s.UpdateProblemStatus("task-s5a", model.ProblemStatusRunning))
	task.Status = model.ProblemStatusRunning

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-s5a", "api")
	taskDir := filepath.Join(tmpDir, "tasks", "task-s5a")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	goalsFromDB, _ := s.GetClimbsForProblem("task-s5a")
	runner := &ProblemRunner{
		task:        task,
		dag:         BuildDAG(goalsFromDB),
		worktrees:   map[string]string{"api": worktreeDir},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		git:         mg,
		tmuxSession: "belayer-problem-task-s5a",
		problemDir:  taskDir,
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession(runner.tmuxSession))
	require.NoError(t, lm.EnsureDir("task-s5a"))

	sett := &Belayer{
		config: Config{
			InstanceName: "test-instance",
			InstanceDir:  tmpDir,
			MaxLeads:     8,
			StaleTimeout: 30 * time.Minute,
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		problems: map[string]*ProblemRunner{"task-s5a": runner},
	}

	// Disable validation for this test
	runner.validationEnabled = false

	// Spawn goal and write TOP.json
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-s5a"}))
	sett.activeLeads = 1
	doneData, _ := json.Marshal(TopJSON{Status: "complete", Summary: "done"})
	goalDoneDir := filepath.Join(worktreeDir, ".lead", "api-1")
	os.MkdirAll(goalDoneDir, 0o755)
	os.WriteFile(filepath.Join(goalDoneDir, "TOP.json"), doneData, 0o644)

	// First tick: detect completion, transition to reviewing
	require.NoError(t, sett.tick())
	updatedTask, _ := s.GetProblem("task-s5a")
	assert.Equal(t, model.ProblemStatusReviewing, updatedTask.Status)

	// Second tick: single-repo should skip anchor and go straight to complete
	require.NoError(t, sett.tick())
	assert.False(t, runner.AnchorRunning(), "anchor should not be spawned for single-repo task")
	updatedTask, _ = s.GetProblem("task-s5a")
	assert.Equal(t, model.ProblemStatusComplete, updatedTask.Status)
	assert.NotContains(t, sett.problems, "task-s5a") // cleaned up
}

func TestSetter_AnchorApproveFlow(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-s5", RepoName: "api", Description: "test api", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "web-1", ProblemID: "task-s5", RepoName: "web", Description: "test web", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	s, tm, lm, sp, tmpDir := setupTestEnv(t)
	mg := newMockGitRunner()
	insertTestTask(t, s, "task-s5", goals)

	task, _ := s.GetProblem("task-s5")
	require.NoError(t, s.UpdateProblemStatus("task-s5", model.ProblemStatusRunning))
	task.Status = model.ProblemStatusRunning

	apiWorktreeDir := filepath.Join(tmpDir, "tasks", "task-s5", "api")
	webWorktreeDir := filepath.Join(tmpDir, "tasks", "task-s5", "web")
	taskDir := filepath.Join(tmpDir, "tasks", "task-s5")
	require.NoError(t, os.MkdirAll(apiWorktreeDir, 0o755))
	require.NoError(t, os.MkdirAll(webWorktreeDir, 0o755))

	goalsFromDB, _ := s.GetClimbsForProblem("task-s5")

	runner := &ProblemRunner{
		task:        task,
		dag:         BuildDAG(goalsFromDB),
		worktrees:   map[string]string{"api": apiWorktreeDir, "web": webWorktreeDir},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		git:         mg,
		tmuxSession: "belayer-problem-task-s5",
		problemDir:  taskDir,
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession(runner.tmuxSession))
	require.NoError(t, lm.EnsureDir("task-s5"))

	setter := &Belayer{
		config: Config{
			InstanceName: "test-instance",
			InstanceDir:  tmpDir,
			MaxLeads:     8,
			StaleTimeout: 30 * time.Minute,
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		problems: map[string]*ProblemRunner{"task-s5": runner},
	}

	// Disable validation for this test (tests anchor flow, not spotter)
	runner.validationEnabled = false

	// Spawn both goals and write TOP.json for each (goal-scoped paths)
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-s5"}))
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[1], TaskID: "task-s5"}))
	setter.activeLeads = 2
	doneData, _ := json.Marshal(TopJSON{Status: "complete", Summary: "done"})

	apiDoneDir := filepath.Join(apiWorktreeDir, ".lead", "api-1")
	os.MkdirAll(apiDoneDir, 0o755)
	os.WriteFile(filepath.Join(apiDoneDir, "TOP.json"), doneData, 0o644)

	webDoneDir := filepath.Join(webWorktreeDir, ".lead", "web-1")
	os.MkdirAll(webDoneDir, 0o755)
	os.WriteFile(filepath.Join(webDoneDir, "TOP.json"), doneData, 0o644)

	// First tick: detect completion, transition to reviewing
	require.NoError(t, setter.tick())
	updatedTask, _ := s.GetProblem("task-s5")
	assert.Equal(t, model.ProblemStatusReviewing, updatedTask.Status)
	assert.Equal(t, 0, setter.activeLeads)

	// Second tick: spawn anchor (multi-repo requires anchor)
	require.NoError(t, setter.tick())
	assert.True(t, runner.AnchorRunning())

	// Write VERDICT.json — approve
	verdict := anchor.VerdictJSON{
		Verdict: "approve",
		Repos: map[string]anchor.RepoVerdict{
			"api": {Status: "pass"},
			"web": {Status: "pass"},
		},
	}
	verdictData, _ := json.Marshal(verdict)
	os.WriteFile(filepath.Join(taskDir, "VERDICT.json"), verdictData, 0o644)

	// Third tick: read verdict, create PRs, mark complete
	require.NoError(t, setter.tick())
	updatedTask, _ = s.GetProblem("task-s5")
	assert.Equal(t, model.ProblemStatusComplete, updatedTask.Status)
	assert.NotContains(t, setter.problems, "task-s5") // cleaned up
}

func TestSetter_AnchorRejectThenApprove(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-s6", RepoName: "api", Description: "test api", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "web-1", ProblemID: "task-s6", RepoName: "web", Description: "test web", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	s, tm, lm, sp, tmpDir := setupTestEnv(t)
	mg := newMockGitRunner()
	insertTestTask(t, s, "task-s6", goals)

	task, _ := s.GetProblem("task-s6")
	require.NoError(t, s.UpdateProblemStatus("task-s6", model.ProblemStatusRunning))
	task.Status = model.ProblemStatusRunning

	apiWorktreeDir := filepath.Join(tmpDir, "tasks", "task-s6", "api")
	webWorktreeDir := filepath.Join(tmpDir, "tasks", "task-s6", "web")
	taskDir := filepath.Join(tmpDir, "tasks", "task-s6")
	require.NoError(t, os.MkdirAll(apiWorktreeDir, 0o755))
	require.NoError(t, os.MkdirAll(webWorktreeDir, 0o755))

	goalsFromDB, _ := s.GetClimbsForProblem("task-s6")
	runner := &ProblemRunner{
		task:        task,
		dag:         BuildDAG(goalsFromDB),
		worktrees:   map[string]string{"api": apiWorktreeDir, "web": webWorktreeDir},
		instanceDir: tmpDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		git:         mg,
		tmuxSession: "belayer-problem-task-s6",
		problemDir:  taskDir,
		startedAt:   make(map[string]time.Time),
	}
	require.NoError(t, tm.NewSession(runner.tmuxSession))
	require.NoError(t, lm.EnsureDir("task-s6"))

	sett := &Belayer{
		config: Config{
			InstanceName: "test-instance",
			InstanceDir:  tmpDir,
			MaxLeads:     8,
			StaleTimeout: 30 * time.Minute,
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		problems: map[string]*ProblemRunner{"task-s6": runner},
	}

	// Disable validation for this test (tests anchor reject/approve flow)
	runner.validationEnabled = false

	// Spawn both goals and complete them (goal-scoped paths)
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-s6"}))
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[1], TaskID: "task-s6"}))
	sett.activeLeads = 2
	doneData, _ := json.Marshal(TopJSON{Status: "complete", Summary: "done"})

	apiDoneDir := filepath.Join(apiWorktreeDir, ".lead", "api-1")
	os.MkdirAll(apiDoneDir, 0o755)
	os.WriteFile(filepath.Join(apiDoneDir, "TOP.json"), doneData, 0o644)

	webDoneDir := filepath.Join(webWorktreeDir, ".lead", "web-1")
	os.MkdirAll(webDoneDir, 0o755)
	os.WriteFile(filepath.Join(webDoneDir, "TOP.json"), doneData, 0o644)

	// Tick 1: detect completion -> reviewing
	require.NoError(t, sett.tick())

	// Tick 2: spawn anchor (multi-repo)
	require.NoError(t, sett.tick())

	// Write reject verdict
	rejectVerdict := anchor.VerdictJSON{
		Verdict: "reject",
		Repos: map[string]anchor.RepoVerdict{
			"api": {Status: "fail", Goals: []string{"Fix the schema"}},
			"web": {Status: "pass"},
		},
	}
	rejectData, _ := json.Marshal(rejectVerdict)
	os.WriteFile(filepath.Join(taskDir, "VERDICT.json"), rejectData, 0o644)

	// Tick 3: read reject verdict -> back to running with correction goals
	// tick() also calls processLeadQueue(), so correction goal is spawned immediately
	spawnedBefore := len(sp.spawned)
	require.NoError(t, sett.tick())
	updatedTask, _ := s.GetProblem("task-s6")
	assert.Equal(t, model.ProblemStatusRunning, updatedTask.Status)

	// Correction goal should have been spawned (via processLeadQueue in tick)
	assert.Greater(t, len(sp.spawned), spawnedBefore)
	assert.Equal(t, 1, sett.activeLeads)

	// Complete the correction goal (goal-scoped path)
	corrGoalID := "api-corr-1-1"
	corrDoneDir := filepath.Join(apiWorktreeDir, ".lead", corrGoalID)
	os.MkdirAll(corrDoneDir, 0o755)
	doneData2, _ := json.Marshal(TopJSON{Status: "complete", Summary: "fixed schema"})
	os.WriteFile(filepath.Join(corrDoneDir, "TOP.json"), doneData2, 0o644)

	// Tick 4: detect correction goal completion -> reviewing again
	require.NoError(t, sett.tick())
	updatedTask, _ = s.GetProblem("task-s6")
	assert.Equal(t, model.ProblemStatusReviewing, updatedTask.Status)

	// Tick 5: spawn anchor again
	require.NoError(t, sett.tick())
	assert.Equal(t, 2, runner.AnchorAttempt())

	// Write approve verdict
	approveVerdict := anchor.VerdictJSON{
		Verdict: "approve",
		Repos: map[string]anchor.RepoVerdict{
			"api": {Status: "pass"},
			"web": {Status: "pass"},
		},
	}
	approveData, _ := json.Marshal(approveVerdict)
	os.WriteFile(filepath.Join(taskDir, "VERDICT.json"), approveData, 0o644)

	// Tick 6: read approve -> complete
	require.NoError(t, sett.tick())
	updatedTask, _ = s.GetProblem("task-s6")
	assert.Equal(t, model.ProblemStatusComplete, updatedTask.Status)

	// Verify reviews are in SQLite
	reviews, _ := s.GetAnchorReviewsForProblem("task-s6")
	require.Len(t, reviews, 2)
	assert.Equal(t, "reject", reviews[0].Verdict)
	assert.Equal(t, "approve", reviews[1].Verdict)
}

func TestSetter_AnchorMaxReviewsStuck(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-s7", RepoName: "api", Description: "test api", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "web-1", ProblemID: "task-s7", RepoName: "web", Description: "test web", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	s, tm, lm, sp, tmpDir := setupTestEnv(t)
	mg := newMockGitRunner()
	insertTestTask(t, s, "task-s7", goals)

	task, _ := s.GetProblem("task-s7")
	require.NoError(t, s.UpdateProblemStatus("task-s7", model.ProblemStatusReviewing))
	task.Status = model.ProblemStatusReviewing

	apiWorktreeDir := filepath.Join(tmpDir, "tasks", "task-s7", "api")
	webWorktreeDir := filepath.Join(tmpDir, "tasks", "task-s7", "web")
	taskDir := filepath.Join(tmpDir, "tasks", "task-s7")
	require.NoError(t, os.MkdirAll(apiWorktreeDir, 0o755))
	require.NoError(t, os.MkdirAll(webWorktreeDir, 0o755))

	goalsFromDB, _ := s.GetClimbsForProblem("task-s7")
	runner := &ProblemRunner{
		task:           task,
		dag:            BuildDAG(goalsFromDB),
		worktrees:      map[string]string{"api": apiWorktreeDir, "web": webWorktreeDir},
		instanceDir:    tmpDir,
		store:          s,
		tmux:           tm,
		logMgr:         lm,
		spawner:        sp,
		git:            mg,
		tmuxSession:    "belayer-problem-task-s7",
		problemDir:     taskDir,
		startedAt:      make(map[string]time.Time),
		anchorAttempt: 2, // already at max
		anchorRunning: true,
	}
	require.NoError(t, tm.NewSession(runner.tmuxSession))
	require.NoError(t, lm.EnsureDir("task-s7"))

	// Mark both goals complete
	runner.dag.MarkComplete("api-1")
	runner.dag.MarkComplete("web-1")

	sett := &Belayer{
		config: Config{
			InstanceName: "test-instance",
			InstanceDir:  tmpDir,
			MaxLeads:     8,
			StaleTimeout: 30 * time.Minute,
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		problems: map[string]*ProblemRunner{"task-s7": runner},
	}

	// Write reject verdict (2nd rejection at attempt 2)
	rejectVerdict := anchor.VerdictJSON{
		Verdict: "reject",
		Repos: map[string]anchor.RepoVerdict{
			"api": {Status: "fail", Goals: []string{"Still broken"}},
			"web": {Status: "pass"},
		},
	}
	rejectData, _ := json.Marshal(rejectVerdict)
	os.WriteFile(filepath.Join(taskDir, "VERDICT.json"), rejectData, 0o644)

	// Tick: should detect reject at max reviews -> stuck
	require.NoError(t, sett.tick())
	updatedTask, _ := s.GetProblem("task-s7")
	assert.Equal(t, model.ProblemStatusStuck, updatedTask.Status)
	assert.NotContains(t, sett.problems, "task-s7") // cleaned up
}

func TestProblemRunner_GatherSummaries(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-gs", RepoName: "api", Description: "endpoint", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "app-1", ProblemID: "task-gs", RepoName: "app", Description: "ui", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, _, _, _, _, _ := newTestRunner(t, "task-gs", goals)

	// Mark both complete and write TOP.json
	runner.dag.MarkComplete("api-1")
	runner.dag.MarkComplete("app-1")

	apiDone := TopJSON{Status: "complete", Summary: "Added endpoint", Notes: "Used middleware"}
	appDone := TopJSON{Status: "complete", Summary: "Added UI component"}

	apiData, _ := json.Marshal(apiDone)
	appData, _ := json.Marshal(appDone)
	apiGoalDir := filepath.Join(runner.worktrees["api"], ".lead", "api-1")
	os.MkdirAll(apiGoalDir, 0o755)
	os.WriteFile(filepath.Join(apiGoalDir, "TOP.json"), apiData, 0o644)
	appGoalDir := filepath.Join(runner.worktrees["app"], ".lead", "app-1")
	os.MkdirAll(appGoalDir, 0o755)
	os.WriteFile(filepath.Join(appGoalDir, "TOP.json"), appData, 0o644)

	summaries := runner.GatherSummaries()
	assert.Len(t, summaries, 2)

	summaryMap := make(map[string]climbctx.ClimbSummary)
	for _, s := range summaries {
		summaryMap[s.ClimbID] = s
	}

	assert.Equal(t, "Added endpoint", summaryMap["api-1"].Summary)
	assert.Equal(t, "Used middleware", summaryMap["api-1"].Notes)
	assert.Equal(t, "Added UI component", summaryMap["app-1"].Summary)
}

func TestProblemRunner_GatherDiffs(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-gd", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, _, _, _, mg, _ := newTestRunner(t, "task-gd", goals)

	mg.responses[runner.worktrees["api"]+":diff"] = "+func NewHandler() {}"

	diffs := runner.GatherDiffs()
	require.Len(t, diffs, 1)
	assert.Equal(t, "api", diffs[0].RepoName)
	assert.Contains(t, diffs[0].Diff, "NewHandler")
}

func TestProblemRunner_SpawnSpotter(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-sp1", RepoName: "api", Description: "add endpoint", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, s, _, sp, _, _ := newTestRunner(t, "task-sp1", goals)

	// Spawn and complete the goal first
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-sp1"}))
	assert.Equal(t, model.ClimbStatusRunning, runner.dag.Get("api-1").Status)

	// Write TOP.json to goal-scoped path
	goalDir := filepath.Join(runner.worktrees["api"], ".lead", "api-1")
	os.MkdirAll(goalDir, 0o755)
	doneData, _ := json.Marshal(TopJSON{Status: "complete", Summary: "Added endpoint"})
	os.WriteFile(filepath.Join(goalDir, "TOP.json"), doneData, 0o644)

	// Now spawn spotter on this goal
	goal := runner.dag.Get("api-1")
	err := runner.SpawnSpotter(goal)
	require.NoError(t, err)

	// Goal should be in spotting status
	assert.Equal(t, model.ClimbStatusSpotting, runner.dag.Get("api-1").Status)

	// Verify spotter was spawned (2 total spawns: lead + spotter)
	require.Len(t, sp.spawned, 2)
	assert.Equal(t, "spot-api-1", sp.spawned[1].WindowName)
	assert.NotEmpty(t, sp.spawned[1].AppendSystemPrompt)
	// Verify GOAL.json was written with spotter context (goal-scoped path)
	goalJSON, goalErr := os.ReadFile(filepath.Join(runner.worktrees["api"], ".lead", "api-1", "GOAL.json"))
	require.NoError(t, goalErr)
	assert.Contains(t, string(goalJSON), "spotter")
	assert.Contains(t, string(goalJSON), "Added endpoint") // TOP.json content

	// Verify spotter_spawned event was recorded
	events, _ := s.GetEventsForProblem("task-sp1")
	foundSpotterSpawned := false
	for _, e := range events {
		if e.Type == model.EventSpotterSpawned && e.ClimbID == "api-1" {
			foundSpotterSpawned = true
		}
	}
	assert.True(t, foundSpotterSpawned)
}

func TestProblemRunner_CheckSpotResult_Pass(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-sp2", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, s, _, _, _, _ := newTestRunner(t, "task-sp2", goals)

	// Put goal into spotting status
	runner.dag.MarkSpotting("api-1")

	// Write passing SPOT.json to goal-scoped path
	goalDir := filepath.Join(runner.worktrees["api"], ".lead", "api-1")
	require.NoError(t, os.MkdirAll(goalDir, 0o755))
	spotData := `{"pass": true, "project_type": "backend", "issues": []}`
	require.NoError(t, os.WriteFile(filepath.Join(goalDir, "SPOT.json"), []byte(spotData), 0o644))

	goal := runner.dag.Get("api-1")
	spot, found, err := runner.CheckSpotResult(goal)
	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, spot.Pass)

	// Goal should be complete
	assert.Equal(t, model.ClimbStatusComplete, runner.dag.Get("api-1").Status)

	// SPOT.json should be removed
	_, statErr := os.Stat(filepath.Join(goalDir, "SPOT.json"))
	assert.True(t, os.IsNotExist(statErr))

	// Events should be recorded
	events, _ := s.GetEventsForProblem("task-sp2")
	foundVerdict := false
	foundCompleted := false
	for _, e := range events {
		if e.Type == model.EventSpotterVerdict && e.ClimbID == "api-1" {
			foundVerdict = true
		}
		if e.Type == model.EventClimbCompleted && e.ClimbID == "api-1" {
			foundCompleted = true
		}
	}
	assert.True(t, foundVerdict)
	assert.True(t, foundCompleted)
}

func TestProblemRunner_CheckSpotResult_Fail(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-sp3", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, s, _, _, _, _ := newTestRunner(t, "task-sp3", goals)

	// Put goal into spotting status
	runner.dag.MarkSpotting("api-1")

	// Write TOP.json and SPOT.json to goal-scoped paths
	goalDir := filepath.Join(runner.worktrees["api"], ".lead", "api-1")
	require.NoError(t, os.MkdirAll(goalDir, 0o755))

	doneData, _ := json.Marshal(TopJSON{Status: "complete", Summary: "done"})
	require.NoError(t, os.WriteFile(filepath.Join(goalDir, "TOP.json"), doneData, 0o644))

	// Write failing SPOT.json
	spotData := `{"pass": false, "project_type": "frontend", "issues": [{"check": "build", "description": "Build failed", "severity": "error"}]}`
	require.NoError(t, os.WriteFile(filepath.Join(goalDir, "SPOT.json"), []byte(spotData), 0o644))

	goal := runner.dag.Get("api-1")
	spot, found, err := runner.CheckSpotResult(goal)
	require.NoError(t, err)
	assert.True(t, found)
	assert.False(t, spot.Pass)
	assert.Len(t, spot.Issues, 1)
	assert.Equal(t, "build", spot.Issues[0].Check)

	// Goal should be failed
	assert.Equal(t, model.ClimbStatusFailed, runner.dag.Get("api-1").Status)

	// Attempt should be incremented
	assert.Equal(t, 1, runner.dag.Get("api-1").Attempt)

	// SPOT.json should be removed
	_, statErr := os.Stat(filepath.Join(goalDir, "SPOT.json"))
	assert.True(t, os.IsNotExist(statErr))

	// TOP.json should be removed so retry starts fresh
	_, doneStatErr := os.Stat(filepath.Join(goalDir, "TOP.json"))
	assert.True(t, os.IsNotExist(doneStatErr))

	// Events should be recorded
	events, _ := s.GetEventsForProblem("task-sp3")
	foundVerdict := false
	foundFailed := false
	for _, e := range events {
		if e.Type == model.EventSpotterVerdict && e.ClimbID == "api-1" {
			foundVerdict = true
		}
		if e.Type == model.EventClimbFailed && e.ClimbID == "api-1" {
			foundFailed = true
		}
	}
	assert.True(t, foundVerdict)
	assert.True(t, foundFailed)
}

func TestProblemRunner_CheckSpotResult_NotFound(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-sp4", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, _, _, _, _, _ := newTestRunner(t, "task-sp4", goals)

	runner.dag.MarkSpotting("api-1")

	goal := runner.dag.Get("api-1")
	spot, found, err := runner.CheckSpotResult(goal)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, spot)

	// Goal should still be spotting
	assert.Equal(t, model.ClimbStatusSpotting, runner.dag.Get("api-1").Status)
}

func TestSetter_SpottingFlow_Pass(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-sf1", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
		{ID: "api-2", ProblemID: "task-sf1", RepoName: "api", Description: "depends on api-1", DependsOn: []string{"api-1"}, Status: model.ClimbStatusPending},
	}
	s, tm, lm, sp, tmpDir := setupTestEnv(t)
	mg := newMockGitRunner()
	insertTestTask(t, s, "task-sf1", goals)

	task, _ := s.GetProblem("task-sf1")
	require.NoError(t, s.UpdateProblemStatus("task-sf1", model.ProblemStatusRunning))
	task.Status = model.ProblemStatusRunning

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-sf1", "api")
	taskDir := filepath.Join(tmpDir, "tasks", "task-sf1")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	goalsFromDB, _ := s.GetClimbsForProblem("task-sf1")

	runner := &ProblemRunner{
		task:              task,
		dag:               BuildDAG(goalsFromDB),
		worktrees:         map[string]string{"api": worktreeDir},
		instanceDir:       tmpDir,
		store:             s,
		tmux:              tm,
		logMgr:            lm,
		spawner:           sp,
		git:               mg,
		tmuxSession:       "belayer-problem-task-sf1",
		problemDir:        taskDir,
		startedAt:         make(map[string]time.Time),
		validationEnabled: true,
	}
	require.NoError(t, tm.NewSession(runner.tmuxSession))
	require.NoError(t, lm.EnsureDir("task-sf1"))

	sett := &Belayer{
		config: Config{
			InstanceName: "test-instance",
			InstanceDir:  tmpDir,
			MaxLeads:     8,
			StaleTimeout: 30 * time.Minute,
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		problems: map[string]*ProblemRunner{"task-sf1": runner},
	}

	// Spawn goal
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-sf1"}))
	sett.activeLeads = 1

	// Write TOP.json to goal-scoped path
	goalDir := filepath.Join(worktreeDir, ".lead", "api-1")
	os.MkdirAll(goalDir, 0o755)
	doneData, _ := json.Marshal(TopJSON{Status: "complete", Summary: "done"})
	os.WriteFile(filepath.Join(goalDir, "TOP.json"), doneData, 0o644)

	// Tick 1: detect TOP.json -> goal transitions to spotting (spotter spawned)
	require.NoError(t, sett.tick())
	assert.Equal(t, model.ClimbStatusSpotting, runner.dag.Get("api-1").Status)
	assert.Equal(t, 1, sett.activeLeads) // still 1 active lead (spotter running)

	// Write passing SPOT.json to goal-scoped path
	spotData := `{"pass": true, "project_type": "backend", "issues": []}`
	os.WriteFile(filepath.Join(goalDir, "SPOT.json"), []byte(spotData), 0o644)

	// Tick 2: detect SPOT.json pass -> goal complete, api-2 unblocked and spawned
	require.NoError(t, sett.tick())
	assert.Equal(t, model.ClimbStatusComplete, runner.dag.Get("api-1").Status)
	assert.Equal(t, 1, sett.activeLeads) // spotter resolved (-1) + api-2 spawned (+1)

	// api-2 should have been queued and spawned
	assert.Equal(t, model.ClimbStatusRunning, runner.dag.Get("api-2").Status)
}

func TestSetter_SpottingFlow_FailRetry(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-sf2", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	s, tm, lm, sp, tmpDir := setupTestEnv(t)
	mg := newMockGitRunner()
	insertTestTask(t, s, "task-sf2", goals)

	task, _ := s.GetProblem("task-sf2")
	require.NoError(t, s.UpdateProblemStatus("task-sf2", model.ProblemStatusRunning))
	task.Status = model.ProblemStatusRunning

	worktreeDir := filepath.Join(tmpDir, "tasks", "task-sf2", "api")
	taskDir := filepath.Join(tmpDir, "tasks", "task-sf2")
	require.NoError(t, os.MkdirAll(worktreeDir, 0o755))

	goalsFromDB, _ := s.GetClimbsForProblem("task-sf2")

	runner := &ProblemRunner{
		task:              task,
		dag:               BuildDAG(goalsFromDB),
		worktrees:         map[string]string{"api": worktreeDir},
		instanceDir:       tmpDir,
		store:             s,
		tmux:              tm,
		logMgr:            lm,
		spawner:           sp,
		git:               mg,
		tmuxSession:       "belayer-problem-task-sf2",
		problemDir:        taskDir,
		startedAt:         make(map[string]time.Time),
		validationEnabled: true,
	}
	require.NoError(t, tm.NewSession(runner.tmuxSession))
	require.NoError(t, lm.EnsureDir("task-sf2"))

	sett := &Belayer{
		config: Config{
			InstanceName: "test-instance",
			InstanceDir:  tmpDir,
			MaxLeads:     8,
			StaleTimeout: 30 * time.Minute,
		},
		store:   s,
		tmux:    tm,
		logMgr:  lm,
		spawner: sp,
		problems: map[string]*ProblemRunner{"task-sf2": runner},
	}

	// Spawn goal
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-sf2"}))
	sett.activeLeads = 1
	spawnCountAfterLead := len(sp.spawned)

	// Write TOP.json to goal-scoped path
	goalDir := filepath.Join(worktreeDir, ".lead", "api-1")
	os.MkdirAll(goalDir, 0o755)
	doneData, _ := json.Marshal(TopJSON{Status: "complete", Summary: "done"})
	os.WriteFile(filepath.Join(goalDir, "TOP.json"), doneData, 0o644)

	// Tick 1: detect TOP.json -> goal transitions to spotting
	require.NoError(t, sett.tick())
	assert.Equal(t, model.ClimbStatusSpotting, runner.dag.Get("api-1").Status)
	spawnCountAfterSpotter := len(sp.spawned)
	assert.Greater(t, spawnCountAfterSpotter, spawnCountAfterLead)

	// Write failing SPOT.json to goal-scoped path
	spotData := `{"pass": false, "project_type": "backend", "issues": [{"check": "build", "description": "Build failed", "severity": "error"}]}`
	os.WriteFile(filepath.Join(goalDir, "SPOT.json"), []byte(spotData), 0o644)

	// Tick 2: detect SPOT.json fail -> goal re-queued with feedback, lead respawned
	require.NoError(t, sett.tick())
	assert.Equal(t, 1, runner.dag.Get("api-1").Attempt) // attempt incremented by CheckSpotResult
	assert.Equal(t, 1, sett.activeLeads) // lead re-spawned from queue

	// The re-spawned lead should have spotter feedback in GOAL.json (goal-scoped path)
	goalJSON, goalErr := os.ReadFile(filepath.Join(goalDir, "GOAL.json"))
	require.NoError(t, goalErr)
	assert.Contains(t, string(goalJSON), "FAILED CHECKS")
	assert.Contains(t, string(goalJSON), "Build failed")
}

func TestDAG_AllComplete_FalseForSpotting(t *testing.T) {
	goals := []model.Climb{
		{ID: "a", ProblemID: "p1", Status: model.ClimbStatusComplete},
		{ID: "b", ProblemID: "p1", Status: model.ClimbStatusPending},
	}
	dag := BuildDAG(goals)

	dag.MarkSpotting("b")
	assert.False(t, dag.AllComplete(), "AllComplete should return false when a goal is spotting")

	dag.MarkComplete("b")
	assert.True(t, dag.AllComplete())
}

func TestProblemRunner_SpawnClimbWithSpotterFeedback(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-sfb", RepoName: "api", Description: "test goal", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, _, _, sp, _, _ := newTestRunner(t, "task-sfb", goals)

	feedback := "FAILED CHECKS:\n- build [error]: Build failed\n"
	err := runner.SpawnClimb(QueuedClimb{
		Goal:            goals[0],
		TaskID:          "task-sfb",
		SpotterFeedback: feedback,
	})
	require.NoError(t, err)

	// Check that spotter feedback was written to GOAL.json (goal-scoped path)
	require.Len(t, sp.spawned, 1)
	goalJSON, goalErr := os.ReadFile(filepath.Join(sp.spawned[0].WorkDir, ".lead", "api-1", "GOAL.json"))
	require.NoError(t, goalErr)
	assert.Contains(t, string(goalJSON), "FAILED CHECKS")
	assert.Contains(t, string(goalJSON), "Build failed")
}

func TestSpotterFeedbackForGoal(t *testing.T) {
	t.Run("nil spot returns empty", func(t *testing.T) {
		result := SpotterFeedbackForGoal(nil)
		assert.Equal(t, "", result)
	})

	t.Run("passing spot returns empty", func(t *testing.T) {
		spot := &spotter.SpotJSON{Pass: true}
		result := SpotterFeedbackForGoal(spot)
		assert.Equal(t, "", result)
	})

	t.Run("failing spot formats issues", func(t *testing.T) {
		spot := &spotter.SpotJSON{
			Pass: false,
			Issues: []spotter.Issue{
				{Check: "build", Severity: "error", Description: "Build failed"},
				{Check: "lint", Severity: "warning", Description: "Unused import"},
			},
		}
		result := SpotterFeedbackForGoal(spot)
		assert.Contains(t, result, "FAILED CHECKS:")
		assert.Contains(t, result, "- build [error]: Build failed")
		assert.Contains(t, result, "- lint [warning]: Unused import")
	})
}

func TestSpawnClimb_SetsAppendSystemPromptAndRemainOnExit(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-cmd1", RepoName: "api", Description: "test goal", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, _, tm, sp, _, _ := newTestRunner(t, "task-cmd1", goals)

	err := runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-cmd1"})
	require.NoError(t, err)

	// Verify AppendSystemPrompt contains lead template content
	require.Len(t, sp.spawned, 1)
	assert.Contains(t, sp.spawned[0].AppendSystemPrompt, "Belayer Lead")

	// Verify SetRemainOnExit was called on the window
	assert.True(t, tm.remainOnExit["belayer-problem-task-cmd1:api-api-1"])

	// Verify InitialPrompt is used (not Prompt)
	assert.NotEmpty(t, sp.spawned[0].InitialPrompt)
	assert.Contains(t, sp.spawned[0].InitialPrompt, "GOAL.json")
}

func TestSpawnSpotter_SetsAppendSystemPromptAndProfiles(t *testing.T) {
	goals := []model.Climb{
		{ID: "api-1", ProblemID: "task-cmd2", RepoName: "api", Description: "test spotter", DependsOn: []string{}, Status: model.ClimbStatusPending},
	}
	runner, _, tm, sp, _, _ := newTestRunner(t, "task-cmd2", goals)

	// Spawn lead first
	require.NoError(t, runner.SpawnClimb(QueuedClimb{Goal: goals[0], TaskID: "task-cmd2"}))

	// Write TOP.json to goal-scoped path
	goalDir := filepath.Join(runner.worktrees["api"], ".lead", "api-1")
	os.MkdirAll(goalDir, 0o755)
	doneData, _ := json.Marshal(TopJSON{Status: "complete", Summary: "Added endpoint"})
	os.WriteFile(filepath.Join(goalDir, "TOP.json"), doneData, 0o644)

	// Spawn spotter
	goal := runner.dag.Get("api-1")
	err := runner.SpawnSpotter(goal)
	require.NoError(t, err)

	// Verify AppendSystemPrompt contains spotter template content
	require.Len(t, sp.spawned, 2)
	assert.Contains(t, sp.spawned[1].AppendSystemPrompt, "Belayer Spotter")

	// Verify spotter gets its own window name
	assert.Equal(t, "spot-api-1", sp.spawned[1].WindowName)

	// Verify profiles were written to .lead/<goalID>/profiles/
	profileDir := filepath.Join(runner.worktrees["api"], ".lead", "api-1", "profiles")
	_, statErr := os.Stat(profileDir)
	assert.False(t, os.IsNotExist(statErr), "profiles directory should exist")

	// Verify SetRemainOnExit was called for spotter
	assert.True(t, tm.remainOnExit["belayer-problem-task-cmd2:spot-api-1"])

	// Verify GOAL.json contains TOP.json content and profiles (goal-scoped path)
	goalJSON, goalErr := os.ReadFile(filepath.Join(runner.worktrees["api"], ".lead", "api-1", "GOAL.json"))
	require.NoError(t, goalErr)
	assert.Contains(t, string(goalJSON), "spotter")
	assert.Contains(t, string(goalJSON), "Added endpoint")

	// Verify InitialPrompt is used
	assert.Contains(t, sp.spawned[1].InitialPrompt, "GOAL.json")
}

func TestLooksLikeInputPrompt(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"bare prompt", ">", true},
		{"output then prompt", "some output\n>", true},
		{"thinking", "thinking...", false},
		{"empty", "", false},
		{"prompt with trailing space", "working on task\n> ", true},
		{"prompt mid-line not last", ">\nmore output", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, looksLikeInputPrompt(tt.input))
		})
	}
}

