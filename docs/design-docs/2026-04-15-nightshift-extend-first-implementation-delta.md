---
status: proposed
created: 2026-04-15
supersedes:
implemented-by:
consulted-learnings:
  - docs/PHILOSOPHY.md
  - docs/ARCHITECTURE.md
  - docs/design-docs/2026-04-09-sandbox-runtime-architecture-design.md
  - docs/design-docs/2026-04-15-nightshift-extend-first-architecture.md
  - ~/Documents/Programs/work/extend-localenv/README.md
  - ~/Documents/Programs/work/extend-clamshell/README.md
---

# Belayer → Nightshift for Extend: Implementation Delta

This document turns the Extend-first Nightshift architecture into concrete engineering changes against the current Belayer codebase.

The goal is not to preserve every abstraction Belayer currently has. The goal is to get a trustworthy Extend-first Nightshift loop working. If that means deleting speculative layers, we should delete them.

---

## Executive summary

Belayer currently has three different stories mixed together:

1. **a solid control-plane idea** — sessions, events, broker, tools, daemon
2. **a speculative generic runtime stack** — local, docker, clamshell, vendor adapters, generic environment configs
3. **a still-hypothetical workbench/orchestration model** — generic compose workbench, generic templates, generic vendor assumptions

For Extend-first Nightshift, the recommendation is:

- **keep** Belayer's session/event/control-plane core
- **keep and strengthen** clamshell integration
- **replace generic workbench provisioning with an Extend-localenv adapter**
- **replace local vendor CLI assumptions with a Hermes runtime adapter**
- **shrink or delete** generic Docker runtime and generic vendor abstraction code where it no longer pays for itself

This is not just a refactor. It is a scope correction.

---

## What the current codebase suggests

Approximate internal package weight today:

- `internal/cli`: 24 files / 4742 lines
- `internal/docker`: 9 files / 3062 lines
- `internal/daemon`: 5 files / 1975 lines
- `internal/agent`: 10 files / 1461 lines
- `internal/store`: 3 files / 804 lines
- `internal/broker`: 4 files / 694 lines
- `internal/vendor`: 6 files / 623 lines
- `internal/workspace`: 2 files / 529 lines
- `internal/session`: 2 files / 472 lines
- `internal/clamshell`: 2 files / 137 lines
- `internal/runtime`: 2 files / 70 lines

The biggest immediate smell is that the most speculative code is also some of the heaviest:

- `internal/docker` is large and central
- `internal/cli/session_start.go` is very large and is carrying too many concerns
- `internal/vendor` exists largely to adapt local CLI harnesses that are no longer the primary target
- `internal/runtime` is a thin selector over local/docker/clamshell modes, but the meaningful implementation complexity lives elsewhere

That supports your instinct that a lot of code can probably go away.

---

## The target system boundary

Nightshift for Extend should have four clear layers.

### 1. Belayer/Nightshift control plane

Owns:

- sessions
- events
- artifacts
- typed orchestration state
- message routing
- work dispatch
- review gates
- morning handoff

### 2. Hermes specialist runtime

Owns:

- specialist identities
- role memory
- skills
- tool-using task execution
- optional sub-delegation inside a specialist run

### 3. extend-clamshell sandbox

Owns:

- sandbox creation
- host-owned credentials
- network policy
- runtime inspection
- auditability

### 4. extend-localenv workbench

Owns:

- Extend local environment bring-up
- teardown
- status/health checks
- token generation
- concrete local dev topology

This means Belayer should become thinner and more intentional.

---

## What to keep mostly as-is

These parts are still strategically correct.

### Keep: `internal/store`

Why keep it:

- sessions and events remain the root primitive
- SQLite is fine for v1
- workbench/session state persistence is useful

What changes:

- add artifact tables and typed run state
- add specialist run records
- add explicit session phase and outcome fields
- stop treating message history and event log as the only coordination record

### Keep: `internal/daemon`

Why keep it:

- daemon is the natural control plane
- HTTP/Unix socket API is appropriate
- this is the right place for orchestration state and work dispatch

What changes:

- add artifact endpoints
- add run dispatch endpoints oriented around specialists
- add planner-visible orchestration APIs
- reduce daemon ownership of generic compose/workbench machinery

### Keep: `internal/broker`

Why keep it:

- message delivery still matters
- debounce/coalescing can still help
- the abstraction is useful even if message content becomes more typed

What changes:

- messages become one coordination primitive among several, not the only one
- introduce typed event envelopes like `task_assigned`, `artifact_ready`, `blocked`, `ready_for_review`

### Keep: `internal/clamshell`

Why keep it:

- tiny, useful package
- directly aligned with production target

