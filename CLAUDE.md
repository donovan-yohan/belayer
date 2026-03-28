# belayer

Standalone Go CLI that orchestrates autonomous coding agents through declarative YAML pipelines. Three phases: **Explore** (intake via spec.md), **Climb** (per-repo implementation pipeline + PR), **Summit** (post-merge monitoring). Multi-repo adds setter (fan-out) and spotter (fan-in) as additive coordination layers.

## Quick Reference

| Action | Command |
|--------|---------|
| Build | `go build -o belayer ./cmd/belayer` |
| Test | `go test ./...` |
| Run | `./belayer` |
| Setup | `belayer setup --framework claude-tmux` |

## Documentation Map

| Category | Path | When to look here |
|----------|------|-------------------|
| Architecture | `docs/ARCHITECTURE.md` | Module boundaries, data flow, pipeline engine |
| Design | `docs/DESIGN.md` | Patterns, framework model, agentic node contracts |
| Quality | `docs/QUALITY.md` | Test runner, test files, isolation patterns |
| Plans | `docs/PLANS.md` | Active work, completed plans, tech debt |
| Learnings | `docs/LEARNINGS.md` | Past learnings, corrections, patterns discovered across sessions |
| Design Docs | `docs/design-docs/` | Feature brainstorm outputs and design decisions |
| ADRs | `docs/adrs/` | Architecture decision records |
| TODOs | `docs/TODOS.md` | Deferred items, P2/P3 backlog, tech debt tracker |
| Plugins | `docs/PLUGINS.md` | Plugin authoring patterns, invocation mandates, version sync, merge-friendly formats |
| Review Guidance | `docs/REVIEW_GUIDANCE.md` | Adversarial review config, deployment context, question bank |

## Key Patterns

- **Pipeline-as-YAML**: Nodes define `command:` (what to exec), `description:` (what to do), routing (`on_pass`/`on_retry`/`on_fail`). Belayer execs the command, polls for completion.
- **Framework model**: `belayer setup --framework <name-or-path>` scaffolds pipeline + scripts into `.belayer/`. Orchestration config is committed; runtime state is in `.belayer/.internal/` (gitignored).
- **Node protocol**: Core writes `node-context.json` before spawning. Framework commands read it for context. Commands write completion files when done.
- **ExecSpawner**: Core spawner execs `command:` from YAML via `sh -c`. Returns exit channel for fast-fail. Context-aware (kills process on cancellation).
- **Score-then-route**: Gate nodes produce structured scores; Go code computes weighted average; YAML thresholds route PASS/RETRY/FAIL. Anti-gaming by design.
- **Three-phase model**: Explore (intake) -> Climb (implementation) -> Summit (output). Multi-repo adds setter (fan-out) + spotter (fan-in) as additive layers.
- **Plugin version sync**: When modifying `plugins/*/`, bump version in all 3 locations: `plugin.json`, `registry.go` constant, and `agentassets_test.go` `TestPluginVersion`
- **New output types**: Adding a pipeline output type requires updates in: `validate.go` (validOutputTypes map), `model.go` (OutputConfig comment), and `outcome/detect.go` (typeDefault switch). Missing `detect.go` causes silent false-positive outcomes.

## Plugin Development

When editing files under `plugins/*/`, you MUST use `plugin-dev` skills before making changes. See `docs/PLUGINS.md` for full patterns (invocation mandates, MANDATORY blocks, merge-friendly formats, version sync).

## Workflow

> brainstorm -> plan -> orchestrate -> complete

| Step | Command | Purpose |
|------|---------|---------|
| 1 | `/harness:brainstorm` | Design through collaborative dialogue |
| 2 | `/harness:plan` | Create living implementation plan |
| 3 | `/harness:orchestrate` | Execute with agent teams + micro-reflects |
| 4 | `/harness:complete` | Reflect, review, and create PR |
