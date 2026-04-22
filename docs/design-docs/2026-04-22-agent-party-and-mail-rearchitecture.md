# Agent party and mail rearchitecture

**Date:** 2026-04-22
**Status:** Proposed
**Author:** Donovan + assistant, informed by empirical findings from run-codex-3 (web/backend/integrator/review MVP) and the scion-athenaeum multi-agent coordination model

> **Relation to prior docs:**
> This doc supersedes the mail-ack phases of
> `2026-04-21-agent-runtime-state-and-mail-acks.md`. The budget-
> exhaustion, two-axis status, deterministic handoff, and concurrent-
> cap phases from that doc are preserved (some rescoped). The
> identity/activation split
> (`2026-04-21-agent-identity-activation-split.md`) remains deferred
> and layers cleanly on top of the tiering introduced here.

## Problem

The run-codex-3 clamshell demo exposed that belayer's mail model is
built for a coordination pattern it doesn't actually use.

Observed in the run:

- **The supervisor read worker output via `final_response`** returned
  from `belayer_spawn_agent`, never via its inbox.
- **Six worker→supervisor messages were dead letters.** `delivered=0`,
  `acknowledged_at=NULL` at end of run. All 6 were status updates
  (final SHA, heartbeat, "integration done", "review verdict: FAIL")
  that the supervisor either already had from `final_response` or did
  not need.
- **Twelve supervisor→worker messages were queued for 20+ minutes.**
  Workers poll their inbox only at turn boundaries — specifically in
  the post-completion branch of `hermes_bridge/__main__.py`. Mid-turn
  non-urgent directives are effectively no-ops.
- **All 13 supervisor→worker messages were `urgent=0`.** The stdin-
  interrupt path exists and demonstrably works (operator→supervisor
  boot delivered + acked in 7s), but the supervisor never used it.
- **Broadcast was absent from the run.** Not because the feature was
  uninteresting — because it has no per-recipient persistence
  (`internal/daemon/messages.go:152` TODO) and cannot survive a
  subscriber joining late.
- **Agents had no peer-to-peer coordination.** backend-mvp and
  web-mvp needed to agree on the `/mcp` schema; they did not talk to
  each other. Coordination happened out-of-band in the supervisor's
  prompt.

