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

The daemon loader at `internal/daemon/agents.go:agentIdentityPaths` resolves identities through two layers today:

1. `repo/.belayer/agents/<name>/<file>` — project-local override, with walk-up so nested cwds (worktrees, climb workspaces) still see the project root copy
2. `<belayerRoot>/agents/<name>/<file>` — shipped defaults, embedded in binary via `embed.go`

The 4-tier precedence (repo → linked crag → talent-catalog → shipped) described in `docs/CRAG_FILESYSTEM.md` is the design contract; the crag and catalog tiers are not yet wired into the spawn-time loader. `belayer team add` copies catalog identities into `repo/.belayer/agents/` so they reach the daemon through tier 1.

Each identity directory contains:
- `agent.yaml` — `kind`, vendor, model, ephemeral flag, `belayer_tools` allowlist
- `system-prompt.md` — the agent's soul
- `agents.md` — operating instructions, tools, workflows
- `talent.yaml` — optional. Two distinct schemas: `belayer-talent/v1` for catalog talents (role, domain, activation, runtime.lifecycle, contract, authority, memory, retention) per `docs/CRAG_MODE.md`, and `belayer-generated-talent/v1` for runtime-scaffolded generated talents (id, domain, role, lifecycle, status, source_request, reason) per `internal/generatedtalent/`.

Shipped default team:
- **Mains:** `supervisor` (party lead), `backend-dev`, `web-dev`.
- **Sides:** `pm` (completion gate), `qa`, `reviewer`.

Customize in `.belayer/agents/` — see `agents/README.md` for worked examples. `belayer team add <category>` copies team identities into `.belayer/agents/`. The `development` category is a CLI special-case that copies the embedded shipped team from `agents/`; other categories (e.g. `story`) live in `examples/talent-catalog/<category>/`.

System prompts are loaded by the daemon at spawn time and injected via Hermes `ephemeral_system_prompt`. The daemon defaults to the `belayer` Hermes profile (`~/.hermes/profiles/belayer/`); run `belayer auth ensure` once to scaffold it. Per-talent profile forks (`belayer-<crag>-<instance>/`) are coming in Phase 2 (#135). See `docs/design-docs/2026-05-03-belayer-hermes-profiles-spec.md` for the full plan.

The `hermes_bridge` Python package lives in the daemon's runtime dir (default `$XDG_STATE_HOME/belayer/runtime`), not inside any workspace. This protects the module from workspace agent cleanup (`rm -rf .belayer/`).

## Acceptance gate (PM is the default)

When the supervisor calls `belayer finish`, the daemon intercepts and auto-spawns the PM side for adversarial spec-vs-reality verification. PM approves or rejects (up to 3 cycles). PM is currently the only runtime-enforced gate. The generic `belayer-gate/v1` contract (`docs/CRAG_MODE.md`) describes additional gate kinds (code-review, runtime-qa, continuity, etc.) as documented contracts and proof examples; the daemon does not yet discover or execute crag-defined task-stage gates. See `docs/AGENT_ARCHITECTURE.md` for the daemon enforcement path.

## Crag mode

Crags are durable cross-project operating contexts (software company, story world, research group) at `~/.belayer/crags/<name>/`. They store reusable teams, gates, evaluations, promotions, and generated talent metadata as documented contracts. A repo opts in by `belayer crag link <name>`, writing the link into `.belayer/config.yaml`. The CLI surfaces (`belayer crag init/list/link`, `belayer team add/remove`) and filesystem layout have shipped; runtime crag enforcement (gate discovery, identity precedence) is still in flight. See `docs/CRAG_MODE.md` and `docs/CRAG_FILESYSTEM.md`.

## CLI surface highlights

- `belayer climb start` (alias `run`) — start a climb
- `belayer auth ensure|status` — scaffold/inspect the base `belayer` Hermes profile
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
