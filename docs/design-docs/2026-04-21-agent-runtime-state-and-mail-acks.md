# Agent runtime state, mail acks, and deterministic handoff

**Date:** 2026-04-21
**Status:** Proposed
**Author:** Donovan + assistant, informed by Codex staff-engineer review of run-codex-2 gaps

## Problem

The run-codex-1 and run-codex-2 retros exposed a cluster of failures that
share a single root cause: the daemon observes agent behavior but does not
own execution state. Specifically:

1. **Gap 10 — iteration budget silent done.** Hermes ships a 90-turn cap
   that is not plumbed from `agent.yaml#agent.max_turns`. When an agent
   hits the cap, Hermes asks the model for a final summary and returns
   `completed=True`. The bridge emits `bridge:finished`, indistinguishable
   from a real completion. The supervisor sees a normal done event and
   assumes the work is finished. No backpressure signal reaches the
   control plane.

2. **Gap 11 — incomplete escalation leaves nothing behind.** When the
   supervisor reports `status=incomplete`, the daemon's persistence hook
   reprompts the supervisor once, then accepts whatever the supervisor
   produces. If the supervisor has already exhausted its own budget, the
   run ends with no artifact, no summary of what was actually attempted,
   and the next operator starts from zero.

3. **Mail delivery has no acknowledgement.** `belayer send` writes a row
   to `messages`, flips `delivered=1` the instant a poll reads it, and
   considers itself done. If the bridge crashes between the poll response
   and the model's turn that consumes the message, the message is gone.
   Urgent messages write directly to bridge stdin, bypassing the DB — a
   bridge restart loses them entirely.

4. **Status is single-axis.** `agent_runs.status` collapses both
   lifecycle (is the subprocess alive?) and outcome (did the work
   succeed?) into one string. The roster can't answer "this agent
   finished, but the supervisor hasn't seen the result yet" vs "this
   agent is still running."

The common shape: control-plane invariants are carried by prompts, not
by mechanism. The supervisor system prompt tells the supervisor to
wait for peers before calling `belayer_request_completion`. The
persistence strategy tells it to open a PR before reporting incomplete.
The review-round cap at N=2 is a sentence. None of these are enforced
by code. When a prompt drifts, the invariant drifts with it.

## Goals

1. Budget exhaustion is an explicit signal the supervisor cannot miss.
2. Mail delivery is three-state (queued → delivered → acked) and
   survives bridge restarts.
3. Status carries lifecycle and outcome independently.
4. Supervisor-incomplete produces a deterministic handoff artifact
   written by the daemon, not the supervisor.
5. Concurrent-agent cap is a daemon config value, not a prompt line.

## Non-goals

- **No AgentIdentity/Activation split this phase.** The Codex review
  proposed durable `agent_identities` + ephemeral `activations` as the
  correct long-term shape. That's a larger refactor; deferred to a
  follow-up design doc. This phase lives in the existing `agent_runs`
  table with additive columns.
- **No automatic retry or redelivery.** The daemon records mail state;
  the supervisor decides when to resend. Same shape as today's ship
  gate.
- **No deterministic coordinator.** The handoff artifact is facts from
  the store (roster snapshot, unacked messages, last events, git state,
  registered artifacts). The supervisor writes narrative if any. The
  daemon writes inventory.

## Plumbing principles preserved

Belayer is the agent control plane for one run: session bus, Hermes
driver, bridge transport. Mechanism lives in the daemon; judgment
lives in the LLM. This design moves enforcement of four invariants
from natural-language discipline into runtime mechanism:

| Invariant today | After this change |
|---|---|
| "Wait for peers before requesting completion" (prompt) | Roster lifecycle/outcome columns make busy-vs-done a query |
| "Open a draft PR before reporting incomplete" (prompt) | Daemon emits handoff artifact on incomplete; supervisor's job is narrative, not inventory |
| "Review rounds capped at two per diff" (prompt) | Out of scope this phase; if codified later, count by identity metadata, not name suffix |
| "Don't spawn more than N agents" (none) | Daemon enforces config `max_concurrent_agents` at `belayer_spawn_agent` time |

Budget exhaustion goes from "looks identical to success" to an
explicit event type the supervisor handles. Mail gains a delivered-vs-
acked distinction so the daemon can tell the supervisor "your
message reached the subprocess but the model hasn't consumed it
yet."

