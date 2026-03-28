# Design

Patterns and conventions for the belayer codebase.

## Pipeline-as-YAML

Nodes define `command:` (what to exec), `description:` (what to do), routing (`on_pass`/`on_retry`/`on_fail`). Belayer execs the command via ExecSpawner, polls for completion, and routes to the next node based on outcome.

Two pipeline primitives:
- **Nodes** (constructive): Produce artifacts (code, specs, PRs)
- **Gates** (adversarial): Evaluate artifacts with multi-dimensional scoring. Produce `gate-result.json` + `rationale.md`

## Framework Model

`belayer setup --framework <name-or-path>` scaffolds pipeline + scripts into `.belayer/`. Orchestration config is committed; runtime state is in `.belayer/.internal/` (gitignored).

- Built-in frameworks embedded via `//go:embed` in `frameworks/`
- Core ships ExecSpawner (generic command exec); specific agent integrations live in frameworks
- `claude-tmux` is the reference framework for Claude Code sessions via tmux

## Node Protocol

Core writes `.belayer/.internal/input/node-context.json` before spawning. Framework commands read it for context. Commands write `.belayer/.internal/completion/<id>-<node>-attempt-<N>.json` when done (via `belayer node-complete` or directly).

## Score-then-Route

Gate nodes produce structured scores per dimension. Deterministic Go code computes weighted average. YAML thresholds route PASS/RETRY/FAIL. The rationale.md is mandatory as an anti-gaming measure -- no score without explanation.

## Validation Pipeline

Validation flows through four layers. See [review-loops-test-infra-design](design-docs/2026-03-16-review-loops-test-infra-design.md) for full design.

1. **Plan node**: Produces implementation plan from spec
2. **Implement node**: Executes the plan, produces code
3. **Review gate**: Multi-dimensional quality scoring with threshold routing
4. **PR-author node**: Creates pull request from completed work

## Setter/Spotter Contracts

Setter and spotter are first-class belayer concepts, not generic pipeline nodes. They are multi-repo only -- single-repo problems bypass both.

**Setter contract**: `spec.md` in -> per-repo `spec.md` out.

- Receives the top-level problem spec.
- Produces one `spec.md` per target repo, scoped to that repo's responsibilities.
- Belayer routes each per-repo spec to the appropriate lead as the climb input.

**Spotter contract**: N commit hashes in -> gate score + `feedback/rationale.md` out.

- Receives the final commit hashes from all leads after their climbs complete.
- Produces a numeric gate score and a rationale document covering cross-repo consistency.
- A failing spotter score blocks PR creation and re-dispatches affected leads with the rationale as feedback.

Belayer provides the contracts and orchestration. Users implement the nodes. This is what keeps belayer agent-agnostic -- the runtime inside a setter or spotter is not prescribed.

## PR Manifest

The PR manifest is the typed interface between the Climb and Summit phases. It is written after all leads complete and all spotters pass, and is consumed by the Summit phase to create and monitor pull requests.

```json
{
  "prs": [
    {
      "repo": "api",
      "url": "...",
      "number": 42,
      "branch": "...",
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

## Naming Convention

Climbing metaphors throughout:

| Name | Role |
|------|------|
| **Crag** | Long-lived workspace (repos, config) |
| **Problem** | Work item submitted by the user |
| **Climb** | Per-repo subtask derived from a problem |
| **Setter** | Multi-repo work distributor (multi-repo only) |
| **Belayer** | Orchestrator / pipeline runner |
| **Lead** | Implementation agent (per-repo) |
| **Spotter** | Multi-repo cross-repo validator (multi-repo only) |
| **Boulderer** | One-off specialist for small tasks (deferred) |

> The **setter** defines **problems** at the **crag**. The **belayer** sends **leads** up their **climbs**. When they **top** out, the **spotter** validates. If no retries were needed, it was **flashed**.

## Strategic Principles

1. **Belayer optimizes for autonomy, not efficiency** -- Redundant work is acceptable if it enables self-correction without human intervention.
2. **Multi-repo is additive, not transformative** -- The per-repo pipeline is unchanged; setter and spotter layer on top without altering what each lead does.
3. **Belayer is plumbing** -- Belayer provides contracts and orchestration, not node implementations. What runs inside a node is not belayer's concern.
4. **Agent-agnostic** -- Nodes are black boxes. Use whatever agent fulfills the contract: Claude, Codex, a shell script, or a future runtime. Core ships ExecSpawner (generic command exec); specific agent integrations live in frameworks.
5. **Orchestration is owned by the environment** -- Pipeline config and node scripts live in the target repo's `.belayer/` directory, not in belayer core. `belayer setup --framework` scaffolds the orchestration definition; users customize freely.
6. **Boring by default** -- Solve specific problems with opinionated plumbing. Don't over-abstract or generalize beyond the stated use case.

## Plugin Marketplace

The belayer repo doubles as a Claude Code marketplace. A `.claude-plugin/marketplace.json` at the repo root lists bundled plugins, and `plugins/` contains their source (markdown commands, agents, skills).

- **Bundled plugins**: `harness` (documentation + execution workflow) and `pr` (PR lifecycle management)
- **Auto-install**: `belayer init` registers the belayer GitHub repo as a marketplace in Claude Code's `~/.claude/plugins/` registry
- **Canonical source**: Belayer owns these plugins. Changes flow from here back to llm-agents, not the reverse.
- **Atomic writes**: Registry file updates use temp-file + rename to avoid corrupting Claude Code's plugin state on interrupt

## Why Belayer

| belayer | competitors |
|---------|-------------|
| Agent-agnostic orchestration | Model-locked agents |
| Multi-repo as additive layer | Multi-repo as agent feature |
| Pipeline-as-YAML | Hardcoded workflows |
| Three phases with typed contracts | Monolithic pipelines |
| You own your nodes | Platform owns your agents |

## See Also

- [Architecture](ARCHITECTURE.md) -- module boundaries and data flow
- [Quality](QUALITY.md) -- testing strategy
