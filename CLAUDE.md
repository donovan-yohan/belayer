# CLAUDE.md

Belayer v7 — climb-local agent control plane for Nightshift.

## What Belayer is

> The **agent control plane** inside a single Nightshift worker climb.

One worker, one request, one Belayer session. Belayer coordinates supervisor + specialist agents via the Hermes bridge. Crag mode (`docs/CRAG_MODE.md`) layers durable cross-project knowledge on top of the same primitives.

## What Belayer is not

Not a cluster scheduler, autoscaler, hypervisor, or hosted identity service. It manages one climb at a time.

## Architecture

Three layers:

1. **Session bus** — Go daemon + SQLite. Sessions, agent roster, messages, events, artifacts.
2. **Hermes driver** — Bridge subprocess (`python -m hermes_bridge`) wraps Hermes AIAgent. Identity injected via `ephemeral_system_prompt`; belayer_* tools registered by the Hermes 0.12 plugin at `plugins/belayer/` (loaded automatically by Hermes during AIAgent import).
3. **Bridge transport** — Python subprocess managed by Go. Heartbeats, exit detection, event streaming over stdout.

## Agent kinds

Every agent has `kind: main | side` in its `agent.yaml`. The distinction is one axis: does the agent have a mailbox?

- **Main** — long-lived party member. Mailbox, outbox, pre-turn mail poll, broadcasts, peer-to-peer dialogue. Mains talk to each other directly.
- **Side** — short-lived scoped worker. No mailbox. Receives task in spawn message; returns via `final_response` + artifacts. Interrupt is the only mail surface.

One party lead (a main that declares `belayer_spawn_agent` + `belayer_request_completion`, conventionally `supervisor`) spawns teammates. The PM side is auto-spawned by the daemon on `belayer_request_completion`.

## Coordination model

Agents coordinate through the daemon:

- **messages** — direct agent-to-agent via session bus
- **broadcasts** — party-wide to every main (persisted per-recipient)
- **events** — machine-readable state transitions
- **artifacts** — durable outputs registered in the session

## Agent identity

Agent identities resolve through four layers (highest precedence first), per `docs/CRAG_FILESYSTEM.md`:

1. `repo/.belayer/agents/<name>/` — project-local override (`belayer init` scaffolds)
2. `~/.belayer/crags/<crag>/teams/` — linked crag (only when `.belayer/config.yaml` declares one)
3. `~/.belayer/talent-catalog/<category>/<name>/` — local talent supply (`belayer team add` copies from here)
4. `agents/<name>/` — shipped defaults, embedded in binary

The loader at `internal/daemon/agents.go` reads in this order. Each identity directory contains:
- `agent.yaml` — `kind`, vendor, model, ephemeral flag, `belayer_tools` allowlist
- `system-prompt.md` — the agent's soul
- `agents.md` — operating instructions, tools, workflows
- `talent.yaml` — optional `belayer-talent/v1` metadata (role, domain, activation, runtime.lifecycle, contract, authority, memory, retention)

Shipped default team:
- **Mains:** `supervisor` (party lead), `backend-dev`, `web-dev`.
- **Sides:** `pm` (completion gate), `qa`, `reviewer`.

Customize in `.belayer/agents/` — see `agents/README.md` for worked examples. Reusable talent supply lives in `examples/talent-catalog/` (development + story categories) and copies via `belayer team add <category>`.

System prompts are loaded by the daemon at spawn time and injected via Hermes `ephemeral_system_prompt`. All agents use the `default` Hermes profile for now (see profile bootstrap TODO in AGENT_ARCHITECTURE.md).

The `hermes_bridge` Python package lives in the daemon's runtime dir (default `$XDG_STATE_HOME/belayer/runtime`), not inside any workspace. This protects the module from workspace agent cleanup (`rm -rf .belayer/`).

## Acceptance gate (PM is the default)

When the supervisor calls `belayer finish`, the daemon intercepts and auto-spawns the PM side for adversarial spec-vs-reality verification. PM approves or rejects (up to 3 cycles). The PM gate is one preset of the generic `belayer-gate/v1` contract — crags can declare additional task-stage gates (code-review, runtime-qa, continuity, etc.) or replace the acceptance gate with a stricter one. See `docs/CRAG_MODE.md` for the gate contract and `docs/AGENT_ARCHITECTURE.md` for the daemon enforcement path.

## Crag mode

Crags are durable cross-project operating contexts (software company, story world, research group) at `~/.belayer/crags/<name>/`. They store reusable teams, gate presets, evaluations, promotions, and generated talent metadata. A repo opts in by `belayer crag link <name>`, writing the link into `.belayer/config.yaml`. See `docs/CRAG_MODE.md` and `docs/CRAG_FILESYSTEM.md`.

## CLI surface highlights

- `belayer climb start` (alias `run`) — start a climb
- `belayer crag init|list|link` — manage local crags
- `belayer team list|add|remove` — manage talent catalog rosters
- Full surface: `belayer --help` and `belayer <cmd> --help`

Default config schema (including `exit_conditions`) is generated from `internal/cli/init.go`. Agent tool surface (baseline + role-specific) is defined by the Hermes plugin at `plugins/belayer/tools.py` and gated by `plugins/belayer/__init__.py:register()` based on agent kind + `BELAYER_TOOLS` allowlist.

## Docs

- `docs/PHILOSOPHY.md` — the six runtime interfaces (conceptual, not implementation-specific)
- `docs/AGENT_ARCHITECTURE.md` — agent kinds, tools, mail, spawn, PM/acceptance gate, lifecycle modes
- `docs/CRAG_MODE.md` — talent contract, team catalogs, gate contracts, crag events, proof climbs
- `docs/CRAG_FILESYSTEM.md` — repo, talent catalog, and crag directory contracts; resolution precedence
- `docs/ARTIFACT_SCHEMAS.md` — content schemas for `org-plan`, `gate-result`, `talent-evaluation`, `org-retro`, `world-state`, `continuity-report`, `generated-talent`, `talent-request`
- `docs/DEPLOYMENT.md` — deployment topologies, trust model, credentials, ports/sockets
- `docs/LOG_FORMAT.md` — event schema, SSE, archive format, aggregates, HTTP API contract
- `docs/OBSERVABILITY.md` — operator guide: tier selection, recipes, dashboard integration
- `docs/design-docs/` — detailed design documents (see index.md)
- Log tiers: standard / verbose / trace — see `docs/LOG_FORMAT.md`

## Development

```bash
go build ./cmd/belayer
go test ./...
```
