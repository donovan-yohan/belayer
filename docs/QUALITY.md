# Quality

Testing strategy and conventions for belayer.

## Test Runner

Go's built-in `go test` with `testing` package. No external test framework.

## Test Files

| File | Coverage |
|------|----------|
| `internal/db/db_test.go` | Open, Migrate (idempotent), foreign keys |
| `internal/config/config_test.go` | Default config, JSON round-trip |

## Conventions

- Table-driven tests preferred
- Test helpers in `internal/testutil/`
- SQLite tests use in-memory databases (`:memory:`)
- No tests should require network access or running services
