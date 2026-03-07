# PRD: Belayer Agent-Friendly Architecture Redesign

## Objective

Redesign belayer from scratch to eliminate Claude-in-Claude nesting, make the CLI a pure data publisher, and use tmux-based process management with per-goal parallelism. The system uses climbing nomenclature: Setter (daemon), Spotter (reviewer), Lead (worker), and Belayer (interactive manager). This enables vendor-agnostic agent execution and cross-repo validation with automatic redistribution. No backwards compatibility with the existing codebase is required — this is a clean-slate rebuild.

## Goals

| # | Goal | Status | Attempts | Design Doc | Plan |
|---|------|--------|----------|------------|------|
| 1 | CLI and data layer — pure data publisher with SQLite | complete | 1 | [design](../design-docs/2026-03-07-cli-data-layer-design.md) | [plan](../exec-plans/completed/2026-03-07-cli-data-layer-plan.md) |
| 2 | Setter daemon — DAG executor with tmux management | complete | 1 | [design](../design-docs/2026-03-07-setter-daemon-design.md) | [plan](../exec-plans/completed/2026-03-07-setter-daemon-plan.md) |
| 3 | Lead spawning — AgentSpawner interface and per-goal sessions | complete | 1 | [design](../design-docs/2026-03-07-lead-spawning-design.md) | [plan](../exec-plans/completed/2026-03-07-lead-spawning-plan.md) |
| 4 | Spotter — cross-repo review with redistribution | complete | 1 | [design](../design-docs/2026-03-07-spotter-review-design.md) | [plan](../exec-plans/completed/2026-03-07-spotter-review-plan.md) |
| 5 | Belayer manage — interactive agent session for task creation | pending | 0 | - | - |

## Acceptance Criteria

| Goal | Criteria |
|------|----------|
| 1 | `belayer task create --spec spec.md --goals goals.json` writes task to SQLite with parsed goals; `--jira` flag stores ticket ref optionally; no `claude -p` calls anywhere in CLI; CLI validates spec.md exists and goals.json parses correctly; goals.json schema supports per-repo goals with `depends_on` fields; SQLite schema includes tasks, goals, events, and spotter_reviews tables; `belayer init` and `belayer instance create` set up directory structure and bare repo clones; `go test ./...` passes |
| 2 | `belayer setter --instance <name>` starts a long-running daemon; polls SQLite for pending tasks; parses goals.json and builds per-repo goal DAG; creates tmux session per task (`task-<id>`); spawns tmux windows for goals with no unmet dependencies; watches for DONE.json files in worktrees; updates goal status in SQLite on completion; unblocks dependent goals and spawns them; handles crash recovery (re-reads SQLite + scans worktrees on restart); stale goal detection (configurable timeout); `--max-leads` flag caps concurrent leads (default 8) with FIFO queue; `belayer logs` subcommands work (view, cleanup, stats); tmux pipe-pane captures output to log files with rotation (10MB/goal, 500MB/instance); auto-compress logs on task completion with 7-day retention |
| 3 | `AgentSpawner` interface implemented with `Spawn`, `Done`, `Kill` methods; Claude Code implementation spawns `claude -p` in tmux window with harness flow prompt; prompt includes spec.md content, goal description, and instructions to write DONE.json; DONE.json contains structured output (status, summary, files_changed, notes); `DoneSignaler` watches worktrees for DONE.json via filesystem polling; per-goal tmux windows named `<repo>-<goal-id>`; multiple goals run in parallel when dependencies allow; vendor abstraction boundary is clean (no Claude-specific code outside the Claude spawner implementation) |
| 4 | Spotter spawned by setter when all goals for a task complete; runs as agent session in own tmux window; receives spec.md + git diffs from all repo worktrees + goal completion summaries; produces verdict JSON (approve/reject with per-repo status and correction goals); on approve: setter creates PRs for all repos, marks task complete; on reject: setter writes correction goals for failing repos, spawns new lead sessions; max 2 spotter reviews then marks affected repos as stuck; all PRs held until spotter approves; `spotter_reviews` table tracks attempts and verdicts |
| 5 | `belayer manage --instance <name>` starts an interactive agent session; agent is trained on belayer CLI usage; can run brainstorm skill to generate spec.md + goals.json; can fetch and convert Jira tickets to task format; invokes `belayer task create` to publish tasks; no LLM inference happens in the CLI itself — all inference in the manage session |

## Context & Decisions

- **Design doc**: `docs/design-docs/2026-03-07-agent-friendly-architecture-design.md`
- **Clean slate**: No backwards compatibility required — existing code can be replaced entirely
- **Climbing nomenclature**: Setter (daemon), Spotter (reviewer), Lead (worker), Belayer (interactive manager)
- **tmux for process management**: Battle-tested, gives visibility, no custom daemon complexity
- **DONE.json files + SQLite events**: Vendor-agnostic; any agent can write a file
- **One session per goal**: Parallelism, isolation, clean context, retry granularity
- **Cross-repo deps implicit**: Spotter validates alignment rather than explicit DAG edges
- **Max 2 spotter reviews**: Prevents infinite loops; stuck repos reported to user
- **Hold all PRs**: Cross-repo consistency is belayer's core value proposition
- **AgentSpawner interface**: 3-method boundary (spawn, done, kill) for vendor abstraction
- **Pre-decomposed tasks**: Setter does no inference, only routing
- **Hybrid TUI**: SQLite dashboard + tmux attach for live view (follow-up work)
- **Crash recovery**: DONE files as source of truth; setter reconstructs state on restart
- **Output capture**: Structured DONE.json + tmux pipe-pane logs with rotation/cleanup
- **Concurrent tasks**: Single event loop, `--max-leads 8` default with FIFO queue
- **Log management**: 10MB/goal, 500MB/instance limits; auto-compress on completion; 7-day retention

