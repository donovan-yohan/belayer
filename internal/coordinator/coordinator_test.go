package coordinator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLeadRunner simulates lead execution for testing.
type mockLeadRunner struct {
	runFunc func(ctx context.Context, cfg lead.RunConfig) (*lead.RunResult, error)
	calls   []lead.RunConfig
}

func (m *mockLeadRunner) Run(ctx context.Context, cfg lead.RunConfig) (*lead.RunResult, error) {
	m.calls = append(m.calls, cfg)
	if m.runFunc != nil {
		return m.runFunc(ctx, cfg)
	}
	return &lead.RunResult{
		Status: model.LeadStatusComplete,
		Output: "mock complete",
	}, nil
}

// mockWorktreeCreator simulates worktree creation for testing.
type mockWorktreeCreator struct {
	paths map[string]string // repoName -> path
}

func (m *mockWorktreeCreator) CreateWorktree(instanceDir, taskID, repoName string) (string, error) {
	if m.paths != nil {
		if p, ok := m.paths[repoName]; ok {
			return p, nil
		}
	}
	return filepath.Join(instanceDir, "tasks", taskID, repoName), nil
}

// mockDiffCollector returns canned diff/stat strings for testing.
type mockDiffCollector struct {
	diffs map[string]string // worktreePath -> diff
	stats map[string]string // worktreePath -> stat
}

func (m *mockDiffCollector) CollectDiff(worktreePath string) (string, error) {
	if m.diffs != nil {
		if d, ok := m.diffs[worktreePath]; ok {
			return d, nil
		}
	}
	return "diff --git a/main.go b/main.go\n+// mock change", nil
}

func (m *mockDiffCollector) CollectDiffStat(worktreePath string) (string, error) {
	if m.stats != nil {
		if s, ok := m.stats[worktreePath]; ok {
			return s, nil
		}
	}
	return " main.go | 1 +\n 1 file changed, 1 insertion(+)", nil
}

// mockPRCreator records PR creation calls for testing.
type mockPRCreator struct {
	calls  []prCreateCall
	err    error
	prURLs map[string]string // worktreePath -> URL
}

type prCreateCall struct {
	worktreePath string
	title        string
	body         string
}

func (m *mockPRCreator) PushAndCreatePR(worktreePath, title, body string) (string, error) {
	m.calls = append(m.calls, prCreateCall{worktreePath, title, body})
	if m.err != nil {
		return "", m.err
	}
	if m.prURLs != nil {
		if url, ok := m.prURLs[worktreePath]; ok {
			return url, nil
		}
	}
	return "https://github.com/org/repo/pull/1", nil
}

