# belayer

Standalone Go CLI tool that orchestrates autonomous coding agents across multiple repositories. Takes work items (problems), decomposes them into per-repo climbs, spawns isolated lead execution loops in git worktrees, monitors progress, and validates cross-repo alignment before creating PRs.

## Quick Reference

| Action | Command |
|--------|---------|
| Build | `go build -o belayer ./cmd/belayer` |
| Test | `go test ./...` |
| Run | `./belayer` |

## Documentation Map

| Category | Path | When to look here |
|----------|------|-------------------|
| Architecture | `docs/ARCHITECTURE.md` | Module boundaries, data flow, belayer daemon engine |
| Design | `docs/DESIGN.md` | Patterns, SQLite schema, agentic node contracts |
| Quality | `docs/QUALITY.md` | Test runner, test files, isolation patterns |
| Plans | `docs/PLANS.md` | Active work, completed plans, tech debt |
| Design Docs | `docs/design-docs/` | Feature brainstorm outputs and design decisions |
| ADRs | `docs/adrs/` | Architecture decision records |

## Key Patterns

- **Stripe-style blueprint**: Deterministic Go code handles orchestration; ephemeral Claude sessions for judgment calls
- **3 layers**: User (setter) -> Belayer (Go DAG executor daemon) -> Lead (bundled execution loop)
- **Bare repos + worktrees**: extend-cli pattern for repo isolation
- **SQLite**: Single source of truth for all state (problems, leads, verdicts, events)
- **Agentic nodes**: Ephemeral Claude sessions for: sufficiency checks, problem decomposition, alignment reviews, stuck analysis
- **Long-lived crags with problem isolation**: Crag (repos, config) persists; each problem gets isolated worktrees
- **Setter session context**: `internal/defaults/claudemd/setter.md` and `internal/defaults/commands/*.md` are deployed into `belayer setter` sessions. When CLI commands change, update these files. Verify during code review.

## Workflow

> brainstorm -> plan -> orchestrate -> complete

| Step | Command | Purpose |
|------|---------|---------|
| 1 | `/harness:brainstorm` | Design through collaborative dialogue |
| 2 | `/harness:plan` | Create living implementation plan |
| 3 | `/harness:orchestrate` | Execute with agent teams + micro-reflects |
| 4 | `/harness:complete` | Reflect, review, and create PR |