What changes:

- expand it from a tiny wrapper to the primary sandbox backend integration
- add inspect/doctor helper calls and provider attachment support if needed

---

## What to modify heavily

### 1. `internal/cli/session_start.go`

Current problem:

- it is carrying too much runtime-specific orchestration
- it branches across local/docker/clamshell
- it contains launch flow, worktree creation, tool registration, environment loading, runtime selection, summary printing, and attach logic in one huge file

This file is the clearest signal that Belayer's runtime abstractions are too leaky.

#### Proposed change

Split into a much smaller orchestration entrypoint that does only:

1. resolve workspace and config
2. create session
3. ask a runtime launcher to start the named roles
4. print status/attach instructions

Move the rest into:

- `internal/nightshift/launcher.go`
- `internal/nightshift/hermes_runtime.go`
- `internal/nightshift/clamshell_runtime.go`
- `internal/nightshift/localenv_adapter.go`

#### Result

- much less CLI complexity
- runtime logic becomes testable in packages with narrower responsibilities
- session start stops being a graveyard of every experiment Belayer has tried

### 2. `internal/session/template.go`

Current problem:

- built-in templates still reflect intake/implement/deliver and generic vendor choices
- agent specs still encode `Vendor` and `Model` directly as if the execution runtime is a local CLI concern
- built-ins (`explorer`, `merger`, etc.) are broader and more generic than Extend-first needs

#### Proposed change

Replace the current built-in template assumptions with Extend-first roles:

- `planner`
- `extend-api-specialist`
- `extend-app-specialist`
- `qa`
- `reviewer`

Change `AgentSpec` to distinguish:

- **role identity**
- **runtime backend** (`hermes`, optional legacy `vendor-cli`)
- **profile/identity name**
- **repo binding**

Not:

- vendor CLI name as the core abstraction

#### Result

The template layer becomes about the team and role contract, not about which local binary happens to run.

### 3. `internal/daemon/tools.go` and `internal/agent/executor.go`

Current problem:

- tool execution assumes `agent`, `workbench`, `infra`, `host` routed partly through docker-compose execution
- this is still very compose-centric

#### Proposed change

Evolve the execution targets to something like:

- `host`
- `sandbox`
- `localenv`
- `session`

Where:

- `sandbox` means “inside the specialist's clamshell/Hermes run context”
- `localenv` means “host-side Extend environment operations via `xt` and related host commands”
- `session` means “Belayer-owned orchestration/meta tools”

This does **not** need to be solved by growing the executor into a larger generic shell DSL. It should become simpler and more intentional.

---

## What to de-emphasize or delete

This is the part where code removal is likely justified.

### A. De-emphasize and likely delete most of `internal/docker`

Current state:

- `internal/docker` contains environment loading, generic compose workbench logic, generic sandbox logic, runtime metadata, compose generation
- it is one of the largest packages in the codebase
- much of its value is tied to generic workbench/compose abstractions

Why it should shrink:

- Extend-first workbench should go through `extend-localenv`, not Belayer-generated compose first
- clamshell is the real sandbox boundary, so Belayer should not also maintain a parallel generic sandbox/runtime story unless needed
- the generic environment model is overbuilt for the current target

#### Suggested keep/remove breakdown

**Keep temporarily:**

- any environment config bits needed to load repo metadata during migration
- any tests that protect session/worktree assumptions while refactoring

**Plan to remove or retire:**

- generic compose workbench generation in `internal/docker/workbench.go`
- generic sandbox/compose coupling in `internal/docker/sandbox.go`
- runtime metadata and compose output paths that only exist for Docker mode
- generic environment/config fields that only serve compose-based workbench generation

#### Replacement

Introduce:

- `internal/nightshift/extendenv` or `internal/localenv`
- explicit `xt` adapter functions
- explicit Extend repo topology config

#### Why this is good

You could realistically remove a large amount of speculative code here and replace it with a much smaller, more truthful integration.

### B. De-emphasize `internal/vendor`

Current state:

- Belayer has adapters for Claude, Codex, and a generic opencode adapter
- this makes sense if Belayer is launching local vendor CLIs as its primary runtime behavior

Why it should shrink:

- Hermes should be the primary runtime backend for Extend-first Nightshift
- Belayer does not need to be a best-in-class adapter layer for every local agent harness before Nightshift works

#### Suggested path

**Keep initially:**

- local vendor adapters only as migration/dev fallback

**Add:**

- `HermesAdapter` or a new `runtime/hermes` integration that is clearly primary

**Then remove or demote:**

- assumptions in docs/tests/templates that local vendor adapters are the default path
- broad vendor-specific output parsing that is irrelevant once the session-level integration is Hermes-native

