---
name: strangler-fig
description: Use when breaking up a god class, extracting responsibilities from an oversized module, incrementally replacing legacy code, or when old and new must coexist during a refactor — symptoms include classes over 500 lines, modules with mixed concerns, or "we can't rewrite this all at once"
---

# Strangler Fig Refactoring

Decompose an incremental refactoring into production-safe goals using the Strangler Fig pattern — build new alongside old, redirect traffic, then remove legacy. Informed by Martin Fowler's StranglerFigApplication and Shopify's incremental migration approach.

## When to Use

- Extracting responsibilities from a God Object or oversized module
- Replacing a legacy component without big-bang rewrite
- Any refactor where old and new must coexist during transition
- Classes over 500 lines with multiple distinct responsibilities
- "We can't rewrite this all at once" situations

## Workflow

Execute three phases interactively with the user:

### Phase 1: Identify Extraction Target

1. Ask what code to refactor — the oversized class, tangled module, or legacy component
2. Read the target code and map its responsibilities (list each distinct responsibility)
3. Classify the refactor type:
   - **Backend service extraction** — persistent state involved, data migration likely needed
   - **Frontend component extraction** — UI/state extraction, typically no database migration
   - **Pure code extraction** — logic extraction only, no persistent state changes
   - **Mixed** — evaluate based on which responsibilities involve persistent state
4. Ask which responsibilities to extract, or recommend based on cohesion analysis

### Phase 2: Dependency & Seam Analysis

1. Trace consumers — who calls, imports, or depends on the extraction target
2. Trace dependencies — what the target depends on (other modules, DB, external services)
3. Identify seams — natural boundaries where old and new systems can coexist
4. Assess risk:
   - Blast radius — how many callers/modules are affected
   - Data consistency risk — are there shared mutable state or transactions
   - Rollback complexity — how easy is it to undo each step
5. Determine if persistent state is involved (indicates data migration goals will be needed)

### Phase 3: Decompose into Production-Safe Goals

Use strangler fig thinking and the dependency analysis from Phase 2 to identify natural goal boundaries. Each goal must be safe to merge to production independently.

**Grouping heuristic**: combine steps that cannot be independently verified or deployed into a single goal. A goal is the smallest unit of work that is safe to ship alone.

**Production-safety criteria** — a goal is production-safe if it meets at least one:
- Protected by a feature flag (default off)
- Unconsumed scaffolding (new code exists but nothing calls it yet)
- Additive-only change (no existing behavior is altered or removed)
- Safe deletion (removing code with verified zero consumers)

For each goal, provide:
- **Concrete mapping**: specific files, classes, tables, or components involved
- **Rollback note**: how to reverse this goal if problems arise
- **Merge criteria**: what must be true before this goal's PR is considered ready (advisory, for human reviewers)

**Output format:**

Goals table:

| # | Goal | Description | Depends On | Production Safety |
|---|------|-------------|------------|-------------------|
| 1 | `kebab-goal-name` | What this goal accomplishes | — | Additive-only / Feature flag / etc. |
| 2 | `kebab-goal-name` | What this goal accomplishes | Goal 1 | Unconsumed scaffolding |
| … | … | … | … | … |

**Execution Order**: Describe which goals are sequential and which can be parallelized. Goals with no shared dependency can be worked in parallel by separate engineers or teams.

**Merge Criteria**: For each goal, document what reviewers should verify before merging. This is advisory guidance for human reviewers — not an automated gate.

## Reference Framework

The 7 strangler fig steps are a reference framework for analysis during Phases 2-3. They help identify what work needs to happen but do not dictate goal boundaries. The agent groups these into production-safe goals based on the project's actual needs.

| Step | What | Applies When |
|------|------|-------------|
| 1. Define new interface | Create the new module/class/component with extracted responsibilities | Always |
| 2. Redirect consumers | Update callers/importers to use the new interface | Always |
| 3. Establish new data source | Create new storage (DB table, state store, context, cache) | Persistent state involved |
| 4. Dual writes | Write to both old and new sources simultaneously for consistency | Persistent state involved |
| 5. Backfill | Migrate existing data from old source to new | Historical data exists |
| 6. Switch reads | Move read operations from old source to new | Persistent state involved |
| 7. Remove legacy | Delete old code, old storage, old dependencies | Always |

For detailed examples per refactor type, read the reference file at:
`plugins/harness/skills/strangler-fig/references/steps-by-context.md`

For common merge criteria patterns and observation strategies, read:
`plugins/harness/skills/strangler-fig/references/gate-patterns.md`

## Output

Produce a goal dependency graph and per-goal summaries as structured content. Do NOT save files — the invoking command (`/harness:refactor`) handles document creation.

## Key Principles

- **Reversibility is the core value** — every goal (except final deletion goals) should be reversible
- **Transitional architecture is acceptable** — temporary code enabling coexistence is not waste, it's risk reduction
- **Goals have explicit dependencies** — each goal declares what must be merged before it begins
- **Merge criteria document deployment readiness** — they are advisory checkpoints, not automated gates
- **Goal count and scope varies by refactor** — a simple extraction might be 2 goals; a complex service split might be 8

## Common Mistakes

| Mistake | Why It's Dangerous | Fix |
|---------|-------------------|-----|
| Scoping a goal that isn't production-safe to merge independently | If the PR breaks prod when merged alone, it's not a valid goal boundary | Restructure: add a feature flag, split further, or make the change additive-only |
| Making goals too granular (15 PRs for a simple extraction) | Review fatigue, coordination overhead, unclear progress | Combine steps that can't be independently verified into a single goal |
| Making goals too coarse (one mega-PR) | Defeats the purpose of incremental safety; hard to review and impossible to roll back cleanly | Split at seams where production safety is naturally maintained |
| Not documenting rollback for each goal | You won't remember how to undo under incident pressure | Every goal needs a rollback note written before implementation |
| Not identifying which goals can parallelize | Missed opportunity to reduce calendar time | Explicitly map which goals have no shared dependency and can run in parallel |

## Red Flags — STOP and Reassess

- "This goal's PR would break prod if merged alone" — the goal scope is wrong; restructure it
- "We need all goals merged simultaneously" — goals aren't independent enough; find a way to sequence them safely
- "Skip the merge criteria, it obviously works" — verify anyway; obvious things fail silently in production
- "Let's just do it all in one PR" — defeats the purpose of incremental safety and eliminates the ability to roll back individual changes

**All of these mean: revisit goal boundaries, restore production safety, then proceed.**
