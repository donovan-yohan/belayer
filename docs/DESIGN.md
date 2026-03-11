# Design

Patterns and conventions for the belayer codebase.

## Coordinator Pattern: Stripe-Style Blueprints

The belayer daemon is deterministic Go code (state machine + event loop). It spawns ephemeral Claude sessions ("agentic nodes") only for judgment calls:

1. **Sufficiency check**: Does this problem have enough context?
2. **Problem decomposition**: Break problem into per-repo climb specs
3. **Spotter**: Per-repo runtime validation (browser checks, dev server, console errors)
4. **Anchor** (formerly "alignment review"): Cross-repo alignment review
5. **Stuck analysis**: Why is a lead stuck? Suggest recovery

Agentic nodes receive structured input (from SQLite), produce structured output (written to SQLite), and exit.

## SQLite Schema

Defined across `internal/db/migrations/`. Key tables:

| Table | Purpose |
|-------|---------|
| `schema_migrations` | Tracks applied migration versions |
| `crags` | Long-lived workspaces with repos |
| `problems` | Work items submitted by users |
| `problem_repos` | Per-repo decomposition of a problem |
| `leads` | Execution loop state per repo per problem |
| `events` | Audit trail of all state changes |
| `agentic_decisions` | Outputs from ephemeral Claude sessions |

Uses pure Go SQLite (`modernc.org/sqlite`) — no CGO required. WAL mode and foreign keys enabled.

## Three-Layer Validation Pipeline

Validation flows through three layers after a lead completes a climb:

1. **Lead** (self-check): Runs build + tests, writes `TOP.json` on completion
2. **Spotter** (per-repo validation): Pre-created window activated when all climbs for a repo top. Validates the repo's complete body of work against the PRD. Writes `SPOT.json`. On failure, spotter feedback is injected into the lead prompt on retry.
3. **Anchor** (cross-repo alignment): Reviews all repos for consistency after all leads/spotters pass. Writes `VERDICT.json`.

### Signal Files

| File | Writer | Schema |
|------|--------|--------|
| `TOP.json` | Lead | `{ "status": "complete"/"failed", "summary": "...", "files_changed": [...] }` |
| `SPOT.json` | Spotter | `{ "pass": true/false, "project_type": "frontend", "issues": [...], "screenshots": [...] }` |
| `VERDICT.json` | Anchor | `{ "verdict": "approve"/"reject", "repos": { ... } }` |

## Mail System

Filesystem-backed inter-agent messaging. Messages are JSON files in per-address directories.

### Architecture

- **Storage**: JSON files in `<cragDir>/mail/<address>/unread/` and `read/`. No external processes.
- **Delivery**: Sender-driven via tmux send-keys. `belayer message` writes to filesystem AND delivers a nudge in one operation.
- **Identity**: `BELAYER_MAIL_ADDRESS` env var set per-window via inline export at spawn time. Agents derive identity automatically.
- **Templates**: Embedded via `embed.FS` at `internal/mail/templates/`. Prepend actionable instructions at send time.

### Message Types

| Type | Direction | Purpose |
|------|-----------|---------|
| `climb_assignment` | belayer → lead | New or updated work |
| `done` | lead → belayer | Climb completion signal |
| `spot_result` | spotter → belayer | Validation result |
| `verdict` | anchor → belayer | Alignment result |
| `feedback` | belayer → lead | Spotter/anchor feedback for retry |
| `instruction` | user/setter → any | Ad-hoc command or info |

### Addresses

Path-like strings that deterministically map to tmux targets: `belayer`, `problem/<id>/lead/<repo>/<climb>`, `problem/<id>/spotter/<repo>`, `problem/<id>/anchor`.

## Lead Execution

