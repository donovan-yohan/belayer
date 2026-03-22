---
status: ACTIVE
created: 2026-03-20
branch: clarify-gstack-implementation-philosophy
supersedes: (CEO plan, rewritten after eng + codex reviews)
---
# Design: Intake Plugin Model + Pipeline Template Library

## Summary

Add an `intake:` section to belayer's v3 pipeline YAML (`PipelineConfig`) that defines where work comes from. Intake plugins poll external sources (Jira, Linear, GitHub Issues) or accept interactive submissions, convert items to specs, and start `ClimbWorkflow` runs. Output is modeled as late-pipeline nodes (pr-creator, ci-monitor) within the existing flat-node graph.

All code snippets in this document show **target state after Phase 1 is complete**, not the current codebase.

## Design Principles

**Belayer is plumbing.** It routes typed references between nodes. Nodes are black boxes.

- Belayer does not read file contents, compute diffs, or interpret code
- Belayer passes references (commit SHAs, file paths) between nodes
- Nodes (agent sessions) decide what to do with the references they receive
- The only "business logic" Belayer owns is gate scoring and threshold routing — because routing decisions ARE the plumbing

## Architecture

v3 is the **only** pipeline model. v1 and v2 are deleted. The runtime serves the model.

```
┌─────────────────┐
│  INTAKE LAYER   │    Boundary process — outside the pipeline graph
│                 │
│ interactive     │    belayer start → user submits via MCP tool
│ jira            │    Temporal Scheduled Workflow polls Jira API
│ linear          │    (Phase 2)
│ github-issues   │    (Phase 2)
│ exec            │    (Phase 2)
│                 │
│ Each produces   │
│ SubmitSpec JSON │
└────────┬────────┘
         │ SubmitSpec → ClimbInput bridge
         │ (creates worktree, mints branch, resolves pipeline)
         ▼
┌─────────────────────────────────────────────────────┐
│  PIPELINE (ClimbWorkflow)                            │
│                                                      │
│  Flat nodes with on_pass/on_retry/on_fail routing    │
│  Gates with dimensions, thresholds, scoring          │
│  Belayer passes references — nodes do the work       │
│                                                      │
│  lead ──[commit]──► code-review gate                 │
│    ▲                     │                            │
│    └─────────────────────┘ on_retry                  │
│                     │ on_pass                         │
│                     ▼                                 │
│              pr-creator ──[file: pr-url]──► stop     │
└─────────────────────────────────────────────────────┘
```

### Three Boundary Concerns

1. **Intake → Pipeline:** Polls sources, produces `SubmitSpec`, creates worktree+branch, starts `ClimbWorkflow`
2. **Pipeline (ClimbWorkflow):** Routes typed references between nodes. Gate scoring determines routing. This is v3 today.
3. **Output → Pipeline feedback (Phase 2):** PR creation and CI monitoring as late-pipeline nodes. CI failure starts a new `ClimbWorkflow` with `from_node` + feedback context.

## Node Contract Model

### Output Types

Nodes produce one of two output types:

| Type | What it is | Example |
|------|-----------|---------|
| `commit` | A commit SHA on the worktree branch | Lead finishes coding, commits, outputs `a1b2c3d` |
| `file` | A file path relative to the worktree | Decomposer writes `specs.json`, outputs the path |

Gates produce `gate_result` — structured scoring JSON + rationale file path. This is a specialized `file` output.

### What Belayer Does With References

The activity's only job per node:
1. Write the input reference to `.belayer/input/context.json`
2. Spawn the agent session
3. Read the output reference from `.belayer/output/result.json`
4. Pass the reference to the next node

Belayer never reads file contents, never computes diffs, never interprets code. It moves references.

### Input Materialization

When a node receives an input, the activity writes a context file:

```json
// .belayer/input/context.json — written by activity, read by agent session
{
  "type": "commit",
  "sha": "a1b2c3d",
  "base": "e4f5g6h",
  "description": "Implement user authentication for all platforms"
}
```

The agent session decides what to do with it. A code-review gate session might run `git diff e4f5g6h..a1b2c3d`. A pr-creator session might run `gh pr create --base main --head belayer/auth-impl-abc123`. Belayer doesn't prescribe — it just delivers the reference.

For `file` inputs:
```json
{
  "type": "file",
  "path": ".belayer/output/specs.json",
  "description": "Problem decomposition output"
}
```