// setupCoordTestDB uses a temp file for SQLite to support concurrent goroutine access.
// In-memory SQLite gives each connection its own empty database, which breaks
// goroutine-based tests where the coordinator and lead runner share the same DB.
func setupCoordTestDB(t *testing.T) (*Store, *db.DB) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	require.NoError(t, database.Migrate())

	now := time.Now().UTC()
	_, err = database.Conn().Exec(
		`INSERT INTO instances (id, name, path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"test-instance", "test", "/tmp/test", now, now,
	)
	require.NoError(t, err)

	return NewStore(database.Conn()), database
}

func testConfig() CoordinatorConfig {
	return CoordinatorConfig{
		PollInterval:   50 * time.Millisecond,
		MaxLeadRetries: 3,
		BaseRetryDelay: 10 * time.Millisecond,
		MaxRetryDelay:  100 * time.Millisecond,
		AgenticModel:   "test-model",
	}
}

func TestNewCoordinator(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	runner := &mockLeadRunner{}
	wt := &mockWorktreeCreator{}

	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig())

	assert.NotNil(t, coord)
	assert.Equal(t, 0, coord.ActiveLeadCount())
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 2*time.Second, cfg.PollInterval)
	assert.Equal(t, 3, cfg.MaxLeadRetries)
	assert.Equal(t, 5*time.Second, cfg.BaseRetryDelay)
	assert.Equal(t, 5*time.Minute, cfg.MaxRetryDelay)
	assert.Equal(t, "claude-sonnet-4-6", cfg.AgenticModel)
}

func TestProcessPendingTask_Decomposition(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	leadStore := lead.NewStore(store.db)
	runner := &mockLeadRunner{
		runFunc: func(ctx context.Context, cfg lead.RunConfig) (*lead.RunResult, error) {
			if err := leadStore.SetLeadStarted(cfg.LeadID); err != nil {
				return nil, err
			}
			if err := leadStore.SetLeadFinished(cfg.LeadID, model.LeadStatusComplete, "done"); err != nil {
				return nil, err
			}
			return &lead.RunResult{Status: model.LeadStatusComplete, Output: "done"}, nil
		},
	}
	wt := &mockWorktreeCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig())

	task := &model.Task{
		ID:          "task-test-1",
		InstanceID:  "test-instance",
		Description: "Add user authentication",
		Source:      "text",
		Status:      model.TaskStatusPending,
	}
	require.NoError(t, store.InsertTask(task))

	ctx := context.Background()
	coord.processTick(ctx)

	// Wait for lead goroutine to finish
	time.Sleep(200 * time.Millisecond)

	updated, err := store.GetTask("task-test-1")
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusRunning, updated.Status)

	taskRepos, err := store.GetTaskReposForTask("task-test-1")
	require.NoError(t, err)
	assert.Len(t, taskRepos, 1)
	assert.Equal(t, "api", taskRepos[0].RepoName)

	leads, err := store.GetLeadsForTask("task-test-1")
	require.NoError(t, err)
	assert.Len(t, leads, 1)

	decisions, err := store.GetAgenticDecisionsForTask("task-test-1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(decisions), 2) // sufficiency + decomposition

	assert.Len(t, runner.calls, 1)
}

func TestProcessRunningTask_AllLeadsComplete(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	runner := &mockLeadRunner{}
	wt := &mockWorktreeCreator{}
	dc := &mockDiffCollector{}
	pr := &mockPRCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig(),
		WithDiffCollector(dc), WithPRCreator(pr))

	task := &model.Task{
		ID:          "task-align-1",
		InstanceID:  "test-instance",
		Description: "test alignment",
		Source:      "text",
		Status:      model.TaskStatusRunning,
	}
	require.NoError(t, store.InsertTask(task))

	tr := &model.TaskRepo{
		ID:           "tr-align-1",
		TaskID:       "task-align-1",
		RepoName:     "api",
		Spec:         "test spec",
		WorktreePath: "/tmp/test/tasks/task-align-1/api",
	}
	require.NoError(t, store.InsertTaskRepo(tr))

	l := &model.Lead{
		ID:         "lead-align-1",
		TaskRepoID: "tr-align-1",
		Status:     model.LeadStatusComplete,
		Attempt:    1,
		Output:     "all goals passed",
	}
	require.NoError(t, store.InsertLead(l))

	ctx := context.Background()
	coord.processTick(ctx)

	// Wait for alignment goroutine
	time.Sleep(300 * time.Millisecond)

	updated, err := store.GetTask("task-align-1")
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusComplete, updated.Status)

	decisions, err := store.GetAgenticDecisionsForTask("task-align-1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(decisions), 1)

	// PR should have been created
	assert.Len(t, pr.calls, 1)
}

func TestProcessRunningTask_LeadFailed_SchedulesRetry(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	runner := &mockLeadRunner{}
	wt := &mockWorktreeCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig())

	task := &model.Task{
		ID:          "task-retry-1",
		InstanceID:  "test-instance",
		Description: "test retry",
		Source:      "text",
		Status:      model.TaskStatusRunning,
	}
	require.NoError(t, store.InsertTask(task))

	tr := &model.TaskRepo{
		ID:           "tr-retry-1",
		TaskID:       "task-retry-1",
		RepoName:     "api",
		Spec:         "test spec",
		WorktreePath: "/tmp/test/tasks/task-retry-1/api",
	}
	require.NoError(t, store.InsertTaskRepo(tr))

	l := &model.Lead{
		ID:         "lead-retry-1",
		TaskRepoID: "tr-retry-1",
		Status:     model.LeadStatusFailed,
		Attempt:    1,
		Output:     "process crashed",
	}
	require.NoError(t, store.InsertLead(l))

	ctx := context.Background()
	coord.processTick(ctx)

	assert.True(t, coord.retries.Has("lead-retry-1"))
}

func TestProcessRetries(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	leadStore := lead.NewStore(store.db)
	runner := &mockLeadRunner{
		runFunc: func(ctx context.Context, cfg lead.RunConfig) (*lead.RunResult, error) {
			if err := leadStore.SetLeadStarted(cfg.LeadID); err != nil {
				return nil, err
			}
			if err := leadStore.SetLeadFinished(cfg.LeadID, model.LeadStatusComplete, "retry success"); err != nil {
				return nil, err
			}
			return &lead.RunResult{Status: model.LeadStatusComplete, Output: "retry success"}, nil
		},
	}
	wt := &mockWorktreeCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig())

	task := &model.Task{
		ID:          "task-process-retry",
		InstanceID:  "test-instance",
		Description: "test process retry",
		Source:      "text",
		Status:      model.TaskStatusRunning,
	}
	require.NoError(t, store.InsertTask(task))

	tr := &model.TaskRepo{
		ID:           "tr-process-retry",
		TaskID:       "task-process-retry",
		RepoName:     "api",
		Spec:         "test spec",
		WorktreePath: "/tmp/test/tasks/task-process-retry/api",
	}
	require.NoError(t, store.InsertTaskRepo(tr))

	origLead := &model.Lead{
		ID:         "lead-process-retry",
		TaskRepoID: "tr-process-retry",
		Status:     model.LeadStatusFailed,
		Attempt:    1,
		Output:     "crashed",
	}
	require.NoError(t, store.InsertLead(origLead))

	// Schedule with immediate retry (delay already elapsed for test)
	coord.retries.schedule["lead-process-retry"] = retryEntry{
		attempt:   1,
		nextRetry: time.Now().Add(-1 * time.Second),
	}

	ctx := context.Background()
	coord.processRetries(ctx)

	// Wait for goroutine
	time.Sleep(200 * time.Millisecond)

	assert.False(t, coord.retries.Has("lead-process-retry"))

	leads, err := store.GetLeadsForTask("task-process-retry")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(leads), 2) // original + retry
}

func TestProcessStuckLead(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	runner := &mockLeadRunner{}
	wt := &mockWorktreeCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig())

	task := &model.Task{
		ID:          "task-stuck-1",
		InstanceID:  "test-instance",
		Description: "test stuck handling",
		Source:      "text",
		Status:      model.TaskStatusRunning,
	}
	require.NoError(t, store.InsertTask(task))

	tr := &model.TaskRepo{
		ID:           "tr-stuck-1",
		TaskID:       "task-stuck-1",
		RepoName:     "api",
		Spec:         "test spec",
		WorktreePath: "/tmp/test/tasks/task-stuck-1/api",
	}
	require.NoError(t, store.InsertTaskRepo(tr))

	l := &model.Lead{
		ID:         "lead-stuck-1",
		TaskRepoID: "tr-stuck-1",
		Status:     model.LeadStatusStuck,
		Attempt:    1,
		Output:     "stuck after 3 attempts",
	}
	require.NoError(t, store.InsertLead(l))

	ctx := context.Background()
	coord.processTick(ctx)

	// Wait for stuck analysis
	time.Sleep(200 * time.Millisecond)

	decisions, err := store.GetAgenticDecisionsForTask("task-stuck-1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(decisions), 1)

	assert.True(t, coord.retries.Has("lead-stuck-1"))
}

func TestStartStop(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	runner := &mockLeadRunner{}
	wt := &mockWorktreeCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig())

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- coord.Start(ctx)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	err := <-errCh
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFullLifecycle(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	leadStore := lead.NewStore(store.db)
	runner := &mockLeadRunner{
		runFunc: func(ctx context.Context, cfg lead.RunConfig) (*lead.RunResult, error) {
			if err := leadStore.SetLeadStarted(cfg.LeadID); err != nil {
				return nil, err
			}
			if err := leadStore.SetLeadFinished(cfg.LeadID, model.LeadStatusComplete, "all goals passed"); err != nil {
				return nil, err
			}
			return &lead.RunResult{Status: model.LeadStatusComplete, Output: "all goals passed"}, nil
		},
	}
	wt := &mockWorktreeCreator{}
	dc := &mockDiffCollector{}
	pr := &mockPRCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig(),
		WithDiffCollector(dc), WithPRCreator(pr))

	task := &model.Task{
		ID:          fmt.Sprintf("task-lifecycle-%d", time.Now().UnixNano()),
		InstanceID:  "test-instance",
		Description: "Full lifecycle test task",
		Source:      "text",
		Status:      model.TaskStatusPending,
	}
	require.NoError(t, store.InsertTask(task))

	ctx := context.Background()

	// Tick 1: pending -> decomposing -> running (spawns lead goroutine)
	coord.processTick(ctx)

	// Wait for lead goroutine to finish and update DB
	time.Sleep(300 * time.Millisecond)

	// Tick 2: running -> all leads complete -> aligning (spawns alignment goroutine)
	coord.processTick(ctx)

	// Wait for alignment goroutine to finish
	time.Sleep(500 * time.Millisecond)

	// Verify task completed the full lifecycle
	updated, err := store.GetTask(task.ID)
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusComplete, updated.Status, "task should complete full lifecycle")

	// Verify all agentic decisions were recorded
	decisions, err := store.GetAgenticDecisionsForTask(task.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(decisions), 3) // sufficiency + decomposition + alignment

	// Verify events were recorded
	var eventCount int
	err = store.db.QueryRow(`SELECT COUNT(*) FROM events WHERE task_id = ?`, task.ID).Scan(&eventCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, eventCount, 1)
}

func TestProcessPendingTask_SkipsSufficiencyWhenPreChecked(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	leadStore := lead.NewStore(store.db)
	runner := &mockLeadRunner{
		runFunc: func(ctx context.Context, cfg lead.RunConfig) (*lead.RunResult, error) {
			if err := leadStore.SetLeadStarted(cfg.LeadID); err != nil {
				return nil, err
			}
			if err := leadStore.SetLeadFinished(cfg.LeadID, model.LeadStatusComplete, "done"); err != nil {
				return nil, err
			}
			return &lead.RunResult{Status: model.LeadStatusComplete, Output: "done"}, nil
		},
	}
	wt := &mockWorktreeCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig())

	task := &model.Task{
		ID:                 "task-prechecked-1",
		InstanceID:         "test-instance",
		Description:        "Pre-checked task",
		Source:             "text",
		Status:             model.TaskStatusPending,
		SufficiencyChecked: true, // Already checked at intake
	}
	require.NoError(t, store.InsertTask(task))

	ctx := context.Background()
	coord.processTick(ctx)

	time.Sleep(200 * time.Millisecond)

	decisions, err := store.GetAgenticDecisionsForTask("task-prechecked-1")
	require.NoError(t, err)

	// Should only have decomposition decision (no sufficiency)
	assert.Equal(t, 1, len(decisions))
	assert.Equal(t, model.AgenticDecomposition, decisions[0].NodeType)
}

func TestProcessPendingTask_InstanceAwareDecomposition(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	leadStore := lead.NewStore(store.db)
	runner := &mockLeadRunner{
		runFunc: func(ctx context.Context, cfg lead.RunConfig) (*lead.RunResult, error) {
			if err := leadStore.SetLeadStarted(cfg.LeadID); err != nil {
				return nil, err
			}
			if err := leadStore.SetLeadFinished(cfg.LeadID, model.LeadStatusComplete, "done"); err != nil {
				return nil, err
			}
			return &lead.RunResult{Status: model.LeadStatusComplete, Output: "done"}, nil
		},
	}
	wt := &mockWorktreeCreator{}

	cfg := testConfig()
	cfg.RepoNames = []string{"api", "frontend", "shared-lib"}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", cfg)

	task := &model.Task{
		ID:                 "task-repo-aware-1",
		InstanceID:         "test-instance",
		Description:        "Instance-aware decomposition test",
		Source:             "text",
		Status:             model.TaskStatusPending,
		SufficiencyChecked: true,
	}
	require.NoError(t, store.InsertTask(task))

	ctx := context.Background()
	coord.processTick(ctx)

	time.Sleep(200 * time.Millisecond)

	// Verify decomposition decision includes repo names in the prompt
	decisions, err := store.GetAgenticDecisionsForTask("task-repo-aware-1")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(decisions), 1)

	// Find the decomposition decision
	for _, d := range decisions {
		if d.NodeType == model.AgenticDecomposition {
			assert.Contains(t, d.Input, "api, frontend, shared-lib")
			assert.Contains(t, d.Input, "MUST only use repos from the available list")
		}
	}
}

func TestSufficiencyCheckedFieldPersistence(t *testing.T) {
	store, _ := setupCoordTestDB(t)

	task := &model.Task{
		ID:                 "task-suff-persist",
		InstanceID:         "test-instance",
		Description:        "Test persistence",
		Source:             "text",
		Status:             model.TaskStatusPending,
		SufficiencyChecked: true,
	}
	require.NoError(t, store.InsertTask(task))

	loaded, err := store.GetTask("task-suff-persist")
	require.NoError(t, err)
	assert.True(t, loaded.SufficiencyChecked)

	// Test false case
	task2 := &model.Task{
		ID:                 "task-suff-persist-2",
		InstanceID:         "test-instance",
		Description:        "Test persistence false",
		Source:             "text",
		Status:             model.TaskStatusPending,
		SufficiencyChecked: false,
	}
	require.NoError(t, store.InsertTask(task2))

	loaded2, err := store.GetTask("task-suff-persist-2")
	require.NoError(t, err)
	assert.False(t, loaded2.SufficiencyChecked)
}

func TestActiveLeadTracking(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	started := make(chan struct{})
	blocked := make(chan struct{})
	runner := &mockLeadRunner{
		runFunc: func(ctx context.Context, cfg lead.RunConfig) (*lead.RunResult, error) {
			close(started)
			<-blocked
			return &lead.RunResult{Status: model.LeadStatusComplete, Output: "done"}, nil
		},
	}
	wt := &mockWorktreeCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig())

	task := &model.Task{
		ID:          "task-tracking-1",
		InstanceID:  "test-instance",
		Description: "test active lead tracking",
		Source:      "text",
		Status:      model.TaskStatusPending,
	}
	require.NoError(t, store.InsertTask(task))

	ctx := context.Background()
	coord.processTick(ctx)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("lead did not start in time")
	}

	assert.Equal(t, 1, coord.ActiveLeadCount())

	close(blocked)
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, 0, coord.ActiveLeadCount())
}

func TestAlignmentFailure_RedispatchesMisalignedRepos(t *testing.T) {
	store, _ := setupCoordTestDB(t)

	// Create mock claude that returns alignment failure
	mockDir := t.TempDir()
	mockScript := filepath.Join(mockDir, "claude")
	script := `#!/bin/bash
PROMPT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -p) shift ;;
        --model|--output-format) shift; shift ;;
        *) PROMPT="$1"; shift ;;
    esac
