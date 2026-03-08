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
| `internal/instance/instance_test.go` | Instance create/load/delete, worktree management |
| `internal/lead/runner_test.go` | Lead execution, event handling, goal tracking |
| `internal/lead/store_test.go` | Lead store CRUD operations |
| `internal/intake/intake_test.go` | Intake pipeline, sufficiency, brainstorm, Jira parsing |
| `internal/lead/prompt_test.go` | Lead prompt builder, spotter feedback injection |
| `internal/anchor/prompt_test.go` | Anchor prompt builder (template rendering, data injection) |
| `internal/spotter/prompt_test.go` | Spotter prompt builder (template rendering, multiple profiles) |
| `internal/spotter/types_test.go` | SPOT.json type parsing |
| `internal/belayerconfig/config_test.go` | Config loader, resolution chain, TOML parsing |
| `internal/defaults/defaults_test.go` | Embedded file system (belayer.toml, prompts, profiles exist) |
| `internal/defaults/write_test.go` | WriteToDir (file creation, no-overwrite behavior) |
| `internal/setter/setter_test.go` | Setter lifecycle, spotting flow, anchor flow, crash recovery |
| `internal/setter/dag_test.go` | DAG construction and traversal |

## Conventions

- Table-driven tests preferred
- Test helpers in `internal/testutil/`
- SQLite tests use in-memory databases (`:memory:`)
- No tests should require network access or running services
- Mock claude scripts: Prepend temp dir to `PATH` with mock `claude` bash scripts that return canned JSON based on prompt content
- Lead runner mock scripts must output verdict JSON to stdout (not write files) — `claude -p` is print-only mode

## Known Issues

- `TestProcessPendingTask_Decomposition`: Flaky due to TempDir cleanup race condition. Passes ~2/3 runs. Pre-existing, low severity.