### Output Contract

Nodes write their output to `.belayer/output/result.json`:

```json
// Commit output (from lead node)
{ "type": "commit", "sha": "a1b2c3d", "base": "e4f5g6h" }

// File output (from decomposer, pr-creator)
{ "type": "file", "path": ".belayer/output/specs.json" }

// Gate result output (from gates)
{ "type": "gate_result", "path": ".belayer/output/gate-result.json", "rationale": ".belayer/output/rationale.md" }
```

The `base` field in commit outputs records what the branch was created from. This flows naturally — the worktree creation step knows the base ref, and the lead node inherits it. No pipeline-level config needed.

### The Full Contract Table

```
NODE TYPE    | TYPICAL INPUT    | TYPICAL OUTPUT   | WHAT THE AGENT DOES
-------------|-----------------|------------------|--------------------
lead         | description     | commit           | Implements spec, commits code
gate         | commit          | gate_result      | Reads diff (however it wants), scores
decomposer   | description     | file             | Breaks spec into sub-tasks
pr-creator   | commit          | file (pr-url)    | Creates PR from branch
ci-monitor   | file (pr-url)   | commit or file   | Monitors CI, may fix failures
doc-release  | commit          | file             | Generates docs from changes
```

### Gate Scoring — The One Piece of Business Logic

Gates are the exception to "Belayer is just plumbing." After a gate session produces `gate-result.json`, Belayer:
1. Parses the JSON (dimension scores)
2. Computes the weighted score
3. Applies thresholds to determine PASS/RETRY/FAIL
4. Routes accordingly

Score-then-route, not route-then-score. The gate session doesn't decide the outcome — the thresholds do. This prevents gaming.

## Pipeline YAML Schema

```yaml
name: gstack-review-loop
intake:
  - name: jira-backlog
    type: jira
    config:
      project: FOX
      filter: "status = 'Ready for Dev' AND priority >= High"
      credential_env: JIRA_API_TOKEN
      poll_interval: 5m
  - name: user-session
    type: interactive

nodes:
  - name: lead
    type: node
    description: "Implement the spec. Commit your changes when done."
    input: { type: description }
    output: { type: commit }
    on_pass: code-review
    on_retry: self
    on_fail: stop
    max_retries: 3

  - name: code-review
    type: gate
    description: |
      You are an adversarial code reviewer. Read the input context to find
      the commit SHA and base ref. Review the changes for quality.
    input: { type: commit }
    output: { type: gate_result }
    dimensions:
      - { name: correctness, weight: 0.3, description: "Does the code do what the spec says?" }
      - { name: architecture, weight: 0.25, description: "Clean abstractions?" }
      - { name: test_coverage, weight: 0.25, description: "Are changes tested?" }
      - { name: code_quality, weight: 0.2, description: "Naming, style, no dead code" }
    thresholds: { pass: 7.0, retry: 4.0 }
    on_pass: pr-creator
    on_retry: lead
    on_fail: stop
    max_retries: 2

  - name: pr-creator
    type: node
    description: "Create a PR from the changes. Read the commit SHA from input context."
    input: { type: commit }
    output: { type: file, key: pr-url }
    on_pass: stop
    on_fail: stop

safety:
  max_concurrent_runs: 3
```

### ci-monitor Node (Phase 2 — YAML example for reference)

```yaml
  - name: ci-monitor
    type: node
    description: "Monitor the PR's CI status. If CI fails, attempt a fix and commit."
    input: { type: file, key: pr-url }
    output: { type: commit }
    on_pass: stop           # CI passed, done
    on_fail: lead           # CI failed after fix attempt, route back to lead
    max_retries: 1
```

The feedback loop (CI failure → new ClimbWorkflow) is a Phase 2 implementation detail. It will use either a Temporal child workflow or the `from_node` restart mechanism.

## Model Types

