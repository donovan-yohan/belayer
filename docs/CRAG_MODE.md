# Crag Mode

Crag mode is a thin layer over Belayer's existing climb-local control
plane. Its Belayer-native nouns are crags and climbs: a crag is the durable
operating context, and a climb is one execution run against that context.

The current implementation target is a walking skeleton:

- local team catalogs, grouped by category
- durable artifact content schemas
- cross-project crag knowledge under `~/.belayer/crags/<crag-name>/`
- direct `org:*` event types in the existing event log
- prompt guidance that works for both software delivery and story worlds

It is not a new sandbox boundary, not a remote talent marketplace, and not a
replacement for the Hermes plugin tool architecture.

## Why This Fits Belayer

Belayer already has the substrate an organization layer needs:

- agent identity directories under `agents/<name>/` and `.belayer/agents/<name>/`
- bridge-level `kind` and `ephemeral` mechanics for current Hermes spawns
- a Go daemon that owns roster, mail, events, artifacts, and completion state
- Hermes plugin tools that expose daemon-backed coordination functions to agents
- durable artifacts that PM, QA, reviewer, or custom gates can inspect

Crag mode names the higher-level contracts that currently live mostly
in prompts.

## Talent Identities

A talent identity is an addable agent identity plus optional metadata describing
when and how to use it. In v1, the portable identity contract remains the
existing directory shape:

```text
.belayer/agents/<name>/
├── agent.yaml
├── system-prompt.md
└── agents.md
```

Talent metadata currently lives beside the identity as `talent.yaml` to preserve
the existing artifact/schema vocabulary. The current daemon only needs the
existing identity fields at spawn time; metadata is for catalog browsing,
documentation, and future selection logic.

```yaml
schema_version: "belayer-talent/v1"
name: design-engineer
category: development
summary: Product/design talent for UX review and interaction design
role: "design engineer"
domain: "product"
capabilities:
  - interaction-design
  - ux-review
  - accessibility-review
activation:
  mode: on_demand
runtime:
  lifecycle: resumable
contract:
  accepts:
    - task
    - gate-request
  produces:
    - design-review-notes
  requires:
    - repo-workspace
authority:
  tools: []
  gates:
    - design-review
memory:
  scope: crag
retention:
  scope: crag
  promotion: proposed
default_gates:
  - design-review
```

`role` and `domain` are prompt-visible metadata, not framework enums. A
software crag can use roles like "CEO", "HR", "QA engineer", or "design
engineer"; a story crag can use "storyteller", "lorekeeper", or "tavernkeep".
Belayer core should route on the smaller generic contract:

- `activation.mode`: `climb_start`, `on_demand`, `gate_triggered`, or
  `generated`
- `runtime.lifecycle`: `resident`, `resumable`, or `ephemeral`
- `contract.accepts` / `contract.produces` / `contract.requires`
- `authority.tools` and `authority.gates`
- `memory.scope` and `retention`

The existing `agent.yaml#kind` and bridge `ephemeral` flag remain compatibility
mechanics for current Hermes spawns. New crag metadata should describe the
talent contract above and let the adapter derive bridge settings when possible.

## Hermes Profile Materialization

When a talent is spawned, the daemon automatically forks the base `blyr` Hermes
profile into a per-talent profile (`blyr-<crag>-<instance>/`) that symlinks shared
auth credentials and plugin state from the base. Operators run `belayer auth ensure`
once to set up the base profile; per-talent forks are managed entirely by the daemon.
Profiles with `memory.scope: crag` or `memory.scope: talent` are preserved across
climbs and skipped by `belayer prune`. See
`docs/design-docs/2026-05-03-belayer-hermes-profiles-spec.md` for the full
profile lifecycle design.

## Talent Lifecycle

Crag mode separates a talent definition from a running process:

```text
catalog talent
  -> assigned talent
  -> active employee
  -> checkpointed/dormant employee
  -> resumed or retired
```

