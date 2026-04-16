# CLAUDE.md

Belayer — Nightshift v1 / Extend-first redesign.

## Current architectural stance

Belayer is no longer best thought of as a generic global orchestration platform.

For the current direction, Belayer is:

> the **run-local agent control plane** inside a single Nightshift worker run.

Nightshift has two control planes:

1. **Worker control plane** — outer service that queues requests and assigns workers
2. **Agent control plane** — Belayer, inside one worker run, coordinating planner + specialists

Belayer owns the second one.

## Working assumptions right now

- one worker handles one request at a time
- one Belayer session exists inside that worker
- Hermes is the default harness
- tmux is the default transport adapter
- Belayer is the session bus for messages, events, artifacts, and roster state
- Extend-localenv (`xt`) is the preferred Extend workbench interface
- Clamshell remains the preferred sandbox boundary for production deployment

## Current implemented slice

Implemented in the repo now:

- `belayer run start`
- `belayer spawn`
- `belayer roster`
- `belayer finish`
- `belayer artifact create`
- `belayer artifact list`
- daemon-backed `agent_runs`
- daemon-backed artifact registry
- Hermes launch wrapper with Belayer env injection
- project-local Hermes plugin + Belayer communication skill
- exit-without-finish detection that marks runs blocked

## Key design rule

Belayer should not be trying to be:

- the cluster scheduler
- the worker autoscaler
- the hypervisor
- the universal hosted identity service

It should be trying to be excellent at:

- run-local session management
- planner/specialist coordination
- messages / events / artifacts
- observable progress and completion state

## Coordination model

Inside one run, agents coordinate through Belayer using:

- **messages** — direct communication
- **events** — orchestration state transitions
- **artifacts** — durable outputs

Agents should never rely on raw tmux for communication. tmux is only the transport adapter under Belayer.

## Hermes-specific guidance

Hermes is the preferred harness because:

- profiles give us controlled behavior loading
- skills/plugins/hooks are versionable and owned by us
- the stack is git-traceable
- we can iterate on behavior without depending on opaque upstream prompt changes

Current MVP identity injection uses:

- Hermes profile selection
- Belayer env vars (`BELAYER_SESSION_ID`, `BELAYER_AGENT_ID`, `BELAYER_SOCKET`, `BELAYER_RUN_DIR`)
- project-local Hermes plugin enablement
- Belayer communication skill preload
- workdir binding

This is enough for the MVP; canonical git-backed identity materialization is still a later layer.

## Relevant design docs

When working on Belayer now, use these docs as the current truth:

- `docs/PHILOSOPHY.md`
- `docs/ARCHITECTURE.md`
- `docs/DESIGN.md`
- `docs/design-docs/2026-04-15-nightshift-v1-deployment-topology.md`
- `docs/design-docs/2026-04-15-belayer-run-model-for-nightshift-v1.md`
- `docs/design-docs/2026-04-15-nightshift-extend-first-implementation-delta.md`
- `docs/AGENT_ARCHITECTURE.md` (how agents communicate, coordinate, and resume)
- `docs/design-docs/2026-04-15-crag-daemon.md` (forward-looking: always-on worker control plane)
- `docs/design-docs/2026-04-15-git-backed-agent-identity.md` (forward-looking: soul + capabilities)
- `docs/design-docs/2026-04-16-product-manager-agent.md` (forward-looking: spec-vs-reality completion gate)

## Development guidance

- Prefer deleting old generic code if it gets in the way of the new run-local model.
- Backwards compatibility is not the priority right now.
- New work should align with planner + api first, then expand.
- Keep the system inspectable by humans; avoid magical hidden orchestration.
- tmux is a pragmatic transport layer, not the architecture.