## Design

### Bridge budget exhaustion

`agent.yaml` already carries `agent.max_turns`. Wire it end-to-end:

- `internal/daemon/agents.go` reads `cfg.Agent.MaxTurns`, passes into
  `bridge.Config.MaxTurns`.
- `internal/bridge` forwards via env var `BELAYER_MAX_TURNS`.
- `hermes_bridge/__main__.py` passes to `AIAgent(..., max_turns=...)`.
- Hermes returns `result.budget_exhausted=True` (confirmed present in
  the SDK) when the cap is hit.
- Bridge emits a new event type `bridge:budget_exhausted` with
  `{turns_used, max_turns, last_message}` alongside — not in place of —
  the normal completion path.

Daemon handling: `handleBridgeEvent` treats `bridge:budget_exhausted` as
a terminal event that sets `agent_runs.outcome='budget_exhausted'`
(new column), emits an `agent_status` event with the same outcome, and
sends a structured message to the supervisor: "Agent <name> exhausted
its turn budget (N/N). Its last message is attached. Decide whether to
respawn, retire, or continue without it." No automatic respawn.

### Two-axis status

Add `outcome` column to `agent_runs`. Keep existing `status` as
lifecycle.

Lifecycle values (rename implicit today):
- `starting` — bridge spawned, waiting for `bridge:ready`
- `running` — bridge has an active LLM turn
- `idle` — non-ephemeral bridge in the post-completion poll loop
- `stopping` — retire/shutdown requested, subprocess still alive
- `exited` — subprocess gone

Outcome values:
- `active` — no terminal event yet
- `succeeded` — `bridge:finished` with normal completion
- `blocked` — agent self-reported blocked
- `incomplete` — agent self-reported incomplete
- `failed` — bridge crash, stderr error pattern, or synthesized failure
- `budget_exhausted` — new; turn cap hit

`belayer roster` renders as `<lifecycle>/<outcome>` (e.g.
`idle/succeeded`, `exited/budget_exhausted`,
`running/active`). Destructive-action flag stays as an orthogonal
column.

The SSE `session_digest` frame gains per-agent `outcome` alongside
existing `lifecycle`/`name`/`destructive_actions`. Cost is ~20 bytes
per agent per digest — negligible given the 60s/50-event cadence.
Digest subscribers (dashboards, runners) get the new axis for free
without hitting the roster endpoint.

No heartbeat-based "stalled" or "slow" lifecycle. A bridge that is
taking a long time on a turn is `running/active` — the fact, not a
speculation. The supervisor reads the roster, notices mail has been
queued for N minutes, and decides whether to wait, retire, or send
an urgent nudge. Adding a `stalled` inference would bake policy
into the daemon; keep that in the supervisor's judgment surface.

Migration: additive `outcome` column defaulted to `active`. The
existing `status` column keeps its current string values; we rename
values in the bridge-event handlers only. No client code breaks
because status strings have always been opaque to callers.

### Mail ack states

Replace the single `delivered INTEGER` with explicit timestamps:

```sql
ALTER TABLE messages ADD COLUMN delivered_at DATETIME;
ALTER TABLE messages ADD COLUMN acknowledged_at DATETIME;
```

State derived from columns (no new string field):

- `delivered_at IS NULL` → queued
- `delivered_at NOT NULL AND acknowledged_at IS NULL` → delivered
- `acknowledged_at NOT NULL` → acknowledged

Transitions:
- **queued → delivered**: poll response (`GET
  /sessions/{id}/messages?for=X&pending=true`) returns a message;
  daemon sets `delivered_at=now`.
- **delivered → acknowledged**: implicit. Bridge tracks the
  delivery IDs injected into a turn's input and emits
  `bridge:message_ack {ids: [...]}` at turn-end. No new tool surface
  on the agent side. Rationale: "acknowledged" semantically means
  "the LLM has consumed this message"; coupling the ack to the turn
  that consumed it makes the state meaningful. An explicit
  `belayer_ack_message` tool can be added later if a concrete
  recipient needs finer control; not this phase.
- **Urgent path**: stdin writer sets `delivered_at` only after
  `stdin_reader.py` writes an ack line back over the event channel.
  Urgent messages flip both `delivered_at` and `acknowledged_at` on
  the same ack event — the stdin injection is atomic, so they never
  sit in the delivered-but-unacked state.

