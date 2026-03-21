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
