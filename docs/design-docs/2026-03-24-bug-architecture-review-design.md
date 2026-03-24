---
status: current
created: 2026-03-24
branch: master
consulted-learnings: []
---

# Bug Architecture Review & Learnings Enforcement

> **Goal**: Transform `/harness:bug` from "find and fix this bug" into "find this bug, understand why it was possible, and prevent the class of bug from recurring" — then enforce those learnings in future code review.

## Context

The harness bug flow currently produces a root cause diagnosis and a single `debugging`-category learning. This misses the opportunity to answer the harder question: *"Why was it possible for this bug to be written, and how do we avoid writing it in the future?"*

Additionally, learnings are only consumed by `/harness:brainstorm`. They are not checked during bug investigation (to accelerate diagnosis), planning (to inform task design), or code review (to enforce compliance). This means learnings are write-heavy and read-light — they accumulate but don't actively prevent recurrence.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Architecture review prompt location | Reference file (`references/architecture-review-prompt.md`) | Keeps bug.md lean; review dimensions can be iterated independently. Prompt is purpose-built for post-root-cause analysis, not generic. |
| Testing dimension depth | Specific test cases + infrastructure gaps | Concrete tests are immediately actionable for the plan. Infrastructure gaps catch structural patterns. Full test design critique risks scope creep. |
| Learnings output | One learning per actionable dimension | Multi-category learnings (architecture, testing, patterns, debugging) are independently searchable. No forced output — agent's judgment on what's actionable. |
| Harness doc handling | Flag only, don't fix | Flags feed into `/harness:plan` as tasks. Fixing docs during diagnosis muddies commit history and duplicates `/harness:reflect`. |
| Severity gate | Agent's judgment, not severity-gated | A "Low" severity bug might reveal a huge design gap. Default to running the review; produce "N/A — isolated implementation error" when dimensions don't apply. |
| Learnings consumption | Three points: bug (advisory), plan (advisory), review (enforcement) | Advisory in early stages surfaces context. Enforcement in review closes the loop and makes learnings actionable. |
| Enforcement mechanism | Dedicated review agent (`harness:learnings-reviewer`) | A specialized agent can match diffs against learnings more effectively than bolting checks onto existing review steps. Runs alongside the 5 existing pr-review-toolkit agents. |

## Changes

All file paths below are relative to `plugins/harness/`.

### 1. `commands/bug.md` — Architecture review step + enhanced learnings

The full step sequence after changes:

1. Verify initialized
2. Read prior bug analyses
3. **2.5 (new): Check prior learnings**
4. Systematic debugging → confirmed root cause
5. Save bug analysis doc (with new Architecture Review section)
6. **4.5 (new): Architecture review** — read reference file, append findings
7. **4.7 (modified, was 4.5): Multi-dimensional learnings**
8. Update index
9. Guide to next step

**Step 2.5 (new): Check prior learnings**

After reading prior bug analyses and before systematic debugging, scan `docs/LEARNINGS.md` for learnings matching the bug's affected area. Surface relevant ones to:
- Accelerate diagnosis: "We've seen this class of bug before — see L-012"
- Detect recurrence: "L-012 said to always check X, but this bug is exactly that class — the learning failed to prevent it"

Match by: category vs. reported affected area, keyword overlap between learning titles/bodies and the bug description. Surface top 3 most relevant. Skip silently if LEARNINGS.md doesn't exist or has no matches.

**Step 4.5 (new): Architecture review**

After root cause is confirmed and the bug analysis doc is written, read `references/architecture-review-prompt.md` (an LLM-facing prompt, same pattern as `references/adversarial-review-prompt.md`) and conduct the review. Append findings to the bug analysis doc as a new `## Architecture Review` section.