#### Important nuance

Do **not** rip out local adapters immediately if they are useful for local debugging. But stop designing around them.

### C. Delete or reduce `internal/runtime`

Current state:

- `internal/runtime/runtime.go` is a very small selector over `local`, `docker`, `clamshell`, `kubernetes`

Why it may not be worth keeping in its current form:

- it abstracts almost nothing meaningful
- the real complexity is elsewhere
- it encourages the fiction that all runtime modes are co-equal

#### Suggested change

Replace with one of two options:

1. a more meaningful launcher/runtime interface centered on actual Nightshift backends:
   - `HermesInClamshell`
   - `HermesLocal`
   - maybe `LegacyVendorLocal`

or

2. delete the package and move runtime selection into a new `internal/nightshift/runtime` package once the real interfaces are clear

### D. Reduce generic workspace abstraction if it duplicates Extend reality

Current state:

- `internal/workspace/workspace.go` models repos.json, clone, ensure ready, repo paths

Potential issue:

- if Extend-first Nightshift has a simpler, explicit repo map (`extend-api`, `extend-app`) and worktree behavior, the current generic abstraction may be more general than necessary

Suggested stance:

- keep if it still cleanly solves repo path/worktree concerns
- remove or slim down if it becomes dead weight after adopting explicit Extend-first workspace config

---

## New packages to add

### 1. `internal/nightshift`

This should become the orchestration package for the Extend-first system.

Responsibilities:

- session phase transitions
- planner-facing orchestration helpers
- specialist dispatch
- artifact assembly
- review gating
- morning handoff

Suggested files:

- `internal/nightshift/session.go`
- `internal/nightshift/dispatch.go`
- `internal/nightshift/artifacts.go`
- `internal/nightshift/handoff.go`
- `internal/nightshift/roles.go`

### 2. `internal/hermes` or `internal/nightshift/hermesruntime`

Responsibilities:

- launch Hermes-backed specialists
- map Belayer role specs to Hermes profiles
- pass task payload, repo binding, and session metadata
- capture run lifecycle and result summaries

This is the new primary runtime adapter.

### 3. `internal/localenv` or `internal/nightshift/localenv`

Responsibilities:

- run `xt doctor`
- run `xt up` / `xt down`
- run `xt status`
- run `xt token`
- normalize outputs for artifact recording

This is the replacement for “generic workbench first.”

### 4. `internal/artifacts`

Responsibilities:

- artifact schemas
- file naming/storage rules
- typed serialization/deserialization
- summary helpers

This can remain small, but it should exist explicitly so artifacts stop being hidden in ad hoc event JSON.

---

## Data model changes

### Sessions table

Current session model is too thin:

- `id`
- `name`
- `status`
- `template`

#### Add fields

- `kind` (`ticket`, `fullstack_ticket`, `planner_only`)
- `phase` (`intake`, `plan`, `implement`, `verify`, `review`, `handoff`)
- `risk_class`
- `outcome`
- `ticket_ref`
- `planner_run_id` or planner identity reference

### New artifacts table

Add a dedicated table, for example:

- `id`
- `session_id`
- `kind`
- `producer`
- `path`
- `summary`
- `created_at`

Artifact kinds for v1:

- `ticket_intake`
- `task_graph`
- `shared_contract`
- `specialist_report`
- `verification_report`
- `handoff`
- `review_report`

### New specialist_runs table

Add explicit specialist run tracking:

- `id`
- `session_id`
- `role`
- `runtime`
- `identity_ref`
- `workspace_path`
- `status`
- `started_at`
- `ended_at`
- `result_summary`

This is where the “identity vs invocation” distinction becomes real.

---

## CLI changes

### Keep

- `belayer daemon`
- `belayer status`
- `belayer logs`
- `belayer message ...`
- `belayer note`

### Modify heavily

#### `belayer session start`

Current flags:

- `--docker`
- `--clamshell`
- `--environment`
- template-driven local runtime branching

#### Proposed direction

Move toward something like:

```bash
belayer session start \
  --kind fullstack_ticket \
  --ticket PROD-1234 \
  --spec ./spec.md \
  --profile extend
```

And for local/dev fallback, maybe:

```bash
belayer session start --mode dev-local ...
```

The important point is that `--docker` should stop being a first-class top-level story.

### Replace generic workbench commands with Extend intent

Current:

- `belayer workbench up`
- `belayer workbench status`
- `belayer workbench down`

These can remain, but under the hood they should speak in Extend-localenv terms, not generic compose terms.

For example:

- `belayer workbench up` → invokes `xt` flows + records status artifact
- `belayer workbench status` → wraps `xt status`
- `belayer workbench down` → wraps `xt down`

