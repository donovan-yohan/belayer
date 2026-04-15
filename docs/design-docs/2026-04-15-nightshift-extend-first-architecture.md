---
status: proposed
created: 2026-04-15
supersedes:
implemented-by:
consulted-learnings:
  - docs/PHILOSOPHY.md
  - docs/ARCHITECTURE.md
  - docs/design-docs/2026-04-09-sandbox-runtime-architecture-design.md
  - ~/Documents/Programs/work/CLAUDE.md
  - ~/Documents/Programs/work/extend-localenv/README.md
  - ~/Documents/Programs/work/extend-localenv/docs/specs/2026-04-08-rename-to-extend-localenv-design.md
  - ~/Documents/Programs/work/extend-clamshell/README.md
  - ~/Documents/Programs/work/extend-clamshell/docs/clamshell.md
---

# Nightshift Architecture for Extend-First Execution

Belayer should first become a working overnight engineering system for Extend, then abstract the stable pattern. This document updates the current Belayer architecture with that bias.

## Why this design exists

The earlier Belayer direction was conceptually strong but too easy to fizzle out in implementation because too many things were still hypothetical at once:

- a generic multi-agent runtime
- a generic sandbox runtime
- a generic workbench layer
- generic agent identities tied to local vendor CLIs
- generic YAML workflow definitions

That stack left too much surface area under active invention.

The Extend environment gives us a much sharper target:

- `extend-api` and `extend-app` are the primary fullstack repos
- `extend-localenv` already owns important local environment setup/teardown behavior via `xt`
- `extend-clamshell` already owns the security boundary for single-host sandboxing
- Hermes provides a persistent agent harness with identities, memory, skills, delegation, and remote continuity that do **not** need to live only on the client machine

The goal is to stop designing a universal theory of agent orchestration and instead build a concrete, trustworthy Nightshift system for Extend.

---

## Design thesis

Nightshift should be:

> a **Belayer-style control plane** that orchestrates **persistent Hermes specialist identities** running inside **extend-clamshell sandboxes**, using **extend-localenv** as the workbench/environment substrate for Extend-specific bring-up, teardown, and verification.

This keeps the strongest parts of Belayer:

- session as the root primitive
- orchestration as LLM judgment inside explicit interfaces
- sandbox as a real trust boundary
- communication and tools as runtime services, not prompt tricks
- memory and identity as durable assets

But it changes a critical implementation assumption:

> the durable agent identity should no longer be assumed to live only inside a local vendor CLI process on the same machine as the runtime.

Hermes is useful precisely because the identity layer can persist separately from any one invoked run.

---

## What remains correct in current Belayer

The following ideas should stay intact.

### 1. The six interfaces

Belayer's six interfaces are still the right abstraction boundary:

- Session
- Orchestration
- Sandbox
- Communication
- Memory
- Tools

These interfaces are useful because they say **what the runtime must provide** without over-constraining how the LLM coordinates within those surfaces.

### 2. Runtime handles plumbing, agents handle judgment

This remains the most important design rule.

The runtime should explicitly own:

- session persistence
- state transitions
- event logging
- policy enforcement
- message delivery
- tool routing
- sandbox provisioning
- artifact storage

The agents should own:

- decomposition
- prioritization
- task routing
- asking for help from other specialists
- deciding which tools are relevant
- deciding when a task is done or needs rework

### 3. Avoid rigid workflow YAML

Belayer was right to be suspicious of explicit workflow graphs pretending to know how all future work should proceed.

Declarative configuration is still desirable for:

- team rosters
- sandbox policies
- tool registries
- artifact schemas
- environment templates
- review gates
- escalation thresholds

It should **not** try to fully encode:

- orchestration logic
- decision trees
- agent sequencing for every task class
- exhaustive review loop choreography

### 4. Sandbox is a hard boundary

This is even more true now than before. Overnight autonomy without a hard sandbox is not credible.

---

## What must change from current Belayer

Current Belayer still reflects several assumptions that do not fit the Extend Nightshift target well enough.

### A. Replace the "bring your own local pilot process" default assumption

Current Belayer centers local vendor CLIs (`claude`, `codex`, generic) as if the identity and the execution process are effectively the same thing.

