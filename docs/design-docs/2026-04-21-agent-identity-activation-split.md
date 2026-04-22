# Agent identity / activation split

**Date:** 2026-04-21
**Status:** Proposed — deferred until 2026-04-21 runtime-state plan lands
**Author:** Donovan + assistant, informed by Codex staff-engineer review of run-codex-2 gaps

> **Read first:**
> `docs/design-docs/2026-04-21-agent-runtime-state-and-mail-acks.md` lands
> the minimum viable fixes (budget signalling, mail acks, two-axis
> status, handoff artifact, spawn cap) inside the current `agent_runs`
> table. This doc describes the refactor that cleanly separates *who
> the agent is* from *the subprocess running it right now*. Wait until
> the runtime-state plan is shipped and observed in a real clamshell
> run before starting this one.

## Problem

Today the `agent_runs` table conflates two lifetimes:

1. **Identity lifetime** — the supervisor's mental model of "there is
   a `web-dev-1` working on the checkout branch." Lives as long as
   the supervisor still cares about that peer. Durable. Sends and
   receives mail under this name. Shows up in the roster. Holds
   registered artifacts.
2. **Activation lifetime** — a specific `hermes_bridge` subprocess,
   attached to a specific `AIAgent` instance, with a specific
   `max_turns` budget and a specific event stream. Starts at spawn,
   ends when the bridge exits (finished, failed, budget exhausted,
   retired).

`agent_runs` treats them as the same row. This is why:

- **Budget exhaustion reads as termination.** When Hermes hits the cap
  and returns, we end up with a dead row even though the supervisor's
  mental `web-dev-1` should persist. The runtime-state plan works
  around this by emitting a separate event and marking `outcome`; the
  row still ends its useful life.
- **Respawning is painful.** To resume an identity after a crash, the
  supervisor calls `belayer_spawn_agent` again and gets a *new*
  `agent_runs` row with a *new* subprocess. The two rows have no
  explicit relationship. Continuity is carried by the supervisor's
  prompt ("remember, `web-dev-1` is now in this new row").
- **Mail targets the subprocess.** `messages.recipient_id` points at
  the `agent_runs.name` that happens to exist. A crashed and
  restarted identity loses the queue because the new row has a new
  primary key, and the send-to-exited-agent path returns 410 Gone.
- **Crash recovery is narrative.** The daemon can't say "this
  identity has been active for 3 activations, the last one exhausted
  budget after 90 turns." It just sees "a dead row" and "a new row."
  Operators reconstruct the story from events.
- **Roster pressure calculations are wrong.** The 15-agent cap (from
  the runtime-state plan) is intended to bound *concurrent
  identities*, not concurrent subprocesses. With conflated rows, a
  respawn of the same identity counts as two toward the cap for the
  brief window both rows are `running`.

Codex's framing: **the control plane currently observes the bridge
subprocess and infers identity from its name. The correct shape
inverts that — the control plane owns identity, and a subprocess is a
lease on a compute slot for that identity.**

## Goals

1. Identity persists across respawn. `web-dev-1` is one row from spawn
   to retire, regardless of how many bridges attach and detach.
2. Activations are first-class. Each bridge subprocess gets its own
   row with its own lifetime, its own budget, its own event log.
3. Mail targets identity. Redelivery on respawn is automatic because
   the queue is bound to identity, not activation.
4. Roster reads naturally. "Current activation of <identity> is in
   lifecycle X, outcome Y" becomes a join.
5. Concurrent-agent cap counts identities, not activations.

## Non-goals

- **No auto-respawn policy.** A terminated activation does not
  automatically get a new one. The supervisor decides whether to
  reactivate. The daemon is plumbing; scheduling is the supervisor's
  job.
- **No cross-session identity.** Identities live inside a single
  session. Nothing about this design enables shared long-lived agents
  across runs.
- **No activation queue.** If the cap is hit, spawn rejects. No
  queueing, no waiting. Matches the existing rejection shape.
- **No policy field that grows into workflow config.** Identity-level
  policy is narrow to activation parameters (default `max_turns`,
  default idle timeout). It never holds "how should this agent
  behave" — that's the system prompt's job.

