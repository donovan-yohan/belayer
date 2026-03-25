---
status: current
source: /plan-eng-review + /plan-ceo-review (2026-03-25)
---

# Design: Belayer Three-Phase Architecture (Explore / Climb / Summit)

## Goal

Document belayer's three-phase architecture as the canonical reference for all future development. This supersedes the v3 naming (setter/spotter as pipeline nodes) and establishes:
- Three imperative phases (Explore / Climb / Summit) with clear contracts
- Multi-repo as an additive layer via opinionated setter/spotter
- PR creation inside Climb for natural retry loops
- Config hierarchy (~/.belayer global + ./.belayer repo-level)
- Competitive positioning as an agent-agnostic orchestration standard

**This is a documentation deliverable, not a code change.** Code restructuring (renaming pipeline nodes, removing FanOut fields) is a separate future plan.

## Approach

Write documentation artifacts that capture the architecture decisions from the eng review (7 issues resolved) and CEO review (4 proposals, 3 accepted). All content has been reviewed by both eng and CEO review skills plus a Codex outside voice (10 findings, 3 tensions resolved).

Deliverables:
1. **This design doc** — canonical architecture reference with FAQ, pipeline examples, config hierarchy, and positioning
2. **Updated ARCHITECTURE.md** — three-phase model, role definitions, contracts
3. **Updated DESIGN.md** — setter/spotter contracts, additive principle, PR manifest interface
4. **Updated CLAUDE.md** — reflect new naming and concepts
5. **TODOS.md** — capture deferred `using-belayer` skill
6. **Go parse test** — `internal/v3/pipeline/doc_examples_test.go` validates example YAMLs from this doc

## Key Decisions

### From Eng Review (7 resolved)
1. **Three phases: Explore -> Climb -> Summit** (imperative verbs, maps to CLI commands)
2. **Multi-repo is additive, not transformative** — per-repo pipeline unchanged, setter/spotter layer on top
3. **Setter/spotter are first-class belayer concepts** with enforced contracts, NOT generic pipeline nodes
4. **PR creation/review is inside Climb** — natural retry on CI/review failure
5. **Climbers -> Summit interface: PR manifest** (JSON with PR refs, CI status, validation scores)
6. **Boulderer: lightweight specialist agent** — CLI + pipeline dispatched, deferred implementation
7. **Generic FanOut/Per/FanIn to be removed** — replaced by opinionated setter/spotter

### From CEO Review (3 accepted)
1. **Config hierarchy spec** — ~/.belayer (global) + ./.belayer (repo-level)
2. **"Why Belayer" positioning** — agent-agnostic orchestration standard
3. **Pipeline YAML examples** — 3 templates showing single-repo, multi-repo, custom TDD

### From Codex Outside Voice (key tensions resolved)
- PR before spotter in multi-repo is intentional (additive workflow principle)
- Opinionated vs generic: user's intentional choice, boring by default
- Boulderer deferred until retry demonstrably fails

## Architecture

### Three Phases

```
EXPLORE (intake)          CLIMB (implementation)              SUMMIT (output)
+-----------------+   +-------------------------------+   +----------------+
|                 |   |                               |   |                |
|  intake sources |   |  +---------+    +----------+  |   |  auto-merge    |
|  - - - - - - ->+-->|  | setter* |-->| lead     |  |   |  observability |
|  - - - - - - ->|   |  |(decomp) |   |(per-repo)|--+-->|  monitoring    |
|  - - - - - - ->|   |  +----^----+   +----------+  |   |  - - - - - - >|
|                 |   |       |feedback +----------+  |   |  - - - - - - >|
|    spec.md      |   |       +---------| spotter* |  |   |                |
|                 |   |                 |(validate)|  |   |  PR manifest   |
|                 |   |  *multi-repo    +----------+  |   |                |
+-----------------+   +-------------------------------+   +----------------+
```

| Phase | CLI Command | Interface In | Interface Out | Status |
|-------|------------|-------------|--------------|--------|
| **Explore** | `belayer explore` | intake sources (Jira, interactive, etc.) | spec.md | Exists |
| **Climb** | `belayer climb` | spec.md | PR manifest (JSON) | Exists (needs restructure) |
| **Summit** | `belayer summit` | PR manifest | Merged PRs + monitoring | Not implemented |

### Multi-repo Additive Layer (within Climb)

Multi-repo coordination is additive — the per-repo pipeline runs identically whether you're working on one repo or ten.

