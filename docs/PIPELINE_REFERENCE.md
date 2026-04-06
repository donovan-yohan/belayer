# Pipeline YAML Reference

Complete guide to writing belayer pipeline YAML files. Use this when constructing or modifying `.belayer/pipeline.yaml` and subpipeline files.

## Top-Level Structure

```yaml
name: my-pipeline                    # required: pipeline identifier

intake:                              # optional: how work enters the pipeline
  - name: design-doc
    type: trigger
    check: .belayer/scripts/check-ready.sh

nodes:                               # required: at least one node
  - name: implement
    # ... node config

safety:                              # optional: pipeline-wide limits
  max_concurrent_runs: 3
```

## Node Types

Every node has a `type` field. Three types exist:

| Type | Purpose | Key Fields |
|------|---------|------------|
| `node` | Constructive step — produces artifacts | `command`, `output` |
| `gate` | Adversarial quality check — scores and routes | `dimensions`, `thresholds`, `output: { type: gate_result }` |
| `agent` | Vendor-driven node — belayer builds the CLI command | `vendor`, `prompt`, `output` |

Agent nodes with `dimensions` are treated as gates (agent does the scoring). Agent nodes with `routes` are routers (agent picks a path).

## Common Fields (All Node Types)

```yaml
- name: my-node                      # required: unique within pipeline
  type: node                         # node | gate | agent (default: node)
  description: |                     # optional: human-readable purpose
    What this node does.

  # Input: what the node receives
  input:
    type: description                # description | file | commit
    key: previous_node               # optional: artifact key from a prior node

  # Output: what the node produces
  output:
    type: file                       # required: file | commit | gate_result | pr | route_result
    path: output/result.txt          # optional: expected output file path
    key: my_output                   # optional: artifact key (default: node name)

  # Routing: where to go after this node
  on_pass: next-node                 # node name | "next" | "stop" (default: next sequential)
  on_retry: self                     # node name | "self" (default: self)
  on_fail: stop                      # node name | "stop" (default: stop)
  max_retries: 3                     # max retry attempts (default: 0 = no retries)
```

## Script Nodes (`type: node`)

Execute a shell command. Belayer runs the command via `sh -c`, polls for a completion file, and routes based on outcome.

```yaml
- name: implement
  type: node
  command: .belayer/scripts/run.sh
  description: |
    Implement the feature described in the design doc.
  input: { type: file, key: design_doc }
  output: { type: commit }
  on_pass: review
  on_retry: self
  max_retries: 3
```

**Environment variables** set before command execution:
- `BELAYER_TASK_ID` — workflow ID
- `BELAYER_NODE` — node name
- `BELAYER_ATTEMPT` — attempt number (0-based)
- `BELAYER_WORK_DIR` — worktree directory

**Context file**: `.belayer/.internal/input/node-context.json` is written before the command runs. Contains task details, node config, and artifacts from prior nodes.

**Completion**: The command (or a hook) writes `.belayer/.internal/completion/<task-id>-<node>-attempt-<N>.json` when done.

## Agent Nodes (`type: agent`)

Belayer resolves the vendor to a CLI command. No shell script needed.

```yaml
- name: implement
  type: agent
  vendor: claude                     # required: claude | codex
  prompt: |                          # required: what to tell the agent
    Implement the design specification at %{INPUT}.
    Write tests. Commit when done.
  input: { type: file, key: design_doc }
  output: { type: commit }
  on_pass: review
  max_retries: 3
```

**Supported vendors:**

| Vendor | CLI Command | Schema Flag |
|--------|-------------|-------------|
| `claude` | `claude -p --output-format stream-json` | `--json-schema` (inline JSON) |
| `codex` | `codex exec -s read-only --json` | `--output-schema` (temp file) |

**Variable interpolation** in prompts: Use `%{VAR}` syntax (not `$VAR`, which clashes with shell and agent skills).

| Variable | Resolves To |
|----------|-------------|
| `%{INPUT}` | Input artifact path or description |
| `%{WORK_DIR}` | Worktree directory |

## Gate Nodes (Adversarial Quality Checks)

Gates evaluate artifacts and produce structured scores. Belayer computes a weighted average and routes based on YAML thresholds. The gate session never decides its own outcome (score-then-route anti-gaming).

Gates can be `type: gate` with a `command`, or `type: agent` with `dimensions`:

