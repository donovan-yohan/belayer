# Belayer v2: Temporal Orchestrator Platform

> **Status**: Completed | **Created**: 2026-03-19 | **Completed**: 2026-03-19
> **Design Doc**: `docs/designs/temporal-orchestrator-reimagining.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-19 | Design | v2 clean break, not incremental migration | Monolithic daemon (1500 LOC state machine) doesn't scale — rewrite is faster than refactoring |
| 2026-03-19 | Design | Temporal Go SDK as messaging backbone | Provides durable execution, retries, visibility for free |
| 2026-03-19 | Design | Two provider contracts (Type A / Type B) | Judgment calls (JSON in/out) vs interactive sessions (CLI-callback) are fundamentally different execution models |
| 2026-03-19 | Design | Three pipeline phases (Approach / Ascent / Send) | Separates intake from execution from output — users choose depth per phase |
| 2026-03-19 | Design | CLI-callback for interactive sessions | `belayer <role> finish` signals completion via Temporal Signal — works with any tool |
| 2026-03-19 | Design | MVP: setter → lead proving pipes + CLI-callback | Proves the hardest parts first: Temporal pipeline + interactive session contract |
| 2026-03-19 | Retrospective | Plan completed — 14/14 tasks, 68 tests, 7 packages | All v1 tests pass alongside v2. Integration test verified against real Temporal server. |

## Progress

- [x] Task 1: Temporal SDK setup + Go module restructure
- [x] Task 2: Role contract types + pipeline model
- [x] Task 3: Temporal workflow — Route with two roles
- [x] Task 4: Type B provider — session spawner + CLI-callback
- [x] Task 5: CLI commands — `belayer run`, `belayer <role> finish/flare/fail`
- [x] Task 6: Temporal dev server management — `belayer temporal start/stop/status`
- [x] Task 7: Pipeline DSL parser + validator
- [x] Task 8: Type A provider — exec shell-out (JSON in/out)
- [x] Task 9: Risk gate framework
- [x] Task 10: Pipeline visualization — `belayer pipeline show`
- [x] Task 11: Eval framework + fixture recording
- [x] Task 12: Pipeline templates + `belayer init`
- [x] Task 13: Safety controls (depth, budget, dedupe, expiry)
- [x] Task 14: Integration test — full setter → lead pipeline

## Surprises & Discoveries

- Temporal Go SDK v1.41.1 required explicit `go get` for sub-packages (workflow, testsuite, etc.)
- Temporal test framework requires activities to be registered via struct reference before `OnActivity` mocking works — string-based activity names don't work with the latest SDK

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

### Task 1: Temporal SDK setup + Go module restructure

**Goal:** Add the Temporal Go SDK dependency and create the v2 package structure. Keep v1 code intact — v2 packages live alongside v1 until the switch.

**Files:**
- `go.mod` (modify — add `go.temporal.io/sdk`)
- `internal/v2/` (new directory tree)

**Steps:**

1. **Add Temporal SDK dependency:**
   ```bash
   go get go.temporal.io/sdk@latest
   ```
   Verify it resolves. Run `go mod tidy`.

2. **Create v2 package skeleton:** Create empty package files with doc comments to establish the structure:
   ```
   internal/v2/
     pipeline/pipeline.go       # DSL parser, topology types
     temporal/workflow.go        # Route workflow definition
     temporal/activity.go        # Activity wrappers (Type A + Type B)
     temporal/worker.go          # Worker setup + registration
     temporal/signals.go         # Signal types for CLI-callback
     role/contract.go            # TypeA and TypeB interfaces, role registry
     provider/exec.go            # Type A: shell-out provider
     provider/session.go         # Type B: session spawner provider
     riskgate/gate.go            # Risk evaluation logic
     eval/eval.go                # Eval framework
     cli/root.go                 # v2 CLI commands
     model/types.go              # v2 domain types
   ```
   Each file should have a `package <name>` declaration and a brief doc comment describing its purpose.

3. **Write a compile-check test:**
   ```go
   // internal/v2/pipeline/pipeline_test.go
   func TestPackageCompiles(t *testing.T) {
       // Empty — just verifies the package compiles with Temporal SDK
   }
   ```

4. **Verify:** `go build ./...` and `go test ./...` both pass.

**Tests:**
- `go build ./...` succeeds (Temporal SDK resolves, no import cycles)
- `go test ./...` passes (existing tests unbroken)

---

### Task 2: Role contract types + pipeline model

**Goal:** Define the core type system: role contracts (Type A / Type B), pipeline phases, role definitions, and the pipeline model that the DSL parser will produce.

**Files:**
- `internal/v2/role/contract.go`
- `internal/v2/role/contract_test.go`
- `internal/v2/pipeline/model.go`
- `internal/v2/pipeline/model_test.go`
- `internal/v2/model/types.go`

**Steps:**

1. **Define role contract types** in `internal/v2/role/contract.go`:
   ```go
   // ContractType distinguishes execution models.
   type ContractType string
   const (
       TypeA ContractType = "pitch"   // JSON in/out, short-lived
       TypeB ContractType = "ascent"  // Interactive session, CLI-callback
   )

   // Phase classifies roles into pipeline stages.
   type Phase string
   const (
       PhaseApproach Phase = "approach"  // Intake / planning the route
       PhaseAscent   Phase = "ascent"    // Execution / climbing the wall
       PhaseSend     Phase = "send"      // Output / sending it
   )

   // RoleDef defines a role in the pipeline.
   type RoleDef struct {
       Name         string       `yaml:"name"`
       Phase        Phase        `yaml:"phase"`
       ContractType ContractType `yaml:"contract_type"`
       InputSchema  string       `yaml:"input_schema"`   // JSON Schema file path
       OutputSchema string       `yaml:"output_schema"`  // JSON Schema file path
       Provider     ProviderConfig `yaml:"provider"`
   }

   // ProviderConfig specifies how a role is executed.
   type ProviderConfig struct {
       Type    string            `yaml:"type"`    // "builtin" or "exec"
       Command string            `yaml:"command"`  // For exec: command to run
       Args    []string          `yaml:"args"`     // For exec: additional args
       Config  map[string]string `yaml:"config"`   // Provider-specific config
   }
   ```

2. **Define pipeline model** in `internal/v2/pipeline/model.go`:
   ```go
   // Route is the top-level pipeline definition (a climbing route).
   type Route struct {
       Name     string           `yaml:"name"`
       Template string           `yaml:"template"`
       Phases   []PhaseConfig    `yaml:"phases"`
       Safety   SafetyConfig     `yaml:"safety"`
   }

   // PhaseConfig defines one pipeline phase.
   type PhaseConfig struct {
       Phase Phase              `yaml:"phase"`
       Roles []role.RoleDef     `yaml:"roles"`
       Loops []LoopConfig       `yaml:"loops"`
   }

   // LoopConfig defines a retry loop between two roles.
   type LoopConfig struct {
       From          string `yaml:"from"`           // Role that triggers the loop
       To            string `yaml:"to"`             // Role to loop back to
       MaxIterations int    `yaml:"max_iterations"` // Default: 3
       Condition     string `yaml:"condition"`      // e.g., "output.pass == false"
   }

   // SafetyConfig holds safety limits.
   type SafetyConfig struct {
       MaxChildDepth     int           `yaml:"max_child_depth"`     // Default: 2
       GlobalChildBudget int           `yaml:"global_child_budget"` // Default: 50
       ChildDedupe       bool          `yaml:"child_dedupe"`        // Default: true
       GateTimeout       time.Duration `yaml:"gate_timeout"`        // Default: 24h
       HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`  // Default: 60s
       MaxLoopIterations int           `yaml:"max_loop_iterations"` // Default: 3
   }
   ```

3. **Define v2 domain types** in `internal/v2/model/types.go`:
   ```go
   // RunStatus tracks the state of a pipeline run (minimal — Temporal owns execution state).
   type RunStatus string
   const (
       RunStatusActive    RunStatus = "active"
       RunStatusCompleted RunStatus = "completed"
       RunStatusFailed    RunStatus = "failed"
       RunStatusFlared    RunStatus = "flared"  // Needs human help
   )

   // RoleSignal is the payload sent via `belayer <role> finish/flare/fail`.
   type RoleSignal struct {
       TaskID  string          `json:"task_id"`
       Action  SignalAction    `json:"action"`   // finish, flare, fail
       Output  json.RawMessage `json:"output"`   // Role output payload
       Message string          `json:"message"`  // Human-readable context
   }

   type SignalAction string
   const (
       SignalFinish SignalAction = "finish"
       SignalFlare  SignalAction = "flare"
       SignalFail   SignalAction = "fail"
   )
   ```

4. **Write tests:** Table-driven tests for defaults, phase enumeration, signal serialization round-trip.

**Tests:**
- `SafetyConfig` defaults are correct
- `RoleSignal` JSON round-trip preserves all fields
- Phase and ContractType string values match expected constants

---

### Task 3: Temporal workflow — Route with two roles

**Goal:** Implement the Route workflow that sequences roles, waits for CLI-callback signals for Type B roles, and advances the pipeline. MVP: setter → lead (both Type B).

**Files:**
- `internal/v2/temporal/workflow.go`
- `internal/v2/temporal/activity.go`
- `internal/v2/temporal/signals.go`
- `internal/v2/temporal/workflow_test.go`

**Steps:**

1. **Define signal types** in `signals.go`:
   ```go
   const (
       SignalChannelName = "role-signal"
   )
   ```

2. **Implement Route workflow** in `workflow.go`:
   ```go
   // RouteWorkflow is the main pipeline workflow.
   // It sequences roles across phases, waiting for CLI-callback signals on Type B roles.
   func RouteWorkflow(ctx workflow.Context, input RouteInput) (*RouteOutput, error) {
       // For each phase, for each role:
       //   - Type A: ExecuteActivity (synchronous)
       //   - Type B: ExecuteActivity to spawn session, then wait for Signal
       // Handle loops by checking role output against loop conditions
   }
   ```
   The workflow iterates through `input.Route.Phases` and for each role:
   - Type A: calls `ExecuteActivity` with `TypeAPitchActivity` and waits for result
   - Type B: calls `ExecuteActivity` with `TypeBSpawnActivity` (which spawns the session), then blocks on `workflow.GetSignalChannel(SignalChannelName)` waiting for the CLI callback signal

3. **Implement activities** in `activity.go`:
   ```go
   // TypeBSpawnActivity spawns an interactive session for a Type B role.
   // It does NOT wait for completion — the workflow waits for a Signal instead.
   func TypeBSpawnActivity(ctx context.Context, input TypeBSpawnInput) (*TypeBSpawnOutput, error) {
       // Use the provider to spawn the session
       // Return immediately with session metadata (PID, window name, etc.)
   }

   // TypeAPitchActivity runs a Type A role synchronously.
   func TypeAPitchActivity(ctx context.Context, input TypeAPitchInput) (*TypeAPitchOutput, error) {
       // Shell out to the provider command, pass JSON input, read JSON output
   }
   ```

4. **Write workflow tests** using Temporal's `TestWorkflowEnvironment`:
   ```go
   func (s *WorkflowSuite) TestRouteWorkflow_SetterToLead() {
       // Mock TypeBSpawnActivity for setter — returns immediately
       // Send a finish signal after a short delay (simulating CLI callback)
       // Mock TypeBSpawnActivity for lead — returns immediately
       // Send a finish signal after a short delay
       // Assert workflow completes successfully with both role outputs
   }
   ```
   Use `s.env.RegisterDelayedCallback()` to simulate CLI callback signals at the right time.

**Tests:**
- Workflow sequences setter → lead correctly
- Type B roles wait for signal before advancing
- Finish signal with output payload is captured
- Flare signal stops the workflow and records the flare
- Fail signal marks the role as failed
- Duplicate finish signal is idempotent (second signal ignored)

---

### Task 4: Type B provider — session spawner + CLI-callback

**Goal:** Implement the Type B provider that spawns interactive Claude Code / Codex sessions in tmux with a system prompt instructing the session to call `belayer <role> finish` when done.

**Files:**
- `internal/v2/provider/session.go`
- `internal/v2/provider/session_test.go`

**Steps:**

1. **Define the SessionSpawner interface:**
   ```go
   // SessionSpawner launches interactive sessions for Type B roles.
   type SessionSpawner interface {
       Spawn(ctx context.Context, opts SessionOpts) (*SessionInfo, error)
   }

   type SessionOpts struct {
       RoleName    string
       TaskID      string
       WorkDir     string
       InputJSON   json.RawMessage  // Role input data
       Provider    string            // "claude", "codex", or custom command
       ExtraArgs   []string
   }

   type SessionInfo struct {
       TmuxSession string
       WindowName  string
       PID         int
   }
   ```

2. **Implement ClaudeSessionSpawner:** Reuse the existing `shellQuote` and tmux command pattern from `internal/lead/claude.go`. The key addition is the system prompt:
   ```
   You are a {role}. When you have completed your work, you MUST call:
   belayer {role} finish --task-id {taskID}

   If you need help or are stuck, call:
   belayer {role} flare --task-id {taskID} --message "describe the problem"

   If you cannot complete the task, call:
   belayer {role} fail --task-id {taskID} --message "describe why"
   ```
   This is passed via `--append-system-prompt`.

3. **Implement CodexSessionSpawner:** Same pattern but with Codex CLI flags (prepend to prompt, no `--append-system-prompt`).

4. **Write tests with mock TmuxManager** (reuse the mock pattern from existing `internal/lead/claude_test.go`):
   - Verify the system prompt includes the finish command with correct task ID
   - Verify the system prompt includes flare and fail commands
   - Verify Claude uses `--append-system-prompt`, Codex prepends to prompt
   - Verify env exports are set correctly

**Tests:**
- Claude spawner includes `--append-system-prompt` with finish/flare/fail instructions
- Codex spawner prepends finish/flare/fail instructions to prompt
- Task ID is correctly interpolated into the system prompt
- WorkDir is set correctly via `cd`

---

### Task 5: CLI commands — `belayer run`, `belayer <role> finish/flare/fail`

**Goal:** Implement the v2 CLI commands that start pipeline runs and receive CLI callbacks from interactive sessions.

**Files:**
- `internal/v2/cli/root.go`
- `internal/v2/cli/run.go`
- `internal/v2/cli/role_signal.go`
- `internal/v2/cli/temporal.go`
- `internal/v2/cli/status.go`
- `internal/v2/cli/root_test.go`

**Steps:**

1. **Create v2 root command** in `root.go`:
   For the MVP, register v2 commands under the existing root by adding a `v2` subcommand group, OR replace the root entirely. Since this is a v2 clean break, replace the root commands but keep the old ones accessible under a `legacy` subcommand during development.

   Actually — for the MVP, add v2 commands alongside v1. The user can run `belayer run` (v2) or `belayer belayer start` (v1). No conflict because v2 commands are new names.

2. **Implement `belayer run`** in `run.go`:
   ```go
   // belayer run "add user authentication"
   // Starts a Temporal workflow with the default pipeline
   func newRunCmd() *cobra.Command {
       cmd := &cobra.Command{
           Use:   "run [description]",
           Short: "Start a pipeline run",
           RunE: func(cmd *cobra.Command, args []string) error {
               // 1. Connect to Temporal
               // 2. Parse pipeline DSL (or use default)
               // 3. Start RouteWorkflow with the description as input
               // 4. Print run ID and Temporal Web URL
           },
       }
       cmd.Flags().String("from", "", "Start from this role (pipeline slicing)")
       cmd.Flags().String("to", "", "Stop after this role")
       cmd.Flags().String("input", "", "JSON input file for --from role")
       cmd.Flags().String("pipeline", "", "Pipeline DSL file (default: belayer-pipeline.yaml)")
       return cmd
   }
   ```

3. **Implement `belayer <role> finish/flare/fail`** in `role_signal.go`:
   ```go
   // belayer setter finish --task-id abc123
   // belayer lead flare --task-id abc123 --message "stuck on auth"
   // belayer lead fail --task-id abc123 --message "cannot access repo"
   func newRoleSignalCmd(action model.SignalAction) *cobra.Command {
       cmd := &cobra.Command{
           Use:   fmt.Sprintf("%s --task-id <id>", action),
           Short: fmt.Sprintf("Signal %s for an interactive session", action),
           RunE: func(cmd *cobra.Command, args []string) error {
               // 1. Connect to Temporal
               // 2. Build RoleSignal from flags
               // 3. Send Signal to the workflow identified by task-id
               // 4. Print confirmation
           },
       }
       cmd.Flags().String("task-id", "", "Task ID (workflow run ID)")
       cmd.Flags().String("message", "", "Human-readable context")
       cmd.Flags().String("output", "", "JSON output payload (for finish)")
       cmd.MarkFlagRequired("task-id")
       return cmd
   }
   ```
   Register under dynamic role names: `belayer setter finish`, `belayer lead finish`, etc.

4. **Implement `belayer status`** in `status.go`:
   ```go
   // belayer status
   // Queries Temporal for active/completed runs
   func newV2StatusCmd() *cobra.Command { ... }
   ```

5. **Write CLI tests:** Test flag parsing, required flags, and error messages. Do NOT test Temporal integration here — that's the integration test in Task 14.

**Tests:**
- `belayer run` requires a description argument
- `belayer setter finish` requires `--task-id` flag
- `belayer lead flare --task-id abc --message "help"` parses correctly
- Unknown role name produces helpful error

---

### Task 6: Temporal dev server management — `belayer temporal start/stop/status`

**Goal:** Implement CLI commands that manage the Temporal dev server lifecycle so users don't have to install/run it manually.

**Files:**
- `internal/v2/cli/temporal.go`
- `internal/v2/cli/temporal_test.go`

**Steps:**

1. **Implement `belayer temporal start`:**
   - Check if `temporal` CLI is installed (`which temporal`)
   - If not: print install instructions (`brew install temporal` or download URL)
   - If yes: start `temporal server start-dev` as a background process
   - Store PID in `~/.belayer/temporal.pid`
   - Wait for server to be reachable (poll `localhost:7233` with timeout)
   - Print: "Temporal dev server started. Web UI: http://localhost:8233"

2. **Implement `belayer temporal stop`:**
   - Read PID from `~/.belayer/temporal.pid`
   - Send SIGTERM, wait, send SIGKILL if needed
   - Remove PID file
   - Print: "Temporal dev server stopped"

3. **Implement `belayer temporal status`:**
   - Check if PID file exists and process is alive
   - Try connecting to `localhost:7233`
   - Print: "Temporal dev server: running" or "Temporal dev server: not running"

4. **Auto-start in `belayer run`:** If `belayer run` can't connect to Temporal, suggest `belayer temporal start`.

**Tests:**
- `belayer temporal status` when no PID file → "not running"
- `belayer temporal start` when already running → "already running"
- PID file cleanup on stop

---

### Task 7: Pipeline DSL parser + validator

**Goal:** Parse YAML pipeline files into the `Route` model and validate topology (no cycles, valid role references, schema consistency).

**Files:**
- `internal/v2/pipeline/parser.go`
- `internal/v2/pipeline/validate.go`
- `internal/v2/pipeline/parser_test.go`
- `internal/v2/pipeline/validate_test.go`
- `internal/v2/pipeline/defaults.go` (embedded default pipeline)

**Steps:**

1. **Implement YAML parser** in `parser.go`:
   ```go
   func ParseRoute(data []byte) (*Route, error) {
       var route Route
       if err := yaml.Unmarshal(data, &route); err != nil {
           return nil, fmt.Errorf("pipeline parse: %w", err)
       }
       return &route, nil
   }

   func ParseRouteFile(path string) (*Route, error) {
       data, err := os.ReadFile(path)
       // ...
   }
   ```

2. **Implement topology validator** in `validate.go`:
   - All role names are unique
   - Loop targets reference existing roles
   - No circular loops (detect via DFS)
   - All roles have valid phase assignments
   - Schema file references exist (if specified)
   - Safety config values are positive

3. **Embed default pipeline** in `defaults.go`:
   ```yaml
   name: default
   phases:
     - phase: approach
       roles:
         - name: setter
           contract_type: ascent
           provider: { type: builtin }
     - phase: ascent
       roles:
         - name: decomposer
           contract_type: pitch
           provider: { type: builtin }
         - name: lead
           contract_type: ascent
           provider: { type: builtin }
         - name: spotter
           contract_type: pitch
           provider: { type: builtin }
       loops:
         - from: spotter
           to: decomposer
           max_iterations: 3
           condition: "output.pass == false"
     - phase: send
       roles:
         - name: pr-creator
           contract_type: pitch
           provider: { type: builtin }
   safety:
     max_child_depth: 2
     global_child_budget: 50
     child_dedupe: true
     gate_timeout: 24h
     heartbeat_interval: 60s
   ```

4. **Write table-driven tests:** Valid YAML, invalid YAML, circular loops, unknown roles, missing required fields.

**Tests:**
- Valid pipeline YAML parses correctly
- Invalid YAML returns error with context
- Circular loop detected and rejected
- Unknown role in loop target rejected
- Default pipeline validates successfully
- Empty phases array rejected

---

### Task 8: Type A provider — exec shell-out (JSON in/out)

**Goal:** Implement the Type A provider that shells out to a configured command, passes JSON on stdin, and reads JSON from stdout. Pattern matches `internal/envprovider/client.go`.

**Files:**
- `internal/v2/provider/exec.go`
- `internal/v2/provider/exec_test.go`

**Steps:**

1. **Implement ExecProvider:** Mirrors the `envprovider.Client.run()` pattern:
   ```go
   // ExecProvider runs a Type A role by shelling out to a command.
   type ExecProvider struct {
       Command string
       Args    []string
       Timeout time.Duration
   }

   func (e *ExecProvider) Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
       cmd := exec.CommandContext(ctx, e.Command, e.Args...)
       cmd.Stdin = bytes.NewReader(input)
       cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
       cmd.Cancel = func() error { /* kill process group */ }
       out, err := cmd.Output()
       // Parse and validate JSON output
       return out, nil
   }
   ```

2. **Add process group isolation** (same pattern as envprovider — `Setpgid: true` + process group kill on cancel).

3. **Add output validation:** Check that stdout is valid JSON before returning.

4. **Write tests with mock commands:** Create test binaries that echo input or return errors. Use `t.TempDir()` + `PATH` prepend pattern from existing tests.

**Tests:**
- Valid command receives JSON on stdin, returns JSON on stdout
- Command not found returns clear error
- Command exits non-zero returns error with stderr
- Command timeout triggers context cancellation
- Invalid JSON output returns parse error
- Process group isolation (no orphans)

---

### Task 9: Risk gate framework

**Goal:** Implement risk evaluation at role transitions. Below threshold: auto-advance. Above threshold: pause workflow and wait for human signal.

**Files:**
- `internal/v2/riskgate/gate.go`
- `internal/v2/riskgate/gate_test.go`

**Steps:**

1. **Define risk gate types:**
   ```go
   type RiskScore struct {
       Score   float64 `json:"score"`    // 0.0 - 1.0
       Factors []string `json:"factors"` // What contributed to the score
   }

   type GateDecision string
   const (
       GateAutoPass GateDecision = "auto_pass"
       GateHumanReview GateDecision = "human_review"
   )

   type GateConfig struct {
       Threshold float64       // Score above this → human review
       Timeout   time.Duration // Auto-flare after this
   }
   ```

2. **Implement risk scorer:** For MVP, a simple heuristic based on output size, file count, and whether the role reported any warnings. Full AI-based scoring is deferred.

3. **Implement gate evaluation in the workflow:** After each role completes, evaluate risk score. If above threshold, block on a Signal channel for human approval. If timeout expires, auto-flare.

4. **Write tests:** Score computation, threshold decisions, timeout behavior.

**Tests:**
- Score below threshold → auto_pass
- Score above threshold → human_review
- Exact threshold → human_review (conservative)
- Timeout triggers flare
- Human approval signal resumes workflow

---

### Task 10: Pipeline visualization — `belayer pipeline show`

**Goal:** Render the pipeline topology as ASCII art from the DSL model.

**Files:**
- `internal/v2/pipeline/visualize.go`
- `internal/v2/pipeline/visualize_test.go`
- `internal/v2/cli/pipeline.go`

**Steps:**

1. **Implement ASCII renderer:** Takes a `Route` model and renders:
   ```
   APPROACH          ASCENT                    SEND
   ─────────        ──────────────           ────────
   [setter]●  ───►  [decomposer]○  ───►     [pr-creator]○
                         │
                    [lead]○  ───►
                         │
                    [spotter]○
                         │
                    ◄─── loop (max 3)
   ```
   Status markers: `●` = active, `✓` = complete, `○` = pending, `✗` = failed.

2. **Implement `belayer pipeline show` CLI command.**

3. **Implement `belayer pipeline validate` CLI command** — runs the topology validator and reports issues.

4. **Write snapshot tests:** Assert specific ASCII output for known pipeline configurations.

**Tests:**
- Default pipeline renders expected ASCII
- Pipeline with active roles shows status markers
- `belayer pipeline validate` on valid pipeline → "Pipeline valid"
- `belayer pipeline validate` on invalid pipeline → error details

---

### Task 11: Eval framework + fixture recording

**Goal:** Implement `belayer eval <role>` for testing roles against fixtures, and fixture auto-recording during pipeline runs.

**Files:**
- `internal/v2/eval/eval.go`
- `internal/v2/eval/fixture.go`
- `internal/v2/eval/eval_test.go`
- `internal/v2/cli/eval.go`

**Steps:**

1. **Define fixture format:**
   ```go
   type Fixture struct {
       Role      string          `json:"role"`
       Input     json.RawMessage `json:"input"`
       Output    json.RawMessage `json:"output"`
       Timestamp time.Time       `json:"timestamp"`
       RunID     string          `json:"run_id"`
   }
   ```
   Stored at `~/.belayer/fixtures/<role>/<timestamp>.json`.

2. **Implement fixture recorder:** Hook into the activity wrapper — after each role completes, snapshot input/output as a fixture.

3. **Implement eval runner:** Load fixtures for a role, execute the role provider against each fixture's input, compare output against the fixture's expected output using JSON deep-equal.

4. **Implement `belayer eval <role>` CLI command:**
   - Load all fixtures for the role
   - Execute role provider against each
   - Report pass/fail per fixture
   - Summary: "3/5 fixtures passed"

**Tests:**
- Fixture serialization round-trip
- Eval against matching fixture → pass
- Eval against mismatching fixture → fail with diff
- No fixtures → "No fixtures for role 'setter'"
- Fixture recording creates file in correct path

---

### Task 12: Pipeline templates + `belayer init`

**Goal:** Ship pre-built pipeline templates and a `belayer init` that creates the pipeline file from a template.

**Files:**
- `internal/v2/pipeline/templates/` (embedded YAML files)
- `internal/v2/cli/init.go`
- `internal/v2/cli/init_test.go`

**Steps:**

1. **Create pipeline templates** embedded via `embed.FS`:
   - `solo.yaml` — setter → lead → spotter → pr-creator (minimal)
   - `team.yaml` — full Approach → Ascent → Send pipeline
   - `research.yaml` — analyst → explorer only (no execution)

2. **Implement `belayer init --template <name>`:**
   - Creates `belayer-pipeline.yaml` in the current directory from the selected template
   - Prints: "Pipeline created: belayer-pipeline.yaml (template: solo)"
   - If file exists: prompt to overwrite or use `--force`

3. **Write tests:** Template parsing, file creation, overwrite protection.

**Tests:**
- Each template is valid (passes validator)
- `belayer init --template solo` creates the file
- `belayer init` when file exists → error without `--force`
- Unknown template name → error with available list

---

### Task 13: Safety controls (depth, budget, dedupe, expiry)

**Goal:** Implement the safety controls from the design doc within the Temporal workflow.

**Files:**
- `internal/v2/temporal/safety.go`
- `internal/v2/temporal/safety_test.go`

**Steps:**

1. **Implement depth limiter:** Track child workflow depth in workflow context. Reject child emission at max depth.

2. **Implement budget tracker:** Count total child workflows spawned. Reject at budget limit.

3. **Implement deduplication:** Hash child work input. Reject if seen before in this run.

4. **Implement approval expiry:** Risk gate timeout already in Task 9 — wire the configurable timeout from SafetyConfig.

5. **Cancellation propagation:** Use Temporal's `ChildWorkflowOptions.ParentClosePolicy` set to `TERMINATE`. This is Temporal-native.

**Tests:**
- Child at max depth → rejected with warning
- Child at budget limit → rejected
- Duplicate child → deduplicated
- Cancellation propagates to children (Temporal test framework)

---

### Task 14: Integration test — full setter → lead pipeline

**Goal:** End-to-end test that starts a Temporal dev server, runs the full setter → lead pipeline with mock sessions, and verifies completion.

**Files:**
- `internal/v2/integration_test.go`

**Steps:**

1. **Start Temporal dev server** in test setup (or use Temporal's test server).

2. **Register workflow and activities** with the test worker.

3. **Mock session spawner:** Instead of real Claude Code, spawn a simple bash script that sleeps 1 second then calls `belayer setter finish --task-id <id>`.

4. **Execute the full pipeline:**
   - `belayer run "test description"` starts the workflow
   - Mock setter calls finish after 1s
   - Mock lead calls finish after 1s
   - `belayer status` shows the run as completed

5. **Verify:** Workflow completed, both role outputs captured, fixture recorded.

**Tests:**
- Full pipeline: setter → lead → complete
- Setter flares → pipeline stops with flare status
- Lead fails → pipeline stops with failed status
- Duplicate finish → idempotent, pipeline completes once

---

## Outcomes & Retrospective

**What worked:**
- Temporal Go SDK integrated cleanly — v1.41.1 is mature and the test framework (TestWorkflowEnvironment) is excellent for unit-testing workflows without a running server
- The existing envprovider pattern (shell-out + JSON contract) mapped perfectly to the Type A exec provider
- CLI-callback contract (belayer <role> finish/flare/fail) is elegant — sessions don't need to know about Temporal
- Incremental task structure (14 tasks in 6 batches) allowed steady progress with verification at each step
- Two provider contracts (Type A pitch / Type B ascent) honestly model the two execution modes that already existed in v1

**What didn't:**
- Temporal SDK required explicit `go get` for sub-packages — initial `go get go.temporal.io/sdk` wasn't sufficient
- String-based activity names don't work with latest Temporal SDK for mocking — had to switch to method references
- Temporal ListWorkflow API returns protobuf directly, not an iterator — API assumptions from docs were wrong

**Learnings to codify:**
- Temporal activity mocking requires registering the struct first, then using method references (not strings) in OnActivity
- Process group isolation (Setpgid + negative PID kill) pattern is universal — used in envprovider, agentic, and now exec provider
- YAML pipeline DSL with embed.FS for templates is a clean pattern for shipping opinionated defaults with customization
