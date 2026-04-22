# Agent runtime state, mail acks, and deterministic handoff — Implementation Plan

> **Status**: Ready for Review | **Created**: 2026-04-21 | **Last Updated**: 2026-04-21
> **Design Doc**: `docs/design-docs/2026-04-21-agent-runtime-state-and-mail-acks.md`
> **Consulted Learnings**: None (LEARNINGS.md not present)
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-04-21 | Design | Keep `agent_runs` table; add `outcome` column | Minimizes blast radius. Identity/Activation split deferred. |
| 2026-04-21 | Design | `delivered_at` + `acknowledged_at` timestamps instead of enum string | Timestamps carry state and latency data; string would duplicate info |
| 2026-04-21 | Design | Bridge-emitted `bridge:message_ack` on turn-end (implicit ack) | Zero new tool surface; couples ack to LLM consuming the message |
| 2026-04-21 | Design | Daemon writes `handoff.md` inventory on supervisor-incomplete | Facts from store; supervisor writes narrative; keeps plumbing model |
| 2026-04-21 | Design | `max_concurrent_agents: 15` in `.belayer/config.yaml#runtime` | Daemon enforces at spawn; supervisor cannot drift the cap in a prompt |
| 2026-04-21 | Design | Keep `status` column as lifecycle, add `outcome` as orthogonal axis | Roster consumers read both; lifecycle values are rename-only on bridge handlers |
| 2026-04-21 | Design | Implicit mail ack via `bridge:message_ack` at turn-end; no new `belayer_ack_message` tool this phase | "Acknowledged" semantically = "LLM consumed the message"; zero new tool surface |
| 2026-04-21 | Design | `session_digest` SSE frame carries per-agent `outcome` | Dashboards/runners get the axis without roster round-trip; ~20 bytes/agent/digest |
| 2026-04-21 | Design | No `stalled` lifecycle inference | Policy belongs in supervisor judgment, not daemon heuristic |

## Progress

- [ ] Phase 1 — `max_turns` wiring + `bridge:budget_exhausted` event + `outcome` column
  - [ ] Task 1.1: Add `outcome` column to `agent_runs` (default `active`)
  - [ ] Task 1.2: Plumb `agent.max_turns` from YAML → `bridge.Config.MaxTurns` → env `BELAYER_MAX_TURNS`
  - [ ] Task 1.3: `hermes_bridge/__main__.py` passes `max_turns` to `AIAgent` and emits `bridge:budget_exhausted` when Hermes returns `budget_exhausted=True`
  - [ ] Task 1.4: Daemon handles `bridge:budget_exhausted` → set outcome, emit agent_status, send supervisor mail
  - [ ] Task 1.5: `belayer roster` renders `<lifecycle>/<outcome>`
  - [ ] Task 1.6: Extend SSE `session_digest` frame with per-agent `outcome`
  - [ ] Task 1.7: E2E test — spawn agent with `max_turns: 2`, drive past cap, assert supervisor mail + outcome column + digest frame
- [ ] Phase 2 — Mail ack states (implicit ack only)
  - [ ] Task 2.1: Migration — add `delivered_at`, `acknowledged_at`; drop `delivered` column usage (keep for backcompat read one release)
  - [ ] Task 2.2: Store helpers — `MarkDelivered(id)`, `MarkAcknowledged(ids)`, `RollbackUnacked(sessionID)`
  - [ ] Task 2.3: Poll handler sets `delivered_at` on return; daemon restart hook rolls back unacked to queued
  - [ ] Task 2.4: Bridge tracks injected message IDs per turn; emits `bridge:message_ack {ids}` at turn-end; daemon acks via store helper
  - [ ] Task 2.5: Urgent path — `stdin_reader.py` emits ack line after injection; daemon parses and flips both `delivered_at` and `acknowledged_at` on same event
  - [ ] Task 2.6: `belayer roster` exposes `pending_mail` and `unacked_mail` counts
  - [ ] Task 2.7: E2E — send message, crash bridge mid-turn, restart daemon, assert redelivery
- [ ] Phase 3 — Deterministic handoff artifact
  - [ ] Task 3.1: `internal/daemon/handoff.go` — `WriteHandoffArtifact(sessionID) (path, error)`
  - [ ] Task 3.2: Wire into persistence-strategy reprompt path (fires before reprompt, idempotent)
  - [ ] Task 3.3: Register artifact kind=`handoff` with written path
  - [ ] Task 3.4: E2E — supervisor-incomplete, assert artifact exists, contains roster+mail+git+events
