# Design: Project Scaffolding & Core Architecture

**Goal**: 1 — Project scaffolding & core architecture
**Date**: 2026-03-06
**Status**: Active

## Objective

Establish the foundational project structure, CLI entry points, SQLite schema, and config management that all subsequent goals build upon.

## Decisions

### CLI Framework: cobra
- Standard Go CLI framework with subcommand support
- Natural fit for `belayer init`, `belayer instance create`, etc.
- Well-supported, widely used in Go ecosystem

### SQLite Library: modernc.org/sqlite
- Pure Go SQLite (no CGO required) — simplifies cross-compilation
- If performance becomes an issue, can swap to mattn/go-sqlite3 later

### Migration Strategy: Embedded SQL files
- SQL migration files embedded via `embed.FS`
- Simple up-only migrations with version tracking in a `schema_migrations` table
- No external migration tool needed

### Config Format: JSON
- `~/.belayer/config.json` for global config
- Standard `encoding/json` — no external dependency
- Config struct with defaults; created on first `init`

### Project Layout
```
cmd/
  belayer/
    main.go              # Entry point
internal/
  cli/                   # Cobra command definitions
    root.go
    init.go
    instance.go
    task.go
    status.go
    tui.go
  config/                # Config loading/saving
    config.go
  db/                    # SQLite schema, migrations, queries
    db.go
    migrations/
      001_initial.sql
  model/                 # Domain types (Instance, Task, Lead, etc.)
    types.go
pkg/                     # Public API (empty for now, reserved)
```

## SQLite Schema (Initial)

Six core tables covering the full domain model:

- **instances**: Long-lived workspace with repos
- **tasks**: Work items submitted by users
- **task_repos**: Per-repo decomposition of a task
- **leads**: Execution loop per repo per task
- **events**: Audit trail of all state changes
- **agentic_decisions**: Outputs from ephemeral Claude sessions

Plus `schema_migrations` for tracking applied migrations.

## Config Schema

```json
{
  "default_instance": "",
  "instances": {
    "<name>": "/path/to/instance"
  }
}
```

Global config is a registry of known instances. Instance-specific config lives in `instance.json` within the instance directory (Goal 2).

## CLI Commands (Stubs)

All commands are registered but only `init` has real logic in this goal. Others print "not implemented yet" until their respective goals.

| Command | Subcommand | Description |
|---------|-----------|-------------|
| `init` | - | Create `~/.belayer/` directory and config |
| `instance` | `create` | Create a new instance (Goal 2) |
| `task` | `create` | Create a new task (Goal 5) |
| `status` | - | Show task/lead status (Goal 4) |
| `tui` | - | Launch TUI dashboard (Goal 7) |

## Risks & Mitigations

- **Risk**: Pure Go SQLite may be slower than CGO version
  - **Mitigation**: Fine for orchestration workload; swap later if needed
- **Risk**: Schema may need changes in later goals
  - **Mitigation**: Migration system supports adding new migrations