Root cause: belayer's current mail surface was designed around a
supervisor-plus-ephemeral-workers topology with spawn-return as the
dominant coordination primitive. That topology doesn't use mail for
worker coordination, and the mail schema is costly to reason about
for something that is effectively unused. It also forecloses the
multi-agent peer coordination model (scion's "party with a game
master") that belayer's runbook actually wants to support.

## Goals

1. A single structural axis — *does the agent have a mailbox or not?*
   — that determines tooling, lifecycle, and communication surface.
2. Multi-main peer coordination works: `backend-dev` and `web-dev`
   can message each other directly, without the supervisor as a
   routing hop.
3. Non-urgent mail delivery is bounded by one turn, not one
   completion, so peer Q&A is responsive enough to be useful.
4. Broadcast persists per-recipient so a main joining late does not
   miss prior party-wide announcements.
5. Side agents (short-lived, scoped workers) have zero mail surface
   — no inbox, no outbox, no ack state. "Message to a side" is a
   400 at the daemon, by construction.
6. Game-master role (spawn, summon, finale) is encoded as a prompt
   disposition plus a small tool-set delta, not a new kind.

## Non-goals

- **No routing layer.** Peer-to-peer and operator-to-any-main are
  mechanically open. Conventions for "talk to the game-master first"
  live in prompts, not in daemon code.
- **No deterministic coordinator.** Party-wide sequencing (gathering,
  acts, finale) stays with the game-master LLM. Daemon surfaces
  roster, events, artifacts, and broadcast persistence; it does not
  decide who is done or who is next.
- **No mid-turn pre-emption of tool calls.** "Urgent" still means
  stdin injection consumed by the next model turn. It does not kill
  a running tool chain.
- **No retry / redelivery policy.** Mail state is exposed; mains
  decide when to resend. Same shape as today's persistence gate.
- **No multi-main for sides.** Sides are spawn-and-exit. A "party of
  sides" is a contradiction — that is a party of mains.

## The model

### Two tiers

Every agent has `kind: main | side`. There is one axis of
difference: whether the agent has a mailbox.

**Main.** Long-lived party member. Has inbox and outbox. Polls mail
before each turn. Accepts broadcasts. Accepts mid-flight urgent
injections via stdin. Participates in the mail-ack state machine.
Examples: game-master, backend-dev, web-dev, integrator, reviewer.

**Side.** Short-lived worker with a single scoped task. No inbox, no
outbox. Receives task in the initial spawn message. Produces output
via `final_response` + registered artifacts. Exits on completion.
Examples: PM gate, Oracle-style summons, bulk computation workers.

The table is meant to be read as two dispositions of the same
organism, not as two unrelated types. Same `agent.yaml` schema, same
bridge process shape, same event stream. The delta is tooling and
prompt.

### Game-master is a role

The scion-athenaeum model has a game-runner that spawns the party,
announces challenges, and summons peripherals on request. This is
**a disposition within the main tier**, not a third kind. The
game-master's uniqueness is:

- **Tools:** gets `belayer_spawn_main`, `belayer_summon_side`,
  `belayer_finish`. Peer mains do not.
- **Prompt:** routing authority (operator directives arrive here
  first), narrative broadcasting, side-summon budget management,
  finale decision.
- **Everything else:** identical to peer mains. Same mailbox, same
  ack semantics, same runtime state.

Any session has exactly one game-master. Peer mains cannot spawn
other mains, cannot summon sides, cannot call finish. This prevents
runaway spawning without inventing a third tier.

### Addressing is flat

The daemon does not gate `to:` addressing on sender identity.

- `main → peer main`: direct mail, allowed.
- `main → game-master`: direct mail, allowed. Used for privileged
  asks ("summon Oracle," "advance phase," "we're done").
- `main → broadcast`: allowed for any main, including peers. The
  "only GM broadcasts" convention lives in the peer-main prompt, not
  in code.
- `operator → any main`: allowed. Convention says operator normally
  addresses game-master and lets the GM fan out; mechanism does not
  enforce.
- `any → side`: rejected unless `urgent=1` (interrupt). Sides have
  no inbox to receive non-urgent mail into.
- `side → anywhere`: rejected. Sides have no outbox. Status goes via
  events, artifacts, and `final_response`.

"Talk to GM for X, peer for Y" is a runbook concern, enforced by
disposition prompts on each main. Scion does this and it works.

## Design

### `agent.kind` schema

```yaml
# agents/<name>/agent.yaml or .belayer/agents/<name>/agent.yaml
agent:
  name: backend-dev
  kind: main            # default: main (back-compat with existing agents)
  vendor: anthropic
  model: claude-opus-4-6
  max_turns: 80
  max_duration: "2h"
```

Additive field. Existing agent.yaml files without `kind` default to
`main`, preserving today's behavior.

Daemon reads at spawn time, passes into bridge via
`BELAYER_AGENT_KIND`. Bridge selects tool registry based on it.

### Tool registry per kind

`hermes_bridge/tools.py` registers tools per kind:

| Tool | main (peer) | main (game-master) | side |
|---|---|---|---|
| `belayer_send` | ✓ | ✓ | ✗ |
| `belayer_broadcast` | ✓ (rate-limited, convention) | ✓ | ✗ |
| `belayer_check_mail` | ✓ | ✓ | ✗ |
| `belayer_spawn_main` | ✗ | ✓ | ✗ |
| `belayer_summon_side` | ✗ | ✓ | ✗ |
| `belayer_finish` | ✗ | ✓ | ✗ |
| `belayer_report_status` | ✓ | ✓ | ✓ |
| `belayer_register_artifact` | ✓ | ✓ | ✓ |
| file/shell/edit tools | ✓ | ✓ | ✓ |

Game-master status is identified via a second env var
`BELAYER_GAME_MASTER=1` or equivalent metadata on the `agent_runs`
row. One per session.

### Side communication surface

Sides communicate only via:

1. **In:** initial spawn message (daemon writes to bridge stdin at
   spawn; bridge treats as the first user turn input).
2. **In (mid-flight only):** `belayer_send --to <side> --interrupt`
   writes to the side's stdin via the existing interrupt path. Acked
   by `stdin_reader.py` as today. This is the *only* non-trivial mail
   surface a side has, and it is interrupt-only.
3. **Out:** `final_response` at turn-end (already captured by
   daemon).
4. **Out:** `belayer_register_artifact` calls mid-run or at end.
   Artifact registration emits an event; mains observe via event
   stream or roster, not mail.

There is no "drain window" for sides. Nothing to drain. A side's
terminal event is its exit; the spawner consumes `final_response`
and any registered artifacts synchronously with the spawn.

### Main communication surface

Mains communicate via:

1. **`belayer_send --to <name>`** — direct mail to any main (peer or
   GM). Non-urgent default. Adds a row to `messages` with recipient
   set.
2. **`belayer_send --to <name> --interrupt`** — urgent, writes to
   recipient's bridge stdin immediately. Recipient's next model turn
   sees the message at the top of input. Acked via the existing
   `stdin_reader.py` path.
3. **`belayer_broadcast`** — party-wide to all `kind=main`, excluding
   sender. Fans out as one `messages` row per subscribed main
   (persistence fix below).
4. **`belayer_check_mail`** — optional explicit poll. Normally unused
   because the pre-turn poll (below) runs automatically; exposed for
   mains that want to pull mail mid-narrative between tool calls.

### Pre-turn mail poll

Current behavior: `hermes_bridge/__main__.py` polls pending mail only
in the post-completion branch (around lines 520–686). In a mid-run
turn, mail queued during the turn sits until the agent calls
`belayer_request_completion`.

New behavior: the bridge polls `GET /sessions/<id>/messages?for=<name>
&pending=true` as part of **every** pre-turn input assembly. If the
poll returns messages, they are injected into the model input
alongside any tool results, formatted with the existing
`---BEGIN/END BELAYER MESSAGE---` delimiters, and acked at turn-end
via the existing `bridge:message_ack` path.

Consequences:
- Non-urgent mail delivery bounded by one turn, not one completion.
- For peer Q&A (e.g. "what schema does /mcp return?"), worst-case
  latency = one turn of the recipient.
- Urgent still exists and still bypasses to stdin, but the semantic
  narrows: "surface this at the top of the next turn, before the
  model considers its next tool call." Not "arrives faster."

Rollback survivability (from 2026-04-21) stays the same: messages
with `delivered_at NOT NULL AND acknowledged_at IS NULL` revert to
queued when the daemon restarts.

### Broadcast persistence

Today: `broker.Broadcast` fans out in-process via the subscriber
list. Agents that were not subscribed at send time miss the message.
The `messages` table stores nothing for broadcasts
(`messages.go:152` TODO).

New behavior: `handleBroadcastMessage` writes one `messages` row per
`kind=main` agent in the session (excluding sender) with
`recipient_id` set to that main's name. Bridge mains pull these via
the same pre-turn poll as direct mail. Ack state machine applies
per-recipient.

Cost: O(N_mains) rows per broadcast. Mains are capped at
`max_concurrent_mains`, so this is bounded.

Sides are excluded from broadcast fan-out. Broadcasting to sides
would be a category error — they have no mail surface to receive it
on.

### Spawn return semantics

Today: `belayer_spawn_agent` tool returns synchronously on
completion, surfacing `final_response`. This models "spawn a
short-lived worker and await result." It conflicts with long-lived
peer mains: the GM cannot block on `spawn_main(backend-dev)` for
two hours.

New behavior:

- **`belayer_spawn_main`** (GM only) returns immediately with
  `{agent_id, name, status: starting}` once the bridge has emitted
  `bridge:ready`. No `final_response`. Status propagates via the
  session event stream, roster, and the agent's own broadcasts /
  mail.
- **`belayer_summon_side`** (GM only) returns synchronously with
  `final_response` + registered artifact IDs, preserving today's
  spawn-and-await semantics for short-lived workers.

This split makes spawn-and-await a property of the callee's kind,
not a property of the tool. The GM reads agent lifecycle from
roster/events for mains; for sides, the tool return carries the
outcome.

### Game-master disposition

Prompt-level, in `.belayer/agents/game-master/system-prompt.md` and
`agents.md`. Key elements:

- Operator directives arrive here first; GM fans out to peers via
  direct mail or broadcast.
- Party is assembled at session start via `belayer_spawn_main` for
  each party role defined in the template.
- Challenges announced via broadcast (scion's "scene-setting" idiom).
- Peer requests for side help route through GM (direct mail to
  game-master) so GM can track summon budgets.
- Finale: GM calls `belayer_finish` when the party is done.

Peer mains get a different prompt: specialist disposition + the
three-way addressing convention ("direct peers for skill asks,
direct game-master for privileged asks, broadcast for party-wide
news").

### Session lifecycle with peer mains

With peer mains long-lived, session completion is explicit:

- A main's own `bridge:finished` puts it in `lifecycle: idle` (per
  2026-04-21), not `exited`. Peers stay available for Q&A.
- Only `belayer_finish` (GM) or operator-initiated shutdown
  transitions mains to `lifecycle: stopping → exited`.
- `belayer_finish` drains mail per-main before shutting bridges.
  Each main's pre-turn poll runs once more with all pending mail
  delivered; recipients ack what they consumed; then bridges exit.

Budget caps per 2026-04-21 apply per-main. A main that hits
`max_turns` emits `bridge:budget_exhausted` and transitions to
`exited/budget_exhausted`; GM decides whether to respawn.

### Concurrent caps

`.belayer/config.yaml`:

```yaml
runtime:
  max_concurrent_mains: 8
  max_concurrent_sides: 15
  max_side_summons_per_session: 30  # Oracle/Healer-style budget
```

Rationales:
- 8 mains: above the scion-athenaeum party of 6 (five characters +
  GM + Scribe), leaves headroom.
- 15 sides: matches 2026-04-21's original cap; concurrent short-
  lived workers.
- 30 summons: lifetime cap on side spawns per session, so a broken
  GM cannot thrash summon a thousand Oracles.

Spawn handlers reject with structured errors at cap. No queueing.

## Relation to 2026-04-21 runtime-state doc

The 2026-04-21 doc proposed five phases. Under this rearchitecture:

| 2026-04-21 phase | Fate under this doc |
|---|---|
| 1. `max_turns` wiring + `bridge:budget_exhausted` + `outcome` column | Preserved unchanged. Applies to all kinds. |
| 2. Mail ack states | Preserved; scoped to `kind=main` only. Broadcast persistence added. Pre-turn poll added. |
| 3. Deterministic handoff artifact | Preserved unchanged. GM calls `belayer_report_status incomplete`; daemon writes `.belayer/runs/<sid>/handoff.md`. |
| 4. Concurrent-agent cap | Replaced by split cap (`mains`/`sides`/`summons_per_session`). |
| 5. Identity/activation split | Still deferred. Layers on top: identity is shared across activations; kind is a property of identity. |

New phases introduced here:

- **0. `agent.kind` + tool-registry split.** Additive; default
  `main`; back-compat preserved.
- **2a. Pre-turn mail poll in bridge.** Behavioral change in
  `hermes_bridge/__main__.py`; unblocks peer coordination.
- **2b. Broadcast persistence (per-recipient row).** Closes the
  `messages.go:152` TODO.
- **5. Spawn-return semantics split** (`spawn_main` async,
  `summon_side` sync).
- **6. Game-master disposition + party template.** Prompts and
  config; includes a default `.belayer/agents/game-master/` ship.

## Rollout

Ordered by independence and risk:

1. **`agent.kind` field + tool registry split.** Additive. No runtime
   behavior change for existing agents (all default `main`). Strips
   mail tools from any explicitly-tagged `kind=side` agent. PM gate
   template flipped to `kind=side` as the first live side.
2. **2026-04-21 phase 1 unchanged.** `max_turns` wiring +
   `bridge:budget_exhausted` + `outcome` column.
3. **Pre-turn mail poll.** Move pending-mail fetch from post-
   completion into per-turn input assembly. Independent of (4);
   unblocks peer coordination even without broadcast fix.
4. **Broadcast persistence (per-recipient rows).** Closes
   `messages.go:152`. Required for multi-main.
5. **Mail-ack state machine for mains.** Scoped to `kind=main`. Uses
   delivered_at / acknowledged_at columns from 2026-04-21.
6. **Spawn-return split.** `belayer_spawn_main` (async) +
   `belayer_summon_side` (sync). Today's single `belayer_spawn_agent`
   stays as a deprecated alias that dispatches on the spawned kind.
7. **Game-master disposition + party template.** Ship
   `.belayer/agents/game-master/`; update arielcharts template to
   spawn a GM that spawns the four peer mains. First full multi-
   main clamshell run.
8. **Deterministic handoff artifact.** 2026-04-21 phase 3 unchanged.
9. **Concurrent caps (split).** Lightweight; last because it depends
   on knowing `kind` at spawn time.

Phases 1–5 are the critical path. Phase 6 is the ergonomic cleanup.
Phase 7 is the first run that exercises the full model.

## Resolved decisions

1. **Do mains go through GM for peer talk?** No. Addressing is flat.
   Peer-to-peer direct mail is mechanically allowed; convention
   lives in peer-main prompts. Matches scion exactly.
2. **Is game-master a kind or a role?** Role. Same `kind=main`; tool
   registry delta + prompt. One GM per session.
3. **Can operator address non-GM mains?** Mechanically yes. Runbook
   convention says operator→GM; mechanism is open.
4. **Sides with outbox?** No. Side → mail is 400. Status via events
   + artifacts + `final_response` only. Eliminates the 6 dead-letter
   class observed in run-codex-3.
5. **Sides with interrupt inbox?** Yes. `--interrupt` is the only
   mail surface a side has, and it is stdin-inject only — no ack
   state machine, no queued state. Lets GM pre-empt a side mid-work
   if scope changes.
6. **Pre-turn poll vs post-completion poll?** Pre-turn. Post-
   completion poll stays (it drains mail before `exited`), but the
   pre-turn poll is the primary delivery mechanism.
7. **Broadcast fan-out shape?** One `messages` row per subscribed
   main per broadcast. Scales with `max_concurrent_mains`; bounded.
8. **Spawn blocking semantics?** Split by callee kind:
   `spawn_main` async, `summon_side` sync. Not a per-tool flag.

## Open questions

1. **Multi-GM?** Explicitly not supported v1. Could imagine a
   co-GM pattern for very long runs, but no use case yet. Defer.
2. **Peer spawning peers.** Allowed only for sides? Disallowed
   entirely? Recommend disallow v1; peer-mains summon sides via GM
   direct mail ("Game master, please summon a PM side to review
   PR#42"). GM decides and holds the budget.
3. **Side→side messaging.** No use case. Disallowed by construction
   (neither has a mail surface).
4. **Broadcast backpressure.** If a main has unacked broadcasts
   piling up, do we throttle the sender? v1: no; expose
   `unacked_mail_count` in roster (2026-04-21) and let GM decide.
5. **Operator-side broadcast.** Can operator broadcast to the whole
   party from CLI? Trivially yes via the existing broadcast
   endpoint. Convention: operator normally talks to GM.
6. **`final_response` for mains.** Mains don't have a single final
   response — they keep living. What does a main's transcript look
   like to the operator? Answer: the event stream, not a tool
   return. Operator subscribes to SSE; no per-main "final" artifact
   unless explicitly registered.
7. **Turn-budget inflation for mains.** Peer coordination burns
   turns. If `max_turns=50` and a main takes 5 peer Q&A detours,
   effective productive turns = 45. Does the party template need
   a larger default for mains than for sides? Probably yes:
   mains=80, sides=20 as a starting point.

## References

- Empirical findings: run-codex-3 / arielcharts clamshell demo
  messages table (2026-04-22).
- `hermes_bridge/__main__.py` post-completion branch (520–686) —
  current poll-only-at-completion behavior.
- `internal/daemon/messages.go:152` — broadcast persistence TODO.
- `hermes_bridge/stdin_reader.py` — interrupt path / stdin ack.
- `hermes_bridge/tools.py` — tool registration per spawn.
- `docs/design-docs/2026-04-21-agent-runtime-state-and-mail-acks.md`
  — runtime state, mail-ack state machine, handoff artifact.
- `docs/design-docs/2026-04-21-agent-identity-activation-split.md`
  — deferred identity/activation refactor; layers on top of the
  tiering introduced here.
- scion-athenaeum (`~/Documents/Programs/personal/scion-athenaeum`)
  — `.design/game-mechanics.md` and template
  `scion-agent.yaml`/`agents.md` files for the main / peripheral /
  sprite communication and spawn patterns.
