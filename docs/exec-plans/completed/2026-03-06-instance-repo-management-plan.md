# Execution Plan: Instance & Repository Management (Goal 2)

**Design doc**: [2026-03-06-instance-repo-management-design](../../design-docs/2026-03-06-instance-repo-management-design.md)

## Steps

| # | Step | Status | Files |
|---|------|--------|-------|
| 1 | Create `internal/repo/` package with git operations | complete | `internal/repo/repo.go` |
| 2 | Create `internal/instance/` package with instance lifecycle | complete | `internal/instance/instance.go` |
| 3 | Wire up `instance create` CLI command | complete | `internal/cli/instance.go` |
| 4 | Add `instance list` CLI command | complete | `internal/cli/instance.go` |
| 5 | Add `instance delete` CLI command | complete | `internal/cli/instance.go` |
| 6 | Add worktree create/remove functions to repo package | complete | `internal/repo/repo.go` |
| 7 | Write unit tests for repo package | complete | `internal/repo/repo_test.go` |
| 8 | Write unit tests for instance package | complete | `internal/instance/instance_test.go` |
| 9 | Run `go test ./...` and `go build` | complete | - |
