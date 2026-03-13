# Environment Provider Implementation

> **Status**: Complete | **Created**: 2026-03-12 | **Last Updated**: 2026-03-13
> **Design Doc**: `docs/design-docs/2026-03-12-environment-provider-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-12 | Design | Single provider model (no builtin/external split) | Dog-food the contract; one code path in daemon |
| 2026-03-12 | Design | `belayer env` as default provider | Wraps existing bare-repo + worktree logic behind the JSON contract |
| 2026-03-12 | Design | Worktree paths as data, not convention | Daemon stores paths from provider responses rather than computing them |
| 2026-03-12 | Design | Per-problem environments | Leads within a problem share infra; simultaneous problems get separate envs |

## Progress

- [x] Task 1: JSON contract types _(completed 2026-03-13)_
- [x] Task 2: SQLite migration (005_environments) _(completed 2026-03-13)_
- [x] Task 3: Store methods for environments _(completed 2026-03-13)_
- [x] Task 4: Config — add [environment] section _(completed 2026-03-13)_
- [x] Task 5: Provider client (daemon-side shell-out + JSON parsing) _(completed 2026-03-13)_
- [x] Task 6: `belayer env` CLI command (default provider) _(completed 2026-03-13)_
- [x] Task 7: Daemon integration (refactor ProblemRunner) _(completed 2026-03-13)_

## Surprises & Discoveries

| Date | What | Impact | Resolution |
|------|------|--------|------------|
| 2026-03-13 | `agentic.go` no longer exists | Setpgid pattern needed for client had to be implemented from docs | Implemented from DESIGN.md description — works correctly |
| 2026-03-13 | `environments` FK references `problems(id)` | `belayer env create` requires a matching problem record | Correct for daemon usage; standalone env creation would need FK relaxation in future |
| 2026-03-13 | Climbs table was originally `goals`, renamed in migration 002 | Migration 005 correctly targets `climbs` | No issue — worker confirmed correct table name |

## Plan Drift

| Task | Plan Said | Actually Did | Why |
|------|-----------|-------------|-----|
| Task 5 | `NewClient(command, subcommand)` 2-arg | `NewClient(command, subcommand, cragName)` 3-arg | Task 7 revealed `belayer env` needs `--crag` flag passthrough |
| Task 7 | Remove all `crag.CreateWorktree()` calls | Added nil-check fallback (envClient nil = legacy mode) | Preserves backward compat when no environment config is set |

---

## Task 1: JSON Contract Types

**Goal:** Define Go types for all JSON request/response structures from the contract.

**File:** `internal/envprovider/types.go` (new)

**Steps:**

1. Create `internal/envprovider/types.go` with types for all JSON responses:
   - `CreateEnvResponse` — status, name, index, env map, services map, worktrees slice
   - `AddWorktreeResponse` — status, repo, branch, path, env_file
   - `ResetEnvResponse` — status, duration_ms, snapshot
   - `StatusEnvResponse` — status, name, index, services, snapshot, worktrees
   - `LogsEnvResponse` — status, service, lines
   - `ListEnvsResponse` — status, environments
   - `ErrorResponse` — status, error, code
   - `ServiceStatus`, `SnapshotInfo`, `WorktreeInfo`, `LogLine`, `EnvSummary` sub-types

2. Create `internal/envprovider/types_test.go` — test JSON round-trip for each type (marshal → unmarshal) to validate struct tags match the contract.

**Verification:** `go test ./internal/envprovider/...`

---

## Task 2: SQLite Migration

**Goal:** Add `environments` table and `worktree_path` column to support provider-returned paths.

**File:** `internal/db/migrations/005_environments.sql` (new)

**Steps:**

1. Create migration file:
   ```sql
   CREATE TABLE IF NOT EXISTS environments (
       problem_id TEXT NOT NULL REFERENCES problems(id),
       provider_command TEXT NOT NULL,
       env_name TEXT NOT NULL,
       env_json TEXT NOT NULL DEFAULT '{}',
       created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
       PRIMARY KEY (problem_id)
   );

   ALTER TABLE leads ADD COLUMN worktree_path TEXT NOT NULL DEFAULT '';
   ```

   Note: `leads` table doesn't exist — climbs is the actual table. Check the actual table name. The column goes on `climbs` if that's where climb state lives.

2. Verify migration runs: `go test ./internal/db/...`

**Verification:** Build passes, migration applies cleanly on fresh DB.

---

## Task 3: Store Methods for Environments

**Goal:** CRUD operations for the environments table.

**File:** `internal/store/store.go` (add methods)

**Steps:**

1. Add methods to `Store`:
   - `InsertEnvironment(problemID, providerCommand, envName, envJSON string) error`
   - `GetEnvironment(problemID string) (*model.Environment, error)`
   - `DeleteEnvironment(problemID string) error`
   - `UpdateClimbWorktreePath(climbID, worktreePath string) error`
   - `GetClimbWorktreePath(climbID string) (string, error)`

2. Add `model.Environment` type in `internal/model/types.go`:
   ```go
   type Environment struct {
       ProblemID       string
       ProviderCommand string
       EnvName         string
       EnvJSON         string
       CreatedAt       time.Time
   }
   ```

3. Write tests in `internal/store/store_test.go` — insert, get, delete cycle.

**Verification:** `go test ./internal/store/...`

---

## Task 4: Config — Add [environment] Section

**Goal:** Add environment provider config to belayerconfig.

**Files:**
- `internal/belayerconfig/config.go` — add `EnvironmentConfig` struct
- `internal/defaults/belayer.toml` — add default `[environment]` section

**Steps:**

1. Add to `config.go`:
   ```go
   type EnvironmentConfig struct {
       Command    string `toml:"command"`     // e.g., "belayer" or "extend"
       Subcommand string `toml:"subcommand"`  // e.g., "env"
       Snapshot   string `toml:"snapshot"`    // default snapshot name (optional)
   }
   ```

2. Add `Environment EnvironmentConfig \`toml:"environment"\`` field to `Config` struct.

