# TODOs

Deferred items from architecture reviews and implementation plans.

## Active

### P1: Code Restructuring
- Rename default pipeline nodes in `internal/v3/pipeline/defaults.go` (setter->plan, lead->implement, spotter->review, summit->pr-author)
- Remove generic `FanOut`/`Per`/`FanIn` from `internal/v3/pipeline/model.go`
- Update pipeline templates in `internal/v3/pipeline/templates/`
**Context:** Three-phase architecture (2026-03-25) reframes setter/spotter as multi-repo-only roles. Current default pipeline uses these names incorrectly. Blocked by: documentation round completing first.

### P2: Multi-repo Runtime
- Implement setter/spotter as top-level pipeline YAML config
- Temporal child workflows for per-repo fan-out
- Runtime enforcement: multi-repo crag without setter/spotter = error
**Context:** Architecture defined in design doc. Depends on code restructuring (P1) completing first.

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

### P3: Boulderer
- `belayer solo <task>` CLI command
- Pipeline-dispatched boulderer (nodes can spawn one)
- Max dispatch limits per problem
**Context:** Lightweight specialist agent for CI fixups and PR nitpicks. Deferred until lead retry demonstrably fails.

### P3: Pipeline Template Marketplace
- Share and discover pipeline YAML configs
- Similar to Docker Hub for CI/CD workflows
**Context:** Requires adoption first. Deferred in CEO review 2026-03-25.

## Tech Debt

| Issue | Severity | Notes |
|-------|----------|-------|
| Schedule reconciliation is a stub | Medium | `internal/v3/intake/schedule.go` logs intent but does not create Temporal schedules |
| Worker `/status` endpoint is a stub | Low | Returns health only -- full workflow listing deferred |
| `StartSHA` in `NodeActivityInput` never populated | Low | Code-output commit verification guard never fires |