- [ ] Phase 4 — Concurrent-agent cap
  - [ ] Task 4.1: `config.yaml` schema gains `runtime.max_concurrent_agents` (default 15)
  - [ ] Task 4.2: `belayer_spawn_agent` handler rejects with structured error when cap hit
  - [ ] Task 4.3: `init.go` scaffolds the field in project-local config
  - [ ] Task 4.4: Unit test — spawn 15 agents, 16th rejects with cap error

## Phase 1 — `max_turns` wiring + budget-exhausted event + outcome column

### Task 1.1: Add `outcome` column to `agent_runs`

**Files:**
- Modify: `internal/store/migrations.go`
- Modify: `internal/store/store.go` (AgentRun struct)
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Failing test** in `store_test.go`:

```go
func TestMigrate_AgentRunsOutcomeColumn(t *testing.T) {
    s, err := Open(":memory:")
    if err != nil { t.Fatal(err) }
    defer s.Close()
    rows, err := s.DB().Query("PRAGMA table_info(agent_runs)")
    if err != nil { t.Fatal(err) }
    defer rows.Close()
    seen := map[string]string{}
    for rows.Next() {
        var cid int; var name, typ string; var notnull, pk int; var dflt sql.NullString
        if err := rows.Scan(&cid,&name,&typ,&notnull,&dflt,&pk); err != nil { t.Fatal(err) }
        seen[name] = dflt.String
    }
    if _, ok := seen["outcome"]; !ok {
        t.Fatalf("agent_runs missing outcome column")
    }
    if want := "'active'"; seen["outcome"] != want {
        t.Fatalf("outcome default = %q, want %q", seen["outcome"], want)
    }
}
```

- [ ] **Step 2: Run — expect FAIL**
- [ ] **Step 3: Implement** — append to `migrations.go`:

```go
if err := addColumnIfNotExists(db, "agent_runs", "outcome", "TEXT NOT NULL DEFAULT 'active'"); err != nil {
    return err
}
```

Add `Outcome string` field to `AgentRun` struct in `store.go`. Update all
agent_runs SELECT/INSERT/UPDATE helpers to read/write the column.

- [ ] **Step 4: Run — expect PASS**
- [ ] **Step 5: Commit** `feat(store): add outcome column to agent_runs`

### Task 1.2: Plumb `max_turns` from YAML → bridge env

**Files:**
- Read: `agents/*/agent.yaml` (confirm `agent.max_turns` field exists or document absence)
- Modify: `internal/daemon/agents.go` (load MaxTurns from AgentConfig)
- Modify: `internal/bridge/bridge.go` (add `MaxTurns int` to `Config`, export as env)
- Modify: `internal/bridge/bridge_test.go`

- [ ] **Step 1: Inspect** `agents/supervisor/agent.yaml` — confirm the
  `agent.max_turns` key is present in Hermes schema. If absent in
  shipped defaults, add with `max_turns: 90` to preserve current
  behavior.

- [ ] **Step 2: Failing test** — bridge passes `BELAYER_MAX_TURNS` env:

```go
func TestBridgeConfig_MaxTurnsEnv(t *testing.T) {
    cfg := Config{MaxTurns: 42}
    env := cfg.Env()
    if !slices.Contains(env, "BELAYER_MAX_TURNS=42") {
        t.Fatalf("env missing MAX_TURNS: %v", env)
    }
}
```

- [ ] **Step 3: Run — expect FAIL**
- [ ] **Step 4: Implement** `Config.MaxTurns` field + env export. In
  `agents.go` `bridgeLaunchAgent`:

```go
bridgeCfg.MaxTurns = agentCfg.Agent.MaxTurns
```

- [ ] **Step 5: Run — expect PASS**
- [ ] **Step 6: Commit** `feat(bridge): plumb agent.max_turns to BELAYER_MAX_TURNS env`

### Task 1.3: Bridge reads env, passes to Hermes, emits budget event

**Files:**
- Modify: `hermes_bridge/__main__.py`

- [ ] **Step 1: Confirm** Hermes `AIAgent` accepts `max_turns` kwarg and
  the result dict exposes `budget_exhausted` (inspect
  `~/.hermes/hermes/agent.py` or the installed package).