done
if echo "$PROMPT" | grep -qi "alignment"; then
    echo '{"pass": false, "feedback": "API types mismatch between frontend and backend", "criteria": [{"name": "api_contract", "pass": false, "details": "type mismatch"}], "misaligned_repos": ["frontend"]}'
else
    echo '{"result": "mock"}'
fi
`
	require.NoError(t, os.WriteFile(mockScript, []byte(script), 0755))
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))

	leadStore := lead.NewStore(store.db)
	runner := &mockLeadRunner{
		runFunc: func(ctx context.Context, cfg lead.RunConfig) (*lead.RunResult, error) {
			if err := leadStore.SetLeadStarted(cfg.LeadID); err != nil {
				return nil, err
			}
			if err := leadStore.SetLeadFinished(cfg.LeadID, model.LeadStatusComplete, "done"); err != nil {
				return nil, err
			}
			return &lead.RunResult{Status: model.LeadStatusComplete, Output: "done"}, nil
		},
	}
	wt := &mockWorktreeCreator{}
	dc := &mockDiffCollector{}
	pr := &mockPRCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig(),
		WithDiffCollector(dc), WithPRCreator(pr))

	task := &model.Task{
		ID:          "task-redispatch-1",
		InstanceID:  "test-instance",
		Description: "test redispatch on alignment failure",
		Source:      "text",
		Status:      model.TaskStatusRunning,
	}
	require.NoError(t, store.InsertTask(task))

	// Two repos: frontend (will fail alignment) and backend (will pass)
	tr1 := &model.TaskRepo{
		ID: "tr-rd-frontend", TaskID: "task-redispatch-1", RepoName: "frontend",
		Spec: "build UI", WorktreePath: "/tmp/test/tasks/task-redispatch-1/frontend",
	}
	tr2 := &model.TaskRepo{
		ID: "tr-rd-backend", TaskID: "task-redispatch-1", RepoName: "backend",
		Spec: "build API", WorktreePath: "/tmp/test/tasks/task-redispatch-1/backend",
	}
	require.NoError(t, store.InsertTaskRepo(tr1))
	require.NoError(t, store.InsertTaskRepo(tr2))

	// Both leads complete
	require.NoError(t, store.InsertLead(&model.Lead{
		ID: "lead-rd-fe", TaskRepoID: "tr-rd-frontend", Status: model.LeadStatusComplete, Attempt: 1,
	}))
	require.NoError(t, store.InsertLead(&model.Lead{
		ID: "lead-rd-be", TaskRepoID: "tr-rd-backend", Status: model.LeadStatusComplete, Attempt: 1,
	}))

	ctx := context.Background()
	coord.processTick(ctx)

	// Wait for alignment goroutine
	time.Sleep(500 * time.Millisecond)

	// Task should be back to running (re-dispatched)
	updated, err := store.GetTask("task-redispatch-1")
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusRunning, updated.Status)

	// New lead should have been created for frontend only
	leads, err := store.GetLeadsForTask("task-redispatch-1")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(leads), 3) // 2 original + 1 re-dispatch

	// No PRs should have been created
	assert.Len(t, pr.calls, 0)
}

func TestAlignmentFailure_MaxAttemptsExceeded(t *testing.T) {
	store, _ := setupCoordTestDB(t)

	// Create mock claude that always returns alignment failure
	mockDir := t.TempDir()
	mockScript := filepath.Join(mockDir, "claude")
	script := `#!/bin/bash
