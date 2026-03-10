# Plans

Execution plans for active and completed work.

## Active Plans

| Plan | Created | Topic |
|------|---------|-------|
| _(none)_ | | |

## Tech Debt

| Issue | Severity | Notes |
|-------|----------|-------|
| Flaky `TestProcessPendingTask_Decomposition` | Low | TempDir cleanup race condition; passes ~2/3 runs |

## Completed Plans

| Plan | Completed | Topic |
|------|-----------|-------|
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
6. **TUI improvements**: Word-wrapped descriptions, error event payloads in event log, scrollable events pane with indicators.
7. **Progress logging**: Added `log.Printf` at coordinator milestones (decomposition, lead spawn, alignment) and lead runner phase transitions.
8. **`task retry` command**: Resets failed tasks to pending, cleans up worktrees, restarts coordinator. Reuses enriched description from brainstorm.
9. **`task list` command**: Lists all tasks for an instance with status, ID, date, and description.
10. **Viewport-based TUI scrolling**: Replaced manual scroll logic in Detail and Events panes with `bubbles/viewport` for proper content clipping. Long descriptions no longer overflow; scroll percentage shown in title.
11. **Lead audit trail**: Added `lead_exec_output` and `lead_review_output` events to capture agent output snippets (first 500 chars) in the event log and SQLite audit trail. Full output stored in worktree `output/` directory.
|------|-----------|-------|

## See Also

- [Architecture](ARCHITECTURE.md) — module boundaries and invariants
- [Design](DESIGN.md) — patterns and conventions
- Design documents for brainstorm outputs: `design-docs/`
