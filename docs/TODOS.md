# TODOs

Deferred items from architecture reviews and implementation plans.

## Active

### P2: Multi-repo Runtime
- Implement setter/spotter as top-level pipeline YAML config
- Temporal child workflows for per-repo fan-out
- Runtime enforcement: multi-repo crag without setter/spotter = error
**Context:** Architecture defined in design doc. Depends on code restructuring (P1, completed 2026-03-27).

### P2: `using-belayer` Skill
- Create Claude Code / Codex skill that agents load to bootstrap belayer configs
- Natural language -> YAML pipeline generation
- Comprehensive `--help` on all CLI commands
**Context:** Distribution strategy. Depends on CLI and config model being stable. Deferred in CEO review 2026-03-25.

### P2: Summit Operations
- `belayer summit` CLI command for post-merge operations
- Risk-gated auto-merging
- Observability: log monitoring in stage/prod
**Context:** Interface defined (PR manifest JSON). Implementation deferred.

### P2: Multi-language Node SDKs
- Thin SDK libraries (Go, Python, TypeScript) that read `node-context.json` and write completion files
- Orchestrator-side SDK: submit pipelines, check status, stream events via worker HTTP API
- Each SDK wraps the file-based completion contract so users don't hand-write JSON
**Context:** Lowers the barrier for non-Go users to implement belayer nodes. Enables front-end-driven orchestrators. Depends on ExecSpawner + `node-context.json` contract. Identified in CEO review 2026-03-26.

### P3: Boulderer
- `belayer solo <task>` CLI command
- Pipeline-dispatched boulderer (nodes can spawn one)
- Max dispatch limits per problem
**Context:** Lightweight specialist agent for CI fixups and PR nitpicks. Deferred until implement retry demonstrably fails.

### P3: Pipeline Template Marketplace
- Share and discover pipeline YAML configs
- Similar to Docker Hub for CI/CD workflows
**Context:** Requires adoption first. Deferred in CEO review 2026-03-25.

## Tech Debt

| Issue | Severity | Notes |
|-------|----------|-------|
| Schedule reconciliation is a stub | Medium | `internal/intake/schedule.go` logs intent but does not create Temporal schedules |
| Worker `/status` endpoint is a stub | Low | Returns health only -- full workflow listing deferred |
| `StartSHA` in `NodeActivityInput` never populated | Low | Code-output commit verification guard never fires |
