# Intake Plugin Model + Pipeline Template Library

> **Status**: Active | **Created**: 2026-03-21 | **Last Updated**: 2026-03-21
> **Design Doc**: `docs/designs/intake-output-plugin-model.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-20 | CEO Review | Intake is the composable axis, ascent+output are opinionated | User insight: multiple intakes, single pipeline |
| 2026-03-20 | CEO Review | Output dropped from YAML — init-only preset | Codex: avoids redundant state between output: and phases: |
| 2026-03-20 | Eng Review | v3 is canonical, delete v1/v2 | User directive: runtime serves the model |
| 2026-03-21 | Eng Review | Deterministic Temporal workflow IDs for dedup | Temporal rejects duplicates natively, no SQLite table |
| 2026-03-21 | Eng Review | Worker daemon model (long-lived process) | Automated intake requires persistent worker |
| 2026-03-21 | Eng Review | Branch naming uses Temporal run ID, not workflow ID | Retries share branch, resubmissions get new branch |
| 2026-03-21 | Eng Review | Belayer is plumbing — passes references only | Nodes are black boxes, Belayer moves commit SHAs and file paths |
| 2026-03-21 | Eng Review | Fan-out schema now, runtime Phase 2 | Core to the multi-lead model, but child workflows deferred |
| 2026-03-21 | Eng Review | Sub-phase implementation order | Codex: worker first → model → intake → deletion |

## Progress

- [x] Task 1: Fix ClimbWorkflow side effects (move file I/O to activity)
- [x] Task 2: Add commit output type + new fields to model types
- [x] Task 3: Add IntakeConfig, SafetyConfig, fan-out fields to pipeline model
- [x] Task 4: Extend pipeline validator for intake, commit output, fan-out
- [x] Task 5: Extend pipeline parser to call Validate after parse
- [x] Task 6: Add SubmitSpec type and StartClimb bridge function
- [x] Task 7: Refactor belayer climb to use bridge function
- [x] Task 8: Build belayer worker daemon (Temporal + HTTP API)
- [x] Task 9: Build JiraAdapter intake plugin
- [x] Task 10: Build schedule reconciliation for automated intake
- [x] Task 11: Update channel server submit tool for full SubmitSpec
- [x] Task 12: Migrate belayer start from v2 to v3
- [x] Task 13: Build pipeline templates (gstack-review-loop, superpowers-tdd)
- [x] Task 14: Update default pipeline + solo/team templates for v3
- [x] Task 15: Delete v2, wire v3 commands to root
- [x] Task 16: Extend pipeline visualizer for intake sources

## Surprises & Discoveries

- Task 8 worker: `/status` endpoint simplified to a health stub (full workflow listing deferred to Phase 2)
- Task 10 schedule reconciliation: implemented as a logging stub (full Temporal Schedule API deferred)
- Task 7 climb refactor: agent left old functions in file initially; required manual cleanup
- Review found 8 significant issues; 5 fixed inline (timestamp sharing, context propagation, error logging, jira_url requirement, visualizer edge case)

## Plan Drift

- `StartClimb` high-level function not created; worker.go calls bridge functions directly instead
- Schedule reconciliation is a stub — not wired into worker startup yet (Phase 2)
- `Bare string types for PR status fields` tech debt from PLANS.md is now moot (v1/v2 deleted)

---

## Sub-Phase 1A: Runtime Migration

### Task 1: Fix ClimbWorkflow side effects

**Why:** `ClimbWorkflow` writes feedback files with `os.MkdirAll` and `os.WriteFile` directly in workflow code (workflow.go lines 122-127). This is a Temporal replay hazard — workflow code must be deterministic. Must fix before building intake on top.

**Files:** `internal/v3/temporal/workflow.go`, `internal/v3/temporal/activity.go`

**Steps:**
1. Add a new `WriteFeedbackActivity` to `activity.go`:
   ```go
   type WriteFeedbackInput struct {
       WorkDir      string
       FeedbackText string
   }

   func (a *Activities) WriteFeedbackActivity(ctx context.Context, input WriteFeedbackInput) (string, error) {
       feedbackPath := filepath.Join(input.WorkDir, ".belayer", "input", "feedback.md")
       if err := os.MkdirAll(filepath.Dir(feedbackPath), 0o755); err != nil {
           return "", fmt.Errorf("create feedback dir: %w", err)
       }
       if err := os.WriteFile(feedbackPath, []byte(input.FeedbackText), 0o644); err != nil {
           return "", fmt.Errorf("write feedback: %w", err)
       }
       return ".belayer/input/feedback.md", nil
   }
   ```
2. In `workflow.go`, replace the inline `os.MkdirAll`/`os.WriteFile` block (lines 122-127) with:
   ```go
   if result.Feedback != "" {
       wfInput := WriteFeedbackInput{WorkDir: input.WorkDir, FeedbackText: result.Feedback}
       var feedbackPath string
       feedbackCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
           StartToCloseTimeout: 30 * time.Second,
       })
       if err := workflow.ExecuteActivity(feedbackCtx, a.WriteFeedbackActivity, wfInput).Get(ctx, &feedbackPath); err == nil {
           artifacts["feedback"] = feedbackPath
       }
   }
   ```
   Note: `a` must be accessible — it's already declared as `a := &Activities{}` at line 64.
3. Register `WriteFeedbackActivity` in `climb.go` where activities are registered (line 79).
4. **Test:** Run `go test ./internal/v3/temporal/...` — existing workflow tests should pass since the behavior is identical, just moved to an activity.
5. **Verify:** `go vet ./internal/v3/...` passes.

**Done when:** `go test ./internal/v3/...` passes, no `os.WriteFile` or `os.MkdirAll` calls remain in `workflow.go`.

---

### Task 2: Add commit output type + new fields to model types

**Why:** The node contract model needs `commit` as an output type and `CommitSHA`/`BaseRef` fields on `CompletionResult` so nodes can pass commit references.

**Files:** `internal/v3/model/types.go`, `internal/v3/model/types_test.go`

**Steps:**
1. Add fields to `ClimbInput`:
   ```go
   Repos   []string `json:"repos,omitempty"`
   BaseRef string   `json:"base_ref,omitempty"`
   ```
2. Add fields to `CompletionResult`:
   ```go
   CommitSHA string `json:"commit_sha,omitempty"`
   BaseRef   string `json:"base_ref,omitempty"`
   ```
3. Add tests for JSON round-trip with the new fields (ensure they serialize/deserialize correctly, and are omitted when empty).
4. **Verify:** `go test ./internal/v3/model/...`

**Done when:** New fields serialize correctly, existing tests pass.

---

### Task 3: Add IntakeConfig, SafetyConfig, fan-out fields to pipeline model

**Why:** `PipelineConfig` needs `Intake []IntakeConfig` and `Safety SafetyConfig` for the intake plugin model. `NodeConfig` needs `FanOut/Per/FanIn` fields for future multi-lead support.

**Files:** `internal/v3/pipeline/model.go`

**Steps:**
1. Add `IntakeConfig` type:
   ```go
   type IntakeConfig struct {
       Name   string            `yaml:"name" json:"name"`
       Type   string            `yaml:"type" json:"type"`
       Config map[string]string `yaml:"config,omitempty" json:"config,omitempty"`
   }
   ```
2. Add `SafetyConfig` type:
   ```go
   type SafetyConfig struct {
       MaxConcurrentRuns int `yaml:"max_concurrent_runs,omitempty" json:"max_concurrent_runs,omitempty"`
   }
   ```
3. Add fields to `PipelineConfig`:
   ```go
   Intake []IntakeConfig `yaml:"intake,omitempty" json:"intake,omitempty"`
   Safety SafetyConfig   `yaml:"safety,omitempty" json:"safety,omitempty"`
   ```
4. Add fan-out fields to `NodeConfig`:
   ```go
   FanOut string `yaml:"fan_out,omitempty" json:"fan_out,omitempty"`
   Per    string `yaml:"per,omitempty" json:"per,omitempty"`
   FanIn  string `yaml:"fan_in,omitempty" json:"fan_in,omitempty"`
   ```
5. Add `"commit"` to valid output types — update `OutputConfig` comment and `Validate` references.
6. **Verify:** `go test ./internal/v3/pipeline/...` — existing tests pass (new fields are all optional).

**Done when:** New types compile, existing tests pass.

---

### Task 4: Extend pipeline validator for intake, commit output, fan-out

**Why:** Validator needs to check intake configs (known types, required fields, no duplicate interactive) and accept `commit` as a valid output type. Fan-out fields need basic validation.

**Files:** `internal/v3/pipeline/validate.go`, `internal/v3/pipeline/validate_test.go`

**Steps:**
1. Add `"commit"` to `validOutputTypes` map in `Validate()`.
2. Add intake validation after existing node validation:
   ```go
   // Validate intake configs
   intakeNames := make(map[string]bool)
   interactiveCount := 0
   for _, intake := range cfg.Intake {
       if intake.Name == "" {
           return fmt.Errorf("intake: name is required")
       }
       if intakeNames[intake.Name] {
           return fmt.Errorf("intake: duplicate name %q", intake.Name)
       }
       intakeNames[intake.Name] = true
       validTypes := map[string]bool{"jira": true, "interactive": true, "linear": true, "github-issues": true, "exec": true}
       if !validTypes[intake.Type] {
           return fmt.Errorf("intake %q: unknown type %q", intake.Name, intake.Type)
       }
       if intake.Type == "interactive" {
           interactiveCount++
           if interactiveCount > 1 {
               return fmt.Errorf("intake: only one interactive intake allowed")
           }
       }
   }
   ```
3. Add fan-out validation — for Phase 1, just ensure fan-out fields reference valid values:
   ```go
   for _, n := range cfg.Nodes {
       if n.FanOut != "" {
           validFanOuts := map[string]bool{"repos": true}
           if !validFanOuts[n.FanOut] {
               return fmt.Errorf("node %q: unknown fan_out value %q (valid: repos)", n.Name, n.FanOut)
           }
       }
   }
   ```
4. Add tests:
   - Pipeline with valid intake parses and validates
   - Pipeline with unknown intake type fails
   - Pipeline with duplicate intake names fails
   - Pipeline with two interactive intakes fails
   - Pipeline with `commit` output type validates
   - Pipeline with fan-out field validates
5. **Verify:** `go test ./internal/v3/pipeline/...`

**Done when:** All new validation tests pass, existing tests pass.

---

### Task 5: Extend pipeline parser to call Validate after parse

**Why:** Currently `ParsePipeline` only unmarshals YAML — it doesn't call `Validate`. New intake/fan-out fields will be accepted without validation unless the parser calls `Validate`. Per Codex review: "the parser just unmarshals YAML, and the current v3 entry points parse without calling Validate."

**Files:** `internal/v3/pipeline/parser.go`, `internal/v3/pipeline/parser_test.go`

**Steps:**
1. Update `ParsePipeline` to call `Validate`:
   ```go
   func ParsePipeline(data []byte) (*PipelineConfig, error) {
       var cfg PipelineConfig
       if err := yaml.Unmarshal(data, &cfg); err != nil {
           return nil, fmt.Errorf("parse pipeline: %w", err)
       }
       if err := Validate(&cfg); err != nil {
           return nil, fmt.Errorf("validate pipeline: %w", err)
       }
       return &cfg, nil
   }
   ```
2. Add `ParsePipelineNoValidate` for cases where callers want raw parsing (e.g., migration tools):
   ```go
   func ParsePipelineNoValidate(data []byte) (*PipelineConfig, error) {
       var cfg PipelineConfig
       if err := yaml.Unmarshal(data, &cfg); err != nil {
           return nil, fmt.Errorf("parse pipeline: %w", err)
       }
       return &cfg, nil
   }
   ```
3. Update existing parser tests to account for validation being called automatically.
4. **Verify:** `go test ./internal/v3/...` — all tests pass.

**Done when:** `ParsePipeline` validates automatically, existing tests adjusted.

---

## Sub-Phase 1B: Intake Model

### Task 6: Add SubmitSpec type and StartClimb bridge function

**Why:** The bridge function is the core adapter that converts a `SubmitSpec` into a `ClimbInput` and starts a workflow. Both `belayer climb` and intake adapters call this.

**Files:** `internal/v3/intake/spec.go` (new), `internal/v3/intake/bridge.go` (new), `internal/v3/intake/bridge_test.go` (new)

**Steps:**
1. Create `internal/v3/intake/spec.go`:
   ```go
   package intake

   type SubmitSpec struct {
       Spec         string            `json:"spec"`
       Repos        []string          `json:"repos,omitempty"`
       Source       string            `json:"source"`
       ExternalID   string            `json:"external_id"`
       PipelineName string            `json:"pipeline_name"`
       Metadata     map[string]string `json:"metadata,omitempty"`
   }
   ```
2. Create `internal/v3/intake/bridge.go` — extract `createGitWorktree`, `generateBranchSlug`, and `resolvePipelineYAML` from `climb.go`. Add `StartClimb`:
   ```go
   func StartClimb(ctx context.Context, tc client.Client, spec SubmitSpec, pipelineYAML []byte, repoDir string) (workflowID string, runID string, err error)
   ```
   - Generates deterministic workflow ID: `{pipelineName}/{source}/{externalID}`
   - Starts workflow with `WorkflowIDReusePolicy: AllowDuplicate`
   - After workflow starts, uses `run.GetRunID()` to get the unique run ID
   - Generates branch: `belayer/{slug}-{runID}` (using run ID, not workflow ID)
   - Creates worktree at `.belayer/worktrees/{runID}`
   - Detects base ref: `git rev-parse HEAD` in repoDir
   - Builds `ClimbInput` with all fields populated
3. Add tests:
   - `TestStartClimb_DeterministicWorkflowID` — same spec → same workflow ID
   - `TestStartClimb_DifferentIntake_DifferentID` — different source → different workflow ID
   - Note: full integration test requires Temporal; unit test can verify ID generation and ClimbInput construction with mocked client.
4. **Verify:** `go test ./internal/v3/intake/...`

**Done when:** Bridge function compiles, unit tests pass, SubmitSpec type defined.

---

### Task 7: Refactor belayer climb to use bridge function

**Why:** `climb.go` currently has the worktree/branch creation inline. Refactor it to call `StartClimb` from the bridge package.

**Files:** `internal/v3/cli/climb.go`

**Steps:**
1. Remove `createGitWorktree`, `generateBranchSlug`, `resolvePipelineYAML` from `climb.go` (moved to intake package in Task 6).
2. Update `NewClimbCmd.RunE` to:
   - Build a `SubmitSpec` from CLI args (source: "cli", externalID: `climb-{unix_ms}`)
   - Call `intake.StartClimb(...)` instead of inline worktree/workflow creation
   - Keep `--detach` and result display logic
3. Import `internal/v3/intake` package.
4. **Verify:** `go test ./internal/v3/cli/...` — existing CLI tests pass.
5. **Verify:** `go build ./cmd/belayer` — binary builds.

**Done when:** `climb.go` is a thin client using the bridge function, binary builds.

---

### Task 8: Build belayer worker daemon

**Why:** Automated intake requires a long-lived worker process. Migrated from v2's worker.

**Files:** `internal/v3/cli/worker.go` (new), `internal/v3/cli/root.go`

**Steps:**
1. Create `internal/v3/cli/worker.go` modeled on `internal/v2/cli/worker.go`:
   - `newWorkerCmd()` returns a cobra command
   - `startWorker(workDir string)` function:
     - Connects to Temporal
     - Registers `ClimbWorkflow` + `Activities` (including `WriteFeedbackActivity`)
     - Starts HTTP server on port 8780:
       - `POST /start` — accepts `SubmitSpec` JSON, calls `intake.StartClimb`, returns workflow ID
       - `GET /status` — lists active ClimbWorkflow runs via Temporal list query
     - Starts Temporal worker
     - Blocks until SIGINT/SIGTERM
2. Register `newWorkerCmd()` in `root.go`.
3. Add test for HTTP API endpoints (mock Temporal client, verify request/response format).
4. **Verify:** `go build ./cmd/belayer` — binary builds with worker command.

**Done when:** `belayer worker` command exists, HTTP API serves `/start` and `/status`, binary builds.

---

## Sub-Phase 1C: First Automated Intake

### Task 9: Build JiraAdapter intake plugin

**Why:** First automated intake adapter proves the model works end-to-end.

**Files:** `internal/v3/intake/adapter.go` (new), `internal/v3/intake/jira.go` (new), `internal/v3/intake/jira_test.go` (new)

**Steps:**
1. Create `internal/v3/intake/adapter.go` — interface:
   ```go
   type IntakeAdapter interface {
       Poll(ctx context.Context) ([]SubmitSpec, error)
   }

   func NewAdapter(cfg pipeline.IntakeConfig) (IntakeAdapter, error)
   ```
2. Create `internal/v3/intake/jira.go` — `JiraAdapter`:
   - Reads `project`, `filter`, `credential_env` from config
   - Uses `gh` CLI or Jira REST API (reuse patterns from `internal/tracker/jira/`)
   - Converts Jira issues to `SubmitSpec` with `source: "jira"`, `external_id: "{issue_key}"`
3. Add tests with mock HTTP responses (same pattern as `internal/tracker/github/github_test.go`).
4. **Verify:** `go test ./internal/v3/intake/...`

**Done when:** JiraAdapter.Poll() returns correct SubmitSpecs from mock API responses.

---

### Task 10: Build schedule reconciliation for automated intake

**Why:** The worker daemon needs to create/update/pause Temporal schedules for automated intakes on startup.

**Files:** `internal/v3/intake/schedule.go` (new), `internal/v3/intake/schedule_test.go` (new)

**Steps:**
1. Create `internal/v3/intake/schedule.go`:
   ```go
   func ReconcileSchedules(ctx context.Context, sc client.ScheduleClient, pipelineName string, intakes []pipeline.IntakeConfig) error
   ```
   - List existing schedules, filter by `intake/{pipelineName}/` prefix (client-side)
   - For each automated intake in YAML: create or update schedule
   - For intakes in Temporal but not in YAML: pause schedule
   - Skip `interactive` type intakes (no schedule needed)
2. Wire into `worker.go` — call `ReconcileSchedules` on worker startup after reading pipeline YAML.
3. Add tests with mock schedule client.
4. **Verify:** `go test ./internal/v3/intake/...`

**Done when:** Schedule reconciliation creates/updates/pauses correctly in tests.

---

### Task 11: Update channel server submit tool for full SubmitSpec

**Why:** `channel/channel.ts` currently sends `{spec, repos, pipeline}`. Needs to emit full `SubmitSpec` format with `source`, `external_id`, `pipeline_name`.

**Files:** `channel/channel.ts`

**Steps:**
1. Update the submit tool's MCP handler to send:
   ```typescript
   {
     spec: req.params.arguments.spec,
     repos: req.params.arguments.repos || [],
     source: "interactive",
     external_id: `submit-${Date.now()}`,
     pipeline_name: req.params.arguments.pipeline || pipelineName,
     metadata: {}
   }
   ```
2. Ensure the POST to worker `/start` uses the updated format.
3. **Verify:** TypeScript compiles without errors.

**Done when:** Submit tool emits full SubmitSpec JSON.

---

### Task 12: Migrate belayer start from v2 to v3

**Why:** `belayer start` currently lives in `internal/v2/cli/start.go`. It needs to move to v3 CLI before v2 can be deleted.

**Files:** `internal/v3/cli/start.go` (new, migrated from v2), `internal/v3/cli/root.go`

**Steps:**
1. Copy `internal/v2/cli/start.go` to `internal/v3/cli/start.go`.
2. Update imports from `v2` to `v3` packages.
3. Update any references to v2 types/functions to use v3 equivalents.
4. Register `newStartCmd()` in v3's `root.go`.
5. **Verify:** `go build ./cmd/belayer` — binary builds, `belayer start` command available.

**Done when:** `belayer start` works from v3 CLI, v2 start.go is no longer needed.

---

## Sub-Phase 1D: Legacy Deletion + Templates

### Task 13: Build pipeline templates (gstack-review-loop, superpowers-tdd)

**Why:** These templates prove the pipeline model's customizability for real workflows.

**Files:** `internal/v3/pipeline/templates/gstack-review-loop.yaml` (new), `internal/v3/pipeline/templates/superpowers-tdd.yaml` (new)

**Steps:**
1. Create `gstack-review-loop.yaml`:
   ```yaml
   name: gstack-review-loop
   intake:
     - name: user-session
       type: interactive
   nodes:
     - name: lead
       type: node
       description: "Implement the spec. Commit your changes."
       input: { type: description }
       output: { type: commit }
       on_pass: code-review
       on_retry: self
       on_fail: stop
       max_retries: 3
     - name: code-review
       type: gate
       description: "Adversarial code review."
       input: { type: commit }
       dimensions:
         - { name: correctness, weight: 0.3, description: "Does the code match the spec?" }
         - { name: architecture, weight: 0.25, description: "Clean abstractions?" }
         - { name: test_coverage, weight: 0.25, description: "Changes tested?" }
         - { name: code_quality, weight: 0.2, description: "Naming, style, no dead code" }
       thresholds: { pass: 7.0, retry: 4.0 }
       output: { type: gate_result }
       on_pass: qa-gate
       on_retry: lead
       on_fail: stop
       max_retries: 2
     - name: qa-gate
       type: gate
       description: "QA validation. Test the application works end-to-end."
       input: { type: commit }
       dimensions:
         - { name: functionality, weight: 0.4, description: "Does the feature work?" }
         - { name: edge_cases, weight: 0.3, description: "Edge cases handled?" }
         - { name: regression, weight: 0.3, description: "No regressions?" }
       thresholds: { pass: 8.0, retry: 5.0 }
       output: { type: gate_result }
       on_pass: pr-creator
       on_retry: lead
       on_fail: stop
       max_retries: 1
     - name: pr-creator
       type: node
       description: "Create a PR from the changes."
       input: { type: commit }
       output: { type: file, key: pr-url }
       on_pass: stop
       on_fail: stop
   safety:
     max_concurrent_runs: 3
   ```
2. Create `superpowers-tdd.yaml` — similar but with TDD-focused lead and test-coverage gate.
3. Add parse+validate tests for both templates.
4. **Verify:** `go test ./internal/v3/pipeline/...`

**Done when:** Both templates parse, validate, and have tests.

---

### Task 14: Update default pipeline + solo/team templates for v3

**Why:** The default pipeline in `defaults.go` and the v2 templates in `internal/v2/pipeline/templates/` need v3 equivalents with intake sections and commit output types.

**Files:** `internal/v3/pipeline/defaults.go`, `internal/v3/pipeline/templates/solo.yaml` (new), `internal/v3/pipeline/templates/team.yaml` (new)

**Steps:**
1. Update `DefaultPipelineYAML` in `defaults.go`:
   - Add `intake: [{name: user-session, type: interactive}]`
   - Change lead output from `type: code` to `type: commit`
   - Add `safety: {max_concurrent_runs: 3}`
2. Create `solo.yaml` — minimal pipeline: lead → spotter gate → pr-creator, with interactive intake.
3. Create `team.yaml` — full pipeline: setter → lead → spotter gate → pr-creator → pr-reviewer, with interactive intake.
4. Update `defaults_test.go` to expect the new fields.
5. **Verify:** `go test ./internal/v3/pipeline/...`

**Done when:** Default pipeline and templates parse/validate with intake sections.

---

### Task 15: Delete v2, wire v3 commands to root

**Why:** v3 is canonical. v2 must be removed to avoid confusion and maintain one model.

**Files:** `internal/v2/` (delete), `internal/cli/root.go`

**Steps:**
1. Update `internal/cli/root.go`:
   - Remove `v2cli "github.com/donovan-yohan/belayer/internal/v2/cli"` import
   - Remove `v2cli.RegisterCommands(cmd)` call
   - Keep `v3cli.RegisterV3Commands(cmd)` (ensure it registers worker, start, pipeline commands)
2. Delete `internal/v2/` entirely.
3. **Verify:** `go build ./cmd/belayer` — binary builds.
4. **Verify:** `go test ./...` — all tests pass (no v2 test failures since v2 is deleted).

**Done when:** `internal/v2/` is gone, binary builds, all remaining tests pass.

---

### Task 16: Extend pipeline visualizer for intake sources

**Why:** `pipeline show` should display intake sources in the ASCII DAG so users can see their full pipeline topology.

**Files:** `internal/v3/pipeline/visualize.go` (new or extend defaults.go)

**Steps:**
1. Add a `Visualize(cfg *PipelineConfig) string` function that renders:
   ```
   INTAKE:
     [jira-backlog] → poll every 5m
     [user-session] → interactive

   PIPELINE: gstack-review-loop
     lead ──[commit]──► code-review ──[gate_result]──► qa-gate ──[gate_result]──► pr-creator
       ▲                    │                              │
       └────────────────────┘ on_retry                     │
       └───────────────────────────────────────────────────┘ on_retry
   ```
2. Wire into CLI — add or update `pipeline show` command to call `Visualize`.
3. Add tests for visualization output.
4. **Verify:** `go test ./internal/v3/pipeline/...`

**Done when:** `pipeline show` renders intake sources and node graph with routing.

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
- Sub-phasing (1A→1B→1C→1D) prevented the sequencing issues Codex warned about
- Parallel agent dispatch for independent tasks (4 batches, up to 3 concurrent agents)
- CEO→Eng→Codex review chain caught the v2/v3 model mismatch before implementation
- Deterministic workflow IDs using Temporal's native dedup eliminated SQLite table

**What didn't:**
- First plan rewrite needed because CEO review targeted v2, eng review pivoted to v3
- Agent task for climb.go refactor left stale functions requiring manual cleanup
- Schedule reconciliation and worker /status are stubs — limits automated intake to Phase 2

**Learnings to codify:**
- L-007: When a design doc targets a model (v2) but the codebase has a better model (v3), resolve the model question BEFORE creating the implementation plan
- L-008: Temporal workflow code must be deterministic — file I/O (os.WriteFile, os.MkdirAll) must happen in activities, not workflows
- L-009: Use Temporal's native workflow ID uniqueness for dedup instead of building a separate dedup table
