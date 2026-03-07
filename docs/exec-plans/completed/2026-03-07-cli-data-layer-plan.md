# Execution Plan: CLI and Data Layer (Goal 1)

**Date**: 2026-03-07
**Design Doc**: `docs/design-docs/2026-03-07-cli-data-layer-design.md`
**Status**: Complete

## Steps

| # | Step | Status | Files |
|---|------|--------|-------|
| 1 | Replace migrations with new clean schema | complete | `internal/db/migrations/001_initial.sql` (rewrite), delete `002_*.sql`, `003_*.sql` |
| 2 | Rewrite model/types.go with new types | complete | `internal/model/types.go` |
| 3 | Create store package | complete | `internal/store/store.go` |
| 4 | Write store tests | complete | `internal/store/store_test.go` |
| 5 | Remove old packages (coordinator, intake, lead, tui) | complete | Delete `internal/coordinator/`, `internal/intake/`, `internal/lead/`, `internal/tui/` |
| 6 | Rewrite CLI task.go | complete | `internal/cli/task.go` |
| 7 | Rewrite CLI status.go | complete | `internal/cli/status.go` |
| 8 | Update CLI root.go (remove tui cmd) | complete | `internal/cli/root.go` |
| 9 | Remove CLI tui.go | complete | `internal/cli/tui.go` |
| 10 | Update db_test.go for new schema | complete | `internal/db/db_test.go` |
| 11 | Verify go build and go test pass | complete | - |
