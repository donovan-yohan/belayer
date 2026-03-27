# Design

Patterns and conventions for the belayer codebase.

## Coordinator Pattern: Stripe-Style Blueprints

The belayer daemon is deterministic Go code (state machine + event loop). It spawns ephemeral Claude sessions ("agentic nodes") only for judgment calls:

1. **Sufficiency check**: Does this problem have enough context?
2. **Problem decomposition**: Break problem into per-repo climb specs
3. **Per-repo quality gate**: Runtime validation (v1 code calls this "spotter" — three-phase model reserves "spotter" for multi-repo cross-repo validation; see Setter/Spotter Contracts below)
4. **Cross-repo validation**: Multi-repo alignment review (v1 code calls this "anchor" — three-phase model calls this "spotter")
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
| `environments` | Provider-managed environments (per-problem, stores JSON response) |

Uses pure Go SQLite (`modernc.org/sqlite`) — no CGO required. WAL mode and foreign keys enabled.

## Validation Pipeline

Validation flows through four layers. See [review-loops-test-infra-design](design-docs/2026-03-16-review-loops-test-infra-design.md) for full design.

1. **Lead** (multi-agent review loop): Runs implementation, then loops pr-review-toolkit agents (max 3 cycles) until all agents pass. Writes `TOP.json` on completion.
2. **Per-repo quality gate** (v1 code: "spotter"): Activated when all climbs for a repo complete. Validates spec compliance, test contract fulfillment, and runtime correctness. Writes `SPOT.json`. (Three-phase model renames this to a "review" gate node — see TODOS.md P1.)
3. **Cross-repo validation** (v1 code: "anchor", three-phase model: "spotter"): Multi-repo problems only. Reviews all repos for cross-repo consistency. Writes `VERDICT.json`. (Three-phase model merges this into the Spotter role.)
4. **Reflect** (learning capture): Classifies errors, extracts learnings to SQLite, surfaces system improvement recommendations. Runs after final validation, parallel with PR creation.

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

Leads run as full interactive agent sessions (not single-shot) via the `AgentSpawner` interface (`internal/lead/spawner.go`). The `agents.provider` config field selects the runtime:
- **Claude** (`ClaudeSpawner`): `claude --dangerously-skip-permissions --append-system-prompt "role" "prompt"` — role instructions via dedicated flag
- **Codex** (`CodexSpawner`): `codex --dangerously-bypass-approvals-and-sandbox "prompt"` — role instructions prepended to prompt (no system prompt flag)
- Factory function `NewSpawner(provider, tm)` in `spawner.go` selects the spawner based on config
- Climb context written to `.lead/<climbID>/GOAL.json` (climb-scoped to avoid collisions between concurrent same-repo climbs)
- Initial prompt drives harness workflow: `/harness:init` → `/harness:plan` → `/harness:orchestrate` → `/harness:complete`
- Writes `TOP.json` in `.lead/<climbID>/` on completion
- Stuck detection: log file mtime silence monitoring + pane capture for input prompt detection
- Tmux windows use `remain-on-exit on` for exit status inspection via `#{pane_dead}`
- .lead/ directory maintained for crash recovery

## Environment Provider

The daemon uses a single provider model for all environment lifecycle operations (creating worktrees, provisioning infrastructure). Instead of separate code paths for builtin vs. external environments, the daemon always shells out to a configured command and parses JSON responses.

- **Config**: `[environment]` section in `belayer.toml` specifies `command` (default: "belayer") and `subcommand` (default: "env")
- **Default provider**: `belayer env` wraps existing bare-repo + worktree logic behind the JSON contract
- **External providers**: e.g., `extend env` for fullstack projects with Docker, DB, port isolation
- **Swapping is config-only**: Change `command = "extend"` in belayer.toml — no code changes
- **Worktree paths as data**: Daemon stores paths returned by the provider, not computed from convention
- **Per-problem environments**: Leads within a problem share infra; simultaneous problems get separate environments
- **JSON contract**: `create`, `add-worktree`, `remove-worktree`, `reset`, `destroy`, `status`, `logs`, `list` — all with `--json` flag
- **Cleanup on failure**: If CreateEnv succeeds but AddWorktree fails, environment is destroyed (no orphans)

## Repo Isolation

