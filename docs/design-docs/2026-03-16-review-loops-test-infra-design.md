# Review Loops, Test Infrastructure & Persistent Learnings

**Date:** 2026-03-16
**Status:** Accepted
**Inspired by:** [Night Shift](https://jamon.dev/night-shift) (philosophy), [Porting](https://ghuntley.com/porting/) (mechanical patterns)

## Problem Statement

Belayer's current validation pipeline assumes a single review pass catches issues. Leads run `harness:review` once, spotters validate once per repo, and anchors check cross-repo alignment with a max of 2 retries. This is insufficient:

1. **Single-pass reviews miss issues.** Night Shift's approach burns tokens looping reviews until all specialized personas approve — human review should never encounter incomplete work.
2. **No testing-first enforcement.** Leads can implement before writing tests (or skip tests entirely). There's no test contract linking specs to expected test behavior.
3. **The setter is reactive.** It writes spec.md + climbs.json and hands off. It doesn't analyze repo test infrastructure, create test contracts, or validate spec completeness against past failures.
4. **Learnings are ephemeral.** `harness:reflect` captures a one-shot retrospective in markdown. There's no structured persistence, no retrieval mechanism, and no way to surface relevant past failures during future spec creation.

## Design Goals

| Goal | Success Criteria |
|------|-----------------|
| Review loop until green | Leads iterate on multi-persona review until all personas pass (max 3 cycles) |
| Test-first contract | Every spec includes a test contract; leads must satisfy it before implementation is "done" |
| Setter test planning | Setter interactively builds test contracts during brainstorm; test infra scaffolding is a problem concern |
| Spotter as spec compliance | Spotters validate all climbs in a repo against the spec after all leads complete |
| Persistent learnings | SQLite-backed learnings with agentic retrieval and compaction |
| Codified reflect | Reflect step classifies errors, writes learnings to SQLite, surfaces system improvement recommendations |

## Architecture

### 1. Lead-Level Multi-Persona Review Loop

#### Current Flow
```
init → plan → orchestrate → review (single pass) → reflect → complete → TOP.json
```

#### Proposed Flow
```
init → plan → orchestrate → multi-persona review loop (max 3) → complete → TOP.json
```

#### Persona System

Review personas are configured per repo per crag. Each persona has:

```toml
# .belayer/review-personas.toml (in repo root or crag config)
[personas.architect]
description = "Reviews system design, module boundaries, and architectural consistency"
focus = ["separation of concerns", "dependency direction", "API design"]
docs = ["docs/ARCHITECTURE.md", "docs/DESIGN.md"]

[personas.test-engineer]
description = "Reviews test coverage, test quality, and test contract compliance"
focus = ["test coverage", "edge cases", "test contract satisfaction", "test isolation"]
docs = ["docs/QUALITY.md"]

[personas.domain-expert]
description = "Reviews business logic correctness and spec compliance"
focus = ["acceptance criteria", "edge cases from spec", "domain invariants"]
docs = []

[personas.code-quality]
description = "Reviews code style, performance, and maintainability"
focus = ["naming", "complexity", "performance", "error handling"]
docs = []
```

**Auto-initialization:** When a lead starts work on a repo that lacks `review-personas.toml`, the lead's first action is to analyze the repo type (frontend, backend, CLI, library) and generate an appropriate persona config. This happens once per repo.

**Always-on personas:** `test-engineer` and `domain-expert` always apply. Others are repo-type-dependent (e.g., `designer` for frontend repos, `performance-engineer` for backend APIs).

#### Review Loop Mechanics

Leads use **Claude Code subagents** (Task tool) for persona reviews — one subagent per persona, run in parallel.

Each review cycle:
1. Lead spawns a Claude Code subagent per persona (parallel via Task tool)
2. Each subagent reviews the implementation against its persona's focus areas and assigned docs
3. Subagent returns `{ "pass": true/false, "issues": [...] }`
4. If any persona fails: lead addresses issues, then re-runs *only failing personas*
5. Loop until all pass or max 3 cycles reached

On max cycles exhausted without green:
- Lead writes `TOP.json` with `"status": "review_incomplete"`
- `review_incomplete` includes which personas still failing and why
- Daemon advances to spotter — the spotter can create correction climbs (see Section 2) to address remaining issues
- If spotter's correction loop also exhausts, problem enters `needs_human` state and surfaces to setter

#### Changes to Harness Plugin

`harness:review` gains:
- Persona discovery: reads `review-personas.toml` from repo root
- Loop driver: runs personas, collects results, re-runs failures
- Max cycle config: reads from `belayer.toml` `[review]` section, default 3
- Test contract validation: `test-engineer` persona specifically checks that the test contract from the spec is satisfied

### 2. Spotter Role: Post-Repo Spec Compliance

#### Current Behavior
- Activates per-repo when a single lead tops
- Runs validation profiles (build, tests, dev server, browser)
- Writes SPOT.json with pass/fail

#### Proposed Behavior
- Activates per-repo when **all climbs for that repo** complete
- Receives the full problem spec + all TOP.json summaries for climbs in this repo + the test contract
- Validates:
  1. **Spec compliance:** Do the combined climb outputs satisfy the spec's requirements for this repo?
  2. **Test contract fulfillment:** Are all acceptance tests from the test contract passing?
  3. **Runtime validation:** Build succeeds, tests pass, dev server works (existing behavior)
  4. **Completeness:** Are there spec requirements that no climb addressed?

#### Daemon State Machine Change

Current transition:
```
lead tops → activate spotter for that repo
```

Proposed transition:
```
all leads for repo top (complete OR review_incomplete) → activate spotter for that repo
```

This means tracking lead completion per-repo, not just per-lead. The daemon already tracks leads by repo — it needs to gate spotter activation on all leads for that repo being complete (or `review_incomplete`).

#### Spotter Input Contract

The spotter's `GOAL.json` (defined in `internal/climbctx/SpotterClimb`) is extended to include:

| Field | Source | Purpose |
|-------|--------|---------|
| `problem_spec` | spec.md content | Full problem specification |
| `test_contract` | Test Contract section from spec.md | Testable acceptance criteria |
| `climb_summaries` | All TOP.json outputs for this repo | What each lead implemented |
| `review_incomplete_leads` | TOP.json entries with `review_incomplete` status | Which leads couldn't pass all personas and why |
| `validation_profiles` | Existing field | Runtime validation checklists |

The daemon writes these fields before spawning the spotter.

#### Spotter Failure → Correction Climbs

When the spotter finds issues (spec compliance gaps, test contract failures, incomplete review items), it does NOT simply fail. Instead:

1. Spotter writes `SPOT.json` with `"pass": false` and structured issue details
2. Daemon reads the issues and creates **new correction climbs** — one per distinct issue or group of related issues
3. Correction climbs are dispatched as new leads for the repo, with the spotter's feedback as context in GOAL.json
4. When correction leads complete, spotter re-activates and re-validates
5. Max spotter cycles: configurable, default 2. After exhaustion → `needs_human` state

This replaces the current pattern of re-dispatching the original climbs with feedback injected. Correction climbs are narrowly scoped to fix specific issues, not re-do entire features.

#### SPOT.json Extension

```json
{
  "pass": false,
  "project_type": "backend",
  "spec_compliance": {
    "satisfied": ["T-1", "T-2"],
    "unsatisfied": ["T-3: OAuth refresh token handling not implemented"],
    "unverifiable": ["T-4: Requires manual testing"]
  },
  "test_contract": {
    "satisfied": 8,
    "unsatisfied": 2,
    "details": ["Missing: concurrent create idempotency test"]
  },
  "runtime": {
    "build": "pass",
    "tests": "pass",
    "dev_server": "pass"
  },
  "correction_climbs": [
    {
      "description": "Implement OAuth refresh token rotation per T-3",
      "issues_addressed": ["T-3"],
      "context": "Token refresh endpoint exists but doesn't rotate the refresh token itself"
    }
  ],
  "issues": [],
  "screenshots": []
}
```

Note: Requirement IDs (`T-1`, `T-2`, etc.) match the test contract IDs from spec.md. The setter assigns these during test contract creation.

### 3. Anchor: Cross-Repo + Integration Review

Anchors remain the cross-repo alignment step for multi-repo problems. **Single-repo problems already skip anchor** (existing behavior, no change needed).

Changes for multi-repo problems:

- **Multi-persona review:** Anchor gets its own persona set focused on integration concerns:
  - `api-contract`: Do API schemas match between frontend/backend?
  - `shared-types`: Are shared types/schemas consistent?
  - `integration`: Do integration points connect correctly?
  - `feature-parity`: Does each repo deliver its part of the feature?
- **Triggered after all spotters pass** (not after all leads top, as currently)

### 4. Setter: Interactive Test Planning & Test Contracts

#### Philosophy

The setter remains interactive and idea-focused. It doesn't run test commands or analyze repo internals directly. Instead, it builds the test contract through dialogue with the user.

#### Test Contract Creation

During brainstorm/spec creation, the setter:

1. **Asks test-oriented questions:**
   - "How should we verify this feature works? What does success look like?"
   - "What edge cases should we test for?"
   - "Are there integration points that need contract tests?"
   - "Does this repo have existing test infrastructure, or will we need to set it up?"

2. **Builds the test contract** as a new section of spec.md:

```markdown
## Test Contract

### Acceptance Tests
| ID | Scenario | Expected Behavior | Repo |
|----|----------|-------------------|------|
| T-1 | User creates project | Project appears in list, returns 201 | api |
| T-2 | Invalid input | Returns 400 with validation errors | api |
| T-3 | Project card renders | Shows name, description, status badge | web |

### Infrastructure Requirements
| Repo | Requirement | Notes |
|------|-------------|-------|
| api | Integration test harness | Needs DB fixtures |
| web | Component test setup | Vitest + testing-library |

### Test-First Climbs
If infrastructure requirements are missing, the problem should include
prerequisite climbs to scaffold them before feature climbs begin.
```

3. **Adds infrastructure climbs** to climbs.json when the user confirms test infra is missing:

```json
{
  "repos": {
    "api": {
      "climbs": [
        {
          "id": "api-0",
          "description": "Scaffold integration test harness with DB fixtures",
          "depends_on": []
        },
        {
          "id": "api-1",
          "description": "Implement project creation endpoint",
          "depends_on": ["api-0"]
        }
      ]
    }
  }
}
```

#### Changes to Setter Prompt

The setter's `problem-brainstorm` flow gains test planning as an explicit phase. The setter template (`internal/defaults/claudemd/setter.md`) adds:

```markdown
## Test Planning

Every spec MUST include a Test Contract section. During brainstorm:
1. Ask the user how they'd verify the feature works
2. Identify edge cases and failure modes
3. Ask about existing test infrastructure in each repo
4. Build the test contract table
5. If infra is missing, add prerequisite climbs
```

#### Learning Injection

Before starting a brainstorm, the setter queries for relevant learnings (see Section 6). If past problems in this crag had test gaps or spec ambiguity issues, those learnings are surfaced to the setter so it can ask better questions.

### 5. Persistent Learnings: SQLite + Agentic Retrieval

#### Storage: Extend `agentic_decisions`

Learnings are stored as a new decision type in the existing `agentic_decisions` table:

```sql
-- New decision_type values: 'learning', 'learning_retrieval', 'learning_compaction'

-- Learning entries stored as JSON in the existing output column:
-- {
--   "category": "test_gap|spec_ambiguity|infra_issue|review_miss|pattern",
--   "description": "What happened",
--   "recommendation": "What to do differently",
--   "problem_id": "originating problem",
--   "severity": "high|medium|low",
--   "resolved": false,
--   "access_count": 0
-- }
```

No new tables. The existing `agentic_decisions` schema (`id`, `problem_id`, `node_type`, `input`, `output`, `created_at`) already supports this — `node_type` = `"learning"`, `output` = the JSON above.

#### Retrieval: New Agentic Node (Dual Invocation)

**Node type:** `learning_retrieval`

Learning retrieval runs at **two points** in the pipeline, serving different consumers:

**Invocation 1 — CLI level (during `belayer problem create`):**
- Runs in `internal/intake/` after sufficiency check, before the problem is published to the daemon
- Requires direct SQLite access (intake already opens the crag DB)
- Output is surfaced to the setter session as guidance: "Past problems like this had these issues..."
- Helps the setter ask better questions and build better test contracts

**Invocation 2 — Daemon level (before decomposition):**
- Runs in `internal/belayer/` when the daemon picks up a new problem
- Output is injected into the decomposition prompt, so the agentic node can account for past failures when breaking the problem into climbs
- Example: if past auth work had test gaps, decomposition may add a test infra climb automatically

Both invocations use the same agentic node and prompt. Only invoked if the crag has existing learnings (skip for first problem on a crag).

**Input:** Problem spec (the enriched description) + all active learnings for this crag.

Active learnings are queried from SQLite using `json_extract()` on the `output` column:
```sql
SELECT id, output FROM agentic_decisions
WHERE node_type = 'learning'
AND json_extract(output, '$.resolved') = false
AND crag_id = ?
ORDER BY created_at DESC
```

**Output:**
```json
{
  "relevant_learnings": [
    {
      "id": "decision-uuid",
      "description": "Last auth feature lacked integration tests",
      "recommendation": "Include integration test infra climb",
      "relevance": "This problem adds OAuth which is auth-adjacent"
    }
  ],
  "setter_guidance": "Consider asking about integration test coverage for the auth service. Past problems in this area had test gaps."
}
```

#### Compaction: Periodic Agentic Node

**Node type:** `learning_compaction`

**When invoked:** Via `belayer learnings compact` CLI command, or automatically after every N completed problems (configurable, default 10).

**Input:** All learnings for this crag.

**Output:** A compacted set — duplicates merged, resolved issues archived (set `resolved = true`), recurring patterns distilled into principles.

**Mechanism:** The compaction node reads all learnings, produces a new set. The old learnings are marked `resolved = true` (not deleted — audit trail preserved). The compacted learnings are inserted as new entries.

#### CLI Commands

```
belayer learnings list [--category <cat>] [--active]  # List learnings
belayer learnings show <id>                            # Show a learning
belayer learnings add --category <cat> --desc "..."    # Manual learning
belayer learnings compact                              # Run compaction
```

### 6. Codified Reflect: Error Classification & Learning Capture

#### Current `harness:reflect`
- Updates docs (DESIGN.md, QUALITY.md)
- Captures general retrospective
- One-shot, no structured output

#### Proposed Reflect Enhancement

After a problem completes (all leads done, spotter passed, anchor passed if applicable), the reflect step:

1. **Error classification** — Reviews the problem's history:
   - How many review loop cycles did each lead need?
   - Which personas failed most?
   - Did the spotter find issues the lead review loop missed?
   - Did the anchor reject and re-dispatch?
   - Were there stuck leads? What caused it?

2. **Learning extraction** — For each classified error, creates a structured learning:
   ```json
   {
     "category": "review_miss",
     "description": "Code quality persona passed but spotter found unhandled error in auth middleware",
     "recommendation": "Add error handling focus to code-quality persona for backend repos",
     "severity": "medium"
   }
   ```

3. **System improvement recommendations** — Concrete, actionable:
   - "Add 'error handling' to code-quality persona focus areas"
   - "Spec template should ask about error recovery for API endpoints"
   - "Test contract should require error-path tests for all new endpoints"

4. **Write to SQLite** — All learnings stored via `belayer learnings add` (or direct DB write from the reflect agentic node).

5. **Human summary** — A concise report surfaced to the setter:
   ```
   Problem P-42 complete. 3 learnings captured:
   - [high] Test gap: no integration tests for OAuth token refresh
   - [medium] Review miss: code-quality persona didn't catch unhandled errors
   - [low] Spec ambiguity: "support OAuth" didn't specify which grant types

   System recommendations:
   - Add error handling to code-quality persona focus
   - Spec template should enumerate OAuth grant types when auth is involved
   ```

#### Two Levels of Reflect

There are now two distinct reflect operations:

1. **Lead-level `harness:reflect`** (unchanged): Each lead runs this as part of its harness pipeline. Updates repo docs (DESIGN.md, QUALITY.md), captures per-climb retrospective. This is a local concern — the lead reflects on its own work.

2. **Daemon-level reflect node** (new): Runs after the entire problem's validation pipeline completes. Reviews the full problem history across all leads, spotters, and anchors. Classifies errors and writes learnings to SQLite. This is a cross-cutting concern — it reflects on the system's performance.

#### Daemon Integration

The daemon reflect node is a new agentic node (`claude -p`) triggered after final validation:

```
leads complete → spotters pass → anchor passes (if multi-repo) → reflect node + PR creation (parallel)
```

The reflect node runs in parallel with PR creation. If reflect fails (crash, timeout, invalid JSON), it does not block PR creation. The daemon logs the failure and the learnings for this problem are simply not captured — no partial writes to SQLite. The node uses a single transaction for all learning inserts.

## Validation Pipeline Summary

### Current
```
lead (harness:review × 1) → spotter (per-repo, per-lead) → anchor (cross-repo, max 2 retries) → PR
```

### Proposed
```
lead (multi-persona review loop, max 3 cycles)
  → spotter (per-repo, after all repo climbs complete, spec compliance + correction climbs)
    → anchor (cross-repo + integration personas, multi-repo only)
      → reflect + PR creation (parallel)
```

### Daemon State Machine Diff

Current states:
```
imported → enriching → pending → decomposing → running → aligning → pr_creating → pr_monitoring → ...
```

Proposed states:
```
imported → enriching → pending → decomposing → running → spotting → aligning → reflecting/pr_creating → pr_monitoring → ...
```

| State | Status | Trigger | Exit |
|---|---|---|---|
| `running` | Changed | Leads dispatched | All leads for all repos complete or `review_incomplete` |
| `spotting` | New | All leads for a repo done | All spotters pass (may loop with correction climbs per repo) |
| `aligning` | Unchanged | All spotters pass | Anchor approves (multi-repo) or skip (single-repo) |
| `pr_creating` | Unchanged | Anchor passes or single-repo spotters pass | PRs created |
| `reflecting` | New | Same trigger as `pr_creating` — runs in parallel | Reflect node completes; does not gate `pr_creating` |
| `needs_human` | New | Spotter correction loop exhausted | Human intervention via setter |

Notes:
- `spotting` is a problem-level state, but spotter loops are tracked per-repo within it. Multiple repos may be spotting in parallel.
- `reflecting` and `pr_creating` are triggered simultaneously. `reflecting` failure does not block PR creation.
- `needs_human` is a terminal state that requires setter interaction to resume.

## Changes Required

### Belayer Daemon (`internal/belayer/`)
| Change | Description |
|--------|-------------|
| Spotter gating | Gate spotter activation on all leads for a repo completing, not individual leads |
| Correction climb creation | When spotter fails, daemon creates new correction climbs from `correction_climbs` in SPOT.json |
| Spotter retry loop | Re-activate spotter after correction climbs complete (max 2 spotter cycles) |
| `needs_human` state | New terminal state when spotter correction loop exhausts; surfaces to setter |
| `review_incomplete` handling | Daemon treats `review_incomplete` TOP.json as "done enough" for spotter gating |
| Anchor trigger | Trigger anchor after all spotters pass, not after all leads top |
| Anchor skip | Skip anchor for single-repo problems (existing behavior, no change) |
| Reflect node | New agentic node after final validation, writes learnings (parallel with PR creation) |
| Learning retrieval (daemon) | New agentic node before decomposition, queries relevant learnings |
| Learning compaction | New agentic node, on-demand or periodic |
| State machine | New states: `spotting`, `reflecting`, `needs_human` (see State Machine Diff) |

### Harness Plugin (`plugins/harness/`)
| Change | Description |
|--------|-------------|
| Review loop | `harness:review` becomes multi-persona loop with max cycles |
| Subagent mechanism | Use Claude Code Task tool to spawn per-persona review subagents |
| Persona discovery | Read `review-personas.toml` from repo root |
| Persona auto-init | Generate default personas based on repo type if missing; commit to repo |
| Test contract validation | `test-engineer` persona validates test contract satisfaction |

### Agent Prompts (`internal/defaults/claudemd/`)
| Change | Description |
|--------|-------------|
| `lead.md` | Update harness pipeline steps to reflect new review loop |
| `spotter.md` | New SPOT.json schema, spec compliance workflow, correction climb output |
| `anchor.md` | Integration personas, trigger after spotters (not after leads) |

### Climb Context (`internal/climbctx/`)
| Change | Description |
|--------|-------------|
| `SpotterClimb` | Add `problem_spec`, `test_contract`, `climb_summaries` (all TOP.json), `review_incomplete_leads` fields |
| `GOAL.json` writer | Daemon populates new spotter fields before spawn |

### Setter Context (`internal/defaults/`)
| Change | Description |
|--------|-------------|
| Test planning | Add test contract creation to brainstorm flow |
| Learning injection | Surface relevant learnings during brainstorm |
| Infra climbs | Auto-add test infrastructure prerequisite climbs |
| Setter template | Update `setter.md` with test planning section |

### Intake Pipeline (`internal/intake/`)
| Change | Description |
|--------|-------------|
| Learning retrieval (CLI) | Run `learning_retrieval` after sufficiency check, surface to setter |

### CLI (`cmd/belayer/`)
| Change | Description |
|--------|-------------|
| `belayer learnings` | New command group: list, show, add, compact |

### Agentic Nodes (`internal/agentic/` or equivalent)
| Change | Description |
|--------|-------------|
| `learning_retrieval` | Query learnings via `json_extract()`, return relevant subset + guidance |
| `learning_compaction` | Merge duplicates, archive resolved, distill patterns |
| `reflect` | Error classification + learning extraction from problem history |

### Config (`review-personas.toml`)
| Change | Description |
|--------|-------------|
| File location | Lives in repo root, committed to repo, outside belayer config resolution chain |
| Ownership | Generated once by first lead if missing; owned by team long-term |
| `[review]` in belayer.toml | `max_review_cycles` (default 3), `max_spotter_cycles` (default 2) |

## Resolved Decisions

| Decision | Resolution | Rationale |
|----------|-----------|-----------|
| Persona subagent mechanism | Claude Code subagents (Task tool) | Parallel execution, isolated context per persona, thorough review |
| Spotter re-dispatch | Daemon creates new correction climbs from spotter feedback | Narrowly scoped fixes, not re-running entire original climbs |
| `review_incomplete` handling | Advances to spotter; spotter creates correction climbs; `needs_human` on exhaustion | Spotter is the safety net; human is the last resort |
| Learning retrieval timing | Runs at both CLI level (intake) and daemon level (pre-decomposition) | Setter benefits during brainstorm; decomposition benefits during climb planning |
| Persona config location | Per repo, committed to repo root, auto-generated by first lead if missing | Team owns long-term; adapts to repo type |

## Open Questions

1. **Learning decay** — Should learnings have a TTL? Or rely entirely on compaction to archive stale ones?
2. **Cross-crag learnings** — Should learnings be crag-scoped (current design) or global? Some patterns (e.g., "always add error handling tests") apply everywhere.
3. **Compaction identity** — Compacted learnings get fresh IDs and span multiple `problem_id`s. Should compacted entries use a sentinel `problem_id` (e.g., `"compacted"`) or NULL?
4. **Manual learning `problem_id`** — Learnings added via `belayer learnings add` don't originate from a problem. Use NULL or a sentinel value?

## References

- [Night Shift](https://jamon.dev/night-shift) — Multi-persona review, token-burning philosophy, test-first enforcement
- [Porting](https://ghuntley.com/porting/) — Test-specification-first, mechanical loop patterns
- [Self-Improving Coding Agents](https://addyosmani.com/blog/self-improving-agents/) — AGENTS.md pattern, progress logs, stop conditions
- [Effective Harnesses for Long-Running Agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents) — Two-agent pattern, artifact-based continuity