`catalog talent` is metadata, prompt, and tool contract with no process.
`assigned talent` is selected for a task or gate but may not be spawned yet.
`active employee` has a running bridge/session. `dormant employee` has no live
process but remains addressable through its assignment, prior Hermes session ID,
artifacts, and mailbox cursor. `retired` talent is retained for audit history but
not selected by default.

Lifecycle names are runtime behavior, not company roles:

```text
resident
  Live for the climb. Idles and wakes in-process. Suitable for the lead.

resumable
  Durable assigned talent. Can shut down after work, then wake later with prior
  context when directly mailed or explicitly spawned.

ephemeral
  One-shot worker. No durable mailbox or wake contract after completion.
```

The framework should not know "CEO", "HR", "QA", or "design engineer" as
special cases. Those are roles/capabilities in talent metadata. The enforceable
parts are activation, lifecycle, authority, contract shape, and gate binding.

## Persistence Scopes

Crag mode has three filesystem scopes. Keeping them separate prevents
repo-specific context from leaking into global crag knowledge and prevents
global lessons from silently changing a project climb.

`docs/CRAG_FILESYSTEM.md` is the normative filesystem contract. This section
summarizes the model.

```text
repo/.belayer/
  Project-local team, config, overrides, climb artifacts, and explicit crag link.

~/.belayer/talent-catalog/<category>/
  Reusable local talent supply, grouped by category.

~/.belayer/crags/<crag-name>/
  Cross-project crag knowledge: teams, SOPs, gates, evaluations, and reviewed
  promotion history.
```

`repo/.belayer/` answers "what does this repo need?" It is the override layer
for a specific project. A project may link to one global crag, but it should not
receive global changes unless that link is explicit in `.belayer/config.yaml`.

`~/.belayer/talent-catalog/` answers "what teams are available to add?" It is
local-first supply, not a hosted marketplace. `belayer team add` copies selected
identities into `.belayer/agents/` so each project has an auditable runtime
team.

`~/.belayer/crags/<crag-name>/` answers "how does this operating context work
across projects?" It may hold reusable teams, SOPs, gate presets, and talent
performance history:

```text
~/.belayer/crags/software-company/
├── crag.yaml
├── teams/
├── sops/
├── gates/
├── evaluations/
└── promotions/
```

Story-world crags can use the same structure with domain-specific directories
for campaign state:

```text
~/.belayer/crags/story-world/
├── crag.yaml
├── teams/
├── sops/
├── gates/
├── evaluations/
├── promotions/
└── world-state/
```

The first implementation should document this contract before adding CLI
commands. Issue #113 owns the exact filesystem semantics and precedence rules.

## Catalog Categories

Catalogs are local-first. The shipped examples live in the repo, while installed
user catalogs should live under `~/.belayer/talent-catalog/`. Categories keep
unrelated prompts from polluting each other:

```text
examples/talent-catalog/
├── development/
│   ├── supervisor/
│   ├── backend-dev/
│   ├── web-dev/
│   ├── qa/
│   ├── reviewer/
│   └── pm/
└── story/
    ├── storyteller/
    ├── protagonist/
    ├── antagonist/
    ├── lorekeeper/
    └── continuity-editor/
```

```text
~/.belayer/talent-catalog/
├── development/
└── story/
```

The `team add` command copies one category, or one identity within a category,
into `.belayer/agents/`. It skips existing identities by default, supports
`--force`, and prints written/skipped counts. The `team remove` command removes
copied identities from the current project. Remote catalogs and cross-project
markets are intentionally out of scope until local catalogs prove useful.

## Execution Adapter Boundary

The execution adapter is the process loop that runs an identity and presents
Belayer tools to it. Today the default adapter is Hermes plus the Belayer Hermes
plugin.

The adapter does not own agent-to-agent communication. The tools it registers
call into the Belayer daemon:

```text
agent turn
  -> belayer_send_message / belayer_create_artifact / belayer_report_status
  -> Hermes plugin handler
  -> Belayer daemon over the session socket
  -> SQLite session bus, event log, artifact registry, roster, and gate state
```

This keeps all coordination observable and replayable in one daemon even if
future adapters run Codex, Claude Code, clamshell, scripts, or another loop.

## Gate Contract

QA, reviewer, and PM are software-company presets. The generic abstraction is a
gate: a scoped authority that inspects artifacts and produces a durable verdict.

```yaml
schema_version: "belayer-gate/v1"
name: runtime-qa
stage: task
authority: blocking
requires:
  - org-plan
  - implementation-notes
conditions:
  - "Implementation matches the task acceptance criteria"
  - "Runtime evidence proves the changed user path works"
output_artifact: gate-result
verdicts:
  - pass
  - pass-with-notes
  - fail
  - blocked
assigned_talent:
  - qa
```

Software-company gates can map to familiar artifacts:

- `qa-report` -> `gate-result` for runtime/user-path verification
- `review-report` -> `gate-result` for code or plan review
- `verification-report` -> `gate-result` for final acceptance

Story-world gates use the same contract with different content:

- `continuity-report` checks canon, tone, character consistency, and open hooks
- `world-state` records durable state after a scene or session

Do not hard-code QA/reviewer/PM into the framework. Treat them as default
talents and default gate presets in the `development` catalog.

Gate conditions should remain natural language. The structured fields answer
"when does this gate run?", "who has authority?", "which artifacts should be
available?", and "which verdicts are legal?" They should not become a policy
engine that tries to understand every repo, story world, or research workflow.
The gate talent interprets `conditions` against the actual evidence and records
that judgment in a `gate-result`.

## Minimal Climb Mapping

The pre-organization Belayer workflow is still the smallest valid climb:

```text
operator task
  -> supervisor main agent
  -> supervisor calls belayer_request_completion
  -> acceptance gate fires
  -> pm side agent verifies evidence
  -> pm approves or rejects
```

Under the gate model, this is not a separate legacy path. It is the built-in
`acceptance` gate preset:

```yaml
schema_version: "belayer-gate/v1"
name: acceptance
stage: session
authority: blocking
trigger: completion_requested
requires:
  - org-plan
  - gate-result
conditions:
  - "The org-plan acceptance criteria are satisfied"
  - "Required gate-result artifacts have passing or accepted verdicts"
assigned_talent:
  - pm
output_artifact: gate-result
verdicts:
  - pass
  - fail
  - blocked
```

Every climb must resolve at least one session-level acceptance gate. If a project
does not define gates, Belayer falls back to the built-in `acceptance` preset so
existing supervisor + PM climbs continue to work. A project or linked crag can
replace that preset with a stricter acceptance gate, but it cannot silently remove
acceptance. Bypassing acceptance should require an explicit operator action, not
an omitted config file.

Gate resolution should stay boring and inspectable:

1. Per-climb gate declarations in an `org-plan` or climb-start context.
2. Repo-local `.belayer/config.yaml` gate defaults or overrides.
3. Linked crag presets under `~/.belayer/crags/<name>/gates/`.
4. Shipped built-in `acceptance` preset.

This preserves predetermined climbs: an operator or supervisor can start from a
known gate set and run the climb against that contract. It also lets crags make
climbs form organically: the crag exposes available teams, SOPs, and gate
presets, then the supervisor chooses the relevant ones while creating `org-plan`.
Belayer provides the scaffolding, artifact registry, mail, and gate trigger; it
does not need to infer the whole workflow from task traits.

## E2R Loop

Crag mode uses the paper's Explore, Execute, Review loop in Belayer
terms:

1. Explore: a lead talent emits `org:task_planned` and registers an `org-plan`
   artifact.
2. Execute: assigned talents work through normal Belayer tools, mail, status,
   and artifacts.
3. Review: gate talents register `gate-result` artifacts and emit
   `org:task_reviewed`.
4. Retro: the lead registers `org-retro` to capture what to improve next climb.