Follows extend-cli pattern:
- Bare repos in `repos/` directory (shared object storage)
- Git worktrees per problem in `tasks/<problem-id>/<repo-name>/`
- Selective repo creation (only repos relevant to the problem)

## Agentic Node Contract

Each agentic node:
- Receives a structured prompt (Go template with context from SQLite)
- Runs as `claude -p --model <model> <prompt>` (no `--output-format json` — raw text avoids double-JSON-escaping and truncation)
- Produces structured output (JSON to stdout, may be wrapped in markdown code fences)
- `StripMarkdownJSON()` regex strips code fences before parsing
- Process group isolation: `Setpgid: true` + custom `Cancel` kills entire process tree on context cancellation (prevents orphaned `claude` processes)
- Results are parsed and stored in SQLite
- Implementation: `internal/agentic/` package provides `RunNode`, `RunNodeJSON`, `StripMarkdownJSON`

Node types: sufficiency check, problem decomposition, **spotter** (per-repo spec compliance + runtime validation), **anchor** (cross-repo alignment), stuck analysis, **tracker spec assembly** (issue → problem spec), **PR body generation** (problem + climbs → PR title/body), **reflect** (error classification + learning capture), **learning retrieval** (surface relevant past learnings), **learning compaction** (consolidate learnings).

## Persistent Learnings

SQLite-backed system for capturing and retrieving insights across problems. See [review-loops-test-infra-design](design-docs/2026-03-16-review-loops-test-infra-design.md).

- **Storage**: `learnings` table with category, severity, resolved flag, access count
- **Retrieval**: Agentic node matches problem spec against active learnings; runs at CLI level during `belayer problem create`
- **Compaction**: Agentic node merges duplicates and distills patterns; via `belayer learnings compact`
- **CLI**: `belayer learnings list|show|add|compact`

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

- **State machine**: Polls SQLite on a configurable interval (default 2s), drives problems through `imported -> enriching -> pending -> decomposing -> running -> aligning -> pr_creating -> pr_monitoring -> ci_fixing -> review_reacting -> merged/closed/complete/failed`
- **Lead management**: Spawns leads in tmux windows, tracks active leads with `sync.Mutex` protected map
- **Crash recovery**: Detects lead failures, schedules retry with exponential backoff (`min(base * 2^attempt, max)`)
- **Agentic nodes**: Runs ephemeral `claude -p --model <model> <prompt>` for judgment calls; `StripMarkdownJSON()` handles code fences; stores all decisions in `agentic_decisions` table
- **Crag-aware decomposition**: Decomposition prompt includes available repo names from crag config, constraining the agentic node to valid repos
- **Sufficiency skip**: Problems pre-checked at intake skip the daemon's sufficiency check
- **Interfaces**: `LeadRunner`, `WorktreeCreator`, `DiffCollector`, and `PRCreator` interfaces enable mock-based testing
- **Cross-repo alignment (anchor)**: When all leads complete, collects git diffs from worktrees, evaluates per-criterion alignment (API contracts, shared types, feature parity, integration points), re-dispatches misaligned repos with feedback on failure, creates PRs on success
- **Anchor retry**: Max alignment attempts (default 2) prevents infinite re-dispatch loops; alignment attempts counted via `alignment_started` events
- **PR creation**: SCM provider interface (`internal/scm/`) creates PRs via `gh` CLI; supports stacked PRs (greedy bin-packing under configurable line threshold); PR body generated by agentic node
- **PR monitoring**: Daemon polls open PRs for CI status and review activity; reaction engine classifies events and dispatches actions (CI fix leads, review comment replies, auto-merge)
- **Tracker integration**: Plugin interface (`internal/tracker/`) syncs issues from GitHub Issues or Jira; spec assembly agentic node converts issues to problem specs

## CLI Commands

