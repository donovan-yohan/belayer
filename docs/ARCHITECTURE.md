# Architecture

This document describes the high-level architecture of belayer.

## Bird's Eye View

Belayer is a standalone Go CLI that orchestrates autonomous coding agents through declarative YAML pipelines. A pipeline defines nodes (constructive steps) and gates (adversarial quality checks) as `command:` entries. Belayer execs each command via ExecSpawner, polls for file-based completion, scores gate results, and routes to the next node. Temporal provides the workflow backbone. Framework scaffolding (`belayer setup --framework`) drops pipeline config + scripts into a target repo's `.belayer/` directory.

Input: spec.md (work specification), pipeline.yaml (orchestration definition).
Output: per-repo PRs, structured gate scores, JSONL event logs.

## Orchestration Layers

Belayer uses climbing metaphors and a three-phase model:

```
┌─────────────────────────────────────────────────────────────┐
│  EXPLORE                                                    │
│  belayer explore                                            │
│  intake sources (interactive, jira, github issues, ...)     │
│            │                                                │
│            ▼                                                │
│         spec.md                                             │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  CLIMB                                                      │
│  belayer climb                                              │
│                                                             │
│  [single-repo]                                              │
│    spec.md → Lead (plan→implement→review→pr) → PR           │
│                                                             │
│  [multi-repo — additive layers only]                        │
│    spec.md → Setter (fan-out) → per-repo spec.md            │
│                  │                                          │
│                  ▼                (per repo, in parallel)   │
│             Lead(repo-A)   Lead(repo-B)   Lead(repo-C)      │
│                  │                │               │         │
│                  ▼                ▼               ▼         │
│             commit hash    commit hash    commit hash       │
│                  └──────────────┬──────────────┘           │
│                                 ▼                           │
│                   Spotter (fan-in, gate scoring)            │
│                   N commit hashes → gate score + feedback   │
│                                 │                           │
│                           PASS / FAIL                       │
│                                 │                           │
│                                 ▼                           │
│                           PR manifest                       │
└─────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────────┐
│  SUMMIT  (not yet implemented)                              │
│  belayer summit                                             │
│  PR manifest → auto-merge, monitoring, observability        │
└─────────────────────────────────────────────────────────────┘
```

> Setter and Spotter are multi-repo only. Single-repo climbs run Lead directly.

## Named Roles

| Role | Scope | Contract |
|------|-------|----------|
| Setter | Multi-repo only | spec.md → per-repo spec.md |
| Spotter | Multi-repo only | N commit hashes → gate score + feedback |
| Lead | Per-repo | spec.md → commits + PR |
| Boulderer | One-off (deferred) | task → single commit |

## Code Map

| Module | Path | Purpose |
|--------|------|---------|
| CLI entry | `cmd/belayer/main.go` | Binary entry point |
| CLI commands | `internal/cli/` | Cobra command definitions (root, climb, node-complete, status, worker, start, setup) |
| Model | `internal/model/` | Domain types: NodeOutcome, CompletionResult, ClimbInput/Output |
| Pipeline | `internal/pipeline/` | YAML parser, validator, visualizer, pipeline config model |
| Gate | `internal/gate/` | Gate result parsing, weighted scoring, threshold routing, prompt builder |
| Route | `internal/route/` | Route result parsing/validation, route prompt builder for N-way agentic branching |
| Events | `internal/events/` | JSONL event logger for pipeline observability (node + gate events) |
| Outcome | `internal/outcome/` | Outcome detection: verdict.txt > output file first line > type default |
| Session | `internal/session/` | ExecSpawner (generic command exec), path helpers for `.belayer/.internal/`, SpawnOpts |
| Temporal | `internal/temporal/` | ClimbWorkflow, NodeActivity (spawn + heartbeat + poll completion + node-context.json) |
| Intake | `internal/intake/` | Intake adapter interface, SubmitSpec, bridge function, Jira adapter, schedule reconciliation |
| Plugins | `internal/plugins/` | Claude Code marketplace registration: writes to `~/.claude/plugins/` registry files during `belayer init`, Codex skill generation |
| Vendor | `internal/vendor/` | Agent CLI resolution: vendor registry, command builder, %{VAR} interpolation, gate schema generation |
| Frameworks | `frameworks/` | Built-in framework templates (embed.FS), Install/List/EnsureInternalDir |

## Data Flow

> **Note:** This diagram reflects the three-phase model.