For Nightshift, we need a separation between:

- **identity** — long-lived planner/specialist memory, skills, persona, role ownership
- **invocation** — one concrete overnight run inside one sandbox for one task or session

This is where Hermes is materially different from pure client-side Claude Code or Codex assumptions.

**Required change:**
Belayer should treat the agent runtime as a pluggable execution backend with at least two classes:

1. **local CLI backends** — Claude Code, Codex, etc.
2. **persistent harness backends** — Hermes profiles/identities whose durable memory may be hosted outside the individual sandbox run

This is not just a vendor adapter change; it affects memory, wake/resume, and how agent directories are treated.

### B. Stop treating workbench provisioning as a Belayer-invented abstraction first

Belayer currently imagines workbench provisioning in generic terms. For Extend, that is too vague.

`extend-localenv` already gives us a concrete operational substrate:

- `xt up`
- `xt down`
- `xt status`
- `xt doctor`
- `xt token`

It also already defines a shell-native localenv direction and a workspace-root model that is much closer to the real Extend working environment than Belayer's generic compose sketches.

**Required change:**
For Extend-first Nightshift, Belayer should model the workbench as:

- an Extend environment managed primarily through `xt`
- plus any repo-local startup flows that remain outside `xt` today
- plus verification commands for `extend-api` and `extend-app`

Belayer should not try to out-generalize `extend-localenv` before integrating with it.

### C. Treat extend-clamshell as the actual runtime boundary, not just an interchangeable sandbox candidate

Belayer already points at clamshell, but the current framing is still a little abstract.

From the current `extend-clamshell` docs, the important reality is:

- it is single-host
- it has deny-by-default egress
- it uses host-owned providers/credentials
- it exposes operator workflows and runtime inspection already
- it is honest about where Linux vs macOS guarantees differ
- it supports sandbox refresh flows for same-network peers

**Required change:**
For Extend-first Nightshift, clamshell should be treated as the primary supported sandbox runtime, not as one backend among many equal ones.

Local mode can remain for development. Docker mode should be demoted further.

### D. Move from PR-loop-centric orchestration to artifact-plus-PR orchestration

The current Belayer collaboration model is very PR-centric:

- implementer writes code
- implementer creates PR
- reviewer reviews PR
- pilot loops

That is useful but incomplete for Nightshift.

For multi-repo fullstack work, the real coordination artifacts must include:

- task graph
- per-specialist assignment
- shared contract or interface doc
- assumptions taken
- evidence bundle
- local validation results
- blocker/replan events

**Required change:**
Belayer should make PRs an output artifact, not the only serious coordination artifact.

### E. Make the planner's session externalizable and inspectable

The pilot currently feels like an in-session actor inside the same local runtime topology as everyone else.

For Nightshift, the planner/orchestrator needs stronger control-plane semantics:

- can wake or restart specialists
- can spawn or reassign work
- can inspect session state without attaching to a tmux pane
- can compare artifacts across repos
- can decide downgrade paths (draft PR only, stop-and-report, retry)

**Required change:**
The planner should be represented as a first-class control-plane role with explicit APIs and artifacts, not just as another peer in the same prompt-level cluster.

---

## Extend-first component model

### 1. Belayer / Nightshift control plane

This remains the root system.

It owns:

- session lifecycle
- event log
- run state machine
- specialist roster
- orchestration prompts/context assembly
- typed message and artifact transport
- review gates
- morning handoff assembly
- cost/time/autonomy policy

It should not try to own:

- sandbox internals
- Extend environment startup internals
- long-term specialist personality implementation details

### 2. Hermes as specialist execution harness

Hermes should be the first-class runtime backend for Nightshift specialists.

Why:

- persistent identities and profiles
- durable memory and skills
- delegation/subagents
- practical tool use
- dedicated-server deployment model
- ability to separate the identity layer from a single local client process

In practice, this means Nightshift launches **Hermes-backed specialists**, not just raw vendor CLIs.

Examples:

- `planner`
- `extend-api-specialist`
- `extend-app-specialist`
- `qa-specialist`
- `reviewer`
- later: `localenv-specialist`, `security-reviewer`, `docs-specialist`