## Plumbing alignment

This refactor moves enforcement deeper into mechanism:

| Today | After split |
|---|---|
| Identity is implicit in the mutable state of an `agent_runs` row | Identity is its own durable row; activations are leases against it |
| Respawn requires prompt-level bookkeeping ("now `web-dev-1` is at `run_id_B`") | Respawn is a new activation row; all pointers still target the identity |
| Mail delivery can land on a stale row after crash | Mail queue is bound to identity; any live activation drains it |
| "15 concurrent agents" is a count of rows in mixed lifecycle states | "15 concurrent identities" is a count of rows with at least one live activation |

The daemon still does not make policy choices. It surfaces facts:
identity X has no live activation; identity Y's current activation
has outcome budget_exhausted; identity Z is idle with pending mail.
The supervisor reads these facts and acts.

## Design

### Schema

Two new tables; drop `agent_runs` in a follow-up migration after a
deprecation window.

```sql
CREATE TABLE agent_identities (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES sessions(id),
    name            TEXT NOT NULL,
    role            TEXT NOT NULL DEFAULT '',
    profile         TEXT NOT NULL DEFAULT '',
    repo_scope      TEXT NOT NULL DEFAULT '',
    branch          TEXT NOT NULL DEFAULT '',
    worktree_path   TEXT NOT NULL DEFAULT '',
    activation_policy TEXT NOT NULL DEFAULT '{}',  -- JSON; see below
    retired_at      DATETIME,
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    UNIQUE(session_id, name)
);

CREATE TABLE activations (
    id              TEXT PRIMARY KEY,
    identity_id     TEXT NOT NULL REFERENCES agent_identities(id),
    session_id      TEXT NOT NULL REFERENCES sessions(id),
    bridge_pid      INTEGER,
    hermes_session_id TEXT NOT NULL DEFAULT '',
    transport       TEXT NOT NULL DEFAULT 'bridge',
    lifecycle       TEXT NOT NULL DEFAULT 'starting',
    outcome         TEXT NOT NULL DEFAULT 'active',
    turns_used      INTEGER NOT NULL DEFAULT 0,
    max_turns       INTEGER,
    started_at      DATETIME NOT NULL,
    ended_at        DATETIME,
    destructive_actions     INTEGER NOT NULL DEFAULT 0,
    last_destructive_cmd    TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_activations_live
  ON activations(session_id, identity_id, ended_at);
```

`agent_identities.activation_policy` (JSON) holds only knobs that
configure how an activation is spawned:

```json
{
  "max_turns": 90,
  "idle_timeout_seconds": 900,
  "absolute_timeout_seconds": 3600
}
```

No prompt content. No workflow. No review-round counts. If we want
per-identity defaults beyond these three, add them explicitly with a
review — the field is narrow by construction.

### Messages reference identity

```sql
ALTER TABLE messages ADD COLUMN recipient_identity_id TEXT
  REFERENCES agent_identities(id);
-- keep recipient_id (agent name) for backcompat; new code reads the FK
```

Mail delivery joins `messages.recipient_identity_id` against the
current live activation of that identity. If no activation is live
(subprocess crashed, not yet respawned), the message stays queued and
survives across respawn. When the supervisor re-activates the
identity, the new bridge polls and drains the queue.

Three-state delivery (queued → delivered → acked) from the runtime-
state plan carries over unchanged; it lives on the message row, not
the activation.

### Roster as a join

```sql
SELECT
  i.id, i.name, i.role, i.worktree_path,
  a.id AS activation_id, a.lifecycle, a.outcome,
  a.turns_used, a.max_turns,
  (SELECT COUNT(*) FROM messages m
     WHERE m.session_id = i.session_id
       AND m.recipient_identity_id = i.id
       AND m.acknowledged_at IS NULL) AS unacked_mail
FROM agent_identities i
LEFT JOIN activations a
  ON a.identity_id = i.id AND a.ended_at IS NULL
WHERE i.session_id = ? AND i.retired_at IS NULL;
```

Rendered identity row shapes:

- Identity with live activation: `web-dev-1  running/active   15/90  0 mail`
- Identity with dead last activation (not retired): `web-dev-1  no-activation/budget_exhausted  90/90  2 mail`
- Retired identity: not shown (filter by `retired_at IS NULL`).

### Tools

- `belayer_spawn_agent(name, identity_kind, branch, ...)` — creates
  identity if new, creates activation. Rejects if `max_concurrent_identities`
  cap is hit.
- `belayer_reactivate_agent(name)` — creates a new activation for an
  existing identity. Rejects if an activation is already live. No
  new identity row.
- `belayer_retire_agent(name)` — marks identity `retired_at=now`; if
  an activation is live, signals lifecycle `stopping` and waits on
  bridge exit. Frees the concurrency slot.
- `belayer_ack_message(ids)` — unchanged shape; operates on identity-
  bound queue.

Naming note: today's `belayer_spawn_agent` does both "create identity"
and "start subprocess" implicitly. Keep that behavior for the common
case. `reactivate` is the narrow second tool for the respawn path.

### Cap on concurrent identities

`runtime.max_concurrent_agents` (from the runtime-state plan) becomes
`max_concurrent_identities`. Count is
`SELECT COUNT(*) FROM agent_identities WHERE session_id = ? AND retired_at IS NULL`.
A crashed-and-awaiting-respawn identity still occupies a slot — this
is intentional. The supervisor must explicitly retire it to free the
slot. Prevents the "crash and spawn a replacement under a new name"
pattern from blowing past the cap.

### Events

New event types:

- `identity:created {identity_id, name, branch, ...}`
- `identity:retired {identity_id, name}`
- `activation:started {activation_id, identity_id, bridge_pid}`
- `activation:ended {activation_id, identity_id, lifecycle, outcome, turns_used}`

Existing `agent_status` event gains `identity_id` and `activation_id`
alongside `agent_name` so downstream consumers can tell respawns
apart.

### Handoff artifact uses identity model

The handoff artifact from the runtime-state plan now renders a
per-identity history: for each identity, show the activation log
(ended activations with their outcomes, plus the current live one if
any). A supervisor that respawned an identity three times leaves
behind a story, not just a row.

### Migration path

1. **Ship runtime-state plan first.** That plan adds `outcome`,
   mail-ack columns, handoff artifact, cap — all inside `agent_runs`.
2. **Observe in real runs** for at least two clamshell sessions.
   Confirm two-axis status and mail acks behave as intended.
3. **Dual-write phase.** New tables created. Spawn handler writes
   both `agent_runs` (legacy) and `agent_identities` + `activations`.
   Read paths still use `agent_runs`.
4. **Read-switch phase.** Roster and mail handlers switch to the new
   tables. `agent_runs` becomes a trailing mirror.
5. **Drop legacy.** After one more release, delete `agent_runs` and
   the dual-write code.

The dual-write phase is where bugs hide; each write path needs a
unit test asserting the two representations agree.

## Open questions

- **Does a retired identity's mail queue flush or persist?** Default:
  persist for audit, do not redeliver. The handoff artifact surfaces
  it as "retired with N unacked messages." Supervisor can choose to
  reactivate-and-drain before retiring if it matters.
- **Should `reactivate` inherit the prior activation's
  `hermes_session_id`?** Probably yes — Hermes supports session
  resumption if the same id is reused. Confirm by reading
  `hermes/agent.py` before implementing.
- **What happens to registered artifacts when an identity is
  retired?** Artifacts stay. They're session-scoped, not identity-
  scoped. A retired identity's artifacts remain discoverable.
- **Can two identities share a worktree?** Today the daemon creates
  one worktree per `belayer_spawn_agent` call. With identity
  persistence, the worktree follows the identity for its whole life.
  Cross-identity sharing is explicitly not supported.

## References

- Runtime-state plan: `docs/design-docs/2026-04-21-agent-runtime-state-and-mail-acks.md`
- Codex staff-engineer synthesis (prior conversation): "control plane
  does not own execution state; it only observes agent behavior after
  the fact"
- `internal/store/store.go:35-51` — current `AgentRun` struct
- `internal/daemon/messages.go:28-114` — mail handlers that will
  switch to identity FK
