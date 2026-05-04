# belayer

Climb-local agent control plane for Nightshift.

Belayer coordinates a supervisor + specialist agents inside a single worker
climb. One session, one daemon, one request at a time. Agents communicate through a
message broker, register artifacts, and fire events. The Hermes bridge spawns
and manages each agent as a subprocess.

## The agent model: mains and sides

Every agent is one of two kinds. The distinction is whether the agent has a
mailbox.

- **Main** — long-lived party member. Has inbox + outbox. Polls mail before
  every turn. Accepts broadcasts. Participates in peer-to-peer dialogue.
  Examples: `supervisor`, `backend-dev`, `web-dev`.
- **Side** — short-lived worker with a single scoped task. No mailbox. Takes
  its task in the initial spawn message, produces output via `final_response`
  and registered artifacts, and exits. Examples: `pm`, `qa`, `reviewer`.

```mermaid
flowchart LR
    Supervisor["supervisor<br/>kind: main"]:::main
    Backend["backend-dev<br/>kind: main"]:::main
    Web["web-dev<br/>kind: main"]:::main
    PM["pm<br/>kind: side"]:::side
    QA["qa<br/>kind: side"]:::side
    Rev["reviewer<br/>kind: side"]:::side

    Supervisor <-->|"peer mail"| Backend
    Supervisor <-->|"peer mail"| Web
    Backend <-->|"peer mail"| Web
    Supervisor -->|"summon (spawn-and-await)"| PM
    Supervisor -->|"summon"| QA
    Supervisor -->|"summon"| Rev

    classDef main fill:#1a3a2a,stroke:#3fb950,color:#c9d1d9
    classDef side fill:#1a2a3a,stroke:#58a6ff,color:#c9d1d9
```

Mains talk to each other directly — no supervisor hop required. Sides are
spawned for a task, do it, exit. See `docs/AGENT_ARCHITECTURE.md` for the
full coordination model.

## Requirements

| Dependency | Version | Notes |
|------------|---------|-------|
| Go         | 1.22+   | Build toolchain for the daemon binary. |
| Python     | 3.10+   | Runs the per-agent bridge subprocess. |
| Hermes     | 0.12+   | LLM driver. Belayer ships a Hermes plugin (`plugins/belayer/`) that is auto-installed into `$HERMES_HOME/plugins/belayer/` on first daemon start. Older Hermes versions lacked the current session persistence and plugin surface and are no longer supported. |
| Linux kernel | 5.19+ (optional) | Required for Landlock v2 write-confinement (see `docs/DEPLOYMENT.md`). |

The daemon resolves Hermes from `$HERMES_HOME` (or `~/.hermes` by default).
The Python bridge imports Hermes modules from `$HERMES_HOME/hermes-agent/` and
the Hermes venv at `$HERMES_HOME/hermes-agent/venv/`. Override with
`HERMES_AGENT_PATH` to point at a development checkout.

On daemon start, Belayer:

1. Extracts its embedded plugin tree into `$HERMES_HOME/plugins/belayer/` (idempotent; SHA-matched).
2. Adds `belayer` to `plugins.enabled` in `$HERMES_HOME/config.yaml` if missing.

Set `BELAYER_REQUIRE_HERMES_PLUGIN=1` to make plugin install failures abort
the daemon (default is to log a warning and continue).

## Quick start

```bash
go install github.com/donovan-yohan/belayer/cmd/belayer@latest

# One-time setup: scaffold the base blyr Hermes profile and log in
belayer auth ensure

# Create a durable crag for cross-project context
belayer crag init personal --kind development

# In your project repo:
belayer init                     # scaffold .belayer/ (config, agents)
belayer crag link personal       # link this repo to your crag
belayer daemon                   # start the daemon (also installs the Hermes plugin)

# Launch a climb (creates session, spawns supervisor via Hermes bridge)
belayer climb start --task "Add rate limiting to /api/v1/cards"

# Monitor
belayer status
belayer logs <session-id> -f
belayer roster --session <session-id>

# Supervisor signals done, PM verifies
belayer finish "All spec items implemented"
```

The `blyr` base Hermes profile (`~/.hermes/profiles/blyr/`) holds your auth
credentials and the belayer plugin. Each spawned agent automatically gets a
per-talent fork (`blyr-<crag>-<instance>/`) so auth isolation works without
extra `hermes auth` logins. See `docs/design-docs/2026-05-03-belayer-hermes-profiles-spec.md`
for the profile materialization details.

## Climbs And Crags

Use **climbs** when you want agent-powered workflows inside one repo:

```bash
belayer init
belayer daemon
belayer climb start --task "Implement the issue and open a PR"
```

That path only needs repo-local `.belayer/` state. It is the smallest useful
Belayer setup: a supervisor, optional specialist agents, artifacts, gates, and
mail inside one project.

Use **crags** when you want a durable operating context that can learn across
projects or modes:

```bash
belayer crag init software-company --kind development
belayer crag link software-company
belayer team add development
belayer climb start --task "Pick up this backlog item end to end"
```

A crag stores reusable teams, gates, evaluations, promotions, and
generated talent metadata under `~/.belayer/crags/<name>/`. Story worlds use the
same shape with `--kind story`, story teams, continuity gates, and world state.

Catalog talent metadata (`talent.yaml`, schema `belayer-talent/v1`) describes
intent beyond the bridge mechanics: `runtime.lifecycle: resident | resumable | ephemeral`,
`activation.mode`, typed `contract.accepts/produces/requires`, gate authority,
and `memory.scope`. Generated talents use a separate `belayer-generated-talent/v1`
schema for runtime-scaffolded records. Gates follow the `belayer-gate/v1` shape;
PM is the only gate the daemon enforces today (the session-level acceptance
gate). Additional crag-defined gates (code-review, runtime-qa, continuity, etc.)
are documented contracts and proof examples — the daemon does not yet discover
or execute them. See `docs/CRAG_MODE.md` and `docs/CRAG_FILESYSTEM.md`.