Top-down decomposition and agent-to-agent communication are complementary.
Decomposition creates task addresses; communication resolves work inside those
addresses; artifacts and gates aggregate results back up.

```text
lead decomposes
  -> task id, owner, acceptance, expected outputs, gates
talents communicate
  -> questions, handoffs, blockers, review notes
talents publish
  -> artifacts, gate-results, final reports
lead aggregates
  -> task state, org-retro, talent-evaluation
```

Messages, artifacts, gate results, and talent evaluations should carry a
`task_id` when the interaction is about a planned task. Belayer does not need a
full workflow engine to enforce this on day one, but task binding keeps direct
agent communication from becoming invisible coordination fog.

Every meaningful agent conversation should advance a task, create or update an
artifact, unblock another talent, or trigger a gate. The auditable record is the
task-linked artifact and event trail, not the raw chat transcript alone.

## Talent Growth Loop

Talent growth is evidence-driven, not prompt self-editing. A climb can evaluate a
talent, propose a better SOP, or recommend a catalog update, but it must not
silently mutate global crag state.

```text
session evidence
  -> repo-local artifacts
  -> org-retro and talent-evaluation
  -> reviewed promotion proposal
  -> ~/.belayer/crags/<crag-name>/ knowledge
```

The `talent-evaluation` artifact captures per-climb performance for one talent:
assigned tasks, produced artifacts, gate outcomes, common findings, cost, and
recommended changes. The `org-retro` artifact captures climb-level lessons and
promotion proposals.

Promotion is a later explicit operation. A reviewed promotion may update:

- `~/.belayer/crags/<crag>/sops/` with reusable operating procedures
- `~/.belayer/crags/<crag>/gates/` with reviewed gate presets
- `~/.belayer/crags/<crag>/evaluations/` with summarized performance history
- `~/.belayer/talent-catalog/<category>/<talent>/talent.yaml` with maturity or
  capability changes

Talent maturity should be recorded as metadata, not inferred from a single climb:

```yaml
maturity: experimental | active | trusted | deprecated
evaluation:
  runs: 12
  pass_rate: 0.83
  common_failures:
    - misses mobile QA evidence
```

Agents may recommend changes. Operators or explicit promotion commands apply
them. This keeps cross-project learning auditable and reversible.

## Event Types

Use direct event types in the existing event log:

- `org:task_planned`
- `org:task_started`
- `org:task_reviewed`
- `org:talent_evaluated`
- `org:retro_recorded`

These are extension events, not a new table. Consumers should query them through
existing event APIs with `type_prefix=org:`.

## Artifact Kinds

Crag mode defines content schemas for these artifact kinds:

- `org-plan`
- `gate-result`
- `org-retro`
- `talent-evaluation`
- `world-state`
- `continuity-report`

The daemon stores artifact metadata only: `kind`, `path`, `producer`, and
`summary`. The schemas in [Artifact Schemas](ARTIFACT_SCHEMAS.md) describe the
file content agents should write before registering the artifact path.

## Proof Use Cases

### Software Company

The development catalog models a small software organization:

- lead: supervisor
- implementers: backend-dev, web-dev
- gates: reviewer, qa, pm

Success means a climb produces an `org-plan`, implementation artifacts, gate
results for review/QA/final acceptance, and an `org-retro` without requiring new
database tables.

### Story World

The story catalog models an interactive world:

- lead: storyteller
- talents: characters, factions, narrators, lorekeeper
- gates: continuity-editor, tone/editorial check, world-state updater

Success means a climb produces durable `world-state` and `continuity-report`
artifacts while using the same E2R loop and gate contract as the software case.
No software-specific gate name should be required.

## Out Of Scope For The First Proof

- remote talent markets
- runtime-enforced gate graphs
- new daemon database tables for organization state
- dashboard UI for task trees
- Docker/VM/container isolation as an organization primitive
- automatic prompt mutation or self-modifying catalogs