| Command | Purpose |
|---------|---------|
| `belayer init` | Initialize global config |
| `belayer crag create <name> --repos <source>` | Create a new crag from remote URLs or local repo paths (`--local-paths`) |
| `belayer belayer start` | Start the belayer daemon (DAG executor) |
| `belayer problem create [desc]` | Create problem with intake pipeline |
| `belayer problem retry [problem-id]` | Retry a failed problem (reuses enriched description) |
| `belayer problem list` | List problems for the current crag |
| `belayer message <addr> --type <type> --body "..."` | Send a typed mail message to an agent |
| `belayer mail read` | Read all unread messages (marks as read) |
| `belayer mail inbox` | List unread messages without marking read |
| `belayer mail ack <id>` | Mark a specific message as read |
| `belayer status` | Quick status overview |
| `belayer tracker sync` | Fetch issues from tracker and import to local DB |
| `belayer tracker list` | Preview matching tracker issues |
| `belayer tracker show <id>` | Show tracker issue details |
| `belayer pr list` | List monitored PRs |
| `belayer pr show <number>` | Detailed PR view with reaction history |
| `belayer pr retry <number>` | Reset CI fix count for manual retry |
| `belayer explorer` | Launch interactive Claude session for pre-crag research/decomposition in `~/.belayer/explorer/<workspace>/` |
| `belayer env create/add-worktree/destroy/...` | Environment provider CLI (default provider wrapping bare-repo + worktree logic) |
| `belayer setter` | Launch interactive Claude session with belayer context (.claude/ workspace) |

## Setter And Explorer Session Context

`belayer setter` refreshes the crag's `.claude/` directory on each launch:
- **CLAUDE.md** (templated): Rendered from `internal/defaults/claudemd/setter.md` with crag name and repo names. Establishes the setter as the session identity and operating-principles boundary, so user requests stay inside belayer research, drafting, and problem-creation workflows by default.
- **Commands** (static): 20 slash commands (`/blr-config`, `/blr-draft-create`, `/blr-draft-list`, `/blr-draft-review`, `/blr-logs`, `/blr-mail`, `/blr-message`, `/blr-phase-plan`, `/blr-pr`, `/blr-problem-brainstorm`, `/blr-problem-create`, `/blr-problem-list`, `/blr-prs`, `/blr-research`, `/blr-research-url`, `/blr-research-summarize`, `/blr-status`, `/blr-sync`, `/blr-ticket`, `/blr-ticket-list`) copied from `internal/defaults/commands/`, with stale generated legacy command files pruned on reuse.
- **Research + draft artifacts**: setter sessions treat `~/.belayer/crags/<crag>/docs/` as the research root for `research-notes.md`, `research.md`, and `phases.md`, then stage draft problems under `~/.belayer/drafts/<crag>/problems/<nnn>/` before publication. `/blr-draft-review` publishes through `belayer problem create` and removes the draft directory only after success.
- **BELAYER_CRAG env var**: Set in the exec environment so all belayer CLI commands auto-resolve the crag without `--crag` flags.

`belayer explorer` creates a persistent workspace under `~/.belayer/explorer/<project-name>/` or `~/.belayer/explorer/_unnamed-<timestamp>/`:
- **CLAUDE.md** (templated): Rendered from `internal/defaults/claudemd/explorer.md` with the project name and absolute PRD path, if one was supplied. The template teaches the five-phase workflow and the belayer problem/climb drafting model, including the `spec.md` + `climbs.json` quality bar.
- **Commands** (selective): Copies only explorer-safe shared slash commands from `internal/defaults/commands/` when those assets exist, avoiding setter-only commands before a crag is chosen.
- **Interrupted-session handling**: Unnamed explorer sessions always generate a fresh timestamped workspace. Named explorer relaunches detect an existing workspace and prompt for `resume` versus `start fresh` before Claude starts.
- **No crag env leakage**: The shared Claude launcher clears inherited `BELAYER_CRAG` / `BELAYER_INSTANCE` values so explorer sessions start without stale crag context.

This is distinct from the repo's own `.claude/CLAUDE.md` which is for developing belayer itself. The runtime session contexts are generated on demand into the workspaces belayer spawns.

## Process Lifecycle

All child processes (`claude -p`, lead shell scripts) are spawned with `Setpgid: true` to create independent process groups. On context cancellation (Ctrl+C), the custom `cmd.Cancel` sends `SIGKILL` to the entire process group (negative PID), ensuring no orphaned grandchild processes survive.

## Config System

Resolution chain: crag config > global config > embedded defaults.