PROMPT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -p) shift ;;
        --model|--output-format) shift; shift ;;
        *) PROMPT="$1"; shift ;;
    esac
done
if echo "$PROMPT" | grep -qi "alignment"; then
    echo '{"pass": false, "feedback": "still broken", "criteria": [], "misaligned_repos": ["api"]}'
else
    echo '{"result": "mock"}'
fi
`
	require.NoError(t, os.WriteFile(mockScript, []byte(script), 0755))
	t.Setenv("PATH", mockDir+":"+os.Getenv("PATH"))

	runner := &mockLeadRunner{}
	wt := &mockWorktreeCreator{}
	dc := &mockDiffCollector{}
	pr := &mockPRCreator{}

	cfg := testConfig()
	cfg.MaxAlignmentAttempts = 1
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", cfg,
		WithDiffCollector(dc), WithPRCreator(pr))

	task := &model.Task{
		ID:          "task-maxalign-1",
		InstanceID:  "test-instance",
		Description: "test max alignment attempts",
		Source:      "text",
		Status:      model.TaskStatusRunning,
	}
	require.NoError(t, store.InsertTask(task))

	tr := &model.TaskRepo{
		ID: "tr-maxalign", TaskID: "task-maxalign-1", RepoName: "api",
		Spec: "implement", WorktreePath: "/tmp/test/tasks/task-maxalign-1/api",
	}
	require.NoError(t, store.InsertTaskRepo(tr))

	require.NoError(t, store.InsertLead(&model.Lead{
		ID: "lead-maxalign", TaskRepoID: "tr-maxalign", Status: model.LeadStatusComplete, Attempt: 1,
	}))

	// Simulate that we've already had 2 alignment_started events (> max of 1)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		_, err := store.db.Exec(
			`INSERT INTO events (id, task_id, lead_id, type, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("evt-maxalign-%d", i), "task-maxalign-1", "", string(model.EventAlignmentStarted), "{}", now,
		)
		require.NoError(t, err)
	}

	ctx := context.Background()
	coord.processTick(ctx)

	// Wait for alignment goroutine
	time.Sleep(300 * time.Millisecond)

	// Task should be failed (exceeded max attempts)
	updated, err := store.GetTask("task-maxalign-1")
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusFailed, updated.Status)
}