The reference file contains the detailed prompt for four review dimensions:
1. **Systemic spread** — Search for analogous patterns in the codebase
2. **Design gap** — Identify deeper design problems the root cause is a symptom of
3. **Testing gaps** — Specific missing test cases + missing test infrastructure/categories
4. **Harness context gaps** — Flag stale/missing/misleading docs (don't fix)

Each dimension produces actionable findings or an explicit "nothing systemic" signal. No forced output. The `## Architecture Review` section is always written — when a dimension has no findings, use its "None" template text. This keeps the document structure consistent and makes it clear the review was conducted.

**Bug analysis doc template addition:**

```markdown
## Architecture Review

### Systemic Spread
- {list of analogous instances with file:line references, or "None — isolated to this call site"}

### Design Gap
- {specific design weakness and what a better design would look like, or "None — implementation error within sound design"}

### Testing Gaps
- **Missing test cases:** {concrete tests that would have caught this bug}
- **Infrastructure gaps:** {missing test categories/patterns for the affected area, or "Test coverage for this area is adequate — this was a gap in a specific case, not a structural gap"}

### Harness Context Gaps
- {which docs are missing/stale/misleading and what's wrong, or "Docs accurately describe this area"}
```

This section becomes input to `/harness:plan`, directly expanding plan scope. A bug that reveals a design gap gets a plan that addresses the structural problem, not just a patch.

**Step 4.7 (modified, was 4.5): Multi-dimensional learnings**

Produce one learning per dimension that has actionable findings:

| Finding dimension | Learning category |
|-------------------|-------------------|
| Systemic spread | `patterns` |
| Design gap | `architecture` |
| Testing gaps | `testing` |
| Root cause itself | `debugging` |
| Harness context gaps | No learning (flagged for the plan) |

Note: `/harness:bug` intentionally does not produce `review-escape` category learnings. Bugs investigated via `/harness:bug` were already known — review escapes are detected by `/harness:reflect`'s Review Escape Mining phase, which handles that category.

Only write a learning if the finding is actionable. Follow the existing `_learnings-format.md` spec. Source field: `/harness:bug {YYYY-MM-DD}`.

**Implementation note:** The existing LEARNINGS.md scaffold in `bug.md` is missing the `review-escape` category present in `_learnings-format.md`. Update the scaffold to match `_learnings-format.md` when implementing this change.

### 2. `references/architecture-review-prompt.md` (new)

LLM-facing prompt loaded by bug.md step 4.5 (same pattern as `references/adversarial-review-prompt.md`). Framed specifically for "root cause confirmed, now zoom out." Contains:

- The framing question: "Why was it possible for this bug to be written, and how do we prevent it in the future?"
- Four dimension definitions with guidance on what to search for and how deep to go
- The output template (the `## Architecture Review` section format)
- The "nothing systemic" escape hatch for each dimension
- Guidance on testing gaps: both specific test cases (what test with what input catches this exact bug) and infrastructure gaps (missing test categories, not just missing tests)

### 3. `commands/plan.md` — Learnings consultation

Add step 3.5 after reading the design document (between current steps 3 and 4): scan `docs/LEARNINGS.md` for active learnings matching the modules/areas being planned. Match by category vs. modules touched, keyword overlap with task descriptions and file paths.

Surface relevant learnings so the plan can incorporate them. Record consulted learning IDs in the plan's `>` header block as a new line: `> **Consulted Learnings**: L-003, L-012` (plans use `>` status blocks, not YAML frontmatter — different from design docs).

If LEARNINGS.md doesn't exist or has no matches, skip silently.

**Backward compatibility:** `/harness:plan` already consumes bug analysis docs. Bug analyses created before this change will lack the `## Architecture Review` section — plan should handle this gracefully (the section is simply absent, no special handling needed).

### 4. `commands/review.md` — Add learnings reviewer to Phase 4

Add `harness:learnings-reviewer` as a 6th agent in the Phase 4 review loop. It runs in parallel with the 5 existing pr-review-toolkit agents.

Update the agent table:

| Agent | Type | Focus |
|-------|------|-------|
| ... (existing 5) | ... | ... |
| Learnings Reviewer | `harness:learnings-reviewer` | Checks diff against active learnings for violations |

The agent receives the same diff and changed file list as the other agents. It returns findings in the same PASS/FAIL format. Violations are treated as review findings and enter the fix cycle like any other agent's findings.

**Implementation note:** Update review.md's report template to reflect 6 agents: change `{passed}/5 passed` to `{passed}/6 passed` and add a `learnings-reviewer` row to the Per-Agent Results table.

### 5. `agents/learnings-reviewer.md` (new)

Agent definition for the learnings enforcement reviewer. System prompt instructs it to:

1. Read `docs/LEARNINGS.md`, filter to `status: active`
2. Match each learning against the diff using a two-gate filter:
   - **Gate 1 (file relevance):** The learning must reference file paths, modules, or packages that overlap with the diff's changed files. If the learning names no specific paths, match by category against the diff's affected domains (e.g., a `testing` learning matches test file changes).
   - **Gate 2 (semantic relevance):** The learning's recommendation must be about the *kind* of change being made, not just the same files. A learning about "always add migration rollbacks" is irrelevant to a comment fix in a migration file.
3. For each learning that passes both gates, check whether the diff follows or violates the recommendation
4. Report violations as findings with: the learning ID, what the learning recommends, how the diff violates it, and suggested fix
5. Return PASS if no violations, FAIL if any learning is violated

The two-gate filter ensures the agent is conservative — only clear violations are reported, not speculative keyword matches. A learning about database migrations should not fire on unrelated SQL changes.

## Learnings Consumption Summary

| Where | Step | Purpose | Mode |
|-------|------|---------|------|
| `/harness:bug` | 2.5 (new) | Accelerate diagnosis, detect recurrence | Advisory — surface relevant learnings |
| `/harness:brainstorm` | 2.5 (existing) | Inform design decisions | Advisory — surface relevant learnings |
| `/harness:plan` | 3.5 (new) | Inform task design | Advisory — surface relevant learnings |
| `/harness:review` | Phase 4 (modified) | Enforce compliance | Enforcement — PASS/FAIL findings |

## Files Changed

| File | Action | Description |
|------|--------|-------------|
| `commands/bug.md` | Modify | Add steps 2.5, 4.5, modify 4.5→4.7, update bug analysis template |
| `references/architecture-review-prompt.md` | Create | Detailed post-root-cause review prompt with four dimensions |
| `commands/plan.md` | Modify | Add learnings consultation step |
| `commands/review.md` | Modify | Add learnings-reviewer as 6th agent in Phase 4 |
| `agents/learnings-reviewer.md` | Create | Agent definition for learnings enforcement reviewer |