- `belayerconfig.Load()` merges TOML configs following the chain
- `belayerconfig.LoadProfile()` resolves validation profiles from config dirs
- Embedded defaults via Go `embed.FS` in `internal/defaults/`
- `belayer.toml` schema: `[agents]`, `[execution]`, `[validation]`, `[anchor]`, `[environment]` sections
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
| **Setter** | Multi-repo work distributor (multi-repo only) |
| **Belayer** | Daemon / DAG executor |
| **Lead** | Implementation agent (per-repo) |
| **Spotter** | Multi-repo cross-repo validator (multi-repo only) |
| **Anchor** | Cross-repo alignment reviewer |
| **Boulderer** | One-off specialist for small tasks (deferred) |

> The **setter** defines **problems** at the **crag**. The **belayer** sends **leads** up their **climbs**. When they **top** out, the **spotter** validates. If no retries were needed, it was **flashed**.

## Strategic Principles

1. **Belayer optimizes for autonomy, not efficiency** — Redundant work is acceptable if it enables self-correction without human intervention.
2. **Multi-repo is additive, not transformative** — The per-repo pipeline is unchanged; setter and spotter layer on top without altering what each lead does.
3. **Belayer is plumbing** — Belayer provides contracts and orchestration, not node implementations. What runs inside a node is not belayer's concern.
4. **Agent-agnostic** — Nodes are black boxes. Use whatever agent fulfills the contract: Claude, Codex, a shell script, or a future runtime. Core ships ExecSpawner (generic command exec); specific agent integrations live in frameworks.
5. **Orchestration is owned by the environment** — Pipeline config and node scripts live in the target repo's `.belayer/` directory, not in belayer core. `belayer setup --framework` scaffolds the orchestration definition; users customize freely.
6. **Boring by default** — Solve specific problems with opinionated plumbing. Don't over-abstract or generalize beyond the stated use case.

## Setter/Spotter Contracts

Setter and spotter are first-class belayer concepts, not generic pipeline nodes. They are multi-repo only — single-repo problems bypass both.

**Setter contract**: `spec.md` in → per-repo `spec.md` out.

- Receives the top-level problem spec.
- Produces one `spec.md` per target repo, scoped to that repo's responsibilities.
- Belayer routes each per-repo spec to the appropriate lead as the climb input.

**Spotter contract**: N commit hashes in → gate score + `feedback/rationale.md` out.

- Receives the final commit hashes from all leads after their climbs complete.
- Produces a numeric gate score and a rationale document covering cross-repo consistency.
- A failing spotter score blocks PR creation and re-dispatches affected leads with the rationale as feedback.

Belayer provides the contracts and orchestration. Users implement the nodes. This is what keeps belayer agent-agnostic — the runtime inside a setter or spotter is not prescribed.

## PR Manifest

The PR manifest is the typed interface between the Climb and Summit phases. It is written after all leads complete and all spotters pass, and is consumed by the Summit phase to create and monitor pull requests.

```json
{
  "prs": [
    {
      "repo": "api",
      "url": "...",
      "number": 42,
      "branch": "...",
      "commit": "abc1234",
      "ci_status": "passed",
      "reviews": "approved"
    }
  ],
  "validation": {
    "cross_repo": "PASS",
    "spotter_score": 8.5
  }
}
```

## Why Belayer

| belayer | competitors |
|---------|-------------|
| Agent-agnostic orchestration | Model-locked agents |
| Multi-repo as additive layer | Multi-repo as agent feature |
| Pipeline-as-YAML | Hardcoded workflows |
| Three phases with typed contracts | Monolithic pipelines |
| You own your nodes | Platform owns your agents |

## Plugin Marketplace

The belayer repo doubles as a Claude Code marketplace. A `.claude-plugin/marketplace.json` at the repo root lists bundled plugins, and `plugins/` contains their source (markdown commands, agents, skills).

- **Bundled plugins**: `harness` (documentation + execution workflow) and `pr` (PR lifecycle management)
- **Auto-install**: `belayer init` registers the belayer GitHub repo as a marketplace in Claude Code's `~/.claude/plugins/` registry
- **Canonical source**: Belayer owns these plugins. Changes flow from here back to llm-agents, not the reverse.
- **Atomic writes**: Registry file updates use temp-file + rename to avoid corrupting Claude Code's plugin state on interrupt

## See Also

- [Architecture](ARCHITECTURE.md) — module boundaries and data flow
- [Quality](QUALITY.md) — testing strategy