- **Setter** (multi-repo only): receives spec.md, produces per-repo spec.md files (fan-out)
- **Spotter** (multi-repo only): receives N commit hashes, produces gate score + feedback (fan-in)
- **Neither changes the per-repo pipeline** — they wrap it
- **Belayer enforces**: multi-repo crag without setter+spotter = runtime error

Setter and spotter are first-class belayer concepts with enforced contracts — NOT generic pipeline nodes. They get their own top-level pipeline YAML sections:

```yaml
# Future multi-repo pipeline:
setter:
  description: "Decompose spec into per-repo work"
  max_retries: 2
  # Contract: spec.md in -> per-repo spec.md out

spotter:
  description: "Cross-repo validation"
  dimensions:
    - name: api_contract
      weight: 0.4
    - name: integration
      weight: 0.6
  thresholds: {pass: 7.0, retry: 4.0}
  # Contract: N commit hashes in -> gate score + feedback out

nodes:  # per-repo pipeline (same for all repos)
  - name: plan
  - name: implement
  - name: review
  - name: pr-author
  - name: pr-review
```

### Named Roles

| Role | Scope | Required | Contract |
|------|-------|----------|----------|
| **Setter** | Multi-repo only | If multi-repo crag | spec.md -> per-repo spec.md |
| **Spotter** | Multi-repo only | If multi-repo crag | N commit hashes -> gate score + feedback |
| **Lead** | Per-repo | Always (implicit) | spec.md -> commits + PR |
| **Boulderer** | One-off | Never (on-demand) | task -> single commit |

### Relationship to v3 Code

This is an architectural REFRAMING, not a rewrite. The v3 Temporal pipeline, node activities, gate scoring, and intake adapters all remain. Future code restructuring will:
- Rename default pipeline nodes (current "setter" -> "plan", current "spotter" gate -> "review")
- Remove generic FanOut/Per/FanIn from NodeConfig (replaced by first-class setter/spotter)
- Move PR creation from summit node into climb pipeline
- Add setter/spotter as top-level pipeline config for multi-repo

### PR Manifest (Climbers -> Summit Interface)

```json
{
  "prs": [
    {
      "repo": "api",
      "url": "github.com/.../pull/42",
      "number": 42,
      "branch": "belayer/feature-xyz",
      "commit": "abc1234",
      "ci_status": "passed",
      "reviews": "approved"
    }
  ],
  "validation": {
    "cross_repo": "PASS",
    "spotter_score": 8.5
  }
}
```

### Single-Repo Pipeline (Default)

In single-repo, no setter or spotter is needed. The pipeline is the harness:loop steps as individual nodes:

```yaml
name: single-repo-default
intake:
  - name: user-session
    type: interactive
nodes:
  - name: plan
    type: node
    description: |
      Create an implementation plan from the spec.
      Run /harness:plan to create the plan.
      Write the plan to .belayer/output/plan.md.
    input:
      type: file
      key: spec
    output:
      type: file
      path: .belayer/output/plan.md
    on_pass: next
    on_retry: self
    on_fail: stop
    max_retries: 2

  - name: implement
    type: node
    description: |
      Implement the plan. Focus on clean, tested code.
      Run /harness:orchestrate to execute the plan.
      Commit your changes to the current branch.
    input:
      type: file
      key: plan
    output:
      type: commit
    on_pass: next
    on_retry: self
    on_fail: stop
    max_retries: 3

  - name: review
    type: gate
    description: |
      Adversarial code review. Review the changes for
      spec compliance, test coverage, and correctness.
    input:
      type: commit
    dimensions:
      - name: spec_compliance
        description: "Do changes match the plan?"
        weight: 0.35
        rubric: "9-10: exact match, 6-8: minor deviations, 3-5: significant gaps, 0-2: wrong direction"
      - name: test_coverage
        description: "Are changes tested?"
        weight: 0.3
        rubric: "9-10: comprehensive, 6-8: happy path, 3-5: minimal, 0-2: untested"
      - name: correctness
        description: "Would this work in production?"
        weight: 0.35
        rubric: "9-10: production-ready, 6-8: minor concerns, 3-5: significant risks, 0-2: broken"
    thresholds:
      pass: 7.0
      retry: 4.0
    output:
      type: gate_result
      path: .belayer/output/gate-result.json
    on_pass: next
    on_retry: implement
    on_fail: stop
    max_retries: 2

  - name: pr-author
    type: node
    description: |
      Create a pull request for the completed work.
      Run /pr:author to create the PR.
      Write the PR URL to .belayer/output/pr.json.
    input:
      type: gate_result
      key: review
    output:
      type: pr
      path: .belayer/output/pr.json
    on_pass: stop
    on_retry: self
    on_fail: stop
    max_retries: 2

safety:
  max_concurrent_runs: 3
```

