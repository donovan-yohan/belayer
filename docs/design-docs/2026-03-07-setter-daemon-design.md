# Setter Daemon Design

**Date**: 2026-03-07
**Goal**: PRD Goal 2 — Setter daemon: DAG executor with tmux management
**Parent Design**: [agent-friendly-architecture-design](2026-03-07-agent-friendly-architecture-design.md)
**Status**: Proposed

## Overview

The setter daemon (`belayer setter --instance <name>`) is a long-running Go process that polls SQLite for pending tasks, builds per-repo goal DAGs, manages tmux sessions/windows for lead execution, watches for DONE.json completion markers, and handles crash recovery. It is the core orchestration engine of belayer.

## Architecture

### Entry Point

`belayer setter --instance <name> [--max-leads 8] [--poll-interval 5s] [--stale-timeout 30m]`

The CLI command creates the setter and calls `Run()`, which blocks until the process is killed or all tasks complete.

### Package Structure

```
internal/
├── setter/
│   ├── setter.go        # Setter struct, Run() event loop, lifecycle
│   ├── dag.go           # DAG building and ready-goal resolution
│   ├── taskrunner.go    # Per-task state machine
│   └── setter_test.go   # Unit tests
├── tmux/
│   ├── tmux.go          # tmux session/window management wrappers
│   └── tmux_test.go     # Tests (mock-friendly)
├── logmgr/
│   ├── logmgr.go        # Log rotation, compression, cleanup, stats
│   └── logmgr_test.go   # Tests
├── cli/
│   ├── setter.go        # CLI command definition
│   └── logs.go          # `belayer logs` subcommands
```

### Core Components

#### 1. Setter (setter/setter.go)

The main event loop:

```go
type Setter struct {
    instanceName string
    instanceDir  string
    store        *store.Store
    tmux         TmuxManager
    logMgr       *logmgr.LogManager
    maxLeads     int
    pollInterval time.Duration
    staleTimeout time.Duration

    // Runtime state
    tasks     map[string]*TaskRunner  // taskID -> runner
    leadQueue []QueuedGoal            // FIFO queue for overflow
    activeLeads int                   // current concurrent leads
}
```

**Event loop** (every `pollInterval`):
1. Poll SQLite for `pending` tasks → initialize new TaskRunners
2. For each active TaskRunner:
   a. Check for newly completed goals (scan DONE.json files)
   b. Update DAG, queue newly unblocked goals
   c. Check for stale goals (no DONE.json + dead tmux window)
   d. Check if all goals complete → transition to reviewing
   e. Check for spotter verdicts → approve/redistribute/stuck
3. Process lead queue → spawn leads up to `maxLeads`
4. Run log cleanup if needed

#### 2. DAG (setter/dag.go)

Builds and queries the dependency graph for a task's goals.

```go
type DAG struct {
    goals    map[string]*model.Goal  // goalID -> goal
    children map[string][]string     // goalID -> dependent goalIDs
}

func BuildDAG(goals []model.Goal) *DAG
func (d *DAG) ReadyGoals() []model.Goal     // goals with all deps complete
func (d *DAG) MarkComplete(goalID string)
func (d *DAG) AllComplete() bool
```

#### 3. TaskRunner (setter/taskrunner.go)

Per-task state machine managing the lifecycle of a single task.

```go
type TaskRunner struct {
    task       *model.Task
    dag        *DAG
    worktrees  map[string]string  // repoName -> worktreePath
    tmuxSession string            // tmux session name
}
```

States: `pending → running → reviewing → complete/stuck`

#### 4. TmuxManager (tmux/tmux.go)

Wraps tmux CLI commands. Interface-based for testability.

```go
type TmuxManager interface {
    HasSession(name string) bool
    NewSession(name string) error
    KillSession(name string) error
    NewWindow(session, windowName string) error
    KillWindow(session, windowName string) error
    SendKeys(session, windowName, keys string) error
    ListWindows(session string) ([]string, error)
    PipePane(session, windowName, logPath string) error
}
```

Implementation uses `exec.Command("tmux", ...)`.

#### 5. LogManager (logmgr/logmgr.go)

Handles log rotation, compression, and cleanup.

```go
type LogManager struct {
    logsDir          string
    maxGoalLogSize   int64  // default 10MB
    maxInstanceSize  int64  // default 500MB
    retentionDays    int    // default 7
}

func (lm *LogManager) LogPath(taskID, goalID string) string
func (lm *LogManager) CheckRotation(taskID, goalID string) error
func (lm *LogManager) CompressTaskLogs(taskID string) error
func (lm *LogManager) Cleanup() error
func (lm *LogManager) Stats() (map[string]int64, error)
```