func TestAlignmentPass_CreatesPRs(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	runner := &mockLeadRunner{}
	wt := &mockWorktreeCreator{}
	dc := &mockDiffCollector{}
	pr := &mockPRCreator{
		prURLs: map[string]string{
			"/tmp/test/tasks/task-pr-1/api":     "https://github.com/org/api/pull/42",
			"/tmp/test/tasks/task-pr-1/frontend": "https://github.com/org/frontend/pull/7",
		},
	}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig(),
		WithDiffCollector(dc), WithPRCreator(pr))

	task := &model.Task{
		ID:          "task-pr-1",
		InstanceID:  "test-instance",
		Description: "test PR creation",
		Source:      "text",
		Status:      model.TaskStatusRunning,
	}
	require.NoError(t, store.InsertTask(task))

	tr1 := &model.TaskRepo{
		ID: "tr-pr-api", TaskID: "task-pr-1", RepoName: "api",
		Spec: "api spec", WorktreePath: "/tmp/test/tasks/task-pr-1/api",
	}
	tr2 := &model.TaskRepo{
		ID: "tr-pr-fe", TaskID: "task-pr-1", RepoName: "frontend",
		Spec: "fe spec", WorktreePath: "/tmp/test/tasks/task-pr-1/frontend",
	}
	require.NoError(t, store.InsertTaskRepo(tr1))
	require.NoError(t, store.InsertTaskRepo(tr2))

	require.NoError(t, store.InsertLead(&model.Lead{
		ID: "lead-pr-api", TaskRepoID: "tr-pr-api", Status: model.LeadStatusComplete, Attempt: 1,
	}))
	require.NoError(t, store.InsertLead(&model.Lead{
		ID: "lead-pr-fe", TaskRepoID: "tr-pr-fe", Status: model.LeadStatusComplete, Attempt: 1,
	}))

	ctx := context.Background()
	coord.processTick(ctx)

	// Wait for alignment + PR creation
	time.Sleep(500 * time.Millisecond)

	// Task should be complete
	updated, err := store.GetTask("task-pr-1")
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusComplete, updated.Status)

	// Both PRs should have been created
	assert.Len(t, pr.calls, 2)

	// PR event should be recorded
	var eventCount int
	err = store.db.QueryRow(
		`SELECT COUNT(*) FROM events WHERE task_id = ? AND type = ?`,
		"task-pr-1", string(model.EventPRsCreated),
	).Scan(&eventCount)
	require.NoError(t, err)
	assert.Equal(t, 1, eventCount)
}