```yaml
# Option A: gate with shell command
- name: review
  type: gate
  command: .belayer/scripts/run.sh
  description: |
    Review the code for spec compliance and test coverage.
  input: { type: commit }
  dimensions:
    - { name: spec_compliance, weight: 0.35, description: "Changes match spec?" }
    - { name: test_coverage, weight: 0.30, description: "Tests are meaningful?" }
    - { name: correctness, weight: 0.35, description: "Works in production?" }
  thresholds:
    pass: 7.0                        # weighted score >= 7.0 → PASS
    retry: 4.0                       # weighted score >= 4.0 → RETRY (else FAIL)
  output: { type: gate_result }
  on_pass: ship
  on_retry: implement
  max_retries: 2

# Option B: agent gate (belayer builds the CLI command + schema)
- name: review
  type: agent
  vendor: codex
  prompt: |
    Review the code changes. Score each dimension 0-10.
  input: { type: commit }
  dimensions:
    - { name: spec_compliance, weight: 0.35, description: "Changes match spec?" }
    - { name: test_coverage, weight: 0.30, description: "Tests are meaningful?" }
    - { name: correctness, weight: 0.35, description: "Works in production?" }
  thresholds:
    pass: 7.0
    retry: 4.0
  output: { type: gate_result }
  on_pass: ship
  on_retry: implement
  max_retries: 2
```

**Gate rules:**
- At least one dimension required
- Dimension weights must sum to 1.0
- `thresholds.pass` must be in (0, 10]
- `thresholds.retry` must be non-negative and less than `thresholds.pass`
- Output type must be `gate_result`
- Gate prompts should include `%{INPUT}` so the rubric is injected

**Gate output**: Produces `gate-result.json` (structured scores) + `rationale.md` (human-readable explanation). The rationale is mandatory as an anti-gaming measure.

## Router Nodes (Agentic N-Way Branching)

Routers let an LLM choose one of N declared paths. Each path runs as an isolated Temporal child workflow with its own nodes, retry counters, and completion semantics.

```yaml
- name: review-router
  type: agent                        # must be agent
  vendor: claude
  prompt: |
    Read the PR diff at %{INPUT}. Classify this change and choose
    the appropriate review depth. Consider: scope, risk areas,
    public API changes, file count, and test coverage impact.
  input: { type: commit, key: implement }
  output: { type: route_result, path: .belayer/.internal/output/route-result.json }
  routes:
    mode: choose_one                 # required: only mode currently supported
    options:                         # required: at least 2 options
      full-feature-review:
        pipeline: .belayer/pipelines/full-feature-review.yaml   # required: subpipeline path
        description: >                                          # optional but recommended
          Broad or risky change. Needs thorough code review.
      quick-bugfix-review:
        pipeline: .belayer/pipelines/quick-bugfix-review.yaml
        description: >
          Small localized fix. Lightweight review only.
      refactor-review:
        pipeline: .belayer/pipelines/refactor-review.yaml
        description: >
          Structural change. Focus on behavioral preservation.
  on_pass: stop
  on_retry: self
  on_fail: stop
  max_retries: 2
```

**Router rules:**
- Must be `type: agent` with a `vendor` and `prompt`
- `routes.mode` must be `choose_one`
- At least 2 route options required
- Option names must match `^[a-zA-Z0-9][a-zA-Z0-9-]*$`
- Each option must have a non-empty `pipeline` path
- Output type must be `route_result`
- Cannot have `dimensions` (routes and gates are mutually exclusive)
- `on_retry` must be `self` or empty

**How it works:**
1. Belayer injects a route selection prompt with the option names, descriptions, and a JSON Schema with an `enum` constraint (prevents hallucinated routes)
2. The agent writes `route-result.json` with `route`, `confidence`, `reasoning`, and `rejected` alternatives
3. Belayer validates the result, then spawns the chosen subpipeline as a Temporal child workflow
4. The child runs in the same worktree with its own retry budget
5. Child outputs merge back into the parent pipeline (namespaced as `router-name/child-node`)

**Subpipeline files** are standalone pipeline YAMLs (same schema as the top-level). They live in `.belayer/pipelines/` by convention. Example:

