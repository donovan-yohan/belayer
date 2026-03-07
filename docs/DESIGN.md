# Design

Patterns and conventions for the belayer codebase.

## Coordinator Pattern: Stripe-Style Blueprints

The coordinator is deterministic Go code (state machine + event loop). It spawns ephemeral Claude sessions ("agentic nodes") only for judgment calls:

1. **Sufficiency check**: Does this task have enough context?
2. **Task decomposition**: Break task into per-repo specs
3. **Alignment review**: Are cross-repo implementations consistent?
4. **Stuck analysis**: Why is a lead stuck? Suggest recovery

Agentic nodes receive structured input (from SQLite), produce structured output (written to SQLite), and exit.

## SQLite Schema

Defined in `internal/db/migrations/001_initial.sql`. Key tables:

| Table | Purpose |
|-------|---------|
| `schema_migrations` | Tracks applied migration versions |
| `instances` | Long-lived workspaces with repos |
| `tasks` | Work items submitted by users |
| `task_repos` | Per-repo decomposition of a task |
| `leads` | Execution loop state per repo per task |
| `events` | Audit trail of all state changes |
| `agentic_decisions` | Outputs from ephemeral Claude sessions |

Uses pure Go SQLite (`modernc.org/sqlite`) — no CGO required. WAL mode and foreign keys enabled.

## Lead Execution Loop

Bundled lead loop (enhanced from llm-agents lead plugin):
- Execute -> Review -> Verdict cycle per goal
- Writes structured progress to SQLite (not just files)
- Emits events on state changes
- .lead/ directory maintained for crash recovery

## Repo Isolation

Follows extend-cli pattern:
- Bare repos in `repos/` directory (shared object storage)
- Git worktrees per task in `tasks/<task-id>/<repo-name>/`
- Selective repo creation (only repos relevant to the task)

## Agentic Node Contract

Each agentic node:
- Receives a structured prompt (Go template with context from SQLite)
- Runs as `claude -p --model <model> <prompt>`
- Produces structured output (JSON written to stdout or file)
- Has a timeout and retry limit
- Results are parsed and stored in SQLite

## Coordinator Engine

The coordinator (`internal/coordinator/`) is the central orchestration layer:

- **State machine**: Polls SQLite on a configurable interval (default 2s), drives tasks through `pending -> decomposing -> running -> aligning -> complete/failed`
- **Lead management**: Spawns leads as goroutines via `lead.Runner.Run()`, tracks active leads with `sync.Mutex` protected map
- **Crash recovery**: Detects lead failures, schedules retry with exponential backoff (`min(base * 2^attempt, max)`)
- **Agentic nodes**: Runs ephemeral `claude -p --model <model> --output-format json <prompt>` for judgment calls; stores all decisions in `agentic_decisions` table
- **Interfaces**: `LeadRunner` and `WorktreeCreator` interfaces enable mock-based testing

## See Also

- [Architecture](ARCHITECTURE.md) — module boundaries and data flow
- [Quality](QUALITY.md) — testing strategy