```go
// internal/v3/pipeline/model.go — TARGET STATE

type PipelineConfig struct {
    Name   string         `yaml:"name" json:"name"`
    Intake []IntakeConfig `yaml:"intake,omitempty" json:"intake,omitempty"`
    Nodes  []NodeConfig   `yaml:"nodes" json:"nodes"`
    Safety SafetyConfig   `yaml:"safety,omitempty" json:"safety,omitempty"`
}

type IntakeConfig struct {
    Name   string            `yaml:"name" json:"name"`
    Type   string            `yaml:"type" json:"type"`     // jira | interactive (Phase 1); linear | github-issues | exec (Phase 2)
    Config map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
}

type SafetyConfig struct {
    MaxConcurrentRuns int `yaml:"max_concurrent_runs,omitempty" json:"max_concurrent_runs,omitempty"`
}

// Existing NodeConfig gains fan-out fields:
type NodeConfig struct {
    // ... existing fields (Name, Type, Description, Input, Output, Dimensions, Thresholds, OnPass, OnRetry, OnFail, MaxRetries)
    FanOut string `yaml:"fan_out,omitempty" json:"fan_out,omitempty"`
    Per    string `yaml:"per,omitempty" json:"per,omitempty"`
    FanIn  string `yaml:"fan_in,omitempty" json:"fan_in,omitempty"`
}

// OutputConfig updated for commit type:
type OutputConfig struct {
    Type          string `yaml:"type" json:"type"`                                       // file | commit | gate_result
    Key           string `yaml:"key,omitempty" json:"key,omitempty"`
    Path          string `yaml:"path,omitempty" json:"path,omitempty"`
    RationalePath string `yaml:"rationale_path,omitempty" json:"rationale_path,omitempty"`
}
```

```go
// internal/v3/model/types.go — TARGET STATE

type ClimbInput struct {
    Description  string   `json:"description"`
    DesignFile   string   `json:"design_file,omitempty"`
    PipelineFile string   `json:"pipeline_file,omitempty"`
    PipelineYAML []byte   `json:"pipeline_yaml,omitempty"`
    FromNode     string   `json:"from_node,omitempty"`
    InputPath    string   `json:"input_path,omitempty"`
    WorkDir      string   `json:"work_dir"`
    Branch       string   `json:"branch"`
    Repos        []string `json:"repos,omitempty"`    // NEW — from SubmitSpec
    BaseRef      string   `json:"base_ref,omitempty"` // NEW — what the branch was created from
}

type CompletionResult struct {
    Outcome    NodeOutcome `json:"outcome"`
    OutputPath string      `json:"output_path,omitempty"` // for file outputs
    CommitSHA  string      `json:"commit_sha,omitempty"`  // for commit outputs — NEW
    BaseRef    string      `json:"base_ref,omitempty"`    // what was diffed against — NEW
    TargetNode string      `json:"target_node,omitempty"`
    Feedback   string      `json:"feedback,omitempty"`
    Attempt    int         `json:"attempt"`
}
```

## SubmitSpec Contract

Every intake produces this JSON:

```go
// internal/v3/intake/spec.go
type SubmitSpec struct {
    Spec         string            `json:"spec"`
    Repos        []string          `json:"repos,omitempty"`
    Source       string            `json:"source"`        // "jira", "interactive", etc.
    ExternalID   string            `json:"external_id"`   // "FOX-1234", "submit-1710968400123"
    PipelineName string            `json:"pipeline_name"` // which pipeline to run
    Metadata     map[string]string `json:"metadata,omitempty"`
}
```

Note: `channel/channel.ts` currently sends `{spec, repos, pipeline}`. Phase 1 updates it to emit the full `SubmitSpec` format (adding `source`, `external_id`, `pipeline_name`).

## SubmitSpec → ClimbInput Bridge

The bridge is a shared function called by both `belayer climb` (CLI) and the intake polling activity:

```go
// internal/v3/intake/bridge.go
func StartClimb(ctx context.Context, tc client.Client, spec SubmitSpec, pipelineYAML []byte, repoDir string) (workflowID string, err error)
```

Steps:
1. Resolve pipeline YAML (from `spec.PipelineName` → file lookup, or passed directly)
2. Generate deterministic workflow ID: `{pipeline_name}/{intake_name}/{external_id}`
3. Start the Temporal workflow — Temporal assigns a unique **run ID**
4. Generate branch name using the **run ID** (not the workflow ID): `belayer/{slug}-{run_id}`
5. Create git worktree at `.belayer/worktrees/{run_id}`
6. Detect base ref via `git rev-parse HEAD` in the repo (what we branched from)
7. Build `ClimbInput` with `WorkDir`, `Branch`, `Repos`, `BaseRef`

