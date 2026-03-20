---
status: implemented
implemented-by: docs/exec-plans/completed/2026-03-19-v2-wiring-gaps.md
created: 2026-03-19
branch: master
---
# Design: v2 Wiring Gaps — Worker, DSL, Crag Integration

## Summary

The v2 Temporal orchestrator platform has the core contracts, workflow, and providers built. Four wiring gaps remain before an end-to-end demo works:

1. **Worker command** — `belayer v2 worker` starts a Temporal worker that executes the Route workflow and activities. Without this, `belayer v2 run` starts a workflow but nothing picks it up.
2. **DSL wiring** — `belayer v2 run` currently uses a hardcoded MVP route. Wire it to parse the pipeline YAML file.
3. **Pipeline visualization wiring** — `belayer v2 pipeline show` and `validate` are stubs. Wire them to the parser and visualizer.
4. **Crag/repo integration** — The TypeBSpawnActivity needs to create a worktree (via existing v1 crag/repo packages) and set WorkDir before spawning the session.

## Approach

### Worker Command
A `belayer v2 worker` command that:
- Connects to Temporal at `localhost:7233`
- Creates a `worker.New()` with the `belayer-route` task queue
- Registers `RouteWorkflow` and `Activities` (with real providers)
- Blocks until interrupted

The worker uses the real `ClaudeSessionSpawner` (from `internal/v2/provider/session.go`) and the real `ExecProvider` (from `internal/v2/provider/exec.go`). The `Activities` struct wires them together.

### DSL Wiring
Update `belayer v2 run` to:
1. Look for `belayer-pipeline.yaml` in the current directory (or `--pipeline` flag)
2. Parse it via `pipeline.ParseRouteFile()`
3. Validate via `pipeline.ValidateOrError()`
4. Pass the parsed Route as part of `RouteInput` to the workflow

Update the Route workflow to accept a serialized Route in its input instead of using `defaultMVPRoute()`.

### Pipeline CLI Wiring
Update `belayer v2 pipeline show` to:
1. Find and parse the pipeline YAML
2. Call `pipeline.Visualize()` and print the output

Update `belayer v2 pipeline validate` to:
1. Find and parse the pipeline YAML
2. Call `pipeline.ValidateOrError()` and print the result

### Crag Integration
For the MVP, keep it simple: the TypeBSpawnActivity uses the current working directory as the WorkDir. Full crag/worktree integration (bare repos, per-problem worktrees) is a follow-up.

This means for testing: you run `belayer v2 run` from within a git repo, and the setter/lead sessions open in that repo's directory.

## Key Decisions
- Worker is a separate long-running command, not embedded in `belayer v2 run` (matches Temporal conventions)
- Pipeline DSL is optional — if no file exists, use embedded default (solo template)
- Crag integration is deferred to keep this focused on making the demo work
- Route is serialized as JSON in RouteInput rather than re-parsed inside the workflow (Temporal determinism)
