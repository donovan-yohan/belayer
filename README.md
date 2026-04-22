# belayer

Run-local agent control plane for Nightshift.

Belayer coordinates planner + specialist agents inside a single worker run. One session, one daemon, one request at a time. Agents communicate through a message broker, register artifacts, and fire events. The Hermes bridge spawns and manages each agent as a subprocess.

## Quick start

```bash
go build ./cmd/belayer

# Start the daemon
belayer daemon

# Launch a run (creates session, spawns planner via Hermes bridge)
belayer run start --task "Add rate limiting to /api/v1/cards" --workdir /path/to/repo

# Monitor
belayer status
belayer logs <session-id> -f
belayer roster --session <session-id>

# Agents communicate through the daemon
belayer message send --to planner --content "API tests passing"
belayer artifact create --kind spec --path docs/spec.md

# Planner signals done, PM verifies
belayer finish "All spec items implemented"
```

## How it works

```
belayer run start
  -> daemon creates session + SQLite event log
  -> spawns planner via Hermes bridge (python -m hermes_bridge)
  -> planner reads spec, spawns specialists via belayer_spawn_agent tool
  -> specialists write code, send messages, register artifacts
  -> planner calls belayer finish
  -> daemon auto-spawns PM agent for spec-vs-reality verification
  -> PM approves (session complete) or rejects (gap list back to planner, up to 3 cycles)
```

## Architecture

Three layers:

1. **Session bus** .. Go daemon on a Unix socket, SQLite store. Sessions, roster, messages, events, artifacts.
2. **Hermes driver** .. Bridge subprocess wraps Hermes AIAgent. Identity injected via `ephemeral_system_prompt`, coordination tools registered at spawn.
3. **Bridge transport** .. Python subprocess lifecycle: heartbeats, exit detection, event streaming over stdout.

## Agent identity

Belayer ships a starter team under `agents/`, and `belayer init` copies that
tree into the consuming project at `.belayer/agents/`.

Treat the project-local copy as the real runtime source of truth. The shipped
defaults in this repo are examples and onboarding scaffolding, not a normative
team definition for every belayer consumer.

Agent identities live in `.belayer/agents/<name>/` at runtime. Each directory contains:

- `agent.yaml` .. vendor, model, tier, ephemeral flag
- `system-prompt.md` .. the agent's soul (injected as ephemeral_system_prompt)
- `agents.md` .. operating instructions, tools, workflows

The default starter team is: supervisor, pm, web-dev, backend-dev, qa, reviewer.
You can edit, delete, rename, or replace them in your own `.belayer/agents/`
tree without changing belayer source.

## CLI

```
belayer daemon              Start the daemon
belayer run start           Create session + spawn planner
belayer spawn               Spawn an agent mid-session
belayer finish              Signal work complete (triggers PM gate for planner)
belayer roster              List active agents
belayer message             Send/broadcast/list messages
belayer request-completion  Explicit PM gate trigger
belayer artifact            Create/list run artifacts
belayer session list|stop   Session lifecycle
belayer logs                Event stream
belayer status              Running sessions overview
belayer recall              Full-text event search
```

## Docs

- `docs/AGENT_ARCHITECTURE.md` .. agent toolbox, coordination model, completion gate
- `docs/design-docs/` .. detailed design decisions (see index.md)

## Development

```bash
go build ./cmd/belayer
go test ./...
```
