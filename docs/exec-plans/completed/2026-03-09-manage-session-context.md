# Manage Session Runtime Context

> **Status**: Completed | **Created**: 2026-03-09 | **Completed**: 2026-03-10
> **Design Doc**: `docs/design-docs/2026-03-09-manage-session-context-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-09 | Design | Use temp dir + .claude/ instead of --system-prompt | Enables slash commands, hooks, structured context |
| 2026-03-09 | Design | BELAYER_INSTANCE env var for instance resolution | Matches BELAYER_MAIL_ADDRESS pattern, commands "just work" |
| 2026-03-09 | Design | Static commands, templated CLAUDE.md | Only CLAUDE.md needs dynamic data, simpler |

## Progress

- [x] Task 1: Add BELAYER_INSTANCE env var fallback _(completed 2026-03-10)_
- [x] Task 2: Create manage.md CLAUDE.md template _(completed 2026-03-10)_
- [x] Task 3: Create static command files _(completed 2026-03-10)_
- [x] Task 4: Update embed directive and add PrepareManageDir function _(completed 2026-03-10)_
- [x] Task 5: Update belayer manage to use PrepareManageDir _(completed 2026-03-10)_
- [x] Task 6: Update CLAUDE.md with maintenance rule _(completed 2026-03-10)_
- [x] Task 7: Clean up old execClaude and unused imports _(completed 2026-03-10, no changes needed)_
- [x] Task 8: Integration verification _(completed 2026-03-10, all checks pass)_

## Surprises & Discoveries

_None yet — updated during execution by /harness:orchestrate._

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
-

**What didn't:**
-

**Learnings to codify:**
-