- [ ] **Step 2: Read env** at bridge startup:

```python
max_turns_env = os.environ.get("BELAYER_MAX_TURNS")
max_turns = int(max_turns_env) if max_turns_env and max_turns_env.isdigit() else None
```

- [ ] **Step 3: Pass to `AIAgent`** construction (kwarg, only if set).

- [ ] **Step 4: Emit budget event** after `run_conversation` returns:

```python
if result.get("budget_exhausted"):
    emit_event("bridge:budget_exhausted", {
        "turns_used": result.get("turns_used"),
        "max_turns": max_turns,
        "last_message": result.get("last_message", ""),
    })
```

Keep the normal `bridge:finished` emission so downstream handlers that
key off finished still see it. Budget event is additive.

- [ ] **Step 5: Commit** `feat(bridge): emit bridge:budget_exhausted on Hermes turn cap`

### Task 1.4: Daemon handles budget event

**Files:**
- Modify: `internal/daemon/bridge_events.go` (add case for `bridge:budget_exhausted`)
- Modify: `internal/daemon/bridge_events_test.go`

- [ ] **Step 1: Failing test**:

```go
func TestBridgeEvent_BudgetExhausted_SetsOutcomeAndMailsSupervisor(t *testing.T) {
    d := testDaemon(t)
    sid := d.newSession(t)
    spawnAgent(d, sid, "supervisor")
    spawnAgent(d, sid, "backend-dev-1")
    d.handleBridgeEvent(sid, "backend-dev-1", "bridge:budget_exhausted",
        map[string]any{"turns_used": 90, "max_turns": 90, "last_message": "still refactoring..."})
    run := mustGetAgentRun(t, d, sid, "backend-dev-1")
    if run.Outcome != "budget_exhausted" { t.Fatalf("outcome=%q", run.Outcome) }
    msgs := pendingMail(t, d, sid, "supervisor")
    if len(msgs) != 1 { t.Fatalf("supervisor mail=%d", len(msgs)) }
    if !strings.Contains(msgs[0].Content, "exhausted its turn budget") {
        t.Fatalf("mail content=%q", msgs[0].Content)
    }
}
```

- [ ] **Step 2: Implement** — switch case in `handleBridgeEvent`:

```go
case "bridge:budget_exhausted":
    if err := d.store.UpdateAgentOutcome(sid, agentName, "budget_exhausted"); err != nil { /* log */ }
    d.emitAgentStatus(sid, agentName, "exited", "budget_exhausted")
    body := fmt.Sprintf("Agent %s exhausted its turn budget (%v/%v). Its last message: %q. Decide whether to respawn, retire, or continue without it.",
        agentName, data["turns_used"], data["max_turns"], data["last_message"])
    d.sendSystemMessage(sid, "supervisor", body)
```

- [ ] **Step 3: Run — expect PASS**
- [ ] **Step 4: Commit** `feat(daemon): handle bridge:budget_exhausted — set outcome and notify supervisor`

### Task 1.5: Roster renders `<lifecycle>/<outcome>`

**Files:**
- Modify: `internal/cli/roster.go` (or wherever roster print lives)
- Modify: `internal/daemon/api.go` (roster JSON response)

- [ ] **Step 1: Update roster render** — `status` column becomes
  `lifecycle/outcome` string. Preserve old JSON field name; add new
  `outcome` field alongside.
- [ ] **Step 2: Golden test** — roster snapshot with one agent in each
  outcome state (`active`, `succeeded`, `blocked`, `incomplete`,
  `failed`, `budget_exhausted`).
- [ ] **Step 3: Commit** `feat(roster): render lifecycle/outcome as two-axis status`

### Task 1.6: SSE session_digest carries per-agent outcome

**Files:**
- Modify: `internal/daemon/sse.go` (or wherever `session_digest` is built)
- Modify: `internal/daemon/sse_test.go`

- [ ] **Step 1: Failing test** — subscribe to `/sessions/{id}/events`,
  trigger one agent into `outcome=budget_exhausted`, assert the next
  digest frame payload contains `"outcome":"budget_exhausted"` for
  that agent alongside existing fields.
