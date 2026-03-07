# Design: Coordinator Engine (Goal 4)

**Date**: 2026-03-06
**Goal**: Implement the coordinator engine — a deterministic Go state machine that polls SQLite for lead state changes, spawns/monitors lead processes, detects crashes with exponential backoff retry, triggers agentic nodes (ephemeral `claude -p` sessions), and writes decisions to SQLite.

## Overview

The coordinator is the central orchestration layer in belayer's 3-layer architecture. It bridges the gap between user-facing CLI commands and per-repo lead execution loops. The coordinator is deterministic Go code (no persistent AI state) that drives tasks through their lifecycle by polling SQLite and invoking ephemeral Claude sessions ("agentic nodes") only for judgment calls.

## Architecture

```
User (CLI) creates task
        |
        v
  Coordinator Engine (goroutine)
    |-- Polls SQLite for state changes (configurable interval)
    |-- State machine drives task lifecycle:
    |     pending -> decomposing -> running -> aligning -> complete/failed
    |-- Spawns lead processes via lead.Runner
    |-- Monitors running leads (goroutines + in-memory tracking)
    |-- Detects crashes, schedules retry with exponential backoff
    |-- Triggers agentic nodes for:
    |     - Sufficiency check
    |     - Task decomposition
    |     - Alignment review
    |     - Stuck analysis
    |-- Writes all decisions to SQLite (agentic_decisions table)
```

## Design Decisions

### 1. Coordinator as a long-running goroutine with tick-based polling

The coordinator runs as a goroutine started by `Coordinator.Start()`. It uses a `time.Ticker` to poll SQLite at a configurable interval (default: 2 seconds). Each tick triggers a `processTick()` that queries for actionable state and drives transitions.

Why polling over channels/events: SQLite is the single source of truth. Polling keeps the coordinator stateless and crash-recoverable — restart picks up from current DB state.

### 2. Task state machine with explicit transitions

```
pending ──────> decomposing ──────> running ──────> aligning ──────> complete
   |                |                  |                |                |
   └─> failed       └─> failed        └─> failed       └─> failed     (terminal)
```

Each transition is a method on the Coordinator: `processDecomposing()`, `processRunning()`, `processAligning()`. The coordinator only processes one transition per task per tick to keep things simple and debuggable.

### 3. Lead process management via goroutines

Each lead is spawned as a goroutine that calls `lead.Runner.Run()`. The coordinator tracks running leads in an `activeLeads` map (`map[string]context.CancelFunc`). This provides:
- Concurrent execution across repos
- Clean cancellation via context
- Crash detection when the goroutine returns with error

Lead completion is detected on the next poll tick by querying for leads with terminal status.

### 4. Exponential backoff for crash retry

When a lead crashes (status=failed, not stuck), the coordinator schedules a retry. Backoff formula:

```
delay = min(baseDelay * 2^attempt, maxDelay)
baseDelay = 5 seconds
maxDelay = 5 minutes
maxRetries = 3
```

A `retrySchedule` map tracks next-eligible-retry timestamps. The coordinator checks this on each tick.

### 5. Agentic nodes as subprocess invocations

Each agentic node:
1. Builds a prompt from Go templates + SQLite context
2. Runs `claude -p --model <model> --output-format json "<prompt>"` via `os/exec`
3. Captures stdout as the response
4. Parses JSON response
5. Stores input/output/model/duration in `agentic_decisions` table
6. Returns structured result to coordinator

The `AgenticNode` type encapsulates this: `Execute(ctx, input) -> (output, error)`.

### 6. Agentic node types and their contracts

**Sufficiency Check** (Goal 5 will implement prompt, coordinator provides the hook):
- Input: task description, repo list
- Output: `{"sufficient": true/false, "gaps": ["...", "..."]}`
- Triggers: When task transitions from pending to decomposing

**Task Decomposition** (Goal 5 will implement prompt, coordinator provides the hook):
- Input: task description, repo names, instance context
- Output: `{"repos": [{"name": "api", "spec": "..."}, ...]}`
- Triggers: After sufficiency check passes

**Alignment Review** (Goal 6 will implement prompt, coordinator provides the hook):
- Input: diffs from all repo worktrees for a task
- Output: `{"pass": true/false, "criteria": [...], "feedback": "..."}`
- Triggers: When all leads for a task are complete

**Stuck Analysis**:
- Input: lead status, goals, last output, error info
- Output: `{"diagnosis": "...", "recovery": "...", "should_retry": true/false}`
- Triggers: When a lead reports stuck status

