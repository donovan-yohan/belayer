# Architecture

This document describes the high-level architecture of belayer.

## Bird's Eye View

Belayer is a standalone Go CLI that orchestrates autonomous coding agents across multiple repositories. A user creates a long-lived instance (configured with target repos), submits work tasks (text or Jira tickets), and belayer decomposes tasks into per-repo subtasks, spawns lead execution loops in isolated git worktrees, monitors progress via SQLite, and validates cross-repo alignment using ephemeral Claude sessions before creating PRs.

Input: work items (text, Jira tickets), user clarifications during brainstorm.
Output: per-repo PRs with aligned implementations, structured progress reports.

## Orchestration Layers

Belayer uses climbing metaphors for its agent hierarchy:

```
User (CLI / TUI)
  |
  v
Setter (DAG executor daemon — manages routes/tasks)
  |-- Polls SQLite for state changes
  |-- Spawns/monitors leads, spotters, anchors
  |-- Triggers agentic nodes (ephemeral Claude sessions)
  |
  v
Lead (bundled execution loop per repo — does the climbing)
  |-- Runs in isolated git worktree
  |-- Executes goals via claude -p with configurable prompt templates
  |-- Writes progress to SQLite + .lead/ files
  |-- Self-check: build + tests
  |
  v
Spotter (per-goal runtime validator — watches for problems)
  |-- Project type detection (frontend, backend, CLI, library)
  |-- Runs validation profile checklists
  |-- Produces SPOT.json verdict
  |
  v
Anchor (cross-repo alignment reviewer — ties all lines together)
  |-- Reviews changes across all repos for a task
  |-- Prompt builder + verdict types
  |-- PASS → create PRs, FAIL → re-dispatch with feedback
```

**Three-layer validation**: Lead (self-check) → Spotter (runtime validation) → Anchor (cross-repo alignment)

## Code Map

| Module | Path | Purpose |
|--------|------|---------|
| CLI entry | `cmd/belayer/main.go` | Binary entry point |
| CLI commands | `internal/cli/` | Cobra command definitions (root, init, instance, task, status, tui, setter) |
| Belayer Config | `internal/belayerconfig/` | Config loader with resolution chain (instance > global > embedded defaults) |
| Config | `internal/config/` | Global config loading/saving (`~/.belayer/config.json`) |
| Defaults | `internal/defaults/` | Embedded default config files (belayer.toml, prompt templates, validation profiles) via `embed.FS` |
| Database | `internal/db/` | SQLite connection, migration runner, embedded SQL |
| Migrations | `internal/db/migrations/` | SQL migration files (001_initial.sql, 002_lead_execution.sql, 003_task_intake.sql) |
| Model | `internal/model/` | Domain types and status enums |
| Instance | `internal/instance/` | Instance lifecycle (create, load, delete, worktree management) |
| Repo | `internal/repo/` | Git operations (bare clone, worktree add/remove/list) |
| Setter | `internal/setter/` | DAG executor daemon (was coordinator). Manages leads, spotters, and anchors |
| Lead | `internal/lead/` | Lead execution runner, store, embedded shell script, configurable prompt templates |
| Spotter | `internal/spotter/` | Per-goal runtime validator. Project type detection, validation profiles, SPOT.json types |
| Anchor | `internal/anchor/` | Cross-repo alignment reviewer. Prompt builder + verdict types |
| Intake | `internal/intake/` | Task intake pipeline (text/Jira parsing, sufficiency check, interactive brainstorm) |
| Coordinator | `internal/coordinator/` | Coordinator engine (state machine, agentic nodes, retry scheduler) |
| TUI | `internal/tui/` | bubbletea dashboard (model, views, styles, keys, read-only store) |

## Data Flow

```
Task Input (text/Jira) --> Intake Pipeline (sufficiency + brainstorm) --> Decomposition (agentic, instance-aware)
                                                    |
                  +------------------+--------------+
                  v                  v              v
            Lead(repo-A)       Lead(repo-B)    Lead(repo-C)
            self-check:       self-check:      self-check:
            build + tests     build + tests    build + tests
                  |                  |              |
                  v                  v              v
            Spotter(A)         Spotter(B)      Spotter(C)       ← "spotting" state
            runtime validate   runtime validate runtime validate
            → SPOT.json        → SPOT.json      → SPOT.json
                  |                  |              |
                  +--------+---------+--------------+
                           v
                 Setter detects "all spotted"
                           |
                           v
                 Anchor Review (cross-repo alignment)
                      |          |
                    PASS       FAIL
                      |          |
                 Create PRs  Re-dispatch with feedback
```

## Directory Layout

```
~/.belayer/
  config.json                         # Instance registry
  config/                             # Global defaults (written by `belayer init`)
    belayer.toml                      # Agent provider, concurrency, timeouts
    prompts/
      lead.md                         # Lead execution prompt template
      spotter.md                      # Spotter validation prompt template
      anchor.md                       # Anchor cross-repo review prompt template
    profiles/
      frontend.toml                   # Frontend validation checklist
      backend.toml                    # Backend API validation checklist
      cli.toml                        # CLI tool validation checklist
      library.toml                    # Library/package validation checklist

~/.belayer/instances/<name>/
  instance.json                       # Instance config (repos, settings)
  belayer.db                          # SQLite database
  config/                             # Per-instance overrides (optional)
  repos/                              # Bare repo clones
    <repo-name>.git
  tasks/                              # Per-task worktrees
    <task-id>/
      <repo-name>/                    # Git worktree
        .lead/                        # Lead state directory
```

Config resolution: instance config > global config > embedded defaults (via `internal/defaults/`)

## Architecture Decision Records

> Normative constraints documented in `docs/adrs/`.

_To be populated._