- [ ] **Step 2: Implement** — extend the per-agent map inside
  `session_digest` frame builder to read the new column. Preserve
  existing fields; add `outcome` alongside. No schema bump of the
  digest frame (additive JSON).
- [ ] **Step 3: Run — expect PASS**
- [ ] **Step 4: Commit** `feat(sse): surface per-agent outcome in session_digest frames`

### Task 1.7: E2E — max_turns=2 budget exhaustion

**Files:**
- Add: `internal/daemon/e2e_budget_test.go`

Spawn a real bridge with `max_turns=2` against a stub Hermes that always
returns `completed=False, budget_exhausted=True` on turn 2. Assert:
1. `bridge:budget_exhausted` event recorded
2. `agent_runs.outcome = 'budget_exhausted'`
3. Supervisor received mail with expected body
4. SSE digest frame for that session reports the outcome

- [ ] **Commit** `test(daemon): e2e coverage for budget_exhausted path`

## Phase 2 — Mail ack states

### Task 2.1: Migration — delivered_at + acknowledged_at

**Files:**
- Modify: `internal/store/migrations.go`
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Failing test** asserts both columns present with NULL
  default. Also assert `idx_messages_pending` is rebuilt to use
  `delivered_at IS NULL` instead of `delivered=0`.

- [ ] **Step 2: Implement**:

```go
if err := addColumnIfNotExists(db, "messages", "delivered_at", "DATETIME"); err != nil { return err }
if err := addColumnIfNotExists(db, "messages", "acknowledged_at", "DATETIME"); err != nil { return err }
```

Append to `stmts`:

```sql
DROP INDEX IF EXISTS idx_messages_pending;
CREATE INDEX IF NOT EXISTS idx_messages_unacked
  ON messages(session_id, recipient_id, acknowledged_at, created_at);
```

Keep `delivered INTEGER` column for one release; new code reads
timestamps, old readers still compile.

- [ ] **Step 3: Commit** `feat(store): migrate messages to delivered_at/acknowledged_at timestamps`

### Task 2.2: Store helpers

**Files:**
- Modify: `internal/store/messages.go`

- [ ] Add `MarkDelivered(ids []string) error`, `MarkAcknowledged(ids
  []string) error`, `RollbackUnacked(sessionID string) (int, error)`.
- [ ] Unit tests for each.
- [ ] Commit `feat(store): message delivery state helpers`

### Task 2.3: Poll handler + daemon restart rollback

**Files:**
- Modify: `internal/daemon/messages.go`
- Modify: `internal/daemon/daemon.go` (restart hook)

- [ ] Poll handler: after rows are selected for return, call
  `MarkDelivered(ids)` in the same transaction.
- [ ] `New()` / startup path: for every active session, call
  `RollbackUnacked(sid)` and log the count.
- [ ] Unit test: deliver message, simulate restart, assert message
  back in queued state.
- [ ] Commit `feat(daemon): roll back unacked mail on daemon restart`

### Task 2.4: Bridge emits `bridge:message_ack` on turn-end

**Files:**
- Modify: `hermes_bridge/__main__.py`
- Modify: `internal/daemon/bridge_events.go`

- [ ] Bridge tracks `pending_message_ids` — populated when the poll
  response injects a message into the conversation. At turn-end,
  emit `bridge:message_ack {ids: [...]}` and clear the list.
- [ ] Daemon handler calls `MarkAcknowledged(ids)`.
- [ ] Unit test.
- [ ] Commit `feat(bridge): ack consumed mail at turn-end`

### Task 2.5: Urgent path — stdin ack

**Files:**
- Modify: `hermes_bridge/stdin_reader.py`
- Modify: `internal/daemon/bridge_events.go`

- [ ] stdin_reader emits `{"event":"mail:ack_stdin","id":"<mid>"}` over
  the normal event channel after successfully enqueueing the
  interrupt.
- [ ] Daemon: urgent-path `sendMessage` stores the message with
  `delivered_at` unset; ack event flips both `delivered_at` and
  `acknowledged_at`. Urgent messages never sit in the delivered-but-
  unacked state because the stdin writer is atomic.
- [ ] Integration test: urgent send → ack event → columns populated.
- [ ] Commit `feat(bridge): ack urgent-path messages from stdin_reader`

### Task 2.6: Roster exposes mail counts

**Files:**
- Modify: `internal/daemon/api.go`
- Modify: `internal/cli/roster.go`

