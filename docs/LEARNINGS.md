# Learnings

Persistent learnings captured across sessions. Append-only, merge-friendly.

Status: `active` | `superseded`
Categories: `architecture` | `testing` | `patterns` | `workflow` | `debugging` | `performance`

---

### L-001: Temporal activity.RecordHeartbeat panics outside worker context
- status: active
- category: testing
- source: /harness:reflect 2026-03-20
- branch: master

When unit-testing Temporal activities that call `activity.RecordHeartbeat`, wrap the call in a `recover()` helper. The SDK panics if no activity interceptor is registered (which is the case in plain Go tests). Define a `recordHeartbeat` wrapper in the production code that defers `recover()`.

---

### L-002: Temporal test environment runs activities synchronously — tickers never fire
- status: active
- category: testing
- source: /harness:reflect 2026-03-20
- branch: master

The Temporal `TestWorkflowEnvironment` executes activities synchronously without advancing real time. Polling loops based on `time.NewTicker` will never fire their first tick. Always add an immediate pre-tick check before entering the ticker loop. This also improves production behavior for fast-completing sessions.

---

### L-003: File-based rendezvous needs attempt scoping to prevent stale reads
- status: active
- category: architecture
- source: /harness:reflect 2026-03-20
- branch: master

When using file-based rendezvous (activity polls for a completion file written by a hook), scope files by attempt number: `.belayer/completion/<task-id>-<node>-attempt-<N>.json`. Clean stale files from previous attempts before spawning a new session. Without this, a completion file from attempt 1 can satisfy the poll for attempt 2.

---

### L-004: JSON-interpolated shell commands need json.Marshal escaping
- status: active
- category: patterns
- source: /harness:reflect 2026-03-20
- branch: master

When building JSON config files (like hooks.json) that contain shell commands with user-provided values, use `json.Marshal` on the command string to get safe JSON escaping. Plain `fmt.Sprintf` with `%s` can corrupt JSON or allow injection if values contain quotes or metacharacters.

---

### L-005: Extending a pipeline primitive requires updating integration test spawners
- status: active
- category: testing
- source: /harness:reflect 2026-03-21
- branch: research-desloppify-scoping

When adding a new node type (e.g., gates) that changes the default pipeline, integration test spawners (`fakeSpawner`, `retryThenPassSpawner`) must produce the new output format. The default pipeline is used by integration tests — changing `type: node` to `type: gate` means fake spawners need to write gate-result.json + rationale.md, not just completion files.

---

### L-006: Score-then-route prevents adversarial session gaming
- status: active
- category: architecture
- source: /harness:reflect 2026-03-21
- branch: research-desloppify-scoping

When adding quality evaluation gates to a pipeline, the evaluation session should NOT decide the routing outcome. Instead: (1) session produces structured scores per dimension, (2) deterministic Go code computes the weighted average, (3) YAML-declared thresholds determine PASS/RETRY/FAIL. This prevents the session from being "nice" and always passing. The rationale.md is mandatory as an additional anti-gaming measure — no score without explanation.

---
