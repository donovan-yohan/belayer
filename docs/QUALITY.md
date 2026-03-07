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
| `internal/coordinator/store_test.go` | Task/lead/decision CRUD, alignment attempt counting |
| `internal/coordinator/agentic_test.go` | Agentic node execution, mock claude, failure handling |
| `internal/coordinator/coordinator_test.go` | Full lifecycle, alignment, re-dispatch, PR creation, retry |
| `internal/coordinator/retry_test.go` | Exponential backoff scheduling |

## Conventions

- Table-driven tests preferred
- Test helpers in `internal/testutil/`
- SQLite tests use in-memory databases (`:memory:`)
- No tests should require network access or running services