func TestAlignmentPass_PRFailureDoesNotBlockCompletion(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	runner := &mockLeadRunner{}
	wt := &mockWorktreeCreator{}
	dc := &mockDiffCollector{}
	pr := &mockPRCreator{err: fmt.Errorf("push failed: no remote")}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig(),
		WithDiffCollector(dc), WithPRCreator(pr))

	task := &model.Task{
		ID:          "task-prfail-1",
		InstanceID:  "test-instance",
		Description: "test PR failure",
		Source:      "text",
		Status:      model.TaskStatusRunning,
	}
	require.NoError(t, store.InsertTask(task))

	tr := &model.TaskRepo{
		ID: "tr-prfail", TaskID: "task-prfail-1", RepoName: "api",
		Spec: "spec", WorktreePath: "/tmp/test/tasks/task-prfail-1/api",
	}
	require.NoError(t, store.InsertTaskRepo(tr))

	require.NoError(t, store.InsertLead(&model.Lead{
		ID: "lead-prfail", TaskRepoID: "tr-prfail", Status: model.LeadStatusComplete, Attempt: 1,
	}))

	ctx := context.Background()
	coord.processTick(ctx)

	// Wait for alignment goroutine
	time.Sleep(300 * time.Millisecond)

	// Task should still complete even though PR creation failed
	updated, err := store.GetTask("task-prfail-1")
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusComplete, updated.Status)

	// PR attempt was made
	assert.Len(t, pr.calls, 1)
}

