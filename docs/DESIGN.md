# Design

Patterns and conventions for the belayer codebase.

## Coordinator Pattern: Stripe-Style Blueprints

The coordinator is deterministic Go code (state machine + event loop). It spawns ephemeral Claude sessions ("agentic nodes") only for judgment calls:

1. **Sufficiency check**: Does this task have enough context?
2. **Task decomposition**: Break task into per-repo specs
3. **Spotter**: Per-goal runtime validation (browser checks, dev server, console errors)
4. **Anchor** (formerly "alignment review"): Cross-repo alignment review
5. **Stuck analysis**: Why is a lead stuck? Suggest recovery

Agentic nodes receive structured input (from SQLite), produce structured output (written to SQLite), and exit.

## SQLite Schema

Defined across `internal/db/migrations/`. Key tables:

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

## Three-Layer Validation Pipeline

Validation flows through three layers after a lead completes a goal:

1. **Lead** (self-check): Runs build + tests, writes `DONE.json` on completion
2. **Spotter** (per-goal validation): Reuses the lead's tmux window with fresh agent context. Performs runtime validation (browser checks, dev server, console errors). Writes `SPOT.json`. On failure, spotter feedback is injected into the lead prompt on retry.
3. **Anchor** (cross-repo alignment): Reviews all repos for consistency after all leads/spotters pass. Writes `VERDICT.json`.

### Signal Files

| File | Writer | Schema |
|------|--------|--------|
| `DONE.json` | Lead | `{ "status": "complete"/"failed", "summary": "...", "files_changed": [...] }` |
| `SPOT.json` | Spotter | `{ "pass": true/false, "project_type": "frontend", "issues": [...], "screenshots": [...] }` |
| `VERDICT.json` | Anchor | `{ "verdict": "approve"/"reject", "repos": { ... } }` |

## Lead Execution

Leads run as full interactive Claude Code sessions (not `claude -p`):
- Setter prepares worktree with `.claude/CLAUDE.md` (auto-loaded by Claude Code) + `.lead/GOAL.json` (structured context)
- Spawned via `claude --dangerously-skip-permissions "initial prompt"` in tmux
- Agents use the full Claude Code environment: CLAUDE.md, skills, MCP tools, harness workflow
- Writes `DONE.json` signal file on completion
- Stuck detection: log file mtime silence monitoring + pane capture for input prompt detection
- Tmux windows use `remain-on-exit on` for exit status inspection via `#{pane_dead}`
- .lead/ directory maintained for crash recovery

## Repo Isolation

Follows extend-cli pattern:
- Bare repos in `repos/` directory (shared object storage)
- Git worktrees per task in `tasks/<task-id>/<repo-name>/`
- Selective repo creation (only repos relevant to the task)

## Agentic Node Contract

Each agentic node:
- Receives a structured prompt (Go template with context from SQLite)
- Runs as `claude -p --model <model> <prompt>` (no `--output-format json` — raw text avoids double-JSON-escaping and truncation)
- Produces structured output (JSON to stdout, may be wrapped in markdown code fences)
- `StripMarkdownJSON()` regex strips code fences before parsing
- Process group isolation: `Setpgid: true` + custom `Cancel` kills entire process tree on context cancellation (prevents orphaned `claude` processes)
- Results are parsed and stored in SQLite (`agentic_decisions` table)

Node types: sufficiency check, task decomposition, **spotter** (per-goal runtime validation), **anchor** (cross-repo alignment), stuck analysis.

## Task Intake Pipeline

The intake pipeline (`internal/intake/`) handles pre-coordinator task preparation:

- **Text input**: Direct description via CLI args
- **Jira input**: Comma-separated ticket IDs via `--jira` flag, grouped into single task
- **Sufficiency check**: Agentic node at CLI level (before coordinator) evaluates context completeness
- **Interactive brainstorm**: When insufficient, presents gap questions to user via stdin/stdout Q&A loop
- **`--no-brainstorm` flag**: Skips brainstorm for CI/non-interactive usage
- **Instance-aware**: Passes available repo names to sufficiency prompt for better evaluation
- **`AgenticExecutor` interface**: Decouples from real claude for testability

The intake runs before the coordinator starts. The `sufficiency_checked` flag on the task prevents redundant re-checking within the coordinator.

## Coordinator Engine

The coordinator (`internal/coordinator/`) is the central orchestration layer:

- **State machine**: Polls SQLite on a configurable interval (default 2s), drives tasks through `pending -> decomposing -> running -> aligning -> complete/failed`
- **Lead management**: Spawns leads as goroutines via `lead.Runner.Run()`, tracks active leads with `sync.Mutex` protected map
- **Crash recovery**: Detects lead failures, schedules retry with exponential backoff (`min(base * 2^attempt, max)`)
- **Agentic nodes**: Runs ephemeral `claude -p --model <model> <prompt>` for judgment calls; `StripMarkdownJSON()` handles code fences; stores all decisions in `agentic_decisions` table
- **Instance-aware decomposition**: Decomposition prompt includes available repo names from instance config, constraining the agentic node to valid repos
- **Sufficiency skip**: Tasks pre-checked at intake skip the coordinator's sufficiency check
- **Interfaces**: `LeadRunner`, `WorktreeCreator`, `DiffCollector`, and `PRCreator` interfaces enable mock-based testing
- **Cross-repo alignment (anchor)**: When all leads complete, collects git diffs from worktrees, evaluates per-criterion alignment (API contracts, shared types, feature parity, integration points), re-dispatches misaligned repos with feedback on failure, creates PRs on success
- **Anchor retry**: Max alignment attempts (default 2) prevents infinite re-dispatch loops; alignment attempts counted via `alignment_started` events
- **PR creation**: Best-effort push + `gh pr create` per repo; PR URLs recorded as `prs_created` events; failures don't block task completion

## TUI Dashboard

The TUI (`internal/tui/`) is a bubbletea-based terminal dashboard:

- **Read-only**: Polls SQLite at 1-second intervals via `tea.Tick`; never writes to the database
- **Three-pane layout**: Task list (left, ~30% width), task detail with leads (right, ~70%), event log (bottom, ~30% height)
- **Keyboard navigation**: j/k for up/down, tab to cycle panes, enter to select, esc to go back, 1/2/3 to jump to pane, q to quit
- **Responsive**: Adapts to terminal size via `tea.WindowSizeMsg`
- **TUI-specific store**: `tui.Store` wraps `*sql.DB` with read-only queries optimized for the dashboard (denormalized `TaskSummary`, `LeadDetail`, `EventEntry`)
- **Status badges**: Color-coded per status using lipgloss (green=complete, yellow=running, red=failed, orange=stuck)
- **Event scrolling**: Events pane shows scroll indicators (`↑ N more above` / `↓ N more below`) and position (`Events (3/10)`) when focused
- **Testing**: Temp-file SQLite (not `:memory:`) for tests due to connection pool isolation issues

## CLI Commands

| Command | Purpose |
|---------|---------|
| `belayer init <name> --repo <url>` | Create a new instance with repos; writes config directory to disk |
| `belayer setter` | Start the setter daemon (DAG executor) |
| `belayer task create [desc]` | Create task with intake pipeline + start coordinator |
| `belayer task retry [task-id]` | Retry a failed task (reuses enriched description) |
| `belayer task list` | List tasks for the current instance |
| `belayer tui` | Launch the monitoring dashboard |
| `belayer status` | Quick status overview |

## Process Lifecycle

All child processes (`claude -p`, lead shell scripts) are spawned with `Setpgid: true` to create independent process groups. On context cancellation (Ctrl+C), the custom `cmd.Cancel` sends `SIGKILL` to the entire process group (negative PID), ensuring no orphaned grandchild processes survive.

## Config System

Resolution chain: instance config > global config > embedded defaults.

- `belayerconfig.Load()` merges TOML configs following the chain
- `belayerconfig.LoadProfile()` resolves validation profiles from config dirs
- Embedded defaults via Go `embed.FS` in `internal/defaults/`
- `belayer.toml` schema: `[agents]`, `[execution]`, `[validation]`, `[anchor]` sections
- **Validation profiles**: Human-readable TOML checklists the LLM interprets

## Environment Preparation

Instead of prompt templates, agents receive context through worktree files:

- **`.claude/CLAUDE.md`**: Role-specific instructions auto-loaded by Claude Code. Embedded templates in `internal/defaults/claudemd/`. Prepended to existing CLAUDE.md if present.
- **`.lead/GOAL.json`**: Structured context (types in `internal/goalctx/`): `LeadGoal`, `SpotterGoal`, `AnchorGoal` with role-specific fields.
- **`.lead/profiles/*.toml`**: Validation profiles written for spotter agent discovery.
- The setter writes these files before spawning each agent.

## Naming Convention

Climbing metaphors throughout:

| Name | Role |
|------|------|
| **Setter** | Daemon / DAG executor |
| **Lead** | Implementation agent (per-repo) |
| **Spotter** | Per-goal runtime validator |
| **Anchor** | Cross-repo alignment reviewer |

## See Also

- [Architecture](ARCHITECTURE.md) — module boundaries and data flow
- [TUI](TUI.md) — bubbletea component details
- [Quality](QUALITY.md) — testing strategy