### Multi-Repo Pipeline (Future)

```yaml
name: multi-repo-default

setter:
  description: |
    Decompose the spec into per-repo work items.
    Read the spec at the input path. For each repo in the crag,
    produce a focused spec.md that describes only the work
    needed in that repo, with context about the larger problem.
  max_retries: 2

spotter:
  description: |
    Validate cross-repo alignment. Check that:
    - API contracts between repos are consistent
    - Shared types match across boundaries
    - Integration points are compatible
    - No repo's changes break another's assumptions
  dimensions:
    - name: api_contracts
      description: "Do API contracts between repos agree?"
      weight: 0.4
    - name: integration
      description: "Are integration points compatible?"
      weight: 0.35
    - name: consistency
      description: "Are shared types and conventions consistent?"
      weight: 0.25
  thresholds:
    pass: 7.0
    retry: 4.0
  max_retries: 2

nodes:
  - name: plan
    type: node
    description: "Create implementation plan from per-repo spec."
    input: {type: file, key: spec}
    output: {type: file, path: .belayer/output/plan.md}
    on_pass: next
    on_retry: self
    max_retries: 2

  - name: implement
    type: node
    description: "Implement the plan. Commit changes."
    input: {type: file, key: plan}
    output: {type: commit}
    on_pass: next
    on_retry: self
    max_retries: 3

  - name: review
    type: gate
    description: "Per-repo quality gate."
    input: {type: commit}
    dimensions:
      - {name: spec_compliance, weight: 0.35, description: "Matches plan?"}
      - {name: test_coverage, weight: 0.3, description: "Tests adequate?"}
      - {name: correctness, weight: 0.35, description: "Production-ready?"}
    thresholds: {pass: 7.0, retry: 4.0}
    output: {type: gate_result}
    on_pass: next
    on_retry: implement
    max_retries: 2

  - name: pr-author
    type: node
    description: "Create PR for this repo's changes."
    input: {type: gate_result, key: review}
    output: {type: pr, path: .belayer/output/pr.json}
    on_pass: stop
    on_retry: self
    max_retries: 2

safety:
  max_concurrent_runs: 3
```

### Custom TDD Pipeline

```yaml
name: tdd-pipeline
intake:
  - name: user-session
    type: interactive
nodes:
  - name: test-first
    type: node
    description: |
      Write tests FIRST based on the spec. Do not implement yet.
      Focus on: acceptance tests, edge cases, error paths.
      Commit the tests to the current branch.
    input: {type: file, key: spec}
    output: {type: commit}
    on_pass: next
    on_fail: stop
    max_retries: 2

  - name: implement
    type: node
    description: |
      Implement code to make all tests pass.
      Run the test suite. Fix until green.
      Commit implementation changes.
    input: {type: commit, key: test-first}
    output: {type: commit}
    on_pass: next
    on_retry: self
    on_fail: stop
    max_retries: 3

  - name: test-verify
    type: gate
    description: "Verify all tests pass and coverage is adequate."
    input: {type: commit}
    dimensions:
      - {name: test_pass_rate, weight: 0.5, description: "All tests green?"}
      - {name: coverage, weight: 0.3, description: "Coverage above threshold?"}
      - {name: quality, weight: 0.2, description: "Tests are meaningful, not trivial?"}
    thresholds: {pass: 8.0, retry: 5.0}
    output: {type: gate_result}
    on_pass: next
    on_retry: implement
    max_retries: 2

  - name: pr-author
    type: node
    description: "Create PR with TDD evidence."
    input: {type: gate_result, key: test-verify}
    output: {type: pr, path: .belayer/output/pr.json}
    on_pass: stop
    max_retries: 2
```

## Config Hierarchy

```
~/.belayer/                        # global
  config.json                      # global settings
  crags/                           # multi-repo crag definitions
    <crag-name>/
      crag.json                    # repo paths, setter/spotter config
  boulderer/                       # shared boulderer implementation

./.belayer/                        # repo-level (per-repo)
  pipeline.yaml                    # climb pipeline config
  config.json                      # repo-specific settings
  .internal/                       # git-ignored: worktrees, runs, state
    worktrees/
    runs/
```

**Resolution chain:** repo-level > global > embedded defaults

**Git-ignore pattern:** `.belayer/.internal/` should be git-ignored. Config files (pipeline.yaml, config.json) are committed.

