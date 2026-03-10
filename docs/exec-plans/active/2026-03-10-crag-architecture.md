# Crag Architecture Implementation Plan

> **Status**: Active | **Created**: 2026-03-10 | **Last Updated**: 2026-03-10
> **Design Doc**: `docs/design-docs/2026-03-10-crag-architecture-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-10 | Design | Per-role windows in shared session, not separate sessions | Simpler, env isolation already solved with per-window export |
| 2026-03-10 | Design | Spotter per-repo, not per-goal | Leads now run full harness review internally |
| 2026-03-10 | Design | Deferred activation via SendKeys to pre-created windows | Zero cost while idle, window exists as mail target |
| 2026-03-10 | Design | Two-phase approach: rename first, architecture second | Clean foundation before structural changes |

## Progress

- [x] Task 1: SQLite migration (problems, climbs tables) _(completed 2026-03-10)_
- [x] Task 2: Model types rename (Goal→Climb, Task→Problem, events, statuses) _(completed 2026-03-10)_
- [x] Task 3: Store rename (all SQL queries and function signatures) _(completed 2026-03-10)_
- [x] Task 4: Goal context → Climb context package rename _(completed 2026-03-10)_
- [x] Task 5: Instance → Crag (instance package + CLI command) _(completed 2026-03-10)_
- [x] Task 6: DAG rename (Goal→Climb throughout) _(completed 2026-03-10)_
- [x] Task 7: TaskRunner → ProblemRunner + signal file renames _(completed 2026-03-10)_
- [x] Task 8: Setter daemon → Belayer package rename _(completed 2026-03-10)_
- [x] Task 9: CLI commands rename (task→problem, setter→belayer, manage→setter) _(completed 2026-03-10)_
- [ ] Task 10: Default prompts and slash commands rename
- [ ] Task 11: Documentation rename (README, CLAUDE.md, docs/)
- [ ] Task 12: Pre-create spotter/anchor windows at problem init
- [ ] Task 13: Spotter per-repo activation + repo-level completion tracking
- [ ] Task 14: Flash detection

## Surprises & Discoveries

| Date | What was unexpected | Impact | Action taken |
|------|---------------------|--------|-------------|
| 2026-03-10 | Task 3 worker also fixed DAG, setter, and CLI callers | Tasks 6-7 partially done | Will verify and skip completed portions |

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

## Phase 1: Naming (Tasks 1-11)

Tasks 1-4 are sequential (each builds on the previous). Tasks 5-6 can run in parallel after Task 4. Tasks 7-8 depend on Tasks 5-6. Task 9 depends on Task 8. Task 10-11 can run in parallel after Task 9.

### Task 1: SQLite Migration

**Files:**
- Create: `internal/db/migrations/002_rename_crag.sql`
- Modify: `internal/db/db.go` (migration list if manually registered)

- [ ] **Step 1: Write the migration SQL**

Create `internal/db/migrations/002_rename_crag.sql`:

```sql
-- Rename tasks → problems
ALTER TABLE tasks RENAME TO problems;

-- Rename task_repos → problem_repos (if exists)
-- ALTER TABLE task_repos RENAME TO problem_repos;

-- Rename goals → climbs
ALTER TABLE goals RENAME TO climbs;

-- Rename columns where SQLite supports it (3.25.0+)
-- task_id → problem_id in climbs
ALTER TABLE climbs RENAME COLUMN task_id TO problem_id;

-- task_id → problem_id in events
ALTER TABLE events RENAME COLUMN task_id TO problem_id;

-- goal_id → climb_id in events
ALTER TABLE events RENAME COLUMN goal_id TO climb_id;

-- task_id → problem_id in spotter_reviews
ALTER TABLE spotter_reviews RENAME COLUMN task_id TO problem_id;