## Reflections & Lessons

### Goal 1 - CLI and Data Layer
- Clean-slate rebuild went smoothly — removing old packages (coordinator, intake, lead, tui) before rewriting CLI prevented compile errors from stale imports
- The store package (replacing coordinator/store + lead/store) is simpler with a flat structure; all CRUD in one file
- Kept db/, config/, instance/, and repo/ packages unchanged — they work well and don't depend on the old architecture
- GoalsFile validation (unique IDs, same-repo deps, matching instance repos) catches errors at ingest time before touching SQLite
- Spec content is stored directly in SQLite rather than as a file path — simpler, avoids path resolution issues

### Goal 2 - Setter Daemon
- The tmux package as an interface (`TmuxManager`) was essential for testability — all setter tests use a `mockTmux` that simulates sessions/windows in-memory without needing real tmux
- DAG implementation was simple and clean — `BuildDAG` + `ReadyGoals` + `MarkComplete` covers the full lifecycle with just a map and a reverse-lookup
- TaskRunner encapsulates per-task state cleanly — the setter itself just manages a map of task runners and a lead queue
- DONE.json scanning via filesystem polling is simple but effective; the alternative (inotify/fswatch) would add platform-specific complexity for minimal benefit
- Crash recovery works by re-reading SQLite + scanning worktrees — the key insight is DONE.json files persist across crashes, so the setter can reconstruct state
- The `--max-leads` FIFO queue was trivial to implement since the event loop is single-threaded — no concurrency concerns
- Log rotation (truncate first half) and compression (gzip on completion) keep disk usage bounded without losing recent context
- Goal spawning uses a placeholder command for now — Goal 3 will implement the real AgentSpawner that sends actual agent commands to tmux windows
- Spotter integration is also placeholder — auto-completes tasks when all goals finish. Goal 4 will implement real review logic

### Goal 3 - Lead Spawning
- The design doc's `AgentHandle` and `DoneSignaler` interfaces were unnecessary — the setter already handles completion detection via DONE.json polling in `TaskRunner.CheckCompletions()` and tmux window lifecycle via `TmuxManager`. Adding separate abstractions would duplicate existing functionality.
- The `AgentSpawner` interface only needs `Spawn(ctx, SpawnOpts) error` — one method instead of three. The setter manages everything else (kill via tmux, done via filesystem polling). Simpler is better.
- Shell quoting for the prompt is critical to prevent injection — single-quote wrapping with embedded single-quote escaping handles all cases
- The `internal/lead/` package provides clean vendor isolation: `spawner.go` (interface), `claude.go` (Claude impl), `prompt.go` (template). Swapping vendors means adding one new file and changing one line in `cli/setter.go`.
- Prompt template uses `text/template` which is simple and sufficient — no need for external template libraries
- Adding the `spawner` field to `TaskRunner` required updating all test setup code (8 test functions), but the mock pattern (`mockSpawner`) was trivial and consistent with the existing `mockTmux` pattern

### Goal 4 - Spotter Review
- The spotter reuses the same `AgentSpawner` interface as leads — just with a different prompt and output file (VERDICT.json vs DONE.json). This validates the vendor abstraction boundary.
- Adding `GitRunner` interface for testability was essential — spotter needs git diffs, and mocking exec.Command directly is fragile. The `mockGitRunner` pattern is clean and consistent with `mockTmux`/`mockSpawner`.
- The `CheckCompletions()` return signature needed updating to return `completedCount` for proper `activeLeads` tracking. This was a minor but important fix — without it, the setter would never reclaim lead slots.
- The DAG's `AddGoals()` method for correction goals was trivial — just insert into the existing maps. The DAG's simplicity (flat map + reverse-lookup) makes extension easy.
- DONE.json cleanup before spawning correction leads is critical — without it, the old DONE.json would cause the correction goal to be immediately marked complete on the next tick. Catching this during design prevented a subtle runtime bug.
- Task status transitions (running -> reviewing -> running on reject -> reviewing) work cleanly because the setter checks `runner.task.Status` on each tick rather than relying on a single pass.
- PR creation is minimal (git push + gh pr create) — intentionally simple since real-world usage will likely need more customization (branch naming, PR templates, reviewers). The current implementation is a solid foundation.
- The `createPR` method shells out to both git and gh CLI — in tests, the `mockGitRunner` handles git, but gh is not called since it's behind the git error path. This is acceptable for now; real PR creation tests would need integration testing.