## Data Flow

### Task Initialization

1. Setter polls SQLite → finds task with status `pending`
2. Update task status to `running`
3. Parse `goals_json` → build DAG
4. Create worktrees for each repo (reuse `instance.CreateWorktree`)
5. Create tmux session `task-<id>`
6. Identify ready goals (no dependencies)
7. Queue ready goals for spawning

### Goal Spawning

1. Dequeue goal from lead queue (FIFO)
2. Check `activeLeads < maxLeads`
3. Create tmux window `<repo>-<goalID>` in task's session
4. Enable pipe-pane logging
5. Send keys to run the agent command (placeholder for Goal 3's AgentSpawner)
6. Update goal status to `running` in SQLite
7. Insert `goal_started` event
8. Increment `activeLeads`

### Goal Completion

1. Scan worktree for `DONE.json` file
2. Parse DONE.json → extract status, summary
3. Update goal status to `complete` in SQLite
4. Insert `goal_completed` event with DONE.json payload
5. Mark goal complete in DAG
6. Decrement `activeLeads`
7. Check for newly unblocked goals → queue them
8. Check if all goals complete → spawn spotter (Goal 4 scope)

### Stale Goal Detection

1. For goals in `running` status:
   - Check if tmux window still exists
   - If window dead and no DONE.json: increment attempt, mark `failed`
   - If goal has been running longer than `staleTimeout`: mark `failed`
2. Failed goals with `attempt < 3` are re-queued
3. Failed goals at max attempts → mark task as `stuck`

### Crash Recovery

On startup, before entering the event loop:

1. Query SQLite for tasks in `running` or `reviewing` status
2. For each task:
   a. Check tmux session existence
   b. Scan worktrees for DONE.json files
   c. Update SQLite to match filesystem reality
   d. Re-create tmux sessions if needed
   e. Resume DAG execution from current state

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| DONE.json scanning | Filesystem polling every `pollInterval` | Simple, vendor-agnostic, no inotify complexity |
| tmux as interface | `TmuxManager` interface | Enables testing without real tmux |
| Log directory | `<instanceDir>/logs/<taskID>/` | Separate from worktrees, survives worktree cleanup |
| Goal spawning placeholder | `echo "placeholder"` sent via tmux | Goal 3 implements real AgentSpawner; setter just needs to send keys |
| Stale timeout | 30 minutes default | Generous but prevents zombie goals |
| Max retries | 3 attempts per goal | Balance between persistence and futility |
| Poll interval | 5 seconds default | Fast enough for responsiveness, light on resources |

## CLI Commands

### `belayer setter`

```
belayer setter --instance <name> [--max-leads 8] [--poll-interval 5s] [--stale-timeout 30m]
```

### `belayer logs`

```
belayer logs --task <id> [--instance <name>]         # View task logs
belayer logs --goal <id> [--instance <name>]         # View goal log
belayer logs cleanup [--instance <name>]             # Manual cleanup
belayer logs stats [--instance <name>]               # Disk usage stats
```

## Store Additions

New methods needed on `store.Store`:

```go
// GetPendingTasks returns tasks with status=pending for a given instance
func (s *Store) GetPendingTasks(instanceID string) ([]model.Task, error)

// GetRunningTasks returns tasks with status=running or reviewing for a given instance
func (s *Store) GetRunningTasks(instanceID string) ([]model.Task, error)

// IncrementGoalAttempt increments the attempt counter for a goal
func (s *Store) IncrementGoalAttempt(goalID string) error

// InsertSpotterReview records a spotter review verdict
func (s *Store) InsertSpotterReview(review *model.SpotterReview) error

// GetSpotterReviewsForTask returns all spotter reviews for a task
func (s *Store) GetSpotterReviewsForTask(taskID string) ([]model.SpotterReview, error)

// InsertGoals inserts correction goals (for spotter redistribution)
func (s *Store) InsertGoals(goals []model.Goal) error
```

## File Conventions

- DONE.json location: `<worktreePath>/DONE.json`
- Log files: `<instanceDir>/logs/<taskID>/<goalID>.log`
- Compressed logs: `<instanceDir>/logs/<taskID>/<goalID>.log.gz`
- tmux session names: `belayer-task-<taskID>`
- tmux window names: `<repoName>-<goalID>`