### 3. extend-clamshell as sandbox substrate

Clamshell owns:

- deny-by-default egress
- provider attachment
- opaque credential handles
- runtime inspectability
- sandbox operator flows
- audit logs
- binary/process identity enforcement on managed paths

For Extend-first Nightshift, the strongest supported deployment target should be:

- **Linux host with local Docker Engine**

macOS remains valuable for development and experimentation, but not for the strongest overnight trust claims.

### 4. extend-localenv as workbench substrate

`xt` should become the main Extend workbench interface.

Nightshift should call explicit Extend-localenv capabilities such as:

- `xt doctor`
- `xt up --project ...`
- `xt down`
- `xt status`
- `xt token ...`

And then combine those with repo-native commands where necessary.

This avoids inventing a second generic environment model before the Extend one is working.

---

## Current-state to target-state mapping

| Concern | Current Belayer | Extend-first target |
|---|---|---|
| Agent runtime | local vendor CLI assumption | Hermes-backed specialists as primary runtime backend |
| Identity persistence | agent directory near local harness | persistent Hermes identities separate from ephemeral runs |
| Sandbox | clamshell-compatible design | clamshell as primary supported runtime boundary |
| Workbench | generic compose/workbench idea | `extend-localenv` first, repo-native commands second |
| Coordination | mostly message + PR loop | artifact-driven coordination with PRs as outputs |
| Planner | peer agent inside session | explicit control-plane planner role |
| Multi-repo support | climb-fullstack template | Extend-specific app/api orchestration first |
| Abstraction strategy | generic runtime first | Extend-first implementation, abstraction later |

---

## Explicit vs inferred in Nightshift

This needs to be painfully clear.

### Must be explicit

These should be declared in config, policy, or code:

- supported repos (`extend-api`, `extend-app` initially)
- supported specialist roles
- sandbox policy and provider attachments
- environment bring-up/teardown commands
- artifact schema
- session phases and terminal states
- review gates
- escalation thresholds
- branch and PR policy
- allowed network destinations
- which actions are forbidden overnight

### Should be explicit but agent-maintained

These should live in skills, role memory, or versioned knowledge files:

- repo conventions
- local runbooks
- validation playbooks
- common drift patterns
- role-specific review heuristics

### Should be inferred by LLM judgment

- task decomposition
- which specialist to invoke
- task sequencing within a phase
- when to request help from another specialist
- which tools to call first
- when to replan vs retry
- how to summarize findings

### Must not be left to inference

- sandbox escape decisions
- credential scope
- internet access policy
- production or deploy operations
- protected branch policy
- review-gate bypass
- whether a destructive action is permitted

---

## Session model for Extend-first Nightshift

Belayer's existing session concept is still correct, but Nightshift needs a richer session contract.

### Session types

#### 1. Ticket session

Input:

- reviewed Jira ticket or equivalent spec

Output:

- one or more draft PRs
- evidence bundle
- morning summary

#### 2. Fullstack ticket session

Input:

- ticket requiring app + api coordination

Output:

- coordinated repo tasks
- shared contract artifact
- app/api PRs
- integration validation artifact

#### 3. Planner-only session

Input:

- risky or ambiguous ticket

Output:

- execution plan
- repo impact analysis
- autonomy recommendation
- no implementation unless promoted

### Session phases

For Extend-first v1, use these phases:

1. **intake** — classify ticket, determine eligibility, identify repos
2. **plan** — build task graph, shared contracts, specialist assignments
3. **implement** — specialists work in parallel or sequence
4. **verify** — localenv bring-up, checks, screenshots/logs/tests
5. **review** — reviewer plus planner gate
6. **handoff** — PR creation/update, evidence bundle, summary

This is enough structure to keep the system intelligible without hardcoding fine-grained workflow logic.

---

## Specialist model for Extend-first v1

Do not begin with too many roles.

### Required v1 roles

#### Planner

Responsibilities:

- intake classification
- task graph creation
- specialist routing
- shared contract definition
- progress monitoring
- replan/escalation
- handoff assembly

#### extend-api specialist

Responsibilities:

- backend changes in `extend-api`
- repo-local validation
- API contract artifact generation
- commit/branch/PR creation within allowed policy

