# belayer

Standalone Go CLI tool that orchestrates autonomous coding agents across multiple repositories. Takes work items, decomposes them into per-repo tasks, spawns isolated lead execution loops in git worktrees, monitors progress, and validates cross-repo alignment before creating PRs.

## Quick Reference

| Action | Command |
|--------|---------|
| Build | `go build -o belayer ./cmd/belayer` |
| Test | `go test ./...` |
| Run | `./belayer` |

## Documentation Map

| Category | Path | When to look here |
|----------|------|-------------------|
| Architecture | `docs/ARCHITECTURE.md` | Module boundaries, data flow, coordinator engine |
| Design | `docs/DESIGN.md` | Patterns, SQLite schema, agentic node contracts |
| Frontend | `docs/TUI.md` | bubbletea components, state management |
| Quality | `docs/QUALITY.md` | Test runner, test files, isolation patterns |
| Plans | `docs/PLANS.md` | Active work, completed plans, tech debt |
| Design Docs | `docs/design-docs/` | Feature brainstorm outputs and design decisions |
| ADRs | `docs/adrs/` | Architecture decision records |

## Key Patterns

- **Stripe-style blueprint**: Deterministic Go code handles orchestration; ephemeral Claude sessions for judgment calls
- **3 layers**: User -> Coordinator (Go state machine) -> Lead (bundled execution loop)
- **Bare repos + worktrees**: extend-cli pattern for repo isolation
- **SQLite**: Single source of truth for all state (tasks, leads, verdicts, events)
- **Agentic nodes**: Ephemeral Claude sessions for: sufficiency checks, task decomposition, alignment reviews, stuck analysis
- **Long-lived instances with task isolation**: Instance (repos, config) persists; each task gets isolated worktrees
- **Manage session context**: `internal/defaults/claudemd/manage.md` and `internal/defaults/commands/*.md` are deployed into `belayer manage` sessions. When CLI commands change, update these files. Verify during code review.

## Workflow

> brainstorm -> plan -> orchestrate -> complete

| Step | Command | Purpose |
|------|---------|---------|
| 1 | `/harness:brainstorm` | Design through collaborative dialogue |
| 2 | `/harness:plan` | Create living implementation plan |
| 3 | `/harness:orchestrate` | Execute with agent teams + micro-reflects |
| 4 | `/harness:complete` | Reflect, review, and create PR |
