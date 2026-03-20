# v2 Multi-Repo Pipelines

> **Status**: Completed | **Created**: 2026-03-20 | **Completed**: 2026-03-20
> **Design Doc**: `docs/designs/v2-multi-repo-pipelines.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-20 | Design | Unified fan-out via workflow.Go() | Single-repo = fan-out of 1, same code path |
| 2026-03-20 | Design | Per-run worktrees for multi-repo | Isolation for concurrent runs, push before cleanup |
| 2026-03-20 | Design | Signal routing: Role+Repo matching | Parallel leads need disambiguation |
| 2026-03-20 | Design | Risk gate on decomposer output | Prevent silent false negatives on repo selection |

## Progress

- [x] Task 1: Global config — repos + crags registry
- [x] Task 2: `belayer repo add/list/remove` CLI commands
- [x] Task 3: Pipeline model — repos field + fan_out/per/fan_in annotations
- [x] Task 4: Pipeline inheritance — `extends:` with cycle detection
- [x] Task 5: Signal routing — add Repo field + `--repo` flag
- [x] Task 6: Worktree manager — create/push/cleanup per-repo worktrees
- [x] Task 7: Workflow fan-out — parallel `workflow.Go()` with dependency ordering
- [x] Task 8: `belayer crag init/list` + `belayer cd` + CWD context
- [x] Task 9: Multi-repo attach — window naming + `--repo` filter
- [ ] Task 10: Status dashboard — per-repo role progress (deferred — uses Temporal Web UI for now)
- [x] Task 11: Workflow tests — fan-out, partial flare, dependency ordering
- [ ] Task 12: Integration test — multi-repo E2E with worktrees (deferred — unit tests cover the logic)

## Surprises & Discoveries

_None yet._

## Plan Drift

_None yet._

---

### Task 1: Global config — repos + crags registry

**Goal:** Create a config package that reads/writes `~/.belayer/config.json` with repos and crags sections.

**Files:**
- `internal/v2/config/config.go` (new)
- `internal/v2/config/config_test.go` (new)

**Steps:**

1. Define the config types:
   ```go
   type Config struct {
       Repos map[string]RepoEntry `json:"repos"`
       Crags map[string]string    `json:"crags"` // name → path
   }
   type RepoEntry struct {
       Path string `json:"path"`
   }
   ```

2. Implement `Load() (*Config, error)` — reads from `~/.belayer/config.json`, creates default if missing.

3. Implement `Save(cfg *Config) error` — atomic write (temp file + rename).

4. Implement repo CRUD: `AddRepo(name, path)`, `RemoveRepo(name)`, `ResolveRepoPath(name) (string, error)`.

5. Implement crag CRUD: `AddCrag(name, path)`, `RemoveCrag(name)`, `ResolveCragPath(name) (string, error)`.

6. Implement repo auto-detection: `DetectRepoPath(name) (string, error)` — checks `../<name>`, `~/projects/<name>`.

7. Implement repo health check: `ValidateRepoPaths(names []string) error` — verifies all paths exist and are git repos.

**Tests:**
- JSON round-trip for Config
- AddRepo + ResolveRepoPath
- AddRepo with non-existent path → error
- Auto-detect finds sibling directory
- Auto-detect finds ~/projects/ directory
- ValidateRepoPaths with valid paths → nil
- ValidateRepoPaths with missing path → error naming the repo
- ValidateRepoPaths with non-git path → error
- Atomic write (verify no corruption on concurrent access)

---

### Task 2: `belayer repo add/list/remove` CLI commands

**Goal:** CLI commands for managing the global repo registry.

**Files:**
- `internal/v2/cli/repo.go` (new)
- `internal/v2/cli/root.go` (modify — add repo command)

**Steps:**

1. `belayer repo add <name> [path]` — if path omitted, attempt auto-detection. Validate path exists and is a git repo.
2. `belayer repo list` — show all registered repos with paths.
3. `belayer repo remove <name>` — remove from registry.
4. Add `newRepoCmd()` to root.

**Tests:** Build verification. Repo CRUD is tested in config package.

---

### Task 3: Pipeline model — repos field + fan_out/per/fan_in annotations

**Goal:** Extend the pipeline model and parser to support multi-repo fields.

**Files:**
- `internal/v2/pipeline/model.go` (modify — add Repos to Route)
- `internal/v2/role/contract.go` (modify — add FanOut, Per, FanIn to RoleDef)
- `internal/v2/pipeline/validate.go` (modify — validate fan-out/fan-in consistency)
- `internal/v2/pipeline/parser_test.go` (modify — add multi-repo tests)
- `internal/v2/pipeline/validate_test.go` (modify — add fan-out validation tests)

**Steps:**

1. Add `Repos []string` to `Route`.
2. Add `FanOut string`, `Per string`, `FanIn string` to `RoleDef`.
3. Validation: if any role has `per: repo`, there must be a `fan_out: repos` earlier in the same phase. If `fan_in: repos`, there must be `per: repo` roles before it.
4. Validation: repos listed in pipeline YAML must exist in global config (pass config to validator).
5. Update embedded templates with multi-repo example.

**Tests:**
- Parse pipeline with repos field
- Parse pipeline with fan_out/per/fan_in
- Validate: per without fan_out → error
- Validate: fan_in without per → error
- Validate: unknown repo name → error (when config provided)

---

### Task 4: Pipeline inheritance — `extends:` with cycle detection

**Goal:** Support `extends:` field in pipeline YAML for DRY multi-pipeline crags.

**Files:**
- `internal/v2/pipeline/parser.go` (modify — add extends resolution)
- `internal/v2/pipeline/parser_test.go` (modify — add extends tests)

**Steps:**

1. Add `Extends string` to `Route` struct (yaml field).
2. In `ParseRouteFile`, if `Extends` is set:
   - Resolve relative to same directory
   - Parse parent (recursively)
   - Cycle detection via visited-path set
   - Shallow merge: child fields override parent, `repos`/`name`/`safety` fully replaced, phases inherited if not declared
3. Write test fixtures as temp YAML files.

**Tests:**
- Single-level extends: child overrides repos only
- Child overrides phases
- Circular extends → error
- Missing parent → error
- `../` in extends path → error (rejected)

---

### Task 5: Signal routing — add Repo field + `--repo` flag

**Goal:** Add Repo field to RoleSignal for multi-repo Type B signal disambiguation.

**Files:**
- `internal/v2/model/types.go` (modify — add Repo to RoleSignal)
- `internal/v2/cli/role_signal.go` (modify — add --repo flag)
- `internal/v2/temporal/workflow.go` (modify — match on Role+Repo)
- `internal/v2/provider/session.go` (modify — include repo in system prompt)
- `internal/v2/temporal/workflow_test.go` (modify — test multi-repo signal routing)

**Steps:**

1. Add `Repo string` to `RoleSignal`.
2. Add `--repo` flag to all signal commands (optional — defaults to empty for single-repo).
3. Update `executeTypeB` to match signals on `Role+Repo` (if repo is set in the input).
4. Update session system prompt to include `--repo <name>` in the finish/flare/fail instructions.
5. Update workflow tests.

**Tests:**
- Signal with correct Role+Repo → received
- Signal with correct Role but wrong Repo → ignored (keeps waiting)
- Signal with no Repo (single-repo mode) → received
- System prompt includes --repo when repo name is provided

---

### Task 6: Worktree manager — create/push/cleanup per-repo worktrees

**Goal:** Manage git worktrees for multi-repo pipeline runs.

**Files:**
- `internal/v2/worktree/manager.go` (new)
- `internal/v2/worktree/manager_test.go` (new)

**Steps:**

1. Define `WorktreeManager` with:
   - `Create(repoPath, worktreePath, branchName string) error` — `git -C <repo> worktree add <path> -b <branch>`
   - `HasUnpushedCommits(worktreePath string) (bool, error)` — check for commits not on remote
   - `PushBranch(worktreePath, remote, branch string) error` — `git -C <path> push <remote> <branch>`
   - `Remove(worktreePath string) error` — `git worktree remove`
   - `SafeCleanup(worktreePath string) error` — push if needed, warn if unpushed, remove if safe

2. Implement `SetupRunWorktrees(runID string, repos map[string]string) (map[string]string, error)` — creates worktrees under `.belayer/worktrees/<runID>/` for all provided repos. Returns map of repoName → worktreePath.

3. Implement `CleanupRunWorktrees(runID string, repos map[string]string) []string` — returns list of warnings for preserved worktrees.

**Tests:**
- Create worktree in temp dir with real git repo
- PushBranch (skip if no remote — test with local remote)
- HasUnpushedCommits on clean worktree → false
- HasUnpushedCommits after local commit → true
- SafeCleanup with no unpushed → removes
- SafeCleanup with unpushed → preserves + returns warning

---

### Task 7: Workflow fan-out — parallel `workflow.Go()` with dependency ordering

**Goal:** Extend the Route workflow to support parallel per-repo execution with dependency ordering.

**Files:**
- `internal/v2/temporal/workflow.go` (modify — add fan-out logic)
- `internal/v2/temporal/activity.go` (modify — add worktree setup/cleanup activities)
- `internal/v2/temporal/fanout.go` (new — fan-out orchestration logic)

**Steps:**

1. Define decomposer output types:
   ```go
   type DecomposerOutput struct {
       Repos map[string]RepoTask `json:"repos"`
   }
   type RepoTask struct {
       Needed    bool     `json:"needed"`
       Spec      string   `json:"spec,omitempty"`
       Reason    string   `json:"reason,omitempty"`
       DependsOn []string `json:"depends_on,omitempty"`
   }
   ```

2. Implement `executeFanOut(ctx, decomposerOutput, route)`:
   - Create worktrees activity (Type A)
   - Topological sort repos by `depends_on`
   - For each dependency level, spawn `workflow.Go()` goroutines
   - Each goroutine: executeRole(lead, repo) → executeRole(spotter, repo)
   - Collect results into `map[string]RoleResult`
   - Handle partial flare: continue others, mark flared repo

3. Implement `executeFanIn(ctx, repoResults, skipReasons)`:
   - Build combined input for anchor
   - Execute anchor activity

4. Integrate into RouteWorkflow: detect `fan_out` annotation on a role → branch to fan-out path.

5. For single-repo (no repos or 1 repo): synthesize a single-repo DecomposerOutput, same code path.

**Tests:** Covered in Task 11 (dedicated workflow tests).

---

### Task 8: `belayer crag init/list` + `belayer cd` + CWD context

**Goal:** Named crags and CWD context filtering for CLI commands.

**Files:**
- `internal/v2/cli/crag.go` (new)
- `internal/v2/cli/root.go` (modify — add crag + cd commands)
- `internal/v2/cli/run.go` (modify — store crag path as Temporal search attribute)
- `internal/v2/cli/status.go` (modify — filter by CWD)

**Steps:**

1. `belayer crag init --name <name>` — registers CWD as a named crag.
2. `belayer crag list` — shows all named crags.
3. `belayer cd <name>` — prints the crag path (for shell alias).
4. Update `belayer run` to store `crag_path` as Temporal custom search attribute.
5. Update `belayer status` to filter workflows by `crag_path == CWD` (when run from a crag dir).

**Tests:** Build + manual verification. Config CRUD tested in Task 1.

---

### Task 9: Multi-repo attach — window naming + `--repo` filter

**Goal:** Update session spawner and attach command for multi-repo window naming.

**Files:**
- `internal/v2/provider/session.go` (modify — include repo in window name)
- `internal/v2/cli/attach.go` (already has --repo from previous work, verify it works with new naming)

**Steps:**

1. Update `ClaudeSessionSpawner.Spawn` — if repo name provided in opts, window name becomes `<role>-<repo>-<taskid[:8]>`.
2. Verify `belayer attach lead --repo extend-api` matches the new naming pattern.
3. `belayer attach` lists all windows grouped by repo.

**Tests:**
- Session spawned with repo → window name includes repo
- Session spawned without repo (single-repo) → original naming
- Attach --repo matches correctly

---

### Task 10: Status dashboard — per-repo role progress

**Goal:** Rich `belayer status` showing per-repo progress for multi-repo runs.

**Files:**
- `internal/v2/cli/status.go` (modify — rich dashboard output)

**Steps:**

1. Query Temporal for workflow details (custom search attributes: pipeline_name, crag_path, repo_count).
2. For active workflows, show per-repo status if available.
3. Format:
   ```
   belayer-route-123  full-stack  Running
     extend-api:  lead ●
     extend-app:  spotter ✓
     extend-ios:  skipped
   ```

**Tests:** Build verification. Formatting tested manually.

---

### Task 11: Workflow tests — fan-out, partial flare, dependency ordering

**Goal:** Comprehensive Temporal TestWorkflowEnvironment tests for all multi-repo scenarios.

**Files:**
- `internal/v2/temporal/workflow_test.go` (modify — add multi-repo test cases)

**Steps:**

1. Test: 2-repo fan-out, both leads finish → anchor receives both → complete
2. Test: 3-repo fan-out, 1 flare, 2 finish → anchor receives 2 results
3. Test: dependency ordering — repo B depends on A → A runs first
4. Test: decomposer marks all repos unneeded → pipeline completes with "no changes"
5. Test: decomposer returns unknown repo name → warned and skipped
6. Test: single-repo fallback → unified code path, no decomposer

**Tests:** All implemented here using Temporal test framework.

---

### Task 12: Integration test — multi-repo E2E with worktrees

**Goal:** End-to-end test that creates real git repos, runs a multi-repo pipeline with worktrees.

**Files:**
- `internal/v2/temporal/integration_multirepo_test.go` (new)

**Steps:**

1. Create 2 temp git repos with initial commits.
2. Register them in a temp config.
3. Start workflow with 2-repo pipeline.
4. Mock decomposer marks both needed.
5. Mock sessions call finish with per-repo signals.
6. Verify: worktrees created, branches exist, results collected, worktrees cleaned up.

**Tests:** Full E2E against embedded Temporal dev server.

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
- Config package with atomic writes and repo validation catches errors early
- Worktree manager with SafeCleanup (push-before-delete) prevents work loss by design
- Topological sort for dependency ordering is clean and handles edge cases (circular, missing deps)
- Multi-repo signal routing (Role+Repo) naturally extends the single-repo model
- Pipeline inheritance with cycle detection via visited-set DFS is simple and correct

**What didn't:**
- macOS /var → /private/var symlink issue in tests (known from v1, used filepath.EvalSymlinks)

**Learnings to codify:**
- git worktree tests need real git repos with initial commits — can't use bare repos for worktree add
- Pipeline extends resolution needs both cycle detection AND path restriction (no ../) for security
- Fan-out with dependency ordering maps naturally to topological sort levels executed in sequence