Daemon surface:
- `GET /sessions/{id}/messages?for=X&state=unacked` returns queued +
  delivered (for supervisor inspection).
- Roster exposes `pending_mail_count` and `unacked_mail_count` per
  agent so the supervisor can see at a glance.

Reboot survivability: on daemon restart, messages with
`delivered_at NOT NULL AND acknowledged_at IS NULL` are rolled back to
queued (`delivered_at=NULL`) because the bridge process is gone. The
next time the agent polls, it redelivers.

### Concurrent-agent cap

`.belayer/config.yaml` gains:

```yaml
runtime:
  max_concurrent_agents: 15
```

`belayer_spawn_agent` handler rejects with a structured error when the
count of `agent_runs` in lifecycle `{starting, running, idle}` is at
the cap. No queueing, no waiting — supervisor decides what to retire.

Why 15: the retros showed supervisors spawning 20–25 agents during
perfectionism loops. 15 is a soft bound that forces the supervisor to
reason about roster pressure without being so tight it blocks normal
multi-agent work.

### Deterministic handoff artifact

When the supervisor calls `belayer_report_status incomplete` and the
persistence policy is not satisfied, the daemon writes
`.belayer/runs/<sid>/handoff.md` containing:

- Session metadata (ID, task, exit conditions, persistence strategy)
- Roster snapshot: every agent with lifecycle, outcome, last event
  timestamp, registered artifact IDs, branch+worktree path
- Unacked messages: sender, recipient, age, content preview
- Git state per worktree: branch, `ahead`, `behind`, dirty, last
  commit
- Registered artifacts: kind, path, producer, summary
- Last 20 events from the session event log

This artifact is registered with kind `handoff` and path to the
written file. The supervisor can still write a narrative note
alongside — the daemon artifact is inventory, not commentary.

Emission point: in the same hook that today reprompts the supervisor
once on incomplete, before the reprompt fires. If the supervisor
accepts the reprompt and successfully satisfies persistence, the
handoff artifact is still there (it never hurts; the next operator
has a snapshot either way).

Non-goal clarification: the handoff artifact does not commit or push.
Persistence strategy (draft PR, push, etc.) remains the supervisor's
action. The daemon just guarantees the next operator has a factual
starting point.

## Rollout

Five phases, ordered by independence and risk:

1. **`max_turns` wiring + `bridge:budget_exhausted` + `outcome`
   column.** Smallest slice. No schema churn beyond one additive
   column. Unblocks supervisor ability to respond to budget hits.
2. **Mail ack states.** Adds two columns, changes one index, touches
   the message handlers and bridge event path. Independent from
   phase 1.
3. **Deterministic handoff artifact.** Pure daemon write path; no
   agent-facing changes. Depends on phase 1 for accurate outcome
   column.
4. **Concurrent-agent cap.** Config + spawn-handler guard. Tiny.
   Independent.
5. **Identity/Activation split.** Deferred. Own design doc. Not this
   plan.

## Resolved decisions (previously open)

These were flagged as open questions in draft review and resolved
before phase 1 starts. Captured here so the rationale is not lost:

1. **Ack by ID vs ack by turn** → implicit via `bridge:message_ack`
   at turn-end. No new `belayer_ack_message` tool this phase. Turn-
   end is the semantic meaning of "LLM consumed the message."
   Explicit tool can be added later if a recipient needs finer
   control.
2. **`outcome` in SSE roster frames** → yes. `session_digest` carries
   per-agent `outcome` alongside existing fields. Negligible byte
   cost at current cadence.
3. **Slow vs stalled running agent** → no inference. Lifecycle
   `running` + outcome `active` is the fact. Supervisor decides from
   observed mail age + event timestamps. Adding a `stalled`
   lifecycle would bake policy into the daemon.

## References

- run-codex-1 gaps doc (`work/belayer-clamshell-demo/retro/runs/run-codex-1/gaps.md`)
- run-codex-2 gaps doc (`work/belayer-clamshell-demo/retro/runs/run-codex-2/gaps.md`)
- `hermes_bridge/__main__.py:520-686` — post-completion branch
- `internal/daemon/messages.go:28-114` — send/broadcast handlers
- `internal/store/store.go:35-51` — `AgentRun` struct
