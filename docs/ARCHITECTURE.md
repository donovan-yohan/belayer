# Architecture

This document describes the high-level architecture of belayer.

## Bird's Eye View

Belayer is a standalone Go CLI that orchestrates autonomous coding agents across multiple repositories. A user creates a long-lived crag (configured with target repos), submits work problems (text or Jira tickets), and the belayer daemon decomposes problems into per-repo climbs, spawns lead execution loops in isolated git worktrees, monitors progress via SQLite, and validates cross-repo alignment using ephemeral Claude sessions before creating PRs.

Input: work items (text, Jira tickets, or tracker issues via GitHub Issues/Jira), user clarifications during brainstorm.
Output: per-repo PRs with aligned implementations, structured progress reports, automated CI fix attempts and review monitoring.

## Orchestration Layers

Belayer uses climbing metaphors for its agent hierarchy:

```
User (CLI)
  |
  v
Setter (interactive Claude session — human defines problems)
  |
  v
Belayer (DAG executor daemon — manages climbs/problems)
  |-- Polls SQLite for state changes
  |-- Spawns/monitors leads, spotters, anchors
  |-- Triggers agentic nodes (ephemeral Claude sessions)
  |
  v
Lead (bundled execution loop per repo — does the climbing)
  |-- Runs in isolated git worktree
  |-- Full interactive Claude Code session (not claude -p)
  |-- Role via --append-system-prompt, context via .lead/<climbID>/GOAL.json
  |-- Self-check: build + tests
  |
  v
Spotter (per-repo runtime validator — watches for problems)
  |-- Project type detection (frontend, backend, CLI, library)
  |-- Runs validation profile checklists
  |-- Produces SPOT.json verdict
  |
  v
Anchor (cross-repo alignment reviewer — ties all lines together)
  |-- Reviews changes across all repos for a problem
  |-- Prompt builder + verdict types
  |-- PASS → create PRs, FAIL → re-dispatch with feedback
```

**Three-layer validation**: Lead (self-check) → Spotter (per-repo runtime validation) → Anchor (cross-repo alignment)

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
| Belayer | `internal/belayer/` | DAG executor daemon. Manages leads, spotters, and anchors |
| Lead | `internal/lead/` | Lead execution runner, store, ClaudeSpawner (interactive sessions via tmux) |
| Spotter | `internal/spotter/` | Per-repo runtime validator. Project type detection, validation profiles, SPOT.json types |
| Anchor | `internal/anchor/` | Cross-repo alignment reviewer. Verdict types (VerdictJSON, RepoVerdict) |
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

```
Problem Input (text/Jira) --> Intake Pipeline (sufficiency + brainstorm) --> Decomposition (agentic, crag-aware)
                                                    |
                  +------------------+--------------+
                  v                  v              v
            Lead(repo-A)       Lead(repo-B)    Lead(repo-C)
            self-check:       self-check:      self-check:
            build + tests     build + tests    build + tests
                  |                  |              |
                  v                  v              v
            Spotter(A)         Spotter(B)      Spotter(C)       ← "spotting" state (per-repo)
            runtime validate   runtime validate runtime validate
            → SPOT.json        → SPOT.json      → SPOT.json
                  |                  |              |
                  +--------+---------+--------------+
                           v
                 Belayer detects "all spotted"
                           |
                           v
                 Anchor Review (cross-repo alignment)
                      |          |
                    PASS       FAIL
                      |          |
                 Create PRs  Re-dispatch with feedback
                      |
                      v
                 PR Monitoring (polling CI + reviews)
                      |
              +-------+-------+
              v               v
         CI Failure      Review Comment
              |               |
         CI Fix Lead    Notify setter
         (1 attempt)    (human-driven)
              |
              v
         All PRs merged → problem complete
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

## v2: Temporal-Backed Orchestrator (internal/v2/)

Belayer v2 replaces the monolithic daemon with a Temporal-backed pipeline platform. See `docs/designs/temporal-orchestrator-reimagining.md` for the full design.

| Module | Path | Purpose |
|--------|------|---------|
| Pipeline | `internal/v2/pipeline/` | DSL parser, topology validator, visualization, embedded templates |
| Temporal | `internal/v2/temporal/` | Route workflow, Type A/B activities, CLI-callback signals, safety controls |
| Role | `internal/v2/role/` | Contract types (Type A pitch / Type B ascent), phase definitions, safety config |
| Provider | `internal/v2/provider/` | Session spawner (Claude/Codex via tmux), exec shell-out (JSON in/out) |
| Risk Gate | `internal/v2/riskgate/` | Risk evaluation at role transitions, auto-pass / human-review decisions |
| Eval | `internal/v2/eval/` | Fixture recording, loading, comparison for role testing |
| Model | `internal/v2/model/` | Domain types: RoleSignal, RouteInput/Output, RunStatus |
| CLI | `internal/v2/cli/` | v2 commands: run, status, pipeline, temporal, role signals |

### Key Concepts

- **Two provider contracts**: Type A (pitch — JSON in/out) for judgment calls, Type B (ascent — CLI-callback) for interactive sessions
- **Three pipeline phases**: Approach (intake), Ascent (execution with loops), Send (output)
- **CLI-callback**: Interactive sessions signal completion via `belayer v2 <role> finish --task-id <id>`
- **Temporal Signal**: CLI callback → Temporal Signal → workflow advances to next role

## v3: Activity-Per-Node Pipeline (internal/v3/)

Belayer v3 simplifies v2 into an Activity-per-Node model. Each pipeline node is a Temporal Activity that spawns an interactive Claude Code session. File-based rendezvous (completion files) replaces Temporal Signals. YAML pipeline config with natural language node descriptions.

| Module | Path | Purpose |
|--------|------|---------|
| Model | `internal/v3/model/` | Domain types: NodeOutcome, CompletionResult, ClimbInput/Output |
| Pipeline | `internal/v3/pipeline/` | YAML parser, validator, default setter→lead→spotter config |
| Events | `internal/v3/events/` | JSONL event logger for pipeline observability |
| Outcome | `internal/v3/outcome/` | Outcome detection: verdict.txt > output file first line > type default |
| Session | `internal/v3/session/` | Hooks config generator, tmux-backed Claude session spawner |
| Temporal | `internal/v3/temporal/` | ClimbWorkflow, NodeActivity (spawn + heartbeat + poll completion) |
| CLI | `internal/v3/cli/` | v3 commands: climb, node-complete, status |

### Key Concepts

- **Activity Per Node**: Each pipeline node = one Temporal Activity. Simplest model.
- **File-based completion**: Stop hook calls `belayer node-complete` which writes `.belayer/completion/<id>-<node>-attempt-<N>.json`
- **Natural language roles**: Node descriptions are prompts passed via `--append-system-prompt`
- **Attempt-scoped**: Completion files, output paths, and verdict files include attempt number to prevent stale reads
- **CLI entry point**: `belayer climb --file design.md` → Temporal workflow → setter → lead → spotter → branch

## Architecture Decision Records

> Normative constraints documented in `docs/adrs/`.

_To be populated._