### 7. Coordinator store (new package)

A new `internal/coordinator/store.go` provides coordinator-specific DB queries:
- `GetPendingTasks()` - tasks with status=pending
- `GetTasksByStatus(status)` - tasks filtered by status
- `GetLeadsForTask(taskID)` - all leads via task_repos join
- `InsertTask(task)` / `UpdateTaskStatus(taskID, status)`
- `InsertTaskRepo(taskRepo)` / `InsertLead(lead)`
- `InsertAgenticDecision(decision)`
- `GetTaskReposForTask(taskID)`

### 8. Configuration

```go
type CoordinatorConfig struct {
    PollInterval   time.Duration // Default: 2s
    MaxLeadRetries int           // Default: 3
    BaseRetryDelay time.Duration // Default: 5s
    MaxRetryDelay  time.Duration // Default: 5min
    AgenticModel   string        // Default: "claude-sonnet-4-6"
}
```

Configurable via instance settings (future) or sensible defaults.

## Package Structure

```
internal/coordinator/
  coordinator.go     # Core engine: Start/Stop, processTick, state transitions
  coordinator_test.go
  store.go           # Coordinator-specific SQLite queries
  store_test.go
  agentic.go         # AgenticNode: Execute claude -p, parse results, store decisions
  agentic_test.go
  retry.go           # Exponential backoff retry scheduler
  retry_test.go
```

## Key Types

```go
// Coordinator is the central orchestration engine.
type Coordinator struct {
    store       *Store
    leadStore   *lead.Store
    leadRunner  *lead.Runner
    instanceDir string
    config      CoordinatorConfig
    activeLeads map[string]context.CancelFunc // leadID -> cancel
    retries     *RetryScheduler
    mu          sync.Mutex
}

// AgenticNode runs ephemeral claude -p sessions.
type AgenticNode struct {
    store     *Store
    model     string
    nodeType  model.AgenticNodeType
}

// AgenticResult is the parsed output from an agentic node.
type AgenticResult struct {
    Raw      string          // Raw JSON output
    Parsed   json.RawMessage // Parsed for type-specific handling
    Duration time.Duration
}

// RetryScheduler tracks backoff timing for failed leads.
type RetryScheduler struct {
    schedule   map[string]retryEntry // leadID -> entry
    baseDelay  time.Duration
    maxDelay   time.Duration
    maxRetries int
}
```

## State Transition Logic

### processTick()
```
1. Query tasks by status
2. For each pending task:     -> startDecomposition(task)
3. For each decomposing task: -> check if decomposition complete
4. For each running task:     -> checkLeadProgress(task)
5. For each aligning task:    -> check if alignment complete
6. Check retry schedule for eligible retries
```

### startDecomposition(task)
```
1. Update task status -> decomposing
2. Run sufficiency agentic node (placeholder prompt for now)
3. If sufficient: run decomposition agentic node
4. Parse decomposition output -> create TaskRepo records
5. For each TaskRepo: create worktree, insert lead record
6. Spawn lead goroutines
7. Update task status -> running
```

### checkLeadProgress(task)
```
1. Query all leads for this task
2. If any lead stuck: trigger stuck analysis agentic node
3. If all leads complete: update task status -> aligning, trigger alignment
4. If any lead failed: check retry eligibility, schedule or fail task
```

### alignment(task)
```
1. Run alignment agentic node (placeholder prompt for now)
2. If pass: update task status -> complete
3. If fail: re-dispatch failed repos with feedback
```

## Database Changes

No new tables needed — the existing schema (`tasks`, `task_repos`, `leads`, `events`, `agentic_decisions`) already supports all coordinator operations. The coordinator store provides query methods over existing tables.

## Testing Strategy

- **Unit tests**: State transition logic with in-memory SQLite, mock agentic node
- **Store tests**: CRUD operations on tasks, task_repos, leads, agentic_decisions
- **Retry tests**: Backoff calculation, schedule management
- **Agentic node tests**: Mock `claude` command (same pattern as lead tests)
- **Integration test**: Full tick cycle with mock claude — create task, decompose, spawn leads, complete, align
- **Process management**: Verify lead goroutines are tracked, cancelled on shutdown

## Error Handling

- **Agentic node failure**: Log error, mark task as failed, store partial output
- **Lead crash**: Detected by Runner.Run() returning error, schedule retry with backoff
- **SQLite error**: Log and retry on next tick (transient), fatal if persistent
- **Context cancellation**: Cancel all active leads, clean shutdown
- **Malformed agentic output**: Store raw output, log parse error, treat as failure