---

## First runnable vertical slice

To avoid fizzing out again, the first slice must be narrow and demonstrable.

### Goal

Support one reviewed Extend ticket that touches:

- only `extend-api`, or
- `extend-api` + `extend-app` with a simple shared contract

### Required capabilities only

1. Belayer creates a session
2. Planner role starts
3. One or two Hermes specialists run in clamshell sandboxes
4. Specialists write artifacts and code
5. `xt` adapter brings up environment and records status
6. QA or planner runs verification commands
7. Draft PR(s) + `handoff.md` are produced

### Explicitly skip for first slice

- generic multi-repo environments
- arbitrary vendor backends
- generalized workbench compose generation
- broad tier/peripheral/ephemeral experimentation
- complex memory reflection upgrades

---

## Concrete package-by-package change list

### `internal/docker`

**Decision:** deprecate aggressively

- Mark package as legacy in docs/comments
- Stop adding features here
- Replace workbench calls with `internal/localenv`
- Replace sandbox assumptions with `internal/clamshell`
- Delete compose-generation path once Extend-first slice no longer needs it

### `internal/vendor`

**Decision:** demote to legacy runtime support

- Keep for local fallback only
- Stop centering templates around vendor/model pairs
- Add Hermes runtime path before expanding any vendor-specific features

### `internal/runtime`

**Decision:** replace or remove

- remove fake co-equal runtime selector
- replace with a Nightshift-specific launcher abstraction

### `internal/session`

**Decision:** keep, but rewrite templates and role spec assumptions

- remove generic built-ins like `explorer` and `merger`
- add Extend-first role definitions
- change from vendor-centric to identity/runtime-centric fields

### `internal/agent`

**Decision:** partially keep, partially narrow

Keep:

- prompt assembly ideas where still useful
- tool spec concepts if still compatible

Change:

- stop assuming docker-compose-based execution targets
- narrow executor targets to Nightshift-relevant ones

### `internal/daemon`

**Decision:** keep and expand

- add artifacts API
- add specialist run tracking
- add Nightshift-oriented dispatch endpoints
- reduce direct ownership of generic Docker workbench lifecycle

### `internal/cli`

**Decision:** simplify

- shrink `session_start.go`
- remove runtime branching from CLI layer
- push runtime-specific logic down into packages
- keep human-facing commands small and truthful

---

## Code removal candidates

These are the areas most likely to yield meaningful deletion.

### High-confidence removal or major shrink candidates

1. **generic Docker workbench generation**
   - `internal/docker/workbench.go`
   - related tests and compose helpers

2. **generic Docker runtime launch path in CLI**
   - large parts of `internal/cli/session_start.go`
   - `--docker` mode plumbing

3. **runtime mode selector abstraction**
   - `internal/runtime/runtime.go`

4. **generic built-in session templates and broad phase assumptions**
   - current built-ins in `internal/session/template.go`

### Medium-confidence shrink candidates

5. **vendor adapter centrality**
   - keep code for fallback, but reduce its role in architecture/docs/tests

6. **workspace abstraction breadth**
   - keep if it still directly helps Extend-first repo mapping
   - shrink if it remains broader than actual use

---

## Recommended implementation order

### Step 1 — add the new implementation doc set and freeze scope

- keep Extend-first scope explicit
- define the first vertical slice ticket class
- stop adding new generic runtime features

### Step 2 — add artifacts and specialist run records to the store/daemon

Why first:

- gives the system a better control-plane backbone before runtime changes
- makes all later integration work more inspectable

### Step 3 — introduce Hermes runtime integration

- add a primary specialist launcher path for Hermes
- keep local vendor launchers as fallback only

### Step 4 — add `xt` adapter and route workbench commands through it

- replace generic compose-first workbench path
- emit verification artifacts from `xt` runs

### Step 5 — simplify session templates and CLI runtime branching

- swap in Extend-first role roster
- reduce `session_start.go`
- remove dead generic flags/paths where possible

### Step 6 — delete or quarantine legacy Docker runtime code

- once the first slice runs, aggressively remove dead code
- do not keep two equal orchestration stacks alive unless both are really used

---

## Final recommendation

Yes — I do think you are probably right that Belayer can lose a lot of code.

The likely pattern is:

- **more code** in explicit Nightshift control-plane pieces
- **much less code** in speculative generic runtime/workbench/vendor plumbing

If we do this well, Belayer should become **simpler and more opinionated**, not larger.

The strategic test for every package should be:

> does this help Belayer become a working Extend Nightshift control plane now?

If not, it is probably a candidate for deletion, demotion, or quarantine.
