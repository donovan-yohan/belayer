# Architecture

This document describes the high-level architecture of belayer.

## Bird's Eye View

Belayer is a standalone Go CLI that orchestrates autonomous coding agents across multiple repositories. A user creates a long-lived crag (configured with target repos), submits work problems (text or Jira tickets), and the belayer daemon decomposes problems into per-repo climbs, spawns lead execution loops in isolated git worktrees, monitors progress via SQLite, and validates cross-repo alignment using ephemeral Claude sessions before creating PRs.

Input: work items (text, Jira tickets, or tracker issues via GitHub Issues/Jira), user clarifications during brainstorm.
Output: per-repo PRs with aligned implementations, structured progress reports, automated CI fix attempts and review monitoring.

## Orchestration Layers

Belayer uses climbing metaphors and a three-phase model:

```
┌─────────────────────────────────────────────────────────────┐
│  EXPLORE                                                    │
│  belayer explore                                            │
│  intake sources (interactive, jira, github issues, ...)     │
│            │                                                │
│            ▼                                                │
│         spec.md                                             │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  CLIMB                                                      │
│  belayer climb                                              │
│                                                             │
│  [single-repo]                                              │
│    spec.md → Lead (plan→implement→review→pr) → PR           │
│                                                             │
│  [multi-repo — additive layers only]                        │
│    spec.md → Setter (fan-out) → per-repo spec.md            │
│                  │                                          │
│                  ▼                (per repo, in parallel)   │
│             Lead(repo-A)   Lead(repo-B)   Lead(repo-C)      │
│                  │                │               │         │
│                  ▼                ▼               ▼         │
│             commit hash    commit hash    commit hash       │
│                  └──────────────┬──────────────┘           │
│                                 ▼                           │
│                   Spotter (fan-in, gate scoring)            │
│                   N commit hashes → gate score + feedback   │
│                                 │                           │
│                           PASS / FAIL                       │
│                                 │                           │
│                                 ▼                           │
│                           PR manifest                       │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  SUMMIT  (not yet implemented)                              │
│  belayer summit                                             │
│  PR manifest → auto-merge, monitoring, observability        │
└─────────────────────────────────────────────────────────────┘
```

> Setter and Spotter are multi-repo only. Single-repo climbs run Lead directly.

## Named Roles

| Role | Scope | Contract |
|------|-------|----------|
| Setter | Multi-repo only | spec.md → per-repo spec.md |
| Spotter | Multi-repo only | N commit hashes → gate score + feedback |
| Lead | Per-repo | spec.md → commits + PR |
| Boulderer | One-off (deferred) | task → single commit |

## Code Map

