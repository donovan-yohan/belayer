# Talent Lifecycle Contract Plan

## Goal

Issue #119 needs Belayer to distinguish talent availability from running bridge
processes. This plan formalizes the revised contract discussed for OMC-style
crags: top-down task decomposition creates task addresses, direct
agent-to-agent communication resolves work inside those addresses, and
task-linked artifacts/gates aggregate the results.

## Scope

This PR is the docs/schema/proof-example step. It does not implement daemon
wake-on-mail yet. It defines the contract that runtime work should implement
next:

- talent metadata separates `role` and `domain` from runtime behavior
- `activation.mode` describes when a talent is brought into a climb
- `runtime.lifecycle` describes whether a talent is resident, resumable, or
  ephemeral
- `contract.accepts`, `contract.produces`, and `contract.requires` describe the
  typed organizational interface without hard-coding software roles
- `assignment` records why a talent was selected, where it came from, and which
  tasks it owns
- task nodes carry expected outputs, gate bindings, and attempt budgets
- talent evaluations record whether talent was available, assigned, running,
  dormant, resumed, newly spawned, or unavailable

## Implementation Tasks

1. Update `docs/CRAG_MODE.md` to make `belayer-talent/v1` the public contract
   and demote `kind`/`ephemeral` to bridge compatibility mechanics.
2. Update `docs/AGENT_ARCHITECTURE.md` to explain how current main/side bridge
   mechanics map to future `resident`, `resumable`, and `ephemeral` lifecycle
   behavior.
3. Extend `docs/artifact-schemas/org-plan.schema.json` with top-level
   `assignments` and task-level `expected_outputs`, `gates`, and `attempts`.
4. Extend `docs/artifact-schemas/talent-evaluation.schema.json` with assignment
   lifecycle evidence.
5. Update the software-company and story-world proof org-plans so assignments,
   tasks, and gates exercise the new contract.
6. Update story catalog and generated-talent YAML so talent metadata uses
   `role`, `domain`, `activation`, `runtime`, `contract`, `memory`, and
   `retention`.
7. Add agentlint regression tests that fail if proof examples omit lifecycle
   assignments, task output/gate bindings, or evaluation lifecycle evidence.

## Runtime Follow-Up

The daemon follow-up should introduce assigned/dormant state as a runtime-owned
concept:

```text
assigned talent != running process
dormant resumable talent + direct mail -> spawn with prior Hermes session
broadcast -> resident/live recipients only by default
```

The smallest runtime change should preserve existing supervisor + PM climbs,
reuse `agent_runs.hermes_session_id`, and avoid a global scheduler or remote
talent marketplace.

## Validation

Required checks for this PR:

```bash
jq empty docs/artifact-schemas/org-plan.schema.json docs/artifact-schemas/talent-evaluation.schema.json examples/org-proofs/relay-ide-software-company/org-plan.json examples/org-proofs/athenaeum-tavern-story/org-plan.json examples/org-proofs/relay-ide-software-company/talent-evaluation-backend-dev.json examples/org-proofs/athenaeum-tavern-story/talent-evaluation-storyteller.json examples/org-proofs/athenaeum-tavern-story/talent-evaluation-tavernkeep-mara.json
go test ./internal/agentlint
go test ./...
git diff --check
```

