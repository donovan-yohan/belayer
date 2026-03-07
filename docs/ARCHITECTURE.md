# Architecture

This document describes the high-level architecture of belayer.

## Bird's Eye View

Belayer is a standalone Go CLI that orchestrates autonomous coding agents across multiple repositories. A user creates a long-lived instance (configured with target repos), submits work tasks (text or Jira tickets), and belayer decomposes tasks into per-repo subtasks, spawns lead execution loops in isolated git worktrees, monitors progress via SQLite, and validates cross-repo alignment using ephemeral Claude sessions before creating PRs.

Input: work items (text, Jira tickets), user clarifications during brainstorm.
Output: per-repo PRs with aligned implementations, structured progress reports.

## Orchestration Layers

```
User (CLI / TUI)
  |
  v
Coordinator (deterministic Go state machine)
  |-- Polls SQLite for lead state changes
  |-- Spawns/monitors lead processes
  |-- Triggers agentic nodes (ephemeral Claude sessions)
  |
  v
Lead (bundled execution loop per repo)
  |-- Runs in isolated git worktree
  |-- Executes goals via claude -p
  |-- Writes progress to SQLite + .lead/ files
```

## Code Map

_To be populated as modules are implemented._

## Data Flow

```
Task Input --> Sufficiency Check (agentic) --> Decomposition (agentic)
                                                    |
                  +------------------+--------------+
                  v                  v              v
            Lead(repo-A)       Lead(repo-B)    Lead(repo-C)
            writes SQLite      writes SQLite   writes SQLite
                  |                  |              |
                  +--------+---------+--------------+
                           v
                 Coordinator detects "all done"
                           |
                           v
                 Alignment Review (agentic)
                      |          |
                    PASS       FAIL
                      |          |
                 Create PRs  Re-dispatch with feedback
```

## Directory Layout

```
~/.belayer/                           # Global config
  config.json                         # Default settings, instance registry

~/.belayer/instances/<name>/          # Long-lived instance
  instance.json                       # Instance config (repos, settings)
  belayer.db                          # SQLite database
  repos/                              # Bare repo clones
    <repo-name>.git
  tasks/                              # Per-task worktrees
    <task-id>/
      <repo-name>/                    # Git worktree
        .lead/                        # Lead state directory
```

## Architecture Decision Records

> Normative constraints documented in `docs/adrs/`.

_To be populated._