- [ ] Add `pending_mail`, `unacked_mail` per agent in roster JSON and
  CLI render.
- [ ] Commit `feat(roster): expose pending and unacked mail counts`

### Task 2.7: E2E — crash mid-turn, restart, redeliver

- [ ] Spawn bridge, send message, kill bridge between delivery and
  turn-end. Restart daemon. Spawn fresh bridge for same identity.
  Assert message redelivered on first poll.
- [ ] Commit `test(daemon): e2e mail redelivery after bridge crash`

## Phase 3 — Deterministic handoff artifact

### Task 3.1: Handoff writer

**Files:**
- Add: `internal/daemon/handoff.go`
- Add: `internal/daemon/handoff_test.go`

- [ ] Signature:

```go
func (d *Daemon) WriteHandoffArtifact(sessionID string) (path string, err error)
```

- [ ] Produces markdown with six sections: Session, Roster, Unacked
  Mail, Git State, Artifacts, Recent Events.
- [ ] Writes to `<run_dir>/handoff.md` (one per session; overwrite on
  re-call).
- [ ] Unit test with fixture session covering all six sections.
- [ ] Commit `feat(daemon): write deterministic handoff artifact from store state`

### Task 3.2: Wire into persistence-strategy reprompt

**Files:**
- Modify: `internal/daemon/persistence.go` (or wherever the incomplete
  reprompt hook lives)

- [ ] Before reprompt fires, call `WriteHandoffArtifact(sid)`.
  Idempotent — if called twice, overwrites.
- [ ] Register via `RegisterArtifact(kind=handoff, path=<path>,
  producer="daemon")`. Skip re-register if kind=handoff already
  present for the session.
- [ ] Commit `feat(daemon): emit handoff artifact on supervisor-incomplete`

### Task 3.3: Artifact kind registration

**Files:**
- Modify: `internal/store/artifacts.go` (if kind is validated anywhere)

- [ ] Add `handoff` to the known-kinds list if one exists.
- [ ] Commit (squash with 3.2 if trivial).

### Task 3.4: E2E — supervisor-incomplete handoff

- [ ] Drive a session to supervisor-incomplete with a missing persistence
  strategy step. Assert `.belayer/runs/<sid>/handoff.md` exists,
  contains roster entries and unacked messages, and is registered as
  an artifact with kind=`handoff`.
- [ ] Commit `test(daemon): e2e handoff artifact on incomplete`

## Phase 4 — Concurrent-agent cap

### Task 4.1: Config schema + default

**Files:**
- Modify: `internal/config/config.go` (or equivalent)
- Modify: `internal/cli/init.go` (scaffolded defaults)
- Modify: `.belayer/config.yaml.example` if present

- [ ] Add `Runtime.MaxConcurrentAgents int` field, default 15.
- [ ] Init scaffolds:

```yaml
runtime:
  max_concurrent_agents: 15
```

- [ ] Commit `feat(config): add runtime.max_concurrent_agents (default 15)`

### Task 4.2: Spawn handler enforces cap

**Files:**
- Modify: `internal/daemon/agents.go` (or wherever spawn_agent is handled)
- Add test: `internal/daemon/agents_cap_test.go`

- [ ] Count `agent_runs` with lifecycle in `{starting, running, idle}`
  for session. If `>= cap`, return structured error:

```go
return &SpawnError{
    Code: "max_concurrent_agents",
    Message: fmt.Sprintf("cap reached (%d live agents); retire one before spawning", cap),
}
```

- [ ] Unit test: set cap=2, spawn 2, third rejects.
- [ ] Commit `feat(daemon): enforce max_concurrent_agents at spawn time`

### Task 4.3: Init scaffolds field

Covered in 4.1; if init lives in a separate file, split commits.

### Task 4.4: Test — 15 cap

Covered in 4.2.

## Surprises & Discoveries

_(populated during execution)_

## References

- Design doc: `docs/design-docs/2026-04-21-agent-runtime-state-and-mail-acks.md`
- Codex synthesis (prior conversation): root cause = control plane
  does not own execution state, only observes behavior
- `hermes_bridge/__main__.py:452` — `agent.run_conversation()` return
  dict
- `internal/daemon/messages.go:28-114` — mail handlers
- `internal/store/migrations.go` — existing migration pattern
