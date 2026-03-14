# Plans

Execution plans for active and completed work.

## Active Plans

| Plan | Created | Topic |
|------|---------|-------|
_None currently active._

## Tech Debt

| Issue | Severity | Notes |
|-------|----------|-------|
| Flaky `TestProcessPendingProblem_Decomposition` | Low | TempDir cleanup race condition; passes ~2/3 runs |
| Empty `repoDir` in `monitorPRs` SCM calls | Medium | All SCM polling passes "" as repoDir; works incidentally for single-repo crags but fails for multi-repo |
| `HandleApproval` partial failure orphans PRs | Medium | Successful PR inserts not cleaned up when later repos fail; problem stays in `reviewing` |
| Missing integration tests for daemon PR lifecycle | Medium | `executeReaction`, `monitorPRs`, `checkAllPRsMerged`, `HandleApproval` SCM path all untested |
| Bare string types for PR status fields | Low | `CIStatus`, `ReviewStatus`, `State` should be typed constants like `ProblemStatus` |

## Completed Plans

| Plan | Completed | Topic |
|------|-----------|-------|
| [Belayer Marketplace](exec-plans/completed/2026-03-13-belayer-marketplace.md) | 2026-03-13 | Vendor harness + pr plugins, create marketplace, auto-install in init |
| [Environment Provider](exec-plans/completed/2026-03-12-environment-provider.md) | 2026-03-13 | Single provider model with `belayer env` default + external provider support |
| [Multi-Provider Spawner](exec-plans/completed/2026-03-11-multi-provider-spawner.md) | 2026-03-11 | CodexSpawner + factory function + config wiring |
| [Planning & Review Hats](exec-plans/completed/2026-03-11-planning-review-hats.md) | 2026-03-11 | Tracker intake, SCM provider, PR monitoring & reaction engine |
| [Complete Instance-to-Crag Rename](exec-plans/completed/2026-03-11-instance-to-crag-complete-rename.md) | 2026-03-11 | Package rename, config file rename, internal var renames, doc prune |
| [Instance-to-Crag Rename](exec-plans/completed/2026-03-10-instance-to-crag-rename.md) | 2026-03-11 | Rename --instance to --crag, remove TUI references, prune stale docs |
| [Crag Architecture](exec-plans/completed/2026-03-10-crag-architecture.md) | 2026-03-10 | Climbing terminology overhaul + per-role window layout with deferred activation |
| [Crag Review Fixes](exec-plans/completed/2026-03-10-crag-review-fixes.md) | 2026-03-10 | Post-review fixes: rename completion, error handling, robustness |
| [Filesystem Mail Store](exec-plans/completed/2026-03-10-filesystem-mail-store.md) | 2026-03-10 | Replace beads/dolt mail backend with pure filesystem store |
| [Goal-Scoped Isolation](exec-plans/completed/2026-03-09-goal-scoped-isolation.md) | 2026-03-10 | --append-system-prompt for roles, .lead/<goalID>/ paths, separate spotter windows |
| [Manage Session Context](exec-plans/completed/2026-03-09-manage-session-context.md) | 2026-03-10 | .claude/ workspace with slash commands for belayer manage |
| [Mail System](exec-plans/completed/2026-03-09-mail-system.md) | 2026-03-10 | Beads-backed inter-agent mail with tmux send-keys delivery |
| [Interactive Lead Sessions](exec-plans/completed/2026-03-08-interactive-lead-sessions.md) | 2026-03-08 | Replace claude -p with full interactive Claude Code sessions |
| [Context-Aware Validation](exec-plans/completed/2026-03-07-context-aware-validation.md) | 2026-03-07 | Per-goal spotter validation, anchor rename, config system |
| Post-build bugfixes | 2026-03-07 | Real-world testing fixes (see below) |

### Post-Build Bugfix Summary (2026-03-07)

Fixes discovered during real-world testing with `claude -p`:

1. **`--output-format json` removed**: The JSON envelope double-escaped and truncated long responses. Switched to raw text output + `StripMarkdownJSON()` regex.
2. **Markdown code fence stripping**: Claude wraps JSON in ` ```json ``` ` blocks. Added `StripMarkdownJSON()` (exported, shared across agentic nodes and CLI executor).
3. **Orphaned process fix**: `exec.CommandContext` only kills direct child. Added `Setpgid: true` + `cmd.Cancel` to kill entire process group on Ctrl+C. Applied to all 3 exec sites (agentic.go, runner.go, task.go).
4. **Lead verdict.json fix**: `claude -p` cannot write files. Updated lead script to capture stdout, extract JSON (with python3 fence/text stripping), and write verdict.json itself.
5. **Brainstorm bypass fix**: Sufficiency check returning `{sufficient: false, gaps: []}` skipped brainstorm. Now falls back to a default question.
6. **Progress logging**: Added `log.Printf` at coordinator milestones (decomposition, lead spawn, alignment) and lead runner phase transitions.
7. **`task retry` command**: Resets failed tasks to pending, cleans up worktrees, restarts coordinator. Reuses enriched description from brainstorm.
8. **`task list` command**: Lists all tasks for a crag with status, ID, date, and description.
9. **Lead audit trail**: Added `lead_exec_output` and `lead_review_output` events to capture agent output snippets (first 500 chars) in the event log and SQLite audit trail. Full output stored in worktree `output/` directory.

## See Also

- [Architecture](ARCHITECTURE.md) — module boundaries and invariants
- [Design](DESIGN.md) — patterns and conventions
- Design documents for brainstorm outputs: `design-docs/`