#### extend-app specialist

Responsibilities:

- frontend changes in `extend-app`
- repo-local validation
- UI contract consumption
- commit/branch/PR creation within allowed policy

#### QA specialist

Responsibilities:

- `xt`-based environment bring-up
- end-to-end validation
- integration drift detection
- evidence capture

#### Reviewer

Responsibilities:

- code review independent of implementers
- requirements compliance review
- risk summary

### Roles that should wait

- docs specialist
- infra specialist
- security specialist
- data migration specialist

Add them only after the core loop works.

---

## Artifact model

This is the largest architectural shift from the current Belayer doc set.

Nightshift should coordinate primarily through artifacts recorded in the session store.

### Core artifacts

#### `ticket-intake.json`

Contains:

- ticket ID
- normalized requirements
- repos implicated
- risk class
- autonomy eligibility
- acceptance criteria

#### `task-graph.json`

Contains:

- tasks
- owners
- dependencies
- required artifacts
- completion criteria

#### `shared-contract.md`

Contains:

- endpoint/schema/interface changes
- UI expectations
- known assumptions

#### `specialist-report-<role>.md`

Contains:

- work completed
- commands run
- assumptions taken
- blockers encountered
- tests run

#### `verification-report.md`

Contains:

- `xt` environment status
- local validation commands
- app/api health checks
- screenshots/logs if available
- remaining failures or warnings

#### `handoff.md`

Contains:

- PR links
- what changed
- what passed
- what still needs human attention
- confidence/risk notes

### Why this matters

Artifacts make the planner's job inspectable and make morning review much more trustworthy than raw transcript archaeology.

---

## Communication model

Belayer's message broker remains useful, but messages should no longer carry all important state.

### Typed communication events

Nightshift should support typed events such as:

- `task_assigned`
- `artifact_ready`
- `needs_contract_update`
- `verification_failed`
- `replan_requested`
- `blocked`
- `ready_for_review`
- `handoff_ready`

Messages can still contain natural language, but the runtime should record a typed envelope so orchestration is inspectable.

---

## extend-localenv integration requirements

This is one of the biggest concrete implementation deltas.

### Current observation

`extend-localenv` already provides a lightweight shell CLI for local Extend environment operations and is already on this machine's path. `xt doctor` currently passes locally.

### Required Belayer/Nightshift changes

#### 1. Introduce an Extend workbench adapter

Belayer should add an Extend-specific workbench adapter that wraps `xt` rather than generic compose first.

Suggested surface:

- `workbench.prepare` → `xt doctor`
- `workbench.up(projects...)` → `xt up --project ...` or defined project sequence
- `workbench.status` → `xt status`
- `workbench.token(email)` → `xt token <email>`
- `workbench.down` → `xt down`

#### 2. Separate host-side environment bring-up from sandboxed coding

The coding agent should stay in clamshell. The localenv bring-up may need to execute on the host/operator side under explicit tool routing.

This fits Belayer's tool-routing philosophy well.

#### 3. Store Extend environment knowledge as explicit skills/runbooks

Do not make the planner rediscover:

- when `xt up` is sufficient
- when repo-local auth setup is still required
- which project combinations matter
- which health checks correspond to a usable environment

Capture that in explicit Extend skills.

---

## extend-clamshell integration requirements

### Current observation

`clamshell` is already installed locally. `clamshell doctor` succeeds with warnings that are informative:

- gateway not currently running
- macOS is compatibility mode, not Linux reference platform
- filesystem enforcement is weaker than native Linux

This is exactly the kind of truthful operational posture Nightshift should preserve.

### Required Belayer/Nightshift changes

#### 1. Make clamshell the primary supported runtime backend

Belayer should stop presenting local/docker/clamshell as co-equal for Nightshift production goals.

Target support statement:

- **production Nightshift**: Linux + clamshell
- **development and local iteration**: macOS + clamshell compatibility mode or local runtime

#### 2. Encode Extend-specific policies explicitly

Belayer needs explicit clamshell policy packages for Extend workloads, including:

- inference provider access
- GitHub provider attachment
- any required npm/GitHub/API egress
- carefully scoped same-network direct-TCP exceptions only when necessary

