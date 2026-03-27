# Plans

Execution plans for active and completed work.

## Active Plans

| Plan | Created | Topic |
|------|---------|-------|
| [Intake Plugin Model](exec-plans/active/2026-03-21-intake-plugin-model.md) | 2026-03-21 | Intake plugin model, pipeline templates, v2→v3 migration, worker daemon |

## Tech Debt

| Issue | Severity | Notes |
|-------|----------|-------|
| Schedule reconciliation is a stub | Medium | `internal/v3/intake/schedule.go` logs intent but does not create Temporal schedules. Phase 2 |
| Worker `/status` endpoint is a stub | Low | Returns health only — full workflow listing deferred to Phase 2 |
| `StartSHA` in `NodeActivityInput` never populated | Low | Code-output commit verification guard never fires |
| Flaky `TestProcessPendingProblem_Decomposition` | Low | v1 daemon — TempDir cleanup race. Reassess when v1 code is removed |


## Completed Plans

| Plan | Completed | Topic |
|------|-----------|-------|
| [ExecSpawner + Framework Scaffolding](exec-plans/completed/2026-03-26-exec-spawner-framework-scaffolding.md) | 2026-03-26 | Decouple node spawning, framework model, belayer setup, claude-tmux |
| [Three-Phase Architecture Docs](exec-plans/completed/2026-03-25-three-phase-architecture-docs.md) | 2026-03-25 | Explore/Climb/Summit architecture docs, config hierarchy, pipeline examples, competitive positioning |
| [Bug Architecture Review](exec-plans/completed/2026-03-24-bug-architecture-review.md) | 2026-03-24 | Architecture review step in bug flow + learnings enforcement across harness lifecycle |
| [Summit Node & Explorer Plugin](exec-plans/completed/2026-03-23-summit-node-explorer-plugin.md) | 2026-03-23 | Summit PR node, explorer plugin, /explorer:send command |
| [Gate Nodes — Quality Scoring](exec-plans/completed/2026-03-20-gate-nodes.md) | 2026-03-21 | Gate nodes as second pipeline primitive: multi-dimensional scoring, threshold routing, anti-gaming |
| [Belayer v3: Temporal Activity Pipeline](exec-plans/completed/2026-03-20-v3-temporal-pipeline.md) | 2026-03-20 | v3 clean break: Activity-per-node, file-based completion, YAML pipeline config, `belayer climb` CLI |
| [v2 Wiring Gaps](exec-plans/completed/2026-03-19-v2-wiring-gaps.md) | 2026-03-19 | Worker command, DSL wiring, pipeline CLI, WorkDir resolution |
| [Belayer v2: Temporal Orchestrator Platform](exec-plans/completed/2026-03-19-temporal-orchestrator-v2.md) | 2026-03-19 | v2 clean break: Temporal backbone, two provider contracts (Type A pitch / Type B ascent), three pipeline phases (Approach/Ascent/Send), CLI-callback for interactive sessions |
| [Harness Plugin Audit & Workflow Fix](exec-plans/completed/2026-03-19-harness-audit.md) | 2026-03-19 | Learning persistence, frontmatter lifecycle, health score, auto-archive |
| [Work Loss Prevention & PR Stderr Capture](exec-plans/completed/2026-03-17-work-loss-and-stderr-fixes.md) | 2026-03-17 | Push branches on climb completion, capture gh stderr, warn on cleanup of unpushed work |
| [Explorer Session Resume Flow](exec-plans/archive/2026-03-17-explorer-session-resume.md) | 2026-03-17 | Interrupted-session detection and resume-or-start-fresh for named explorer workspaces |
| [Setter Research And Draft Guidance](exec-plans/completed/2026-03-17-setter-research-draft-guidance.md) | 2026-03-17 | Close the remaining setter-session gaps for shared research workflows, draft consumption guidance, and deterministic command publication |
| [Research Toolkit Commands](exec-plans/completed/2026-03-17-research-command-toolkit.md) | 2026-03-17 | Add shared `/blr-research*` command assets, session-root guidance, review fixes, and verified alignment with the coupled explorer/draft workflow surfaces |
| [Explorer Template Guidance](exec-plans/completed/2026-03-17-explorer-template-guidance.md) | 2026-03-17 | Refine `internal/defaults/claudemd/explorer.md` so explorer sessions teach the five-phase workflow and belayer problem/climb drafting model clearly |
| [Draft Workflow Commands](exec-plans/completed/2026-03-17-draft-workflow-commands.md) | 2026-03-17 | Add `/blr-phase-plan`, `/blr-draft-create`, `/blr-draft-list`, and `/blr-draft-review` command assets with draft-workflow guidance and regression coverage |
| [Explorer Session Bootstrap](exec-plans/completed/2026-03-17-explorer-session-bootstrap.md) | 2026-03-17 | Add `belayer explorer`, explorer workspace prep, shared session launcher cleanup, and explorer template/test coverage |
| [Setter BLR Command Rename](exec-plans/completed/2026-03-17-setter-blr-command-rename.md) | 2026-03-17 | Rename embedded setter commands to the `blr-` namespace, align setter guidance, and tighten command-content tests |
| [Crag Create Local Paths](exec-plans/completed/2026-03-17-crag-local-paths.md) | 2026-03-17 | `belayer crag create --local-paths` support, validation, and tracker guardrails |
| [Review Deferred Items](exec-plans/completed/2026-03-16-review-deferred-items.md) | 2026-03-16 | Test coverage, typed enums, HandleApproval bug fix |
| [Review Loops, Test Infra & Learnings](exec-plans/completed/2026-03-16-review-loops-test-infra.md) | 2026-03-16 | Multi-persona review loops, test contracts, spotter shift, persistent learnings |
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
