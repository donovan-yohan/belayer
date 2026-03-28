# Quality

Testing strategy and conventions for belayer.

## Test Runner

Go's built-in `go test` with `testing` package. No external test framework.

## Test Files

| File | Coverage |
|------|----------|
| `internal/model/types_test.go` | Domain types: NodeOutcome, CompletionResult |
| `internal/pipeline/parser_test.go` | YAML pipeline parsing, node/gate config |
| `internal/pipeline/validate_test.go` | Pipeline validation rules |
| `internal/pipeline/doc_examples_test.go` | Pipeline documentation examples |
| `internal/gate/result_test.go` | Gate result parsing, weighted scoring |
| `internal/gate/prompt_test.go` | Gate prompt builder |
| `internal/events/logger_test.go` | JSONL event logging |
| `internal/outcome/detect_test.go` | Outcome detection: verdict.txt, output file, type default |
| `internal/session/exec_spawner_test.go` | ExecSpawner: command exec, exit channel |
| `internal/session/paths_test.go` | Path helpers for `.belayer/.internal/` |
| `internal/session/spawner_test.go` | SpawnOpts, spawner interface |
| `internal/temporal/workflow_test.go` | ClimbWorkflow orchestration |
| `internal/temporal/activity_test.go` | NodeActivity: spawn, heartbeat, poll completion |
| `internal/temporal/integration_test.go` | End-to-end pipeline integration |
| `internal/intake/bridge_test.go` | Intake bridge: SubmitSpec to workflow |
| `internal/intake/jira_test.go` | Jira intake adapter |
| `internal/intake/schedule_test.go` | Schedule reconciliation |
| `internal/cli/node_complete_test.go` | Node-complete CLI command |
| `internal/plugins/registry_test.go` | Plugin registry, marketplace registration |

## Conventions

- Table-driven tests preferred
- No tests should require network access or running services
- Temporal test environment: `TestWorkflowEnvironment` runs activities synchronously; polling loops based on `time.NewTicker` will never fire, so always add an immediate pre-tick check
- Fake spawners in integration tests must produce the output format matching the pipeline node type (e.g., gate nodes need gate-result.json + rationale.md, not just completion files)

## Idempotency Requirement

Pipeline completion files and node-context.json writes must be idempotent. Temporal activities may be retried, so:
- Completion files include attempt number to prevent stale reads
- `NodeActivity` cleans stale completion files from previous attempts before spawning
