# Crag: The Always-On Worker Control Plane

Status: `forward-looking` — design direction, not yet implemented

## The name

Throughout the docs, the outer Nightshift service has been called "the Nightshift daemon", "the worker control plane", "the outer service." It needs a real name.

**Crag** is the always-on daemon that spawns and manages Belayer runs. In climbing terms, a crag is the rock face itself... the fixed terrain that routes are established on. Belayer manages one route (run). Crag manages the whole wall.

## What Crag is

Crag is the process that is always running. It:

- Accepts work requests (from a user, Jira, a spec, a web UI, a CLI)
- Maintains a request queue
- Manages a pool of worker targets (directories, machines, containers)
- Spawns a Belayer daemon per run against a target
- Monitors run lifecycle at a coarse level (running, blocked, complete, failed)
- Collects final artifacts and handoff outputs
- Exposes a web UI for observability, request submission, and run management

Crag is the service that answers:
- Which targets are available?
- Is this request running, blocked, or complete?
- Which run belongs to which ticket?
- Where are the artifacts from last night's run?
- What's the agent doing right now?

Crag is NOT:
- The agent coordinator (that's Belayer)
- The harness (that's Hermes)
- The sandbox boundary (that's Clamshell)
- A build system or CI runner

## Relationship to existing architecture

The current architecture doc describes two control planes:

1. **Worker control plane** — the always-on outer service → **this is Crag**
2. **Agent control plane** — Belayer, inside one run → unchanged

Every reference to "Nightshift daemon" or "worker control plane" in the existing docs is describing Crag. This doc gives it a name, a concrete local-dev story, and a web UI surface.

## Two modes

### Local dev mode

For local development and personal nightshift use, Crag runs on your machine.

You register one or more directories as **targets** for agentic runs:

```bash
crag target add ~/Projects/extend-app
crag target add ~/Projects/extend-api
crag target add ~/Projects/shared-lib
```

Each target is a git repo directory that agents can work against. When a request comes in, Crag picks an available target (or queues if all targets are busy), provisions a run directory, and spawns a Belayer daemon against it.

For the simplest local case: one target, one run at a time. Crag just manages the lifecycle and provides the web UI.

For more capacity: register multiple directories (multiple clones of the same repo, or different repos). Crag can run them in parallel, one Belayer per target.

```
crag (always on, local machine)
├── target: ~/Projects/extend-app    → belayer run A (active)
├── target: ~/Projects/extend-api    → belayer run B (active)
└── target: ~/Projects/shared-lib    → (idle, available)
```

### Remote/production mode

For production Nightshift deployments, Crag runs on a server (or as a service) and manages remote workers.

Targets become machines or containers instead of local directories. The provisioning step is heavier (clone repo, install deps, provision agent capabilities from identity repo, start MCP servers). But the interface is the same: Crag accepts requests, assigns them to available targets, spawns Belayer daemons, monitors lifecycle.

```
crag (always on, server)
├── worker-1 (Linux VM)    → belayer run A (active)
├── worker-2 (Linux VM)    → belayer run B (active)
└── worker-3 (Linux VM)    → (idle, available)
```

The progression from local to remote is:
- **local**: targets are directories, Belayer spawns processes locally, no sandbox
- **remote**: targets are machines/containers, Belayer spawns inside Clamshell, full isolation

Same Crag daemon, different target backends.

## The web UI

Crag is where the rich web UI lives. Not Belayer (which is per-run and ephemeral), not Hermes (which is a terminal harness).

The web UI provides:

### Dashboard
- Active runs with status (running, blocked, complete, failed)
- Request queue with position and age
- Target pool with availability
- Recent artifacts and handoff outputs

### Run detail view
- Live event stream from the Belayer session
- Agent roster with current state
- Message history between agents
- Artifact list with content preview
- Progress timeline (phases, tasks, completion)

### Request submission
- Paste a spec or describe work
- Select target or let Crag auto-assign
- Choose session template (intake, implement, deliver)
- Watch the run start in real time

### Observability
- Run history with outcomes
- Agent performance over time
- Common failure patterns
- Artifact archive

The web UI talks to Crag's API. Crag's API talks to the Belayer daemon running inside each active run. The Belayer daemon exposes its session bus (events, messages, artifacts, roster) via HTTP/Unix socket. Crag aggregates this across all active runs.

## How Crag spawns a run

1. Request arrives (web UI, CLI, webhook)
2. Crag selects an available target (directory or machine)
3. Crag provisions the run environment:
   - For local: create run directory, pull latest, checkout branch
   - For remote: provision workspace, clone repo, install deps, stage credentials
   - For both: read identity repo, provision agent capabilities
4. Crag starts a Belayer daemon pointed at the target
5. Belayer takes over: creates session, spawns agents, coordinates the run
6. Crag monitors the Belayer daemon's health and coarse state
7. When Belayer reports completion, Crag collects artifacts and marks the run done
8. Target becomes available for the next request

## What Crag stores

Crag needs its own persistent state, separate from Belayer's per-run state:

- **Target registry**: which directories/machines are registered, their status
- **Request queue**: pending work, priority, assignment
- **Run index**: which runs happened, when, which target, outcome, artifact locations
- **Identity repo reference**: where to find soul + capabilities definitions

For local dev, this can be a simple SQLite database or flat files in `~/.crag/` or `~/.nightshift/`.

## CLI sketch

```bash
# Daemon lifecycle
crag start                    # Start the Crag daemon (stays running)
crag stop                     # Stop gracefully
crag status                   # Show daemon status + active runs

# Target management
crag target add <path>        # Register a directory as a run target
crag target list              # Show all targets and their status
crag target remove <path>     # Unregister a target

# Run management
crag run submit <spec>        # Submit a work request
crag run list                 # List active and recent runs
crag run inspect <id>         # Show run detail (events, agents, artifacts)
crag run cancel <id>          # Cancel a running request

# Web UI
crag ui                       # Open the web UI in the browser
```

## Relationship to identity repo

Crag is the natural consumer of the identity repo's `capabilities.yaml` files (see [git-backed agent identity](2026-04-15-git-backed-agent-identity.md)).

When provisioning a run, Crag:
1. Reads the session template to know which agents are needed
2. For each agent, reads its `capabilities.yaml` from the identity repo
3. Provisions the required infrastructure (MCP servers, runtimes, credentials)
4. Hands the provisioned environment to Belayer

In local dev mode, provisioning is lightweight (most things are already installed). In remote mode, it's the full bootstrapping sequence.

## Relationship to Belayer

Clear ownership split:

| Concern | Crag | Belayer |
|---------|------|---------|
| Request queue | owns | doesn't know about |
| Target/worker assignment | owns | doesn't know about |
| Run provisioning | owns | receives the environment |
| Agent coordination | delegates | owns |
| Message routing | reads (observability) | owns |
| Artifact collection | aggregates | produces |
| Session lifecycle | monitors coarsely | manages precisely |
| Web UI | hosts | exposes API |
| Identity repo | reads + provisions | reads soul at spawn |

Belayer doesn't know it's running inside Crag. It just sees a target directory, environment variables, and a session to manage. This keeps Belayer simple and testable in isolation.

## Migration path

1. **Now**: document the concept (this doc), continue building Belayer as the run-local control plane
2. **Soon**: build a minimal `crag` CLI that wraps `belayer run start` with target management and a run index. Single-target, single-run. Enough to prove the lifecycle.
3. **Next**: add the web UI. Start with the dashboard (active runs, recent history) and run detail view (events, roster, artifacts from the Belayer session bus).
4. **Later**: add request queue, multi-target parallelism, identity repo provisioning.
5. **Eventually**: remote target backends (SSH, container orchestration). Same Crag API, different provisioners.