**Why run ID for branches but workflow ID for dedup:**
- The workflow ID is deterministic (`pipeline/intake/external_id`) — Temporal rejects duplicates for dedup
- The run ID is unique per execution — so resubmitting a completed ticket creates a new branch, not a git collision
- Retries within a workflow reuse the same branch (same run ID, same worktree)

Refactors `belayer climb` by extracting `createGitWorktree`, `generateBranchSlug`, and the `ClimbInput` construction from `NewClimbCmd.RunE` into this shared bridge function.

## Deterministic Workflow ID

```
workflow_id = "{pipeline_name}/{intake_name}/{external_id}"
```

Examples:
- `gstack-review-loop/jira-backlog/FOX-1234`
- `gstack-review-loop/user-session/submit-1710968400123`

- **Dedup:** Temporal rejects duplicate workflow IDs natively. No SQLite dedup table.
- **Scoping:** Same ticket in different pipelines or intakes gets separate workflows.
- **Queryability:** `belayer status` queries Temporal by workflow ID prefix.
- **Resubmission:** `WorkflowIDReusePolicy: AllowDuplicate` — completed/failed workflows allow a new execution with the same ID. The new execution gets a new run ID → new branch.

For interactive intake, `external_id = submit-{unix_ms}` (millisecond precision, always unique).

## Worker Daemon