```
EXPLORE: Intake sources → spec.md
                |
                v
CLIMB:  [single-repo]  spec.md → Lead (plan→implement→review→pr) → PR manifest
        [multi-repo]   spec.md → Setter (decompose, fan-out)
                                    |
                       +------------+------------+
                       v            v            v
                  Lead(repo-A) Lead(repo-B) Lead(repo-C)
                  (plan→impl   (plan→impl   (plan→impl
                   →review→pr)  →review→pr)  →review→pr)
                       |            |            |
                       v            v            v
                  commit hash  commit hash  commit hash
                       +------------+------------+
                                    |
                                    v
                         Spotter (fan-in, cross-repo gate)
                              |          |
                            PASS       FAIL → feedback → Setter
                              |
                              v
                         PR manifest
                              |
                              v
SUMMIT: PR manifest → auto-merge → monitoring → observability
```

## Pipeline Engine

Belayer uses an Activity-per-Node model. Each pipeline node is a Temporal Activity that spawns a command via ExecSpawner. File-based rendezvous (completion files) replaces Temporal Signals. YAML pipeline config with natural language node descriptions.

### Key Concepts

- **Three pipeline primitives**: Nodes (constructive — produce artifacts), Gates (adversarial — evaluate artifacts with multi-dimensional scoring), and Routers (agentic N-way branching — LLM picks one of N declared paths, each running as an isolated Temporal child workflow)
- **Agent nodes**: Pipeline nodes with `type: agent` + `vendor` + `prompt`. Belayer resolves the vendor to a CLI command (claude -p, codex exec) with structured output schema for gate nodes. No shell scripts needed.
- **Vendor resolution**: `internal/vendor/` maps vendor names to CLI invocations. Claude uses `--json-schema` (inline), Codex uses `--output-schema` (temp file). Gate nodes auto-generate JSON Schema from YAML dimensions. Router nodes auto-generate JSON Schema with enum constraint on route names.
- **Route nodes**: Nodes with a `routes:` field declare N-way agentic routing. The LLM picks from an enum of declared route names. Each route points to a subpipeline YAML in `.belayer/pipelines/`. The chosen route runs as a Temporal child workflow with its own retry counters and completion semantics. Route decisions produce a structured artifact (`route-result.json`) with choice, confidence, reasoning, and rejected alternatives.
- **%{VAR} interpolation**: Belayer variables use `%{INPUT}`, `%{WORK_DIR}` syntax to avoid clashing with shell `$VAR` and agent skill invocations like `$review`.
- **Activity Per Node**: Each pipeline node/gate = one Temporal Activity. Simplest model.
- **File-based completion**: Node commands write `.belayer/.internal/completion/<id>-<node>-attempt-<N>.json` when done (via `belayer node-complete` or directly)
- **Node protocol**: `NodeActivity` writes `.belayer/.internal/input/node-context.json` before spawning. The framework command reads it for context.
- **ExecSpawner**: Core spawner execs the `command:` field from pipeline YAML via `sh -c`. Returns an exit channel for fast-fail on process death. TmuxSpawner removed from core — now lives in the `claude-tmux` framework.
- **Framework model**: `belayer setup --framework <name-or-path>` scaffolds pipeline.yaml + scripts into `.belayer/`. Built-in frameworks embedded via `//go:embed`. Orchestration config is committed; runtime state is in `.belayer/.internal/` (gitignored).
- **Gate scoring**: Gates produce `gate-result.json` (structured scores) + `rationale.md` (human-readable). Activity computes weighted score from YAML-declared dimensions/weights and applies threshold routing (score-then-route anti-gaming)
- **Natural language roles**: Node descriptions are prompts in pipeline YAML, passed to the framework command via node-context.json
- **Attempt-scoped**: Completion files, output paths, and verdict files include attempt number to prevent stale reads
- **CLI entry point**: `belayer climb --file design.md` -> Temporal workflow -> plan -> implement -> review(gate) -> pr-author -> branch
- **Intake plugins**: `intake:` section in pipeline YAML defines where work comes from (interactive, jira). Each intake produces a `SubmitSpec` -> bridge creates worktree -> starts `ClimbWorkflow`
- **Worker daemon**: `belayer worker` runs Temporal worker + HTTP API for submit/status. `belayer start` opens interactive session connected via MCP channel
- **Belayer is plumbing**: Routes typed references (commit SHAs, file paths) between nodes. Nodes are black boxes.

## Config Hierarchy

```
~/.belayer/                        # global
  config.json                      # global settings
  crags/                           # multi-repo crag definitions

./.belayer/                        # repo-level (per-repo)
  pipeline.yaml                    # climb pipeline config
  .internal/                       # git-ignored state
```

Resolution: repo-level > global > embedded defaults

## Architecture Decision Records

> Normative constraints documented in `docs/adrs/`.

_To be populated._
