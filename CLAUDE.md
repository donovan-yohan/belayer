# belayer

Standalone Go CLI that orchestrates autonomous coding agents through declarative YAML pipelines. Pure orchestration layer — not an agent, not a harness. Three phases: **Explore** (intake — idea to spec), **Climb** (implementation — agent does the work), **Summit** (output — review, gates, PR). Agent-agnostic — works with Claude, Codex, or any agent that can run in a shell.

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
| Philosophy | `docs/PHILOSOPHY.md` | Three roles separation, H-as-feature, orchestration-only identity, learning loop rationale |
| Quality | `docs/QUALITY.md` | Test runner, test files, isolation patterns |
| Plans | `docs/PLANS.md` | Active work, completed plans, tech debt |
| Learnings | `docs/LEARNINGS.md` | Past learnings, corrections, patterns discovered across sessions |
| Design Docs | `docs/design-docs/` | Feature brainstorm outputs and design decisions |
| ADRs | `docs/adrs/` | Architecture decision records |
| TODOs | `docs/TODOS.md` | Deferred items, P2/P3 backlog, tech debt tracker |
| Review Guidance | `docs/REVIEW_GUIDANCE.md` | Adversarial review config, deployment context, question bank |

## Key Patterns

- **Pipeline-as-YAML**: Nodes define `command:` (what to exec), `description:` (what to do), routing (`on_pass`/`on_retry`/`on_fail`). Belayer execs the command, polls for completion.
- **Framework model**: `belayer setup --framework <name-or-path>` scaffolds pipeline + scripts into `.belayer/`. Orchestration config is committed; runtime state is in `.belayer/.internal/` (gitignored).
- **Node protocol**: Core writes `node-context.json` before spawning. Framework commands read it for context. Commands write completion files when done.
- **ExecSpawner**: Core spawner execs `command:` from YAML via `sh -c`. Returns exit channel for fast-fail. Context-aware (kills process on cancellation).
- **Score-then-route**: Gate nodes produce structured scores; Go code computes weighted average; YAML thresholds route PASS/RETRY/FAIL. Anti-gaming by design.
- **Three-phase model**: Explore (intake — idea to spec) → Climb (implementation — agent does the work) → Summit (output — review, gates, PR). Belayer is orchestration-only.
- **New output types**: Adding a pipeline output type requires updates in: `validate.go` (validOutputTypes map), `model.go` (OutputConfig comment), and `outcome/detect.go` (typeDefault switch). Missing `detect.go` causes silent false-positive outcomes.

## gstack

- Use `/browse` from gstack for ALL web browsing — never use `mcp__claude-in-chrome__*` tools
- Available skills: /office-hours, /plan-ceo-review, /plan-eng-review, /plan-design-review, /design-consultation, /design-shotgun, /design-html, /review, /ship, /land-and-deploy, /canary, /benchmark, /browse, /connect-chrome, /qa, /qa-only, /design-review, /setup-browser-cookies, /setup-deploy, /retro, /investigate, /document-release, /codex, /cso, /autoplan, /careful, /freeze, /guard, /unfreeze, /gstack-upgrade, /learn
- If gstack skills aren't working, run `cd .claude/skills/gstack && ./setup` to rebuild
