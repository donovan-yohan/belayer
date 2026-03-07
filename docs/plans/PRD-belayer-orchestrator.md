# PRD: Belayer — Multi-Repo Coding Agent Orchestrator

## Objective

Belayer is a standalone Go CLI tool that orchestrates autonomous coding agents across multiple repositories. It takes work items (text input or Jira tickets), decomposes them into per-repo tasks using ephemeral AI decisions, spawns isolated lead execution loops in git worktrees, monitors progress, and validates cross-repo alignment before creating PRs. The design follows Stripe's blueprint pattern: deterministic Go code handles orchestration, ephemeral Claude sessions handle judgment calls.

## Goals

| # | Goal | Status | Attempts | Design Doc | Plan |
|---|------|--------|----------|------------|------|
| 1 | Project scaffolding & core architecture | complete | 1 | [design](../design-docs/2026-03-06-project-scaffolding-design.md) | [plan](../exec-plans/completed/2026-03-06-project-scaffolding-plan.md) |
| 2 | Instance & repository management | complete | 1 | [design](../design-docs/2026-03-06-instance-repo-management-design.md) | [plan](../exec-plans/completed/2026-03-06-instance-repo-management-plan.md) |
| 3 | Bundled lead execution loop | pending | 0 | - | - |
| 4 | Coordinator engine (state machine + agentic nodes) | pending | 0 | - | - |
| 5 | Task intake & decomposition | pending | 0 | - | - |
| 6 | Cross-repo integration & alignment | pending | 0 | - | - |
| 7 | TUI dashboard | pending | 0 | - | - |
| 8 | claude-remote-cli integration API | pending | 0 | - | - |

## Acceptance Criteria

| Goal | Criteria |
|------|----------|
| 1 | Go module compiles; CLI parses commands (`init`, `instance create`, `task create`, `status`, `tui`); SQLite schema created with migrations; config file loaded/saved from `~/.belayer/config.json`; project structured with `cmd/`, `internal/`, `pkg/` layout |
| 2 | `belayer init` creates `~/.belayer/` directory structure; `belayer instance create <name> --repos <url1,url2>` clones bare repos into `repos/` directory; worktrees created per-task per-repo via `git worktree add` from bare repos; worktree cleanup on task completion; instance config persisted in `instance.json` |
| 3 | Lead loop shell script bundled as embedded Go asset; runs in a worktree executing goals via `claude -p`; writes structured progress to SQLite (goal status, attempts, output chunks); emits events on state changes (started, verdict, stuck, complete); handles retry/stuck/complete states; reads verdict.json files and stores in SQLite |
| 4 | Coordinator goroutine polls SQLite for lead state changes (configurable interval); spawns and monitors lead processes via `os/exec`; detects lead crashes and schedules retry with exponential backoff; triggers agentic nodes (ephemeral `claude -p` sessions) for sufficiency checks, decomposition, and alignment; processes agentic node outputs and writes decisions to SQLite |
| 5 | Text input accepted via `belayer task create "description"`; Jira ticket intake via `belayer task create --jira <ticket-ids>`; context sufficiency check as agentic node (returns sufficient/insufficient with gaps); interactive brainstorm mode when insufficient (CLI Q&A that enriches task spec); per-repo task decomposition as agentic node; multiple tickets grouped into single task |
| 6 | Integration agentic node triggered when all leads for a task report complete; collects git diffs from each repo worktree; reviews cross-repo alignment (API contracts, shared types, feature parity); produces structured verdict (pass/fail per criterion); on failure: identifies misaligned repos and re-dispatches leads with alignment feedback; on pass: proceeds to PR creation |
| 7 | bubbletea TUI shows: instance list, active tasks with per-repo lead progress bars, real-time streaming of lead output (selectable), integration verdicts, task history; keyboard navigation (j/k, enter, q, tab between panes); responsive layout; updates via SQLite polling |
| 8 | HTTP server (optional, started with `belayer serve`) exposes: GET /instances, GET /tasks, GET /leads, GET /events (SSE stream); WebSocket endpoint for real-time events; claude-remote-cli can connect and render belayer state in its web UI |

