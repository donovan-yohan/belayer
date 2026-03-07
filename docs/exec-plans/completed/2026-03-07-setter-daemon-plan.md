# Execution Plan: Setter Daemon

**Goal**: PRD Goal 2 — Setter daemon: DAG executor with tmux management
**Design Doc**: [setter-daemon-design](../../design-docs/2026-03-07-setter-daemon-design.md)
**Status**: Complete

## Steps

### 1. Store additions
- [x] Add `GetPendingTasks`, `GetActiveTasks`, `IncrementGoalAttempt`, `ResetGoalStatus`, `InsertSpotterReview`, `GetSpotterReviewsForTask`, `InsertGoals` to `internal/store/store.go`
- [x] Add tests for new store methods in `internal/store/store_test.go`

### 2. tmux package
- [x] Create `internal/tmux/tmux.go` with `TmuxManager` interface and `RealTmux` implementation
- [x] Methods: `HasSession`, `NewSession`, `KillSession`, `NewWindow`, `KillWindow`, `SendKeys`, `ListWindows`, `PipePane`
- [x] Create `internal/tmux/tmux_test.go` with integration tests (skip if no tmux)

### 3. Log manager package
- [x] Create `internal/logmgr/logmgr.go` with `LogManager` struct
- [x] Methods: `LogPath`, `EnsureDir`, `CheckRotation`, `CompressTaskLogs`, `Cleanup`, `Stats`
- [x] Create `internal/logmgr/logmgr_test.go` (11 tests)

### 4. DAG implementation
- [x] Create `internal/setter/dag.go` with `DAG` struct
- [x] Methods: `BuildDAG`, `ReadyGoals`, `MarkComplete`, `MarkFailed`, `MarkRunning`, `AllComplete`, `Get`, `Goals`
- [x] Add tests in `internal/setter/dag_test.go` (8 tests)

### 5. TaskRunner implementation
- [x] Create `internal/setter/taskrunner.go` with per-task state machine
- [x] Methods: `Init`, `SpawnGoal`, `CheckCompletions`, `CheckStaleGoals`, `AllGoalsComplete`, `HasStuckGoals`, `Cleanup`

### 6. Setter core implementation
- [x] Create `internal/setter/setter.go` with main event loop
- [x] Methods: `New`, `Run`, `tick`, `pollPendingTasks`, `processLeadQueue`, `recover`
- [x] Handle shutdown via context cancellation

### 7. Setter tests
- [x] Create `internal/setter/setter_test.go` with mockTmux
- [x] Test: TaskRunner_Init — DAG built, ready goals identified
- [x] Test: TaskRunner_SpawnGoal — window created, status updated
- [x] Test: TaskRunner_CheckCompletions — DONE.json scanned, dependents unblocked
- [x] Test: TaskRunner_CheckStaleGoals — dead window detected, goal retried
- [x] Test: TaskRunner_StaleTimeout — timed-out goal detected
- [x] Test: TaskRunner_HasStuckGoals — max attempts detection
- [x] Test: Setter_MaxLeadsCap — FIFO queue respects cap
- [x] Test: Setter_CrashRecovery — re-reads state from SQLite + DONE.json
- [x] Test: Setter_RunTickCycle — event loop with context cancellation

### 8. CLI commands
- [x] Create `internal/cli/setter.go` — `belayer setter --instance <name> [--max-leads] [--poll-interval] [--stale-timeout]`
- [x] Create `internal/cli/logs.go` — `belayer logs` subcommands (view, cleanup, stats)
- [x] Register in `internal/cli/root.go`

### 9. Integration verification
- [x] `go build -o belayer ./cmd/belayer` succeeds
- [x] `go test ./...` passes (all 50+ tests across 7 packages)
- [x] `belayer setter --help` shows correct flags
- [x] `belayer logs --help` shows subcommands