**Multi-repo crags as global config:** Crags are lightweight config files in `~/.belayer/crags/` grouping repo paths. No bare repo clones needed. The crag config specifies which repos, plus setter/spotter configs if multi-repo.

## Why Belayer

| belayer | competitors |
|---------|------------|
| Agent-agnostic orchestration | Model-locked agents |
| Orchestration standard (like Docker Compose) | Agents that happen to orchestrate |
| Multi-repo as additive layer | Multi-repo as agent feature |
| Pipeline-as-YAML (fully customizable) | Hardcoded workflows |
| Three phases with typed contracts | Monolithic pipelines |
| You own your nodes | Platform owns your agents |

**Competitive landscape (March 2026):** Agent Orchestrator (Composio), Zenflow (Zencoder), Claude Code Teams, Squad (GitHub/Copilot), Intent (Augment Code). All are model-locked. Belayer is the only agent-agnostic orchestration standard with additive multi-repo support.

### Strategic Principles

1. **Belayer optimizes for autonomy, not efficiency** — Redundant work is acceptable if it means self-correction without human intervention
2. **Multi-repo is additive, not transformative** — Per-repo pipeline unchanged; setter/spotter layer on top
3. **Belayer is plumbing** — Provides contracts + orchestration, not node implementations
4. **Agent-agnostic** — Nodes are black boxes; use whatever agent fulfills the contract
5. **Boring by default** — Solve specific problems with opinionated plumbing, don't over-abstract

## FAQ

### Why does belayer create PRs before cross-repo validation?

Belayer follows the **additive workflow principle**: the per-repo flow (including PR creation and review) runs identically whether you're working on one repo or ten. Multi-repo coordination (setter/spotter) layers on top without changing how individual repos work.

If you only automated one repo, you'd follow the full flow to completion including PR. Multi-repo adds validation *after* -- and if the spotter finds issues, the retry loop handles revisions. The setter provides branch/PR context in feedback so leads pick up existing work rather than starting from scratch.

This may mean some PRs get created before cross-repo alignment is verified. That's intentional: **belayer optimizes for autonomy, not efficiency.** Redundant work is acceptable if it means the system can self-correct without human intervention.

### Why are setter and spotter opinionated rather than generic?

Generic fan-out/fan-in would be a more "pure" architecture, but belayer solves a specific problem: coordinating autonomous agents across multiple repos. Setter (decompose and distribute) and spotter (cross-repo validation) are the two coordination patterns that emerge from this problem.

Building a generic fan-out primitive would be over-engineering for a problem with one use case. Belayer is plumbing -- it provides the contracts and orchestration, not the implementations. But the plumbing itself has opinions about how multi-repo coordination should work.

### What's belayer's responsibility vs the node's responsibility?

Belayer connects known-working agents at the right time. If a node implementation is inflexible, can't handle retries, or produces poor output -- that's a node implementation problem, not an orchestration problem.

Belayer provides observability (loop counts, feedback content, scores) so humans can identify and improve underperforming nodes. The goal is continuous improvement through measurement, not architectural constraints that force correctness.

### Why does belayer not implement setter/spotter for you?

Belayer provides contracts: setter expects spec.md in and produces per-repo spec.md out. Spotter expects N commit hashes in and produces a gate score + feedback out. How you implement these contracts is up to you -- your decomposition logic, your validation criteria, your tools.

This keeps belayer agent-agnostic. Your nodes could be Claude Code sessions, Codex, OpenCode, custom scripts, or anything else that fulfills the contract.

## Boulderer (Concept -- Deferred)

Lightweight, one-off specialist agent for small tasks that don't warrant a full pipeline:
- **Use cases**: CI fixups, PR nitpick resolution, small unblocking tasks
- **Dispatch**: CLI (`belayer solo <task>`) + pipeline nodes can spawn one
- **Safety**: Max dispatch limits per problem to prevent over-reliance
- **Scope**: Operates on same branch as the problem, own worktree, single commit focus

Implementation deferred until lead retry demonstrably fails for these use cases.

## Deferred Items

| Item | Rationale | Priority |
|------|-----------|----------|
| Multi-repo fan-out/fan-in runtime | Temporal child workflows, significant work | P2 |
| Boulderer CLI + dispatch | Concept valid, wait for retry evidence | P3 |
| Summit operations (auto-merge, monitoring) | Interface defined, implementation later | P2 |
| Code restructuring (rename nodes, remove fields) | Separate plan after docs in place | P1 |
| `using-belayer` skill | Depends on stable CLI | P2 |
| Pipeline template marketplace | Requires adoption | P3 |