func TestAlignmentCollectsDiffs(t *testing.T) {
	store, _ := setupCoordTestDB(t)
	setupMockClaude(t)

	runner := &mockLeadRunner{}
	wt := &mockWorktreeCreator{}
	dc := &mockDiffCollector{
		diffs: map[string]string{
			"/tmp/test/tasks/task-diff-1/api": "diff content for api",
		},
		stats: map[string]string{
			"/tmp/test/tasks/task-diff-1/api": " api.go | 5 +++++\n 1 file changed",
		},
	}
	pr := &mockPRCreator{}
	coord := NewCoordinator(store, runner, wt, "/tmp/test", "test-instance", testConfig(),
		WithDiffCollector(dc), WithPRCreator(pr))

	task := &model.Task{
		ID:          "task-diff-1",
		InstanceID:  "test-instance",
		Description: "test diff collection",
		Source:      "text",
		Status:      model.TaskStatusRunning,
	}
	require.NoError(t, store.InsertTask(task))

	tr := &model.TaskRepo{
		ID: "tr-diff-1", TaskID: "task-diff-1", RepoName: "api",
		Spec: "implement endpoints", WorktreePath: "/tmp/test/tasks/task-diff-1/api",
	}
	require.NoError(t, store.InsertTaskRepo(tr))

	require.NoError(t, store.InsertLead(&model.Lead{
		ID: "lead-diff-1", TaskRepoID: "tr-diff-1", Status: model.LeadStatusComplete, Attempt: 1,
	}))

	ctx := context.Background()
	coord.processTick(ctx)

	// Wait for alignment goroutine
	time.Sleep(300 * time.Millisecond)

	// Verify alignment decision was stored with diff content in the prompt
	decisions, err := store.GetAgenticDecisionsForTask("task-diff-1")
	require.NoError(t, err)

	found := false
	for _, d := range decisions {
		if d.NodeType == model.AgenticAlignment {
			assert.Contains(t, d.Input, "diff content for api")
			assert.Contains(t, d.Input, "api.go | 5")
			found = true
		}
	}
	assert.True(t, found, "alignment decision should have been recorded")
}