3. Add defaults to embedded `belayer.toml`:
   ```toml
   [environment]
   command = "belayer"
   subcommand = "env"
   snapshot = ""
   ```

4. Test that `Load()` picks up the new section with defaults.

**Verification:** `go test ./internal/belayerconfig/...`

---

## Task 5: Provider Client

**Goal:** Daemon-side code that shells out to the configured provider command and parses JSON responses.

**File:** `internal/envprovider/client.go` (new)

**Steps:**

1. Create `Client` struct:
   ```go
   type Client struct {
       Command    string
       Subcommand string
   }
   ```

2. Implement methods that match the contract:
   - `CreateEnv(ctx, name, snapshot string) (*CreateEnvResponse, error)`
   - `AddWorktree(ctx, envName, repo, branch, baseRef string) (*AddWorktreeResponse, error)`
   - `RemoveWorktree(ctx, envName, repo, branch string) error`
   - `ResetEnv(ctx, envName, snapshot string) error`
   - `DestroyEnv(ctx, envName string) error`
   - `StatusEnv(ctx, envName string) (*StatusEnvResponse, error)`
   - `LogsEnv(ctx, envName, service string) (*LogsEnvResponse, error)`
   - `ListEnvs(ctx) (*ListEnvsResponse, error)`

3. Each method:
   - Builds `exec.CommandContext` with `[command, subcommand, action, --flags..., --json]`
   - Captures stdout
   - Checks exit code — if non-zero, parse `ErrorResponse` from stdout
   - On success, unmarshal JSON into response type
   - Process group isolation (`Setpgid: true`) consistent with existing patterns

4. Write `client_test.go` — test with a mock script that returns known JSON. Use `os.Executable()` trick or temp shell scripts.

**Verification:** `go test ./internal/envprovider/...`

---

## Task 6: `belayer env` CLI Command

**Goal:** Default provider command that wraps existing bare-repo + worktree logic behind the JSON contract.

**Files:**
- `internal/cli/env.go` (new) — cobra command registration
- `internal/env/` (new package) — implementation logic

**Steps:**

1. Create `internal/cli/env.go` with subcommands:
   - `belayer env create --name NAME [--snapshot S] [--crag CRAG] --json`
   - `belayer env add-worktree --name NAME --repo R --branch B [--base-ref R] [--crag CRAG] --json`
   - `belayer env remove-worktree --name NAME --repo R --branch B [--crag CRAG] --json`
   - `belayer env destroy --name NAME [--crag CRAG] --json`
   - `belayer env reset --name NAME [--snapshot S] [--crag CRAG] --json`
   - `belayer env status --name NAME [--crag CRAG] --json`
   - `belayer env logs --name NAME [--service S] [--crag CRAG] --json`
   - `belayer env list [--crag CRAG] --json`