Phase 1 introduces `belayer worker` as a **long-lived daemon process** (migrated from v2's worker). This replaces the in-process worker model in `belayer climb`.

```
belayer worker
  │
  ├── Temporal worker (registers ClimbWorkflow + Activities)
  ├── Schedule reconciliation (on startup, reads pipeline YAML)
  ├── HTTP API:
  │     POST /start  — accepts SubmitSpec, calls bridge, returns workflow ID
  │     GET  /status — returns active workflows
  │
  └── Runs until stopped. Stop and restart to apply YAML changes.
```

`belayer climb` becomes a thin client that connects to the running worker's Temporal instance (no in-process worker). `belayer start` (interactive intake) POSTs to the worker's `/start` endpoint via the channel server.

The HTTP API port is configurable (default 8780, matching v2's existing port).

## Automated Intake Polling

A Temporal Scheduled Workflow polls each automated intake on its configured `poll_interval`.

**Schedule lifecycle (managed by worker on startup):**

```
Worker starts
  │
  ├── 1. Read pipeline YAML → extract intake: section
  ├── 2. List existing Temporal schedules via ScheduleClient().List()
  │      (client-side prefix filtering on "intake/{pipeline_name}/", paginated)
  ├── 3. Reconcile:
  │       - New intakes (in YAML, not in Temporal) → Create schedule
  │       - Changed intakes (interval differs) → Update schedule
  │       - Removed intakes (in Temporal, not in YAML) → Pause schedule (not delete)
  │       - Unchanged → no-op
  │
  └── 4. Worker runs until stopped. Stop and restart to apply YAML changes.
```

Each poll:
1. Fetches items from the source (Jira API)
2. Converts each item to a `SubmitSpec`
3. Calls `StartClimb` bridge function
4. Temporal's workflow ID uniqueness handles dedup

## Interactive Intake

When `belayer start` opens a Claude Code session, it reads the pipeline YAML from CWD to determine the `pipeline_name`. The submit MCP tool sends a `SubmitSpec` with `source: "interactive"` and `external_id: "submit-{unix_ms}"`. The channel server POSTs to the worker's `/start` endpoint.

The `intake: interactive` entry in the YAML is declarative — it tells the pipeline "this pipeline accepts interactive submissions." It does NOT start a session. The YAML validator rejects duplicate `interactive` entries.

`max_concurrent_runs` does NOT apply to interactive submissions. User-initiated work is always accepted. The limit only constrains automated intake.

## Concurrency Limits

```yaml
safety:
  max_concurrent_runs: 3
```

The intake polling activity checks active workflow count (via Temporal list-workflows query with 5s timeout, fail-open on timeout) before submitting new specs. If at capacity, specs are logged and retried on the next poll.

## Fan-Out (Schema Now, Runtime Phase 2)

```go
// On NodeConfig — parsed and validated, not executed in Phase 1
FanOut string `yaml:"fan_out,omitempty"`  // e.g., "repos"
Per    string `yaml:"per,omitempty"`       // e.g., "repo"
FanIn  string `yaml:"fan_in,omitempty"`    // e.g., "repos"
```

Runtime uses Temporal child workflows (one per fan-out item) with future-slice fan-in. Deferred to Phase 2.

## Critical Pre-Requisite: Fix Workflow Side Effects

`ClimbWorkflow` currently writes feedback files with `os.MkdirAll` and `os.WriteFile` directly in workflow code (lines 122-127 of `workflow.go`). This is a **Temporal replay hazard** — workflow code must be deterministic. File I/O is a side effect that breaks replay.

**Fix (must land before Phase 1 intake work):** Move the feedback file writing into a dedicated activity. The workflow calls the activity; the activity does the file I/O.

## Phase 1 Sub-Phases (Implementation Order)

Per Codex review recommendation — sequence matters:

### Sub-Phase 1A: Runtime Migration
- Fix `ClimbWorkflow` side effects (move file I/O to activity)
- Build `belayer worker` daemon (Temporal worker + HTTP API)
- Refactor `belayer climb` to connect to worker (thin client)
- Migrate `belayer start` from v2 to v3 CLI
- Migrate channel server's submit tool to emit full `SubmitSpec`

### Sub-Phase 1B: Intake Model
- Add `IntakeConfig`, `SafetyConfig` to `PipelineConfig`
- Add `commit` output type, `CommitSHA`/`BaseRef` to `CompletionResult`
- Add `Repos`, `BaseRef` to `ClimbInput`
- Add fan-out fields to `NodeConfig` (parse + validate only)
- Extend parser, validator, visualizer for intake + new types
- Build `SubmitSpec → ClimbInput` bridge (extract from `climb.go`)
- Implement deterministic workflow IDs

### Sub-Phase 1C: First Automated Intake
- Build `IntakeAdapter` interface
- Build `JiraAdapter` (reuse patterns from `internal/tracker/`)
- Build schedule reconciliation (create/update/pause)
- Wire concurrency limits

### Sub-Phase 1D: Legacy Deletion + Templates
- Delete `internal/v2/` entirely
- Delete v1 daemon code no longer needed
- Update `internal/cli/root.go` to wire v3 only
- Build templates: gstack-review-loop, superpowers-tdd
- Update solo/team templates to v3 flat-node model with intake

## Phase 2 Scope (Deferred)

- `pipeline init` command (`--intake X --ascent Y --output Z`)
- Remaining intake adapters: linear, github-issues, exec, competitive-analysis
- `pipeline dry-run` command (static validation only)
- `intake test` command (connectivity check, empty results = healthy)
- `pipeline catalog` command
- Fan-out runtime (child ClimbWorkflows, parallel execution)
- Gate threshold recommendations
- ci-monitor node + output → implementation feedback loop
- `belayer intake delete <name>` (permanent schedule deletion)

## Code Touchpoints

| File/Package | Change |
|-------------|--------|
| `internal/v3/pipeline/model.go` | Add `IntakeConfig`, `SafetyConfig`, fan-out fields, `commit` output type |
| `internal/v3/pipeline/parser.go` | Parse `intake:` and `safety:` sections |
| `internal/v3/pipeline/validate.go` | Validate intake configs, fan-out fields, safety limits |
| `internal/v3/pipeline/defaults.go` | Extend `Visualize` for intake sources in ASCII DAG |
| `internal/v3/model/types.go` | Add `Repos`, `BaseRef` to `ClimbInput`; `CommitSHA`, `BaseRef` to `CompletionResult` |
| `internal/v3/temporal/workflow.go` | Move feedback file I/O to activity (replay fix) |
| `internal/v3/intake/` | **New package**: `SubmitSpec`, `IntakeAdapter` interface, `JiraAdapter`, bridge function, schedule reconciliation |
| `internal/v3/cli/climb.go` | Refactor to thin client using bridge function |
| `internal/v3/cli/worker.go` | **New**: `belayer worker` daemon (Temporal + HTTP + schedules) |
| `internal/v3/cli/start.go` | **Migrated from v2**: `belayer start` with channel server |
| `internal/v3/cli/root.go` | Register worker, start, pipeline commands |
| `internal/v3/pipeline/templates/` | **New**: gstack-review-loop.yaml, superpowers-tdd.yaml, updated solo.yaml, team.yaml |
| `internal/v2/` | **Delete entirely** |
| `internal/cli/root.go` | Remove v2, wire v3 commands to root |
| `channel/channel.ts` | Update submit tool to emit full `SubmitSpec` format |