| Module | Path | Purpose |
|--------|------|---------|
| CLI entry | `cmd/belayer/main.go` | Binary entry point |
| CLI commands | `internal/cli/` | Cobra command definitions (root, init, crag, explorer, problem, status, belayer, setter, message, mail, tracker, pr, env) |
| Belayer Config | `internal/belayerconfig/` | Config loader with resolution chain (crag > global > embedded defaults) |
| Config | `internal/config/` | Global config loading/saving (`~/.belayer/config.json`) |
| Defaults | `internal/defaults/` | Embedded default config files (belayer.toml, CLAUDE.md templates, validation profiles, session command markdown) via `embed.FS` |
| Manage | `internal/manage/` | Interactive session workspace preparation (`PrepareManageDir` for setter, `PrepareExplorerDir` for explorer; both render templates, deploy the appropriate `.claude/commands` set, and prune stale generated command files when workspaces are reused) |
| Climb Context | `internal/climbctx/` | GOAL.json types (LeadClimb, SpotterClimb, AnchorClimb) and writer |
| Database | `internal/db/` | SQLite connection, migration runner, embedded SQL |
| Migrations | `internal/db/migrations/` | SQL migration files (001_initial.sql through 005_environments.sql) |
| Model | `internal/model/` | Domain types and status enums |
| Crag | `internal/crag/` | Crag lifecycle (create, load, delete, worktree management) |
| Repo | `internal/repo/` | Git operations (bare clone, worktree add/remove/list) |
| Belayer | `internal/belayer/` | DAG executor daemon (v1). Manages leads and validation |
| Lead | `internal/lead/` | Lead execution runner, store, ClaudeSpawner (interactive sessions via tmux) |
| Spotter | `internal/spotter/` | Per-repo runtime validator (v1 code — three-phase model reframes "spotter" as multi-repo cross-repo validator; this package is the per-repo quality gate, to be renamed) |
| Anchor | `internal/anchor/` | Cross-repo alignment reviewer (v1 code — three-phase model merges this role into "spotter") |
| Store | `internal/store/` | SQLite CRUD operations (problems, climbs, events, tracker_issues, pull_requests, pr_reactions) |
| Tmux | `internal/tmux/` | Tmux session/window/pane management for agent spawning |
| Log Manager | `internal/logmgr/` | Log file management for lead sessions |
| PID File | `internal/pidfile/` | Daemon PID file locking |
| Mail | `internal/mail/` | Filesystem-backed inter-agent mail system (message types, address resolution, FileStore, templates, tmux delivery, send/read) |
| Tracker | `internal/tracker/` | Tracker plugin interface + GitHub Issues implementation (via `gh` CLI). Spec assembly agentic node for converting issues to problem specs |
| SCM | `internal/scm/` | SCM provider interface + GitHub PR implementation (via `gh` CLI). PR stacking logic, PR body generation agentic node |
| Plugins | `internal/plugins/` | Claude Code marketplace registration: writes to `~/.claude/plugins/` registry files during `belayer init` |
| Env Provider | `internal/envprovider/` | Provider client: shells out to configured command (e.g., `belayer env`, `extend env`) for environment lifecycle, parses JSON responses |
| Env (builtin) | `internal/env/` | Default `belayer env` provider implementation: wraps bare-repo + worktree logic behind the JSON contract |
| Review | `internal/review/` | Reaction engine: event classification (CI failures, reviews, comments), decision logic, action dispatch |

## Data Flow

> **Note:** This diagram reflects the three-phase model. The v1 code still uses
> the old per-repo Spotter + Anchor naming — see TODOS.md P1 for the rename plan.

```
EXPLORE: Intake sources → spec.md
                |
                v
CLIMB:  [single-repo]  spec.md → Lead (plan→implement→review→pr) → PR manifest
        [multi-repo]   spec.md → Setter (decompose, fan-out)
                                    |
                       +------------+------------+
                       v            v            v
                  Lead(repo-A) Lead(repo-B) Lead(repo-C)
                  (plan→impl   (plan→impl   (plan→impl
                   →review→pr)  →review→pr)  →review→pr)
                       |            |            |
                       v            v            v
                  commit hash  commit hash  commit hash
                       +------------+------------+
                                    |
                                    v
                         Spotter (fan-in, cross-repo gate)
                              |          |
                            PASS       FAIL → feedback → Setter
                              |
                              v
                         PR manifest
                              |
                              v
SUMMIT: PR manifest → auto-merge → monitoring → observability
```

### Tracker Intake (Planning Hat)

```
Tracker (GitHub Issues / Jira)
    |-- polling via gh/API
    v
Sync → tracker_issues table
    |
    v
Spec Assembly (agentic node, Claude)
    |-- converts issue → problem spec + climbs
    v
Problem created (status: pending)
```

## Directory Layout

```
~/.belayer/
  config.json                         # Crag registry
  config/                             # Global defaults (written by `belayer init`)
    belayer.toml                      # Agent provider, concurrency, timeouts
    profiles/
      frontend.toml                   # Frontend validation checklist
      backend.toml                    # Backend API validation checklist
      cli.toml                        # CLI tool validation checklist
      library.toml                    # Library/package validation checklist

~/.belayer/crags/<name>/
  crag.json                            # Crag config (repos, settings)
  belayer.db                          # SQLite database
  config/                             # Per-crag overrides (optional)
  mail/                               # Filesystem mail store (per-address unread/read dirs)
  repos/                              # Bare repo clones
    <repo-name>.git
  tasks/                              # Per-problem worktrees
    <problem-id>/
      <repo-name>/                    # Git worktree
        .lead/                        # Lead state directory
```

Config resolution: crag config > global config > embedded defaults (via `internal/defaults/`)

## Pipeline Engine (internal/v3/)

