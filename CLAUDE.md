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
2. **Hermes driver** — Bridge subprocess (`python -m hermes_bridge`) wraps Hermes AIAgent. Identity injected via `ephemeral_system_prompt`, tools registered at spawn time.
3. **Bridge transport** — Python subprocess managed by Go. Heartbeats, exit detection, event streaming over stdout.

## Coordination model

Agents coordinate through the daemon:

- **messages** — direct agent-to-agent via session bus
- **events** — machine-readable state transitions
- **artifacts** — durable outputs registered in the session

## CLI surface

```
belayer daemon [--bind addr] [--auth-token tok] [--cors-origin url] [--log-level L]
belayer run start                   # create session, spawn supervisor via bridge
belayer spawn                       # spawn an agent mid-session
belayer finish                      # signal work complete (triggers PM gate)
belayer roster                      # list active agents
belayer message send/broadcast/list
belayer request-completion          # explicit PM gate trigger
belayer logs <sid> [-f] [--agent a] [--type p] [--tier t] [--format fmt] [--tail N] [--since d]
belayer logs --raw --agent <a> <sid>  # raw bridge stdout tail
belayer bridges tail <sid> <agent>  # shorthand for the raw tail above
belayer artifact get <sid> <id>     # download artifact bytes
belayer status                      # running sessions overview
belayer archive <sid>               # write archive bundle to disk
belayer init                        # scaffold .belayer/ with log_level: standard
```

## Agent identity

Agent identities live in `agents/<name>/` (shipped defaults, embedded in the binary)
and `.belayer/agents/<name>/` (project-local override; `belayer init` scaffolds it).
The loader at `internal/daemon/agents.go` reads project-local first, falls back to shipped.
Each directory contains:
- `agent.yaml` — vendor, model, ephemeral flag, tier
- `system-prompt.md` — the agent's soul
- `agents.md` — operating instructions, tools, workflows

System prompts are loaded by the daemon at spawn time and injected via Hermes `ephemeral_system_prompt`. All agents use the `default` Hermes profile for now (see profile bootstrap TODO in AGENT_ARCHITECTURE.md).

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