-- Update indexes (SQLite doesn't support ALTER INDEX, so drop and recreate)
DROP INDEX IF EXISTS idx_tasks_instance_id;
CREATE INDEX idx_problems_instance_id ON problems(instance_id);

DROP INDEX IF EXISTS idx_tasks_status;
CREATE INDEX idx_problems_status ON problems(status);

DROP INDEX IF EXISTS idx_goals_task_id;
CREATE INDEX idx_climbs_problem_id ON climbs(problem_id);

DROP INDEX IF EXISTS idx_goals_status;
CREATE INDEX idx_climbs_status ON climbs(status);

DROP INDEX IF EXISTS idx_events_task_id;
CREATE INDEX idx_events_problem_id ON events(problem_id);

DROP INDEX IF EXISTS idx_spotter_reviews_task_id;
CREATE INDEX idx_spotter_reviews_problem_id ON spotter_reviews(problem_id);
```

- [ ] **Step 2: Verify migration applies cleanly**

Run: `go test ./internal/db/ -v -run TestMigration`
If no migration test exists, write a quick test that opens a DB, runs migrations, and verifies the new table names exist.

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/002_rename_crag.sql
git commit -m "feat: add SQLite migration renaming tasks→problems, goals→climbs"
```

### Task 2: Model Types Rename

**Files:**
- Modify: `internal/model/types.go`

- [ ] **Step 1: Rename Task → Problem**

In `internal/model/types.go`:
- `TaskStatus` → `ProblemStatus`
- `TaskStatusPending` → `ProblemStatusPending` (and all other status constants)
- `Task` struct → `Problem` struct
- `EventType` constants: `EventTaskCreated` → `EventProblemCreated`

- [ ] **Step 2: Rename Goal → Climb**

In `internal/model/types.go`:
- `GoalStatus` → `ClimbStatus`
- `GoalStatusPending` → `ClimbStatusPending` (and all other status constants)
- `GoalStatusSpotting` → `ClimbStatusSpotting`
- `Goal` struct → `Climb` struct, rename `TaskID` field → `ProblemID`
- `GoalsFile` → `ClimbsFile`
- `RepoGoals` → `RepoClimbs`
- `GoalSpec` → `ClimbSpec`
- `GoalSummary` → `ClimbSummary` (in goalctx, handled in Task 4)
- Event constants: `EventGoalStarted` → `EventClimbStarted`, `EventGoalCompleted` → `EventClimbCompleted`, `EventGoalFailed` → `EventClimbFailed`

- [ ] **Step 3: Rename SpotterReview.TaskID → ProblemID**

- [ ] **Step 4: Fix all compile errors**

Run: `go build ./...`
This will cascade errors through the codebase. Fix each caller — this task only fixes `internal/model/` itself to compile. Other packages are fixed in subsequent tasks.

- [ ] **Step 5: Commit**

```bash
git add internal/model/types.go
git commit -m "feat: rename Task→Problem, Goal→Climb in model types"
```

### Task 3: Store Rename

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go` (if exists)

- [ ] **Step 1: Update all SQL queries**

Replace in all query strings:
- Table `tasks` → `problems`
- Table `goals` → `climbs`
- Column `task_id` → `problem_id`
- Column `goal_id` → `climb_id`
- Column `goals_json` → `climbs_json`

- [ ] **Step 2: Rename function signatures**

- `InsertTask` → `InsertProblem`
- `GetTask` → `GetProblem`
- `ListTasksForInstance` → `ListProblemsForInstance`
- `GetGoalsForTask` → `GetClimbsForProblem`
- `UpdateTaskStatus` → `UpdateProblemStatus`
- `UpdateGoalStatus` → `UpdateClimbStatus`
- `GetTasksByStatus` → `GetProblemsByStatus`
- `GetPendingTasks` → `GetPendingProblems`
- `GetActiveTasks` → `GetActiveProblems`
- `IncrementGoalAttempt` → `IncrementClimbAttempt`
- `ResetGoalStatus` → `ResetClimbStatus`
- `InsertGoals` → `InsertClimbs`
- All parameter names: `taskID` → `problemID`, `goalID` → `climbID`
- Return types: `*model.Task` → `*model.Problem`, `[]model.Goal` → `[]model.Climb`

- [ ] **Step 3: Update tests**

Rename test functions and assertions to use new type/function names.

- [ ] **Step 4: Verify compilation**

Run: `go build ./internal/store/...`
Note: other packages will still have compile errors referencing old names.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat: rename store functions Task→Problem, Goal→Climb"
```

### Task 4: Goal Context → Climb Context

**Files:**
- Rename: `internal/goalctx/` → `internal/climbctx/`
- Modify: all files in the package

- [ ] **Step 1: Rename the package directory**

```bash
mv internal/goalctx internal/climbctx
```

- [ ] **Step 2: Update package declaration and types**

In `internal/climbctx/climbctx.go` (renamed from goalctx.go):
- `package goalctx` → `package climbctx`
- `LeadGoal` → `LeadClimb` (rename `GoalID` → `ClimbID`, `TaskSpec` → `ProblemSpec`)
- `SpotterGoal` → `SpotterClimb` (rename `GoalID` → `ClimbID`, `DoneJSON` → `TopJSON`)
- `AnchorGoal` → `AnchorClimb` (rename `TaskSpec` → `ProblemSpec`)
- `GoalSummary` → `ClimbSummary`
- `RepoDiff` stays (it's repo-scoped, not goal-scoped)
- `WriteGoalJSON` → `WriteClimbJSON` (parameter `goalID` → `climbID`)
- Path inside function: `.lead/<goalID>/GOAL.json` → `.lead/<climbID>/GOAL.json` (keep GOAL.json filename for now — it's the agent's goal context regardless of naming)

- [ ] **Step 3: Update test file**

Rename `goalctx_test.go` → `climbctx_test.go`, update package name and type references.

- [ ] **Step 4: Update all import paths**

```bash
# Find all files importing goalctx
grep -r '"github.com/donovan-yohan/belayer/internal/goalctx"' --include='*.go' -l
```

Update each file's import from `internal/goalctx` → `internal/climbctx` and all `goalctx.` prefixes → `climbctx.`.

- [ ] **Step 5: Verify**

Run: `go build ./...`

- [ ] **Step 6: Commit**

```bash
git add internal/climbctx/ internal/goalctx/
git commit -m "feat: rename goalctx package to climbctx, Goal types to Climb"
```

### Task 5: Instance → Crag

**Files:**
- Modify: `internal/instance/instance.go`
- Modify: `internal/instance/instance_test.go`
- Modify: `internal/cli/instance.go` (rename to `internal/cli/crag.go`)

- [ ] **Step 1: Rename types in instance package**

In `internal/instance/instance.go`:
- `InstanceConfig` → `CragConfig`
- `instanceConfigFile` constant: value stays `"instance.json"` for backward compat (or rename to `"crag.json"` with migration)
- Function names: Keep `Create`, `Load`, `List`, `Delete` (they're already generic)
- Update comments from "instance" to "crag"
- `CreateWorktree` parameter `taskID` → `problemID`
- `CleanupTaskWorktrees` → `CleanupProblemWorktrees`

- [ ] **Step 2: Rename CLI command**

Rename `internal/cli/instance.go` → `internal/cli/crag.go`:
- `Use: "instance"` → `Use: "crag"`
- `Short: "Manage belayer instances"` → `Short: "Manage belayer crags"`
- Update all help text
- Function name: `newInstanceCmd` → `newCragCmd`

- [ ] **Step 3: Update root.go wiring**

In `internal/cli/root.go`, change `newInstanceCmd()` → `newCragCmd()`.

- [ ] **Step 4: Update tests**

- [ ] **Step 5: Verify**

Run: `go build ./...`

- [ ] **Step 6: Commit**

```bash
git add internal/instance/ internal/cli/
git commit -m "feat: rename instance→crag in types and CLI command"
```

### Task 6: DAG Rename

**Files:**
- Modify: `internal/setter/dag.go`
- Modify: `internal/setter/dag_test.go`

- [ ] **Step 1: Rename all Goal references to Climb**

In `internal/setter/dag.go`:
- `goals` map field stays as-is (internal) but comments updated
- `BuildDAG(goals []model.Goal)` → `BuildDAG(climbs []model.Climb)`
- `ReadyGoals()` → `ReadyClimbs()`
- `MarkComplete`, `MarkFailed`, `MarkRunning`, `MarkSpotting` — parameters renamed
- `AllComplete` — comments updated
- `Goals()` → `Climbs()`
- `AddGoals()` → `AddClimbs()`
- `allDepsComplete` — comments updated

- [ ] **Step 2: Update tests**

In `dag_test.go`, rename all `Goal` references to `Climb`.

- [ ] **Step 3: Verify**

Run: `go test ./internal/setter/... -run TestDAG`

- [ ] **Step 4: Commit**

```bash
git add internal/setter/dag.go internal/setter/dag_test.go
git commit -m "feat: rename Goal→Climb in DAG"
```

### Task 7: TaskRunner → ProblemRunner + Signal Files

**Files:**
- Modify: `internal/setter/taskrunner.go`
- Modify: `internal/setter/setter_test.go`

- [ ] **Step 1: Rename struct and type references**

In `internal/setter/taskrunner.go`:
- `TaskRunner` → `ProblemRunner`
- `DoneJSON` → `TopJSON`
- `QueuedGoal` → `QueuedClimb`
- All field names: `task` → `problem`, `taskDir` → `problemDir`, `tmuxSession` naming updated
- `NewTaskRunner` → `NewProblemRunner`
- `SpawnGoal` → `SpawnClimb`
- `SpawnSpotter` → stays for now (architecture change in Phase 2)
- `SpawnAnchor` → stays for now
- `CheckCompletions` — rename internal references
- `CheckSpottingGoals` → `CheckSpottingClimbs`
- `CheckDeadGoals` → `CheckDeadClimbs`

- [ ] **Step 2: Rename signal file references**

All `DONE.json` string literals → `TOP.json`:
- `donePath` variables
- `DoneJSON` struct references
- Log messages

All `GOAL.json` path references stay (it's the agent's context file).

- [ ] **Step 3: Rename tmux session format**

`belayer-task-%s` → `belayer-problem-%s`

- [ ] **Step 4: Update mail address format**

`task/%s/lead/%s/%s` → `problem/%s/lead/%s/%s`
`task/%s/spotter/%s/%s` → `problem/%s/spotter/%s/%s`
`task/%s/anchor` → `problem/%s/anchor`

- [ ] **Step 5: Update tests**

In `setter_test.go`, rename all references.

- [ ] **Step 6: Verify**

Run: `go build ./internal/setter/...`

- [ ] **Step 7: Commit**

```bash
git add internal/setter/
git commit -m "feat: rename TaskRunner→ProblemRunner, DONE.json→TOP.json, Goal→Climb"
```

### Task 8: Setter Package → Belayer Package

**Files:**
- Rename: `internal/setter/` → `internal/belayer/` (entire directory)
- Modify: all import paths

- [ ] **Step 1: Rename the directory**

```bash
mv internal/setter internal/belayer
```

- [ ] **Step 2: Update package declaration**

In all files under `internal/belayer/`:
- `package setter` → `package belayer`

- [ ] **Step 3: Rename the Setter struct**

In `internal/belayer/setter.go` (was setter.go):
- `Setter` struct stays or renames — the daemon IS the belayer now
- Consider: `Setter` → `Belayer` for the daemon struct
- `Config` stays generic
- `New()` → still fine
- `Run()` → still fine

- [ ] **Step 4: Update all import paths**

Find all files importing `internal/setter` and update to `internal/belayer`.
Update all `setter.` prefixes to `belayer.`.

- [ ] **Step 5: Verify**

Run: `go build ./...`

- [ ] **Step 6: Commit**

```bash
git add internal/belayer/ internal/setter/
git commit -m "feat: rename setter package to belayer"
```

### Task 9: CLI Commands Rename

**Files:**
- Modify: `internal/cli/setter.go` → rename to `internal/cli/belayer.go`
- Modify: `internal/cli/manage.go` → rename to `internal/cli/setter.go`
- Modify: `internal/cli/task.go` → rename to `internal/cli/problem.go`
- Modify: `internal/cli/root.go`

**Important: This task has a naming collision.** Current `setter.go` (daemon) needs to become `belayer.go`, and current `manage.go` (user session) needs to become `setter.go`. Rename in correct order to avoid conflicts.

- [ ] **Step 1: Rename setter.go → belayer.go (daemon command)**

```bash
mv internal/cli/setter.go internal/cli/belayer_cmd.go
```

In the file:
- `Use: "setter"` → `Use: "belayer"`
- `Short: "Manage the setter daemon"` → `Short: "Manage the belayer daemon"`
- Update all help text: "setter daemon" → "belayer daemon"
- `newSetterCmd` → `newBelayerCmd`
- Subcommands: `setter start` → `belayer start`, etc.

- [ ] **Step 2: Rename manage.go → setter.go (user session)**

```bash
mv internal/cli/manage.go internal/cli/setter_cmd.go
```

In the file:
- `Use: "manage"` → `Use: "setter"`
- `Short: "Start an interactive agent session for task creation"` → `Short: "Start an interactive setter session for problem creation"`
- Update help text
- `newManageCmd` → `newSetterSessionCmd`

- [ ] **Step 3: Rename task.go → problem.go**

```bash
mv internal/cli/task.go internal/cli/problem.go
```

In the file:
- `Use: "task"` → `Use: "problem"`
- `Short: "Manage tasks"` → `Short: "Manage problems"`
- Subcommands: `task create` → `problem create`, `task list` → `problem list`, `task retry` → `problem retry`
- `newTaskCmd` → `newProblemCmd`

- [ ] **Step 4: Update root.go wiring**

Update all `AddCommand` calls to use new function names.

- [ ] **Step 5: Update status.go and logs.go help text**

- `status.go`: "task and goal status" → "problem and climb status"
- `logs.go`: update any task/goal references

- [ ] **Step 6: Update message.go and mail.go**

- Address format docs: `task/` → `problem/`
- Help text updates

- [ ] **Step 7: Verify**

Run: `go build ./cmd/belayer && ./belayer --help`

- [ ] **Step 8: Commit**

```bash
git add internal/cli/
git commit -m "feat: rename CLI commands (setter→belayer, manage→setter, task→problem, instance→crag)"
```

### Task 10: Default Prompts and Slash Commands

**Files:**
- Rename: `internal/defaults/claudemd/manage.md` → `internal/defaults/claudemd/setter.md`
- Modify: `internal/defaults/claudemd/lead.md`
- Modify: `internal/defaults/claudemd/spotter.md`
- Modify: `internal/defaults/claudemd/anchor.md`
- Rename: `internal/defaults/commands/task-create.md` → `internal/defaults/commands/problem-create.md`
- Rename: `internal/defaults/commands/task-list.md` → `internal/defaults/commands/problem-list.md`
- Modify: `internal/defaults/commands/status.md`
- Modify: `internal/defaults/commands/logs.md`
- Modify: `internal/defaults/commands/message.md`
- Modify: `internal/defaults/commands/mail.md`

- [ ] **Step 1: Rename and update manage.md → setter.md**

```bash
mv internal/defaults/claudemd/manage.md internal/defaults/claudemd/setter.md
```

Update content:
- Title: "Belayer Manage Session" → "Belayer Setter Session"
- All "instance" → "crag", "task" → "problem", "goal" → "climb"
- CLI reference table: `belayer task create` → `belayer problem create`, etc.
- "belayer manage" references → "belayer setter"
- Workflow routing section updated

- [ ] **Step 2: Update lead.md**

- "DONE.json" → "TOP.json"
- "GOAL.json" references stay (it's the context file name)
- "goal" → "climb" in descriptions
- "task" → "problem"
- Mail address format: `task/` → `problem/`
- Harness workflow section stays

- [ ] **Step 3: Update spotter.md**

- "goal" → "climb", "task" → "problem"
- "DONE.json" → "TOP.json"
- Mail address format updated

- [ ] **Step 4: Update anchor.md**

- "goal" → "climb", "task" → "problem"
- Mail address format updated

- [ ] **Step 5: Rename and update slash commands**

```bash
mv internal/defaults/commands/task-create.md internal/defaults/commands/problem-create.md
mv internal/defaults/commands/task-list.md internal/defaults/commands/problem-list.md
```

Update content in all command files:
- "task" → "problem", "goal" → "climb", "instance" → "crag"
- Command references: `belayer task create` → `belayer problem create`

- [ ] **Step 6: Update Go code referencing these filenames**

Search for `ReadFile("claudemd/manage.md")` → `ReadFile("claudemd/setter.md")`
Search for `task-create.md` → `problem-create.md`
Search for `task-list.md` → `problem-list.md`

- [ ] **Step 7: Verify**

Run: `go build ./...`

- [ ] **Step 8: Commit**

```bash
git add internal/defaults/
git commit -m "feat: rename default prompts and slash commands to new terminology"
```

### Task 11: Documentation Rename

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/DESIGN.md`
- Modify: `docs/QUALITY.md`
- Modify: `docs/TUI.md`
- Modify: `docs/PLANS.md`
- Modify: `docs/design-docs/index.md`

- [ ] **Step 1: Update README.md**

Full terminology overhaul:
- "instance" → "crag"
- "task" → "problem"
- "goal" → "climb"
- "setter daemon" → "belayer daemon"
- "belayer manage" → "belayer setter"
- CLI examples updated
- Architecture diagram updated
- "DONE.json" → "TOP.json"

- [ ] **Step 2: Update CLAUDE.md**

- Quick Reference table: `belayer setter start` stays (it's now the daemon)
- Actually: daemon command is now `belayer belayer start`... this is awkward.
- Decision needed: the binary is called `belayer`, the daemon subcommand is now `belayer` too. This creates `belayer belayer start`. Consider keeping `belayer start` as a shortcut (daemon is the primary action).
- Key Patterns section updated
- Documentation Map stays (paths unchanged)

- [ ] **Step 3: Update docs/ARCHITECTURE.md**

- Code Map: package names, type names
- Data Flow: task→problem, goal→climb
- Directory Layout: updated paths

- [ ] **Step 4: Update docs/DESIGN.md**

- All sections: terminology swap
- SQLite Schema table: updated table/column names
- Signal Files: DONE.json → TOP.json
- Mail addresses: task/ → problem/
- Naming Convention table updated

- [ ] **Step 5: Update docs/QUALITY.md, docs/TUI.md, docs/PLANS.md**

Terminology swap in all.

- [ ] **Step 6: Verify no broken links**

```bash
grep -r 'internal/setter' docs/ --include='*.md'
grep -r 'internal/goalctx' docs/ --include='*.md'
```

- [ ] **Step 7: Commit**

```bash
git add README.md CLAUDE.md docs/
git commit -m "docs: update all documentation with crag/problem/climb terminology"
```

---

## Phase 2: Architecture (Tasks 12-14)

Tasks 12-13 are sequential. Task 14 can follow Task 13.

### Task 12: Pre-create Spotter/Anchor Windows at Problem Init

**Files:**
- Modify: `internal/belayer/problemrunner.go` (was taskrunner.go)

- [ ] **Step 1: Write test for pre-created windows**

In `internal/belayer/belayer_test.go` (was setter_test.go):

```go
func TestProblemRunner_Init_PreCreatesSpotterWindows(t *testing.T) {
    s, tm, lm, sp, tmpDir := setupTestEnv(t)

    climbs := []model.Climb{
        {ID: "api-1", ProblemID: "prob-1", RepoName: "api", Description: "test", DependsOn: []string{}, Status: model.ClimbStatusPending},
        {ID: "api-2", ProblemID: "prob-1", RepoName: "api", Description: "test2", DependsOn: []string{}, Status: model.ClimbStatusPending},
        {ID: "web-1", ProblemID: "prob-1", RepoName: "web", Description: "test3", DependsOn: []string{}, Status: model.ClimbStatusPending},
    }
    insertTestProblem(t, s, "prob-1", climbs)

    problem, _ := s.GetProblem("prob-1")
    runner := NewProblemRunner(problem, tmpDir, "", "", s, tm, lm, sp)
    // Setup worktree dirs
    os.MkdirAll(filepath.Join(tmpDir, "repos", "api"), 0o755)
    os.MkdirAll(filepath.Join(tmpDir, "repos", "web"), 0o755)

    _, err := runner.Init()
    require.NoError(t, err)

    // Verify spotter windows were pre-created (one per unique repo)
    windows, _ := tm.ListWindows(runner.TmuxSession())
    assert.Contains(t, windows, "spot-api")
    assert.Contains(t, windows, "spot-web")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/belayer/ -run TestProblemRunner_Init_PreCreatesSpotterWindows -v`
Expected: FAIL (windows not created yet)

- [ ] **Step 3: Implement pre-creation in Init()**

In `Init()`, after creating the tmux session, add:

```go
// Pre-create spotter windows (one per unique repo)
repos := make(map[string]bool)
for _, c := range climbs {
    repos[c.RepoName] = true
}
for repo := range repos {
    spotWindow := fmt.Sprintf("spot-%s", repo)
    if err := tr.tmux.NewWindow(tr.tmuxSession, spotWindow); err != nil {
        log.Printf("warning: failed to pre-create spotter window %s: %v", spotWindow, err)
    }
}

// Pre-create anchor window (multi-repo only)
if len(repos) > 1 {
    if err := tr.tmux.NewWindow(tr.tmuxSession, "anchor"); err != nil {
        log.Printf("warning: failed to pre-create anchor window: %v", err)
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/belayer/ -run TestProblemRunner_Init_PreCreatesSpotterWindows -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/belayer/
git commit -m "feat: pre-create spotter and anchor windows at problem init"
```

### Task 13: Spotter Per-Repo + Deferred Activation

**Files:**
- Modify: `internal/belayer/problemrunner.go`
- Modify: `internal/belayer/dag.go`
- Modify: `internal/belayer/belayer_test.go`
- Modify: `internal/climbctx/climbctx.go`

- [ ] **Step 1: Add repo-level completion check to DAG**

In `internal/belayer/dag.go`, add:

```go
// AllClimbsForRepoComplete returns true if every climb assigned to the given
// repo has status Complete.
func (d *DAG) AllClimbsForRepoComplete(repoName string) bool {
    for _, c := range d.goals { // internal map still called goals
        if c.RepoName == repoName && c.Status != model.ClimbStatusComplete {
            return false
        }
    }
    return true
}

// ClimbsForRepo returns all climbs assigned to the given repo.
func (d *DAG) ClimbsForRepo(repoName string) []*model.Climb {
    var result []*model.Climb
    for _, c := range d.goals {
        if c.RepoName == repoName {
            result = append(result, c)
        }
    }
    return result
}
```

- [ ] **Step 2: Write test for repo-level completion**

```go
func TestDAG_AllClimbsForRepoComplete(t *testing.T) {
    climbs := []model.Climb{
        {ID: "a1", RepoName: "api", Status: model.ClimbStatusComplete},
        {ID: "a2", RepoName: "api", Status: model.ClimbStatusComplete},
        {ID: "w1", RepoName: "web", Status: model.ClimbStatusRunning},
    }
    dag := BuildDAG(climbs)
    assert.True(t, dag.AllClimbsForRepoComplete("api"))
    assert.False(t, dag.AllClimbsForRepoComplete("web"))
}
```

- [ ] **Step 3: Run test**

Run: `go test ./internal/belayer/ -run TestDAG_AllClimbsForRepoComplete -v`

- [ ] **Step 4: Update SpotterClimb struct for per-repo context**

In `internal/climbctx/climbctx.go`, update:

```go
type SpotterClimb struct {
    Role        string            `json:"role"`
    RepoName    string            `json:"repo_name"`
    ProblemSpec string            `json:"problem_spec"`
    ClimbTops   []ClimbTopSummary `json:"climb_tops"` // All TOP.json summaries for this repo
    WorkDir     string            `json:"work_dir"`
    Profiles    map[string]string `json:"profiles"`
}

type ClimbTopSummary struct {
    ClimbID     string `json:"climb_id"`
    Description string `json:"description"`
    Status      string `json:"status"`
    Summary     string `json:"summary"`
    Notes       string `json:"notes"`
}
```

- [ ] **Step 5: Implement ActivateSpotter**

Replace `SpawnSpotter` with `ActivateSpotter` in problemrunner.go:

```go
// ActivateSpotter activates the pre-created spotter window for a repo.
// Called when all climbs for a repo have topped.
func (tr *ProblemRunner) ActivateSpotter(repoName string) error {
    windowName := fmt.Sprintf("spot-%s", repoName)
    worktreePath := tr.worktrees[repoName]

    // Gather all TOP.json summaries for this repo
    var tops []climbctx.ClimbTopSummary
    for _, c := range tr.dag.ClimbsForRepo(repoName) {
        topPath := filepath.Join(worktreePath, ".lead", c.ID, "TOP.json")
        data, err := os.ReadFile(topPath)
        if err != nil {
            log.Printf("warning: could not read TOP.json for %s: %v", c.ID, err)
            continue
        }
        var top TopJSON
        json.Unmarshal(data, &top)
        tops = append(tops, climbctx.ClimbTopSummary{
            ClimbID:     c.ID,
            Description: c.Description,
            Status:      top.Status,
            Summary:     top.Summary,
            Notes:       top.Notes,
        })
    }

    // Write profiles
    profiles, err := tr.writeProfiles(worktreePath, repoName)
    if err != nil {
        return fmt.Errorf("writing profiles for spotter %s: %w", repoName, err)
    }

    // Write GOAL.json for spotter
    if err := climbctx.WriteClimbJSON(worktreePath, "spotter-"+repoName, climbctx.SpotterClimb{
        Role:        "spotter",
        RepoName:    repoName,
        ProblemSpec: tr.problem.Spec,
        ClimbTops:   tops,
        WorkDir:     worktreePath,
        Profiles:    profiles,
    }); err != nil {
        return fmt.Errorf("writing spotter GOAL.json for %s: %w", repoName, err)
    }

    appendPrompt, err := defaults.FS.ReadFile("claudemd/spotter.md")
    if err != nil {
        return fmt.Errorf("reading spotter system prompt: %w", err)
    }

    spotterMailAddr := fmt.Sprintf("problem/%s/spotter/%s", tr.problem.ID, repoName)
    goalJSONPath := fmt.Sprintf(".lead/spotter-%s/GOAL.json", repoName)

    // Activate via SendKeys (window already exists)
    if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
        TmuxSession:        tr.tmuxSession,
        WindowName:         windowName,
        WorkDir:            worktreePath,
        AppendSystemPrompt: string(appendPrompt),
        InitialPrompt:      fmt.Sprintf("Read %s and validate the repo's work against the PRD.", goalJSONPath),
        Env:                map[string]string{"BELAYER_MAIL_ADDRESS": spotterMailAddr},
    }); err != nil {
        return fmt.Errorf("activating spotter for %s: %w", repoName, err)
    }

    return nil
}
```

- [ ] **Step 6: Update CheckCompletions for repo-level spotting**

In `CheckCompletions`, instead of transitioning individual climbs to spotting:

```go
// After marking a climb complete, check if all climbs for its repo are done
if tr.validationEnabled && tr.dag.AllClimbsForRepoComplete(c.RepoName) {
    if !tr.repoSpotterActivated[c.RepoName] {
        if spotErr := tr.ActivateSpotter(c.RepoName); spotErr != nil {
            log.Printf("warning: failed to activate spotter for %s: %v", c.RepoName, spotErr)
        } else {
            tr.repoSpotterActivated[c.RepoName] = true
            log.Printf("belayer: all climbs topped for %s — spotter activated", c.RepoName)
        }
    }
}
```

Add `repoSpotterActivated map[string]bool` field to ProblemRunner, initialized in constructor.

- [ ] **Step 7: Update CheckSpottingClimbs for repo-level SPOT.json**

The spotter writes one SPOT.json. Update detection to look for it and mark all repo climbs as validated.

- [ ] **Step 8: Remove old SpawnSpotter (kill-window + create-window pattern)**

Delete the old `SpawnSpotter` method entirely.

- [ ] **Step 9: Update tests**

Update `TestProblemRunner_CheckCompletions_ValidationEnabled` and related tests.

- [ ] **Step 10: Verify**

Run: `go test ./internal/belayer/ -v`
Run: `go build ./...`

- [ ] **Step 11: Commit**

```bash
git add internal/belayer/ internal/climbctx/
git commit -m "feat: spotter per-repo with deferred activation"
```

### Task 14: Flash Detection

**Files:**
- Modify: `internal/belayer/problemrunner.go`
- Modify: `internal/belayer/belayer_test.go`

- [ ] **Step 1: Write test for flash detection**

```go
func TestProblemRunner_FlashDetection(t *testing.T) {
    // All climbs with attempt == 1 and spotter passes first try = flash
    runner := &ProblemRunner{
        repoSpotterAttempts: map[string]int{"api": 1},
    }
    // Build DAG with all attempt-1 climbs
    climbs := []model.Climb{
        {ID: "a1", RepoName: "api", Attempt: 1, Status: model.ClimbStatusComplete},
        {ID: "a2", RepoName: "api", Attempt: 1, Status: model.ClimbStatusComplete},
    }
    runner.dag = BuildDAG(climbs)

    assert.True(t, runner.IsFlashed("api"))
}

func TestProblemRunner_NotFlashed_RetryNeeded(t *testing.T) {
    runner := &ProblemRunner{
        repoSpotterAttempts: map[string]int{"api": 1},
    }
    climbs := []model.Climb{
        {ID: "a1", RepoName: "api", Attempt: 2, Status: model.ClimbStatusComplete}, // retried
        {ID: "a2", RepoName: "api", Attempt: 1, Status: model.ClimbStatusComplete},
    }
    runner.dag = BuildDAG(climbs)

    assert.False(t, runner.IsFlashed("api"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/belayer/ -run TestProblemRunner_Flash -v`

- [ ] **Step 3: Implement IsFlashed**

```go
// IsFlashed returns true if all climbs for the repo completed on first attempt
// and the spotter passed on first attempt.
func (tr *ProblemRunner) IsFlashed(repoName string) bool {
    if tr.repoSpotterAttempts[repoName] > 1 {
        return false
    }
    for _, c := range tr.dag.ClimbsForRepo(repoName) {
        if c.Attempt > 1 {
            return false
        }
    }
    return true
}
```

Add `repoSpotterAttempts map[string]int` to ProblemRunner struct, initialized in constructor. Increment when spotter fails and repo goes back for correction.

- [ ] **Step 4: Add flash reporting**

In the completion flow (after spotter passes), add:

```go
if tr.IsFlashed(repoName) {
    log.Printf("belayer: repo %s was FLASHED! All climbs topped first try.", repoName)
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/belayer/ -run TestProblemRunner_Flash -v`
Expected: PASS

- [ ] **Step 6: Run full test suite**

Run: `go test ./... -v`
Expected: ALL PASS

- [ ] **Step 7: Build final binary**

Run: `go build -o belayer ./cmd/belayer`

- [ ] **Step 8: Commit**

```bash
git add internal/belayer/
git commit -m "feat: flash detection for repos that top first try"
```

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
-

**What didn't:**
-

**Learnings to codify:**
-