## Context & Decisions

### Architecture Decisions
- **Standalone CLI** (not embedded in claude-remote-cli) — belayer is its own tool; claude-remote-cli is one possible UI
- **Go** — best agent-buildability, goroutines for concurrency, bubbletea TUI, single binary, strong compile-time checking
- **3 layers**: User -> Coordinator (Go code) -> Lead (bundled loop)
- **Stripe-style coordinator**: deterministic state machine + ephemeral Claude sessions for judgment calls (no persistent AI coordinator)
- **SQLite** shared database for all state (leads write progress, coordinator reads/writes decisions)
- **Bare repos + worktrees** (extend-cli pattern) for repo isolation — shared object storage, fast creation
- **Long-lived instances with task isolation**: Instance persists (repos, config); each task gets isolated worktrees
- **Belayer bundles its own lead**: No dependency on external lead plugin; full control over execution loop
- **TUI dashboard** via bubbletea for primary user interface

### Research Influences
- **Stripe Minions**: Blueprint pattern (deterministic + agentic nodes), strict 2-iteration CI retry, one-shot agents
- **Gastown**: Mail protocol for messaging, propulsion principle (agents self-manage), convoy for work tracking
- **Bosun**: Event bus for real-time updates, health scoring + intervention ladder, ephemeral agent pool
- **Extend-CLI**: Bare repos + worktrees pattern, selective repo creation, port offset isolation
- **Symphony (OpenAI)**: Workspace lifecycle hooks, retry backoff formula, WORKFLOW.md as in-repo policy
- **Lead Plugin**: .lead/ directory as interface contract, execute->review->verdict cycle, file-based state
- **Anthropic research**: Ephemeral agents with persistent artifacts, Opus for lead / Sonnet for review model split

### Agentic Nodes (ephemeral Claude sessions)
1. **Sufficiency check**: "Does this task have enough context to decompose?" -> skip/brainstorm decision
2. **Task decomposition**: "Break this task into per-repo specs" -> per-repo PRDs
3. **Alignment review**: "Are these repo implementations consistent?" -> pass/fail + feedback
4. **Stuck analysis**: "Why is this lead stuck? Suggest recovery" -> structured report

### Directory Layout
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

### Communication Flow
```
Jira/Text -> Intake -> Sufficiency Check (agentic) -> Decomposition (agentic)
                                                          |
                    +------------------+-----------------+
                    v                  v                  v
              Lead(repo-A)       Lead(repo-B)       Lead(repo-C)
              writes SQLite      writes SQLite      writes SQLite
                    |                  |                  |
                    +--------+---------+-----------------+
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

## Reflections & Lessons

### Goal 1 (2026-03-06)
- Pure Go SQLite (`modernc.org/sqlite`) worked cleanly — no CGO complications
- Cobra provides `completion` command for free which is a nice bonus
- The `cmd/belayer/main.go` -> `internal/cli/root.go` -> individual command files pattern keeps things clean
- Embedding SQL migrations via `embed.FS` is simple and avoids external tools
- Config approach (JSON in `~/.belayer/config.json`) is straightforward; instance registry is just name->path map

### Goal 2 (2026-03-06)
- Separating `internal/repo/` (git operations) from `internal/instance/` (lifecycle) keeps concerns clean
- macOS `/var` -> `/private/var` symlink causes path comparison issues in tests — use `filepath.EvalSymlinks` for path comparisons
- URL trailing slash must be stripped before `.git` suffix — order of trimming matters
- `t.TempDir()` + `t.Setenv("HOME", ...)` provides clean test isolation without touching real filesystem
- Instance creation clones bare repos + initializes DB + writes instance.json atomically; cleanup on any failure
- Using instance name as the SQLite `id` simplifies lookups (no UUID generation needed at this layer)
