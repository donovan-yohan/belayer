# CLI and Data Layer: Pure Data Publisher

**Date**: 2026-03-07
**Status**: Implementing
**Parent**: [Agent-Friendly Architecture Design](2026-03-07-agent-friendly-architecture-design.md)
**PRD Goal**: 1 - CLI and data layer

## Problem

The existing CLI spawns `claude -p` sessions inline, has interactive brainstorm Q&A, and uses a coordinator/intake/lead pipeline that creates Claude-in-Claude nesting. Goal 1 transforms the CLI into a pure data publisher: validate inputs, write to SQLite, exit.

## Scope

- New SQLite schema (clean slate)
- New model types
- New store for CRUD operations
- CLI rewrites: `belayer task create --spec --goals`, `belayer init`, `belayer instance create`
- Remove old packages: coordinator, intake, lead (not needed until later goals)
- Keep: config, db (migration engine), instance (directory/worktree management), repo

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Migration strategy | Replace all old migrations with single clean one | Clean slate rebuild; no data to migrate |
| Store location | `internal/store/store.go` | Flat package replacing coordinator/store and lead/store |
| Task ID format | `task-<unix-nano>` | Simple, unique, sortable; consistent with existing pattern |
| Goal ID format | User-provided from goals.json | Goals have meaningful IDs like `api-1`; no auto-generation |
| Spec storage | Store spec content in tasks table | Avoid file-path dependencies; spec is small enough for SQLite |
| Goals JSON validation | Validate structure on ingest, store raw JSON in task + parsed rows in goals table | Dual storage: raw for audit, parsed for DAG operations |

## SQLite Schema

Single migration replacing all existing ones:

```sql
CREATE TABLE instances (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    path TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    instance_id TEXT NOT NULL REFERENCES instances(id),
    spec TEXT NOT NULL,
    goals_json TEXT NOT NULL,
    jira_ref TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE goals (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    repo_name TEXT NOT NULL,
    description TEXT NOT NULL,
    depends_on TEXT DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'pending',
    attempt INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);

CREATE TABLE events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    goal_id TEXT DEFAULT '',
    type TEXT NOT NULL,
    payload TEXT DEFAULT '{}',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE spotter_reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES tasks(id),
    attempt INTEGER NOT NULL,
    verdict TEXT NOT NULL,
    output TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## CLI Commands

### `belayer task create`

```
belayer task create --spec spec.md --goals goals.json [--jira PROJ-123] [--instance foo]
```

1. Resolve instance name (flag or default)
2. Validate `--spec` file exists and is readable
3. Validate `--goals` file exists and parses as valid GoalsFile JSON
4. Validate goal IDs are unique and `depends_on` references exist
5. Read spec content and goals JSON
6. Open instance DB, insert task row, insert goal rows
7. Emit `task_created` event
8. Print task ID and exit

No `claude -p`. No inference. No interactive prompts.

### `belayer init` (unchanged)

Creates `~/.belayer/` and default `config.json`.

### `belayer instance create` (unchanged)

Creates instance directory, bare clones repos, initializes DB, writes instance.json.

### `belayer task list` (simplified)

Lists tasks with goal counts. No coordinator/lead dependencies.

### `belayer status` (simplified)

Shows task and goal status. No coordinator/lead dependencies.

## Goals JSON Schema

```json
{
  "repos": {
    "<repo-name>": {
      "goals": [
        {
          "id": "<unique-id>",
          "description": "<what to do>",
          "depends_on": ["<goal-id>", ...]
        }
      ]
    }
  }
}
```

Validation rules:
- All goal IDs must be globally unique across repos
- `depends_on` references must point to existing goal IDs
- `depends_on` can only reference goals within the same repo
- Repo names in goals.json must match repos configured in the instance

## Model Types

```go
type TaskStatus string
const (
    TaskStatusPending   TaskStatus = "pending"
    TaskStatusRunning   TaskStatus = "running"
    TaskStatusReviewing TaskStatus = "reviewing"
    TaskStatusComplete  TaskStatus = "complete"
    TaskStatusStuck     TaskStatus = "stuck"
)

type GoalStatus string
const (
    GoalStatusPending  GoalStatus = "pending"
    GoalStatusRunning  GoalStatus = "running"
    GoalStatusComplete GoalStatus = "complete"
    GoalStatusFailed   GoalStatus = "failed"
)

type EventType string
const (
    EventTaskCreated    EventType = "task_created"
    EventGoalStarted   EventType = "goal_started"
    EventGoalCompleted EventType = "goal_completed"
    EventGoalFailed    EventType = "goal_failed"
    EventSpotterSpawned EventType = "spotter_spawned"
    EventReviewVerdict  EventType = "review_verdict"
    EventPRCreated     EventType = "pr_created"
)
```

## Packages Removed

- `internal/coordinator/` - Replaced by setter daemon (Goal 2)
- `internal/intake/` - Replaced by belayer manage (Goal 5)
- `internal/lead/` - Replaced by lead spawning (Goal 3)
- `internal/tui/` - Will be rebuilt for Goal 2+ (keep for now but decouple)

## What Stays

- `internal/config/` - Global config management (unchanged)
- `internal/db/` - Migration engine (unchanged, new migration files)
- `internal/instance/` - Directory structure and worktree management (unchanged)
- `internal/repo/` - Git operations (unchanged)
- `internal/testutil/` - Test helpers (unchanged)