The three-phase model (Explore/Climb/Summit) is an architectural reframing of v3, not a rewrite. The v3 Temporal pipeline, node activities, and gate scoring remain unchanged — the phases provide a vocabulary for how those pieces compose end-to-end.

Belayer v3 simplifies v2 into an Activity-per-Node model. Each pipeline node is a Temporal Activity that spawns an interactive Claude Code session. File-based rendezvous (completion files) replaces Temporal Signals. YAML pipeline config with natural language node descriptions.

| Module | Path | Purpose |
|--------|------|---------|
| Model | `internal/v3/model/` | Domain types: NodeOutcome, CompletionResult, ClimbInput/Output |
| Pipeline | `internal/v3/pipeline/` | YAML parser, validator, default pipeline config (node names to be renamed per TODOS.md P1) |
| Gate | `internal/v3/gate/` | Gate result parsing, weighted scoring, threshold routing, prompt builder |
| Events | `internal/v3/events/` | JSONL event logger for pipeline observability (node + gate events) |
| Outcome | `internal/v3/outcome/` | Outcome detection: verdict.txt > output file first line > type default |
| Session | `internal/v3/session/` | ExecSpawner (generic command exec), path helpers for `.belayer/.internal/`, SpawnOpts |
| Temporal | `internal/v3/temporal/` | ClimbWorkflow, NodeActivity (spawn + heartbeat + poll completion + node-context.json) |
| Intake | `internal/v3/intake/` | Intake adapter interface, SubmitSpec, bridge function, Jira adapter, schedule reconciliation |
| Frameworks | `frameworks/` | Built-in framework templates (embed.FS), Install/List/EnsureInternalDir |
| CLI | `internal/v3/cli/` | Commands: climb, node-complete, status, worker, start, setup |

### Key Concepts

- **Two pipeline primitives**: Nodes (constructive — produce artifacts) and Gates (adversarial — evaluate artifacts with multi-dimensional scoring)
- **Activity Per Node**: Each pipeline node/gate = one Temporal Activity. Simplest model.
- **File-based completion**: Node commands write `.belayer/.internal/completion/<id>-<node>-attempt-<N>.json` when done (via `belayer node-complete` or directly)
- **Node protocol**: `NodeActivity` writes `.belayer/.internal/input/node-context.json` before spawning. The framework command reads it for context.
- **ExecSpawner**: Core spawner execs the `command:` field from pipeline YAML via `sh -c`. Returns an exit channel for fast-fail on process death. TmuxSpawner removed from core — now lives in the `claude-tmux` framework.
- **Framework model**: `belayer setup --framework <name-or-path>` scaffolds pipeline.yaml + scripts into `.belayer/`. Built-in frameworks embedded via `//go:embed`. Orchestration config is committed; runtime state is in `.belayer/.internal/` (gitignored).
- **Gate scoring**: Gates produce `gate-result.json` (structured scores) + `rationale.md` (human-readable). Activity computes weighted score from YAML-declared dimensions/weights and applies threshold routing (score-then-route anti-gaming)
- **Natural language roles**: Node descriptions are prompts in pipeline YAML, passed to the framework command via node-context.json
- **Attempt-scoped**: Completion files, output paths, and verdict files include attempt number to prevent stale reads
- **CLI entry point**: `belayer climb --file design.md` → Temporal workflow → plan → implement → review(gate) → pr-author → branch (current code uses legacy names setter/lead/spotter/summit — see TODOS.md P1)
- **Intake plugins**: `intake:` section in pipeline YAML defines where work comes from (interactive, jira). Each intake produces a `SubmitSpec` → bridge creates worktree → starts `ClimbWorkflow`
- **Worker daemon**: `belayer worker` runs Temporal worker + HTTP API for submit/status. `belayer start` opens interactive session connected via MCP channel
- **Belayer is plumbing**: Routes typed references (commit SHAs, file paths) between nodes. Nodes are black boxes.

## Config Hierarchy

```
~/.belayer/                        # global
  config.json                      # global settings
  crags/                           # multi-repo crag definitions

./.belayer/                        # repo-level (per-repo)
  pipeline.yaml                    # climb pipeline config
  .internal/                       # git-ignored state
```

Resolution: repo-level > global > embedded defaults

## Architecture Decision Records

> Normative constraints documented in `docs/adrs/`.

_To be populated._
