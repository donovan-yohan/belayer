# Architecture

This document describes the high-level architecture of belayer.

## Bird's Eye View

Belayer is a standalone Go CLI that orchestrates autonomous coding agents across multiple repositories. A user creates a long-lived crag (configured with target repos), submits work problems (text or Jira tickets), and the belayer daemon decomposes problems into per-repo climbs, spawns lead execution loops in isolated git worktrees, monitors progress via SQLite, and validates cross-repo alignment using ephemeral Claude sessions before creating PRs.

Input: work items (text, Jira tickets), user clarifications during brainstorm.
Output: per-repo PRs with aligned implementations, structured progress reports.

## Orchestration Layers

Belayer uses climbing metaphors for its agent hierarchy:

```
User (CLI / TUI)
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
| CLI commands | `internal/cli/` | Cobra command definitions (root, init, crag, problem, status, tui, belayer, setter, message, mail) |
| Belayer Config | `internal/belayerconfig/` | Config loader with resolution chain (crag > global > embedded defaults) |
| Config | `internal/config/` | Global config loading/saving (`~/.belayer/config.json`) |
| Defaults | `internal/defaults/` | Embedded default config files (belayer.toml, CLAUDE.md templates, validation profiles, setter session commands) via `embed.FS` |
| Manage | `internal/manage/` | Setter session workspace preparation (PrepareManageDir: renders CLAUDE.md template, copies slash commands) |
| Climb Context | `internal/climbctx/` | GOAL.json types (LeadClimb, SpotterClimb, AnchorClimb) and writer |
| Database | `internal/db/` | SQLite connection, migration runner, embedded SQL |
| Migrations | `internal/db/migrations/` | SQL migration files (001_initial.sql, 002_rename_crag.sql) |
| Model | `internal/model/` | Domain types and status enums |
| Instance | `internal/instance/` | Crag lifecycle (create, load, delete, worktree management) |
| Repo | `internal/repo/` | Git operations (bare clone, worktree add/remove/list) |
| Belayer | `internal/belayer/` | DAG executor daemon. Manages leads, spotters, and anchors |
| Lead | `internal/lead/` | Lead execution runner, store, ClaudeSpawner (interactive sessions via tmux) |
| Spotter | `internal/spotter/` | Per-repo runtime validator. Project type detection, validation profiles, SPOT.json types |
| Anchor | `internal/anchor/` | Cross-repo alignment reviewer. Verdict types (VerdictJSON, RepoVerdict) |
| Intake | `internal/intake/` | Problem intake pipeline (text/Jira parsing, sufficiency check, interactive brainstorm) |
| Coordinator | `internal/coordinator/` | Coordinator engine (state machine, agentic nodes, retry scheduler) |
| Mail | `internal/mail/` | Filesystem-backed inter-agent mail system (message types, address resolution, FileStore, templates, tmux delivery, send/read) |
| TUI | `internal/tui/` | bubbletea dashboard (model, views, styles, keys, read-only store) |

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
  instance.json                       # Crag config (repos, settings)
  belayer.db                          # SQLite database
  config/                             # Per-crag overrides (optional)
  mail/                               # Filesystem mail store (per-address unread/read dirs)
  repos/                              # Bare repo clones
    <repo-name>.git
  problems/                           # Per-problem worktrees
    <problem-id>/
      <repo-name>/                    # Git worktree
        .lead/                        # Lead state directory
```

Config resolution: crag config > global config > embedded defaults (via `internal/defaults/`)

## Architecture Decision Records

> Normative constraints documented in `docs/adrs/`.

_To be populated._
