# v2 Wiring Gaps — Worker, DSL, Pipeline CLI, WorkDir

> **Status**: Completed | **Created**: 2026-03-19 | **Completed**: 2026-03-19
> **Design Doc**: `docs/designs/v2-wiring-gaps.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-19 | Design | Worker is a separate command, not embedded in run | Matches Temporal conventions — worker and client are separate processes |
| 2026-03-19 | Design | Route serialized in RouteInput, not re-parsed in workflow | Temporal workflow determinism — can't do file I/O inside workflows |
| 2026-03-19 | Design | CWD as WorkDir for MVP | Full crag integration deferred — CWD is sufficient for testing |

## Progress

- [x] Task 1: Worker command — `belayer v2 worker`
- [x] Task 2: DSL wiring — Route in RouteInput, run parses YAML
- [x] Task 3: Pipeline CLI wiring — show + validate
- [x] Task 4: WorkDir resolution + worker test

## Surprises & Discoveries

_None yet._

## Plan Drift

_None yet._

---

### Task 1: Worker command — `belayer v2 worker`

**Goal:** Add a `belayer v2 worker` command that starts a Temporal worker, registers the Route workflow and activities with real providers, and blocks until interrupted.

**Files:**
- `internal/v2/cli/worker.go` (new)
- `internal/v2/cli/root.go` (modify — add worker command)

**Steps:**

1. Create `internal/v2/cli/worker.go`:
   - Connect to Temporal via `client.Dial()`
   - Create `worker.New(c, TaskQueueName, worker.Options{})`
   - Wire `Activities` with `ClaudeSessionSpawner` (from provider/session.go) and `ExecProvider` (from provider/exec.go)
   - Register `RouteWorkflow` and activities
   - Call `w.Run(worker.InterruptCh())` to block until Ctrl+C
   - Print startup message with task queue name

2. Add `newWorkerCmd()` to `internal/v2/cli/root.go`.

**Tests:** Build verification — `go build ./...` passes. Worker registration is already tested in `integration_test.go`.

---

### Task 2: DSL wiring — Route in RouteInput, run parses YAML

**Goal:** Wire `belayer v2 run` to parse the pipeline YAML and pass the Route to the workflow. Update RouteInput and the workflow to use the provided Route instead of hardcoded default.

**Files:**
- `internal/v2/model/types.go` (modify — add Route field to RouteInput)
- `internal/v2/temporal/workflow.go` (modify — use input Route instead of defaultMVPRoute)
- `internal/v2/cli/run.go` (modify — parse YAML, pass Route)
- `internal/v2/temporal/workflow_test.go` (modify — pass Route in test inputs)

**Steps:**

1. Add `Route *pipeline.Route` field to `RouteInput` (as `json.RawMessage` to avoid Temporal serialization issues with `time.Duration`).

   Actually — Temporal serializes Go structs via JSON. `time.Duration` serializes as an integer (nanoseconds) which round-trips fine. But the `yaml` tags won't matter since Temporal uses JSON. So we can embed the Route directly, but need to use a serialization-safe representation.

   Simplest: add `RouteJSON json.RawMessage` to `RouteInput`. The CLI serializes the parsed Route to JSON and passes it. The workflow deserializes it back. This avoids any Temporal serialization edge cases.

2. Update `RouteWorkflow` to unmarshal `input.RouteJSON` into a `pipeline.Route`. Fall back to `defaultMVPRoute()` if empty.

3. Update `belayer v2 run` to:
   - Check for `--pipeline` flag or `belayer-pipeline.yaml` in CWD
   - If found: parse, validate, serialize to JSON, set in RouteInput
   - If not found: log "Using default pipeline (solo)" and leave RouteJSON empty

4. Update workflow tests to pass RouteJSON (or leave empty to test fallback).

**Tests:**
- Workflow with explicit RouteJSON uses it
- Workflow with empty RouteJSON falls back to default
- `go test ./internal/v2/...` passes

---

### Task 3: Pipeline CLI wiring — show + validate

**Goal:** Wire the `belayer v2 pipeline show` and `validate` stub commands to actually parse and display/validate the pipeline.

**Files:**
- `internal/v2/cli/pipeline.go` (modify — replace stubs with real implementations)

**Steps:**

1. Extract a shared helper `findAndParsePipeline(pipelineFlag string) (*pipeline.Route, error)` that:
   - Uses `--pipeline` flag if set
   - Otherwise looks for `belayer-pipeline.yaml` in CWD
   - Falls back to embedded default with a note
   - Parses and returns

2. Wire `pipeline show`:
   - Call `findAndParsePipeline`
   - Call `pipeline.Visualize(route, nil)` (no live status for now)
   - Print the output

3. Wire `pipeline validate`:
   - Call `findAndParsePipeline`
   - Call `pipeline.ValidateOrError(route)`
   - Print "Pipeline valid" or the error details

**Tests:** Manual verification via CLI. The parser and visualizer already have unit tests.

---

### Task 4: WorkDir resolution + E2E smoke test

**Goal:** Set WorkDir to the current working directory in the TypeBSpawnActivity, and write a quick manual test script.

**Files:**
- `internal/v2/temporal/activity.go` (modify — resolve WorkDir)
- `internal/v2/cli/worker.go` (modify — pass CWD to activities)

**Steps:**

1. Update `Activities` struct to include a `WorkDir string` field.

2. Update `TypeBSpawnActivity` to use `a.WorkDir` if `input.WorkDir` is empty.

3. Update the worker command to set `Activities.WorkDir` to `os.Getwd()`.

4. Verify the full E2E flow works:
   ```bash
   # Terminal 1: Start Temporal
   belayer v2 temporal start

   # Terminal 2: Start worker
   belayer v2 worker

   # Terminal 3: Run pipeline
   belayer v2 run "test the pipeline"
   belayer v2 status
   # Should show active workflow

   # Verify pipeline visualization
   belayer v2 pipeline show
   belayer v2 pipeline validate
   ```

**Tests:** `go build ./...` and `go test ./internal/v2/...` pass.

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
- Small focused design doc → 4 clean tasks with no ambiguity
- findAndParsePipeline shared helper avoids DRY violation between run, show, and validate
- RouteJSON in RouteInput (json.RawMessage) cleanly separates CLI serialization from workflow deserialization

**What didn't:**
- Nothing — clean wiring work, no surprises

**Learnings to codify:**
- Temporal workflows can't do file I/O — route must be serialized in the workflow input, not parsed inside the workflow
- Cobra flag binding to closure variables works correctly even when multiple subcommands share the variable (only one runs at a time)