Leads run as full interactive Claude Code sessions (not `claude -p`):
- Role instructions passed via `--append-system-prompt` (preserves built-in Claude Code behavior + user plugins)
- Climb context written to `.lead/<climbID>/GOAL.json` (climb-scoped to avoid collisions between concurrent same-repo climbs)
- Spawned via `claude --dangerously-skip-permissions --append-system-prompt "role instructions" "initial prompt"` in tmux
- Initial prompt drives harness workflow: `/harness:init` → `/harness:plan` → `/harness:orchestrate` → `/harness:complete`
- Writes `TOP.json` in `.lead/<climbID>/` on completion
- Stuck detection: log file mtime silence monitoring + pane capture for input prompt detection
- Tmux windows use `remain-on-exit on` for exit status inspection via `#{pane_dead}`
- .lead/ directory maintained for crash recovery

## Repo Isolation

Follows extend-cli pattern:
- Bare repos in `repos/` directory (shared object storage)
- Git worktrees per problem in `problems/<problem-id>/<repo-name>/`
- Selective repo creation (only repos relevant to the problem)

## Agentic Node Contract

Each agentic node:
- Receives a structured prompt (Go template with context from SQLite)
- Runs as `claude -p --model <model> <prompt>` (no `--output-format json` — raw text avoids double-JSON-escaping and truncation)
- Produces structured output (JSON to stdout, may be wrapped in markdown code fences)
- `StripMarkdownJSON()` regex strips code fences before parsing
- Process group isolation: `Setpgid: true` + custom `Cancel` kills entire process tree on context cancellation (prevents orphaned `claude` processes)
- Results are parsed and stored in SQLite (`agentic_decisions` table)

Node types: sufficiency check, problem decomposition, **spotter** (per-repo runtime validation), **anchor** (cross-repo alignment), stuck analysis.

## Problem Intake Pipeline

The intake pipeline (`internal/intake/`) handles pre-belayer problem preparation:

- **Text input**: Direct description via CLI args
- **Jira input**: Comma-separated ticket IDs via `--jira` flag, grouped into single problem
- **Sufficiency check**: Agentic node at CLI level (before belayer daemon) evaluates context completeness
- **Interactive brainstorm**: When insufficient, presents gap questions to user via stdin/stdout Q&A loop
- **`--no-brainstorm` flag**: Skips brainstorm for CI/non-interactive usage
- **Crag-aware**: Passes available repo names to sufficiency prompt for better evaluation
- **`AgenticExecutor` interface**: Decouples from real claude for testability

The intake runs before the belayer daemon starts. The `sufficiency_checked` flag on the problem prevents redundant re-checking within the daemon.

## Belayer Daemon

The belayer daemon (`internal/belayer/`) is the central orchestration layer:

- **State machine**: Polls SQLite on a configurable interval (default 2s), drives problems through `pending -> decomposing -> running -> aligning -> complete/failed`
- **Lead management**: Spawns leads in tmux windows, tracks active leads with `sync.Mutex` protected map
- **Crash recovery**: Detects lead failures, schedules retry with exponential backoff (`min(base * 2^attempt, max)`)
- **Agentic nodes**: Runs ephemeral `claude -p --model <model> <prompt>` for judgment calls; `StripMarkdownJSON()` handles code fences; stores all decisions in `agentic_decisions` table
- **Crag-aware decomposition**: Decomposition prompt includes available repo names from crag config, constraining the agentic node to valid repos
- **Sufficiency skip**: Problems pre-checked at intake skip the daemon's sufficiency check
- **Interfaces**: `LeadRunner`, `WorktreeCreator`, `DiffCollector`, and `PRCreator` interfaces enable mock-based testing
- **Cross-repo alignment (anchor)**: When all leads complete, collects git diffs from worktrees, evaluates per-criterion alignment (API contracts, shared types, feature parity, integration points), re-dispatches misaligned repos with feedback on failure, creates PRs on success
- **Anchor retry**: Max alignment attempts (default 2) prevents infinite re-dispatch loops; alignment attempts counted via `alignment_started` events
- **PR creation**: Best-effort push + `gh pr create` per repo; PR URLs recorded as `prs_created` events; failures don't block problem completion