#### 3. Add runtime inspection into verification artifacts

Planner and QA flows should capture:

- `clamshell runtime inspect <sandbox>`
- `clamshell doctor --sandbox <sandbox>` where useful
- deny-event summaries when relevant

#### 4. Avoid pretending direct-TCP exceptions are free

The clamshell docs are clear that direct TCP weakens the original broker/proxy trust model.

Nightshift should therefore treat direct-TCP exceptions as:

- exceptional
- explicit in policy
- recorded in artifacts
- included in risk summaries

---

## Hermes integration requirements

This is the major new architectural addition.

### Why Hermes matters here

Hermes changes the shape of the system because:

- agent identity can persist independently of a single run
- specialists can accumulate memory and skills over time
- profiles cleanly model specialist roles
- the harness already supports tool use, delegation, memory, and cross-session continuity

That is materially different from Belayer's earlier assumption that the runtime mainly launches client-side Claude Code or Codex sessions and that the identity is effectively local to that harness.

### Required Belayer/Nightshift changes

#### 1. Add a Hermes runtime adapter

Belayer should be able to launch a Hermes-backed specialist with:

- a selected profile/identity
- a sandbox workspace
- a task payload
- environment-scoped tools
- session metadata

#### 2. Distinguish identity store from run workspace

Belayer should represent:

- **identity state** — profile, memory, skills, persona
- **run state** — workspace path, branch, artifacts, event stream, verification outputs

These must not be conflated.

#### 3. Keep explicit Belayer session/event state even if Hermes has its own session model

Hermes sessions are useful, but Nightshift still needs Belayer-owned session truth for:

- orchestration state
- cross-specialist coordination
- artifacts
- morning handoff
- audit of the overall run

Hermes should not replace the Nightshift control plane.

#### 4. Use Hermes memory carefully

For v1:

- use Hermes memory and skills for role expertise and stable Extend conventions
- do **not** use it as the primary storage for orchestration state or evidence

Belayer session artifacts remain the system of record for the overnight run.

---

## Extend-first MVP scope

To avoid another abstraction stall, the first real target should be narrow.

### Supported repos

- `extend-api`
- `extend-app`

### Supported task classes

- bounded tickets with human-reviewed specs
- one-repo backend changes
- one-repo frontend changes
- selected app+api tickets with clear shared contracts

### Unsupported initially

- arbitrary repo graphs
- infra-heavy tickets
- production deploys
- auto-merge
- broad epics with unclear boundaries
- partner/integrator repos

### Definition of success

Nightshift should be considered working when it can reliably:

1. ingest a reviewed Extend ticket
2. classify app/api impact
3. spawn planner + specialists
4. make code changes in the correct repo(s)
5. use `xt` and repo-native validation to verify locally
6. create draft PRs with evidence
7. produce a morning handoff a human can trust

---

## Recommended implementation order

### Phase 1 — Extend-first control plane cleanup

- update Belayer docs and naming around Extend-first scope
- add explicit session phases and artifact schema
- demote generic workflow language in favor of artifact-driven orchestration

### Phase 2 — Hermes runtime backend

- add Hermes-backed specialist launcher
- separate identity store from run workspace
- model planner/api/app/reviewer/qa as explicit Nightshift roles

### Phase 3 — extend-localenv adapter

- add host-side workbench tool routing around `xt`
- codify Extend validation playbooks
- emit verification artifacts

### Phase 4 — extend-clamshell productionization

- define Extend sandbox policies
- add runtime inspection capture
- standardize Linux-host production deployment

### Phase 5 — morning handoff and human review ergonomics

- artifact summary generation
- PR linking
- risk summary
- failure categorization

Only after those phases are working should Belayer try to abstract the pattern back outward.

---

## Final recommendation

Belayer should stop trying to prove a universal agent runtime first.

It should instead become:

> **the Extend Nightshift control plane**
> orchestrating **Hermes-backed specialist identities**
> inside **extend-clamshell sandboxes**
> using **extend-localenv** for Extend environment operations.

That is concrete enough to implement, narrow enough to finish, and still aligned with the deeper Belayer philosophy.