```yaml
# .belayer/pipelines/quick-bugfix-review.yaml
name: quick-bugfix-review

nodes:
  - name: code-review
    type: agent
    vendor: codex
    prompt: |
      Lightweight review: check correctness and regression risk.
    input: { type: commit }
    dimensions:
      - { name: correctness, weight: 0.4, description: "Does the fix work?" }
      - { name: regression_risk, weight: 0.35, description: "Could this break something?" }
      - { name: test_coverage, weight: 0.25, description: "Is the fix tested?" }
    thresholds: { pass: 6.5, retry: 3.0 }
    output: { type: gate_result }
    on_pass: ship
    on_retry: fix
    max_retries: 1

  - name: fix
    type: agent
    vendor: claude
    prompt: "Address the review feedback at %{INPUT}"
    input: { type: file, key: feedback }
    output: { type: commit }
    on_pass: code-review

  - name: ship
    type: agent
    vendor: claude
    prompt: "Create a PR for the changes"
    input: { type: commit }
    output: { type: pr }
    on_pass: stop
```

**Subpipeline YAMLs are pre-resolved** at startup (worker boot or `belayer climb`). The raw YAML is snapshotted and passed into the Temporal workflow for deterministic replay. No file I/O happens during workflow execution.

## Output Types

| Type | Produced By | Description |
|------|-------------|-------------|
| `file` | Nodes | Generic file output (default for script nodes) |
| `commit` | Nodes, agents | Git commit in the worktree |
| `gate_result` | Gates | Structured scores + rationale |
| `pr` | Nodes, agents | Pull request creation |
| `route_result` | Routers | Route decision with confidence and reasoning |

**Adding a new output type** requires updates in three places:
1. `internal/pipeline/validate.go` — `validOutputTypes` map
2. `internal/pipeline/model.go` — `OutputConfig` type comment
3. `internal/outcome/detect.go` — `typeDefault` switch

Missing `detect.go` causes silent false-positive outcomes.

## Intake Configuration

Intake defines how work enters the pipeline.

```yaml
intake:
  - name: design-doc
    type: trigger                    # trigger | interactive | jira
    check: .belayer/scripts/check-ready.sh

  - name: interactive
    type: interactive                # at most one interactive intake
```

| Type | Description |
|------|-------------|
| `trigger` | Script-based polling. `check` script returns artifact path on stdout (exit 0) or nothing (exit 1) |
| `interactive` | `belayer start` session. At most one per pipeline |
| `jira` | Jira issue intake (requires config) |

## Safety Configuration

```yaml
safety:
  max_concurrent_runs: 3             # max parallel workflows (default: 3)
```

## Validation Rules Summary

Belayer validates pipelines at parse time. Common validation errors:

| Rule | Error |
|------|-------|
| Pipeline must have a `name` | `pipeline name is required` |
| At least one node | `pipeline must have at least one node` |
| No duplicate node names | `duplicate node name: "X"` |
| `on_pass`/`on_retry`/`on_fail` must reference a known node or keyword | `references unknown node or keyword` |
| Gate dimensions must sum to 1.0 | `dimension weights sum to X, must sum to 1.0` |
| Agent nodes require `vendor` + `prompt` | `vendor is required`, `prompt is required` |
| `command` and `vendor` are mutually exclusive | `command and vendor are mutually exclusive` |
| Router + gate is not allowed | `routes and dimensions are mutually exclusive` |

## Complete Example

A full pipeline with implementation, routing, and subpipelines:

```yaml
name: full-pipeline

intake:
  - name: design-doc
    type: trigger
    check: .belayer/scripts/check-ready.sh

nodes:
  - name: implement
    type: agent
    vendor: claude
    prompt: "Implement the design specification at %{INPUT}"
    input: { type: file, key: design_doc }
    output: { type: commit }
    on_pass: review-router
    on_fail: stop

  - name: review-router
    type: agent
    vendor: claude
    prompt: |
      Classify this change and choose the review depth.
      Consider scope, risk, API changes, and test coverage.
    input: { type: commit, key: implement }
    output: { type: route_result }
    routes:
      mode: choose_one
      options:
        thorough-review:
          pipeline: .belayer/pipelines/thorough-review.yaml
          description: Large or risky change. Full 5-dimension review.
        quick-review:
          pipeline: .belayer/pipelines/quick-review.yaml
          description: Small fix. 3-dimension lightweight review.
    on_pass: stop
    on_retry: self
    on_fail: stop
    max_retries: 2

safety:
  max_concurrent_runs: 3
```

## See Also

- [Design](DESIGN.md) — patterns and conventions
- [Architecture](ARCHITECTURE.md) — module boundaries and data flow
- [Quality](QUALITY.md) — testing strategy