## CLI Commands

| Command | Purpose |
|---------|---------|
| `belayer init` | Initialize global config |
| `belayer crag create <name> --repo <url>` | Create a new crag with repos |
| `belayer belayer start` | Start the belayer daemon (DAG executor) |
| `belayer problem create [desc]` | Create problem with intake pipeline |
| `belayer problem retry [problem-id]` | Retry a failed problem (reuses enriched description) |
| `belayer problem list` | List problems for the current crag |
| `belayer message <addr> --type <type> --body "..."` | Send a typed mail message to an agent |
| `belayer mail read` | Read all unread messages (marks as read) |
| `belayer mail inbox` | List unread messages without marking read |
| `belayer mail ack <id>` | Mark a specific message as read |
| `belayer status` | Quick status overview |
| `belayer setter` | Launch interactive Claude session with belayer context (.claude/ workspace) |

## Setter Session Context

`belayer setter` creates a temp workspace with a full `.claude/` directory:
- **CLAUDE.md** (templated): Rendered from `internal/defaults/claudemd/setter.md` with crag name and repo names. Establishes the setter as the session identity — all user requests are routed through belayer commands.
- **Commands** (static): 6 slash commands (`/status`, `/problem-create`, `/problem-list`, `/logs`, `/message`, `/mail`) copied from `internal/defaults/commands/`.
- **BELAYER_CRAG env var**: Set in the exec environment so all belayer CLI commands auto-resolve the crag without `--crag` flags.

This is distinct from the repo's own `.claude/CLAUDE.md` which is for developing belayer itself. The setter context is runtime — deployed into sessions belayer spawns.

## Process Lifecycle

All child processes (`claude -p`, lead shell scripts) are spawned with `Setpgid: true` to create independent process groups. On context cancellation (Ctrl+C), the custom `cmd.Cancel` sends `SIGKILL` to the entire process group (negative PID), ensuring no orphaned grandchild processes survive.

## Config System

Resolution chain: crag config > global config > embedded defaults.

- `belayerconfig.Load()` merges TOML configs following the chain
- `belayerconfig.LoadProfile()` resolves validation profiles from config dirs
- Embedded defaults via Go `embed.FS` in `internal/defaults/`
- `belayer.toml` schema: `[agents]`, `[execution]`, `[validation]`, `[anchor]` sections
- **Validation profiles**: Human-readable TOML checklists the LLM interprets

## Environment Preparation

Instead of prompt templates, agents receive context through worktree files:

- **`.claude/CLAUDE.md`**: Role-specific instructions auto-loaded by Claude Code. Embedded templates in `internal/defaults/claudemd/`. Prepended to existing CLAUDE.md if present.
- **`.lead/GOAL.json`**: Structured context (types in `internal/climbctx/`): `LeadClimb`, `SpotterClimb`, `AnchorClimb` with role-specific fields.
- **`.lead/profiles/*.toml`**: Validation profiles written for spotter agent discovery.
- The belayer daemon writes these files before spawning each agent.

## Naming Convention

Climbing metaphors throughout:

| Name | Role |
|------|------|
| **Crag** | Long-lived workspace (repos, config, database) |
| **Problem** | Work item submitted by the user |
| **Climb** | Per-repo subtask derived from a problem |
| **Setter** | Interactive user session for creating problems |
| **Belayer** | Daemon / DAG executor |
| **Lead** | Implementation agent (per-repo) |
| **Spotter** | Per-repo runtime validator |
| **Anchor** | Cross-repo alignment reviewer |

> The **setter** defines **problems** at the **crag**. The **belayer** sends **leads** up their **climbs**. When they **top** out, the **spotter** validates. If no retries were needed, it was **flashed**.

## See Also

- [Architecture](ARCHITECTURE.md) — module boundaries and data flow
- [Quality](QUALITY.md) — testing strategy
