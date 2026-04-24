# CLAUDE.md

Belayer v7 — run-local agent control plane for Nightshift.

## What Belayer is

> The **agent control plane** inside a single Nightshift worker run.

One worker, one request, one Belayer session. Belayer coordinates supervisor + specialist agents via the Hermes bridge.

## What Belayer is not

Not a cluster scheduler, autoscaler, hypervisor, or hosted identity service. It manages one run.

## Architecture

Three layers:

1. **Session bus** — Go daemon + SQLite. Sessions, agent roster, messages, events, artifacts.
2. **Hermes driver** — Bridge subprocess (`python -m hermes_bridge`) wraps Hermes AIAgent. Identity injected via `ephemeral_system_prompt`; belayer_* tools registered by the Hermes 0.11 plugin at `plugins/belayer/` (loaded automatically by Hermes during AIAgent import).
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

## CLI surface

Run `belayer --help` and `belayer <cmd> --help` for current commands and
flags. Default config schema (including `exit_conditions`) is generated
from `internal/cli/init.go`. Agent tool surface (baseline + role-specific)
is defined by the Hermes plugin at `plugins/belayer/tools.py` and gated
by `plugins/belayer/__init__.py:register()` based on agent kind +
BELAYER_TOOLS allowlist.

## Agent identity

Agent identities live in `agents/<name>/` (shipped defaults, embedded in the binary)
and `.belayer/agents/<name>/` (project-local override; `belayer init` scaffolds it).
The loader at `internal/daemon/agents.go` reads project-local first, falls back to shipped.
Each directory contains:
- `agent.yaml` — `kind`, vendor, model, ephemeral flag, `belayer_tools` allowlist
- `system-prompt.md` — the agent's soul
- `agents.md` — operating instructions, tools, workflows

Shipped default team:
- **Mains:** `supervisor` (party lead), `backend-dev`, `web-dev`.
- **Sides:** `pm` (completion gate), `qa`, `reviewer`.

Customize in `.belayer/agents/` — see `agents/README.md` for worked examples.

System prompts are loaded by the daemon at spawn time and injected via Hermes `ephemeral_system_prompt`. All agents use the `default` Hermes profile for now (see profile bootstrap TODO in AGENT_ARCHITECTURE.md).

The `hermes_bridge` Python package lives in the daemon's runtime dir (default `$XDG_STATE_HOME/belayer/runtime`), not inside any workspace. This protects the module from workspace agent cleanup (`rm -rf .belayer/`).

## PM completion gate

When the supervisor calls `belayer finish`, the daemon intercepts and auto-spawns a PM agent for adversarial spec-vs-reality verification. The PM approves or rejects (up to 3 cycles). See `docs/AGENT_ARCHITECTURE.md` for full details.

## Docs

- `docs/PHILOSOPHY.md` — the six runtime interfaces (conceptual, not implementation-specific)
- `docs/AGENT_ARCHITECTURE.md` — how agents communicate, coordinate, and resume
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
