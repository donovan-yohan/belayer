# Quality

Testing strategy and conventions for belayer.

## Test Runner

Go's built-in `go test` with `testing` package. No external test framework.

## Test Files

| File | Coverage |
|------|----------|
| `internal/db/db_test.go` | Open, Migrate (idempotent), foreign keys |
| `internal/config/config_test.go` | Default config, JSON round-trip |
| `internal/repo/repo_test.go` | URL parsing, worktree add/remove, bare clone |
| `internal/crag/crag_test.go` | Crag create/load/delete, worktree management |
| `internal/spotter/types_test.go` | SPOT.json type parsing |
| `internal/climbctx/climbctx_test.go` | GOAL.json types and writer (lead, spotter, anchor variants) |
| `internal/belayerconfig/config_test.go` | Config loader, crag/global/embedded resolution chain, TOML parsing |
| `internal/defaults/defaults_test.go` | Embedded file system (belayer.toml, prompts, profiles exist) |
| `internal/defaults/write_test.go` | WriteToDir (file creation, no-overwrite behavior) |
| `internal/belayer/belayer_test.go` | Belayer daemon lifecycle, spotting flow, anchor flow, crash recovery |
| `internal/belayer/dag_test.go` | DAG construction and traversal |
| `internal/lead/claude_test.go` | ClaudeSpawner env injection (empty, single, multiple env vars) |
| `internal/store/store_test.go` | Store CRUD: tracker_issues, pull_requests, pr_reactions, problem tracker_issue_id, environment idempotency |
| `internal/review/engine_test.go` | Reaction engine: event classification, CI/review state, decision logic |
| `internal/scm/stacking_test.go` | PR stacking: greedy bin-packing, single climb, empty input |
| `internal/scm/prbodygen_test.go` | PR body generation: prompt builder, output parser |
| `internal/scm/github/github_test.go` | GitHub SCM: PR status parsing, activity parsing, CI status logic |
| `internal/tracker/github/github_test.go` | GitHub tracker: issue list/detail JSON parsing |
| `internal/tracker/specassembly_test.go` | Spec assembly: prompt builder, output parser |

## Conventions

- Table-driven tests preferred
- Test helpers in `internal/testutil/`
- SQLite tests use in-memory databases (`:memory:`)
- No tests should require network access or running services
- Mock claude scripts: Prepend temp dir to `PATH` with mock `claude` bash scripts that return canned JSON based on prompt content
- Mock tmux implementations must include all TmuxManager interface methods (SetRemainOnExit, IsPaneDead, CapturePaneContent)

## Idempotency Requirement

All store operations and CLI commands that modify state **must be idempotent** — safe to call multiple times with the same inputs without error. This is critical because:

1. **The daemon retries failed initializations.** If Init fails partway through, it will re-run from the start. Any state written before the failure must not block the retry.
2. **Subprocesses and the daemon share the database.** The daemon calls CLI subcommands (e.g., `belayer env create`) that write to the same SQLite database. The daemon may also write the same data directly. Both writes must succeed.
3. **Crash recovery replays state transitions.** Problems can be reset to `pending` and re-initialized.

**Rules:**
- Use `INSERT OR REPLACE` (not bare `INSERT`) for any store method that may be called during Init or retry paths
- Add an idempotency test for every store method: call it twice with the same inputs, assert no error on the second call
- CLI commands that create resources must handle "already exists" gracefully (warn or no-op, never fatal)

## Known Issues

- `TestProcessPendingProblem_Decomposition`: Flaky due to TempDir cleanup race condition. Passes ~2/3 runs. Pre-existing, low severity.
