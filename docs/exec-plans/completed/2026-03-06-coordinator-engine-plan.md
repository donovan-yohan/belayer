# Execution Plan: Coordinator Engine (Goal 4)

**Date**: 2026-03-06
**Design Doc**: [coordinator-engine-design](../../design-docs/2026-03-06-coordinator-engine-design.md)

## Steps

### Step 1: Coordinator Store (`internal/coordinator/store.go`)
**Status**: complete
**Files**: `internal/coordinator/store.go`, `internal/coordinator/store_test.go`

Create the coordinator-specific SQLite query layer:
- `NewStore(db *sql.DB) *Store`
- `InsertTask(task *model.Task) error`
- `UpdateTaskStatus(taskID string, status model.TaskStatus) error`
- `GetTasksByStatus(status model.TaskStatus) ([]model.Task, error)`
- `GetTask(taskID string) (*model.Task, error)`
- `InsertTaskRepo(tr *model.TaskRepo) error`
- `GetTaskReposForTask(taskID string) ([]model.TaskRepo, error)`
- `InsertLead(lead *model.Lead) error`
- `GetLeadsForTask(taskID string) ([]model.Lead, error)` (joins through task_repos)
- `InsertAgenticDecision(d *model.AgenticDecision) error`
- Unit tests with in-memory SQLite

### Step 2: Retry Scheduler (`internal/coordinator/retry.go`)
**Status**: complete
**Files**: `internal/coordinator/retry.go`, `internal/coordinator/retry_test.go`

Implement exponential backoff retry scheduling:
- `NewRetryScheduler(baseDelay, maxDelay time.Duration, maxRetries int) *RetryScheduler`
- `Schedule(leadID string, attempt int) bool` — returns false if max retries exceeded
- `Ready(leadID string) bool` — true if delay has elapsed
- `Attempt(leadID string) int` — current attempt count
- `Remove(leadID string)` — clear entry after success
- Unit tests for backoff calculation and schedule

### Step 3: Agentic Node (`internal/coordinator/agentic.go`)
**Status**: complete
**Files**: `internal/coordinator/agentic.go`, `internal/coordinator/agentic_test.go`

Implement ephemeral Claude session executor:
- `NewAgenticNode(store *Store, nodeType model.AgenticNodeType, model string) *AgenticNode`
- `Execute(ctx context.Context, taskID, prompt string) (*AgenticResult, error)`:
  1. Run `claude -p --model <model> --output-format json "<prompt>"` via exec.Command
  2. Capture stdout, measure duration
  3. Store in agentic_decisions table
  4. Return AgenticResult with raw output
- Tests with mock `claude` command (same pattern as lead tests)

### Step 4: Coordinator Core (`internal/coordinator/coordinator.go`)
**Status**: complete
**Files**: `internal/coordinator/coordinator.go`, `internal/coordinator/coordinator_test.go`

Implement the central engine:
- `CoordinatorConfig` struct with poll interval, retry settings, model config
- `DefaultConfig() CoordinatorConfig`
- `NewCoordinator(store, leadStore, leadRunner, instanceDir, config) *Coordinator`
- `Start(ctx context.Context) error` — launch polling goroutine
- `Stop()` — cancel context, wait for shutdown
- `processTick(ctx context.Context)` — main loop body:
  - Process pending tasks -> decomposing
  - Process decomposing tasks -> running (spawn leads)
  - Process running tasks -> check lead status
  - Process aligning tasks -> check alignment result
  - Process retry schedule
- `startDecomposition(ctx, task)` — sufficiency + decomposition agentic nodes
- `spawnLeads(ctx, task, taskRepos)` — create leads, start goroutines
- `checkLeadProgress(ctx, task)` — detect all-done, stuck, failed
- `startAlignment(ctx, task)` — alignment agentic node
- `retryLead(ctx, lead, taskRepo)` — re-spawn with new attempt
- Active leads tracking via `sync.Mutex` protected map
- Integration tests with mock claude

### Step 5: Wire Coordinator into CLI
**Status**: complete
**Files**: `internal/cli/task.go`, `internal/cli/status.go`

Update CLI to use coordinator:
- `belayer task create` — inserts task into DB, starts coordinator if not running
- `belayer status` — queries coordinator store for task/lead status
- Coordinator lifecycle management (start on task create, stop on ctrl-C)

### Step 6: Tests & Verification
**Status**: complete
**Files**: All test files

- Run `go test ./...` and verify all tests pass
- Verify `go build -o belayer ./cmd/belayer` compiles
- Verify acceptance criteria:
  - [x] Coordinator goroutine polls SQLite (configurable interval)
  - [x] Spawns/monitors lead processes via os/exec
  - [x] Detects crashes with exponential backoff retry
  - [x] Triggers agentic nodes (ephemeral claude -p sessions)
  - [x] Processes outputs and writes to SQLite