2. Each subcommand resolves the crag (reusing existing `--crag` / `$BELAYER_CRAG` pattern from helpers.go).

3. Implementation logic in `internal/env/`:
   - `create`: Insert environment record in SQLite, return metadata (empty env/services)
   - `add-worktree`: Call `crag.CreateWorktree()`, update SQLite, return path. Use cragDir + env name for path base.
   - `remove-worktree`: Call `crag.RemoveWorktree()`, update SQLite
   - `destroy`: Call `crag.CleanupProblemWorktrees()` (using env name as problem ID), delete environment record
   - `reset`: No-op, return success with `duration_ms: 0`
   - `status`: Query worktree git status via `repo.WorktreeDiff()`, return structured response
   - `logs`: Return empty lines array
   - `list`: Query environments table

4. `--json` flag: when set, output JSON to stdout. When not set, print human-readable table.

5. Register `newEnvCmd()` in `root.go`.

6. Write tests — at minimum test the `create` → `add-worktree` → `destroy` lifecycle against a temp crag with a bare repo.

**Verification:** `go test ./internal/env/...` and manual `belayer env create --name test --crag <crag> --json`

---

## Task 7: Daemon Integration

**Goal:** Refactor `ProblemRunner` to use the provider client instead of direct `crag.CreateWorktree()` calls.

**Files:**
- `internal/belayer/taskrunner.go` — refactor Init, SpawnClimb, Cleanup
- `internal/belayer/belayer.go` — pass provider client to ProblemRunner

**Steps:**

1. Add `envClient *envprovider.Client` field to `ProblemRunner` struct (line ~60).

2. Construct `envprovider.Client` from `belayerconfig.EnvironmentConfig` when creating the `ProblemRunner` in `belayer.go`.

3. Refactor `ProblemRunner.Init()` (line ~117):
   - Replace the `crag.CreateWorktree()` loop with:
     ```go
     // Create environment
     createResp, err := tr.envClient.CreateEnv(ctx, tr.task.ID, tr.envConfig.Snapshot)
     // Store in SQLite
     tr.store.InsertEnvironment(tr.task.ID, fmt.Sprintf("%s %s", tr.envClient.Command, tr.envClient.Subcommand), tr.task.ID, createRespJSON)

     // Add worktrees per repo
     for repoName := range repos {
         branch := fmt.Sprintf("belayer/%s/%s", tr.task.ID, repoName)
         wtResp, err := tr.envClient.AddWorktree(ctx, tr.task.ID, repoName, branch, "")
         tr.worktrees[repoName] = wtResp.Path
         tr.store.UpdateClimbWorktreePath(climbID, wtResp.Path)
     }
     ```

4. Refactor `ProblemRunner.SpawnClimb()` (line ~228):
   - `worktreePath := tr.worktrees[climb.RepoName]` — already uses the map, so this continues to work after Init populates from provider responses.

5. Refactor `ProblemRunner.Cleanup()` (line ~577):
   - Replace any direct cleanup with `tr.envClient.DestroyEnv(ctx, tr.task.ID)`.

6. Add `ResetEnv` call in the spotter-failure retry path if configured.

7. Update existing tests in `internal/belayer/` — mock the provider client or use `belayer env` against temp crags.

**Verification:** `go test ./internal/belayer/...` and `go test ./...`

---

## Outcomes & Retrospective

**What worked:**
- Bottom-up task ordering (types → migration → store → config → client → CLI → daemon) allowed each task to be independently verified
- Single provider model eliminated the need for Go interfaces or multi-implementation branching
- 7 tasks parallelized across 3 workers in 4 batches for efficient execution
- Fix cycle caught 5 real issues (init cleanup, nil guard, context threading, worktree count, error messages)

**What didn't:**
- NewClient signature evolved from 2-arg to 3-arg during Task 7 (cragName needed for --crag flag passthrough) — could have been caught during planning by tracing the full CLI invocation path
- Worker committed go.mod/go.sum cleanup and SCM changes alongside the fix commit — unrelated changes bundled together

**Learnings to codify:**
- When designing a provider/client that shells out to CLI commands, trace the full command line during planning to catch flag requirements early
- Single provider model (dog-food the contract) is strongly preferred over builtin/external splits — one code path, always tested
- Cleanup-on-failure patterns are critical for any multi-step init that creates resources (env → worktrees) — review for orphan risk
