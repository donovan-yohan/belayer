# Review Guidance

Adversarial review configuration for `/harness:review`.

## Deployment Context

| Dimension | Value |
|-----------|-------|
| Language/Runtime | Go 1.22+ |
| Database | None (file-based state in `.belayer/.internal/`) |
| External Dependencies | Temporal (workflow orchestration), `gh` CLI (GitHub API) |
| Scale | CLI tool, single-user |
| Infrastructure | Local machine + Temporal server |
| CI/CD | `go test ./...`, `go vet ./...` |

## Adversarial Question Bank

### Concurrency & Race Conditions
- Are Temporal activities idempotent when retried?
- Can completion file writes race with poll reads?
- Does ExecSpawner properly kill process groups on cancellation?

### File System Safety
- Are `.belayer/.internal/` paths constructed safely (no path traversal)?
- Are completion files scoped by attempt number to prevent stale reads?
- Does the framework installer handle symlinks and permissions correctly?

### Pipeline Correctness
- Can a gate node game its own score (bypass score-then-route)?
- Are YAML validation rules comprehensive enough to catch malformed pipelines?
- Does the retry/routing logic handle all edge cases (max retries, missing routes)?

### Input Validation
- Is pipeline YAML validated before execution?
- Are node command strings sanitized before `sh -c` execution?
- Are intake specs validated before bridge creates workflows?

### Error Handling
- Does NodeActivity handle ExecSpawner failures gracefully?
- Are Temporal heartbeat panics caught in test environments?
- Does the worker daemon handle Temporal connection failures?

## Escape Log

| Date | Finding | Severity | Question Added |
|------|---------|----------|----------------|
| (none yet) | | | |
