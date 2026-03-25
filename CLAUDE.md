# belayer

Standalone Go CLI that orchestrates autonomous coding agents across repositories via three phases: **Explore** (intake via spec.md), **Climb** (per-repo implementation pipeline + PR), **Summit** (post-merge monitoring). Multi-repo adds setter (fan-out) and spotter (fan-in) as additive coordination layers.

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
| Learnings | `docs/LEARNINGS.md` | Past learnings, corrections, patterns discovered across sessions |
| Design Docs | `docs/design-docs/` | Feature brainstorm outputs and design decisions |
| ADRs | `docs/adrs/` | Architecture decision records |
| TODOs | `docs/TODOS.md` | Deferred items, P1/P2/P3 backlog, tech debt tracker |

## Key Patterns

- **Stripe-style blueprint**: Deterministic Go code handles orchestration; ephemeral Claude sessions for judgment calls
- **Three-phase model**: Explore (intake) -> Climb (implementation) -> Summit (output). Multi-repo adds setter (fan-out) + spotter (fan-in) as additive layers
- **Bare repos + worktrees**: extend-cli pattern for repo isolation
- **SQLite**: Single source of truth for all state (problems, leads, verdicts, events)
- **Agentic nodes**: Ephemeral Claude sessions for: sufficiency checks, problem decomposition, alignment reviews, stuck analysis
- **Long-lived crags with problem isolation**: Crag (repos, config) persists; each problem gets isolated worktrees
- **Idempotent store operations**: All DB writes that can be retried (Init, env create, worktree setup) use `INSERT OR REPLACE`. See `docs/QUALITY.md` for the full idempotency requirement.
- **Interactive session context**: `internal/defaults/claudemd/setter.md`, `internal/defaults/claudemd/explorer.md`, and the relevant `internal/defaults/commands/blr-*.md` assets are deployed into `belayer setter` and `belayer explorer` sessions. When these CLI surfaces change, update the templates/commands together and verify them during code review.
- **Plugin version sync**: When modifying `plugins/*/`, bump version in all 3 locations: `plugin.json`, `registry.go` constant (`HarnessVersion`/`PRVersion`/`ExplorerVersion`), and `agentassets_test.go` `TestPluginVersion`
- **New output types**: Adding a pipeline output type requires updates in: `validate.go` (validOutputTypes map), `model.go` (OutputConfig comment), and `outcome/detect.go` (typeDefault switch). Missing `detect.go` causes silent false-positive outcomes.
- **Default pipeline changes**: Modifying `DefaultPipelineYAML` in `defaults.go` breaks Temporal workflow tests using `defaultInput()` — update mocks in `workflow_test.go`. Go raw string constants cannot contain backticks.

## Workflow

> brainstorm -> plan -> orchestrate -> complete

| Step | Command | Purpose |
|------|---------|---------|
| 1 | `/harness:brainstorm` | Design through collaborative dialogue |
| 2 | `/harness:plan` | Create living implementation plan |
| 3 | `/harness:orchestrate` | Execute with agent teams + micro-reflects |
| 4 | `/harness:complete` | Reflect, review, and create PR |
