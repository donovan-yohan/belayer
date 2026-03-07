# Execution Plan: Goal 4 - Spotter Cross-Repo Review

**Design Doc**: [2026-03-07-spotter-review-design.md](../../design-docs/2026-03-07-spotter-review-design.md)

## Steps

### Step 1: Create `internal/spotter/` package - verdict types
- [x] Create `internal/spotter/verdict.go` with VerdictJSON and RepoVerdict types
- **Files**: `internal/spotter/verdict.go`

### Step 2: Create spotter prompt template
- [x] Create `internal/spotter/prompt.go` with SpotterPromptData, RepoDiff, GoalSummary, BuildSpotterPrompt
- [x] Create `internal/spotter/prompt_test.go` with template rendering test
- **Files**: `internal/spotter/prompt.go`, `internal/spotter/prompt_test.go`

### Step 3: Add git diff and summary gathering to TaskRunner
- [x] Add `GatherDiffs() ([]spotter.RepoDiff, error)` method — runs `git diff HEAD` in each worktree via exec
- [x] Add `GatherSummaries() []spotter.GoalSummary` method — reads DONE.json from each worktree
- [x] Add `taskDir` field to TaskRunner for VERDICT.json location
- **Files**: `internal/setter/taskrunner.go`

### Step 4: Add DAG.AddGoals method
- [x] Add `AddGoals(goals []model.Goal)` to DAG — inserts new goals into existing DAG
- [x] Add test for AddGoals
- **Files**: `internal/setter/dag.go`, `internal/setter/dag_test.go`

### Step 5: Implement SpawnSpotter on TaskRunner
- [x] Add `spotterAttempt`, `spotterRunning` fields
- [x] Implement `SpawnSpotter() error` — gathers diffs/summaries, builds prompt, spawns agent
- [x] Spotter window named `spotter-<attempt>`
- **Files**: `internal/setter/taskrunner.go`

### Step 6: Implement CheckSpotterVerdict on TaskRunner
- [x] Implement `CheckSpotterVerdict() (*spotter.VerdictJSON, bool, error)` — reads VERDICT.json if present
- [x] Records review in SQLite via store
- **Files**: `internal/setter/taskrunner.go`

### Step 7: Implement verdict handling
- [x] Implement `HandleApproval() error` — creates PRs per repo (git push + gh pr create)
- [x] Implement `HandleRejection(verdict) ([]QueuedGoal, error)` — creates correction goals, removes old DONE.json, rebuilds DAG
- [x] Correction goal IDs: `<repo>-corr-<attempt>-<index>`
- **Files**: `internal/setter/taskrunner.go`

### Step 8: Update setter.go tick() for reviewing state
- [x] Replace auto-complete placeholder with spotter lifecycle
- [x] Add reviewing state handling: spawn spotter if not running, check verdict if running
- [x] Handle approve: create PRs, mark complete, cleanup
- [x] Handle reject: create correction goals, transition back to running
- [x] Handle max reviews (2): mark stuck
- [x] Decrement activeLeads when goals complete in CheckCompletions
- **Files**: `internal/setter/setter.go`

### Step 9: Write tests
- [x] Test SpawnSpotter (prompt built, agent spawned, window created)
- [x] Test CheckSpotterVerdict (VERDICT.json parsing)
- [x] Test HandleRejection (correction goals created, DONE.json removed, DAG updated)
- [x] Test max review cycles (task marked stuck after 2 rejections)
- [x] Test full approve flow (task marked complete)
- **Files**: `internal/setter/setter_test.go`

### Step 10: Run tests and verify
- [x] `go test ./...` passes
- **Command**: `go test ./...`

### Step 11: Commit
- [ ] `git add -A && git commit -m "lead(spotter-review): implement Goal 4 - cross-repo review with redistribution"`