## How a climb flows

```mermaid
sequenceDiagram
    participant Op as Operator
    participant D as Daemon
    participant Sup as supervisor (main)
    participant Dev as backend-dev / web-dev (main)
    participant PM as pm (side)

    Op->>D: belayer climb start --task "..."
    D->>Sup: spawn (reads .belayer/agents/supervisor/)
    Sup->>D: belayer_spawn_agent(web-dev, backend-dev, ...)
    D->>Dev: spawn as peer mains (worktree isolated)
    Sup<<->>Dev: direct peer mail (mid-turn)
    Dev->>D: belayer_send_message(to=supervisor, "done, PR #42")
    Sup->>D: belayer_request_completion("spec satisfied")
    D->>PM: auto-spawn PM side (adversarial verifier)
    PM->>D: approve | reject (up to 3 cycles)
    D->>Op: session complete | needs_human_review
```

## The default team

`belayer init` scaffolds `.belayer/` and copies the shipped starter team into
`.belayer/agents/`. The default roster:

| Identity       | Kind  | Role                                                |
|----------------|-------|-----------------------------------------------------|
| `supervisor`   | main  | Party lead. Spawns, coordinates, calls `finish`.    |
| `backend-dev`  | main  | Backend/API implementer. Worktree-isolated.         |
| `web-dev`      | main  | Frontend/web implementer. Worktree-isolated.        |
| `pm`           | side  | Adversarial spec-vs-reality verifier (completion gate). |
| `qa`           | side  | Outside-in validation: browser/CLI/real APIs.       |
| `reviewer`     | side  | Diff / plan reviewer with structured verdicts.      |

None of these names are baked into belayer. The framework contract is
`.belayer/agents/<name>/{agent.yaml, system-prompt.md, agents.md}` — the names
themselves are yours.

## Customizing your team

`.belayer/agents/` is project-owned. Edit, rename, delete, or replace
identities. The daemon reads `.belayer/agents/<name>/` first, falls back to the
shipped copy only if the name is not defined locally.

```bash
$EDITOR .belayer/agents/reviewer/system-prompt.md  # edit in place
rm -r .belayer/agents/qa                           # drop unused identity
```

`belayer init --force` refreshes the shipped defaults without touching
`config.yaml`. See `agents/README.md` for field reference, tool table,
4-layer identity resolution, and `examples/templates/` for alternative
team shapes.

## Architecture

Three layers:

1. **Session bus** — Go daemon on a Unix socket, SQLite store. Sessions,
   roster, messages, events, artifacts.
2. **Hermes driver** — Bridge subprocess wraps Hermes `AIAgent`. Identity
   injected via `ephemeral_system_prompt`, coordination tools registered at
   spawn.
3. **Bridge transport** — Python subprocess lifecycle: heartbeats, exit
   detection, event streaming over stdout.

## CLI

```bash
belayer init                Scaffold .belayer/ in the current project
belayer daemon              Start the daemon
belayer climb start         Create a climb + spawn supervisor (`run` is an alias)
belayer spawn               Spawn an agent mid-session
belayer finish              Signal work complete (triggers PM gate)
belayer roster              List active agents
belayer message             Send/broadcast/list messages
belayer request-completion  Explicit PM gate trigger
belayer artifact            Create/list climb artifacts
belayer team                List/add/remove local team identities
belayer crag                Manage local Belayer crags (`space` and `org` aliases)
belayer session list|stop   Session lifecycle
belayer logs                Event stream
belayer status              Running sessions overview
belayer recall              Full-text event search
belayer auth ensure|status  Scaffold/inspect the base blyr Hermes profile
belayer doctor              Report blyr-* profile health (orphans, staleness, disk usage)
belayer prune               Remove orphaned blyr-* profiles
belayer uninstall           Remove belayer profiles and state (per-crag or global)
```

Run `belayer --help` or `belayer <cmd> --help` for current flags.

## Web UI

The daemon serves a dark-theme, vanilla-JS UI at `/ui/` when a TCP listener is
bound. A separate `belayer dashboard` command aggregates multiple daemons.

```bash
belayer daemon --tcp-addr 0.0.0.0:7523
# open http://localhost:7523/ui/

belayer dashboard --config dashboard.yaml --port 7525
# open http://localhost:7525/ui/
```

## Docs

- `docs/README.md` — current docs map and historical-design warning
- `docs/AGENT_ARCHITECTURE.md` — agent toolbox, main/side model, mail, PM gate
- `docs/CRAG_MODE.md` — team catalogs, gate contracts, crag events, proof climbs
- `docs/CRAG_FILESYSTEM.md` — repo, user catalog, and user crag directory contracts
- `docs/ARTIFACT_SCHEMAS.md` — artifact content schemas for crag-mode proofs
- `docs/DEPLOYMENT.md` — topologies, trust model, credentials, sockets
- `docs/PHILOSOPHY.md` — the six runtime interfaces
- `docs/LOG_FORMAT.md` — event schema, SSE, archive format
- `docs/OBSERVABILITY.md` — operator guide
- `docs/design-docs/` — detailed design decisions (see `index.md`)
- `agents/README.md` — shipped starter team overview
- `examples/templates/` — alternative team templates

## Development

```bash
go build ./cmd/belayer
go test ./...
```
