# Bug Architecture Review & Learnings Enforcement — Implementation Plan

> **Status**: Completed | **Created**: 2026-03-24 | **Completed**: 2026-03-24
> **Design Doc**: `docs/design-docs/2026-03-24-bug-architecture-review-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-24 | Design | Reference file for arch review prompt | Keep bug.md lean, iterate independently |
| 2026-03-24 | Design | Multi-dimensional learnings | Each dimension gets its own category for searchability |
| 2026-03-24 | Design | Dedicated review agent for enforcement | Specialized matching > bolting onto existing steps |
| 2026-03-24 | Eng Review | DRY: extract learnings consultation to _learnings-format.md | Same pattern in 3 commands — single source of truth |

## Progress

- [x] Task 1: Add "Consulting Learnings" section to _learnings-format.md
- [x] Task 2: Create references/architecture-review-prompt.md
- [x] Task 3: Create agents/learnings-reviewer.md
- [x] Task 4: Modify commands/bug.md (steps 2.5, 4.5, 4.7)
- [x] Task 5: Modify commands/plan.md (step 3.5)
- [x] Task 6: Modify commands/review.md (add 6th agent)

## Surprises & Discoveries

_None yet — updated during execution by /harness:orchestrate._

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

### Task 1: Add "Consulting Learnings" section to _learnings-format.md

**Files:**
- Modify: `plugins/harness/commands/_learnings-format.md`

This is the DRY extraction from the eng review. The consultation pattern (read LEARNINGS.md, filter active, match by category/keyword, surface top 3) currently lives inline in brainstorm.md step 2.5. We add it as a shared reference so bug.md, plan.md, and brainstorm.md can all point here.

- [ ] **Step 1: Add Consulting Learnings section**

Append to the end of `_learnings-format.md` (before the final line if any), after the "## LEARNINGS.md Scaffold" section:

```markdown
---

## Consulting Learnings

Shared pattern for reading and surfacing relevant learnings. Referenced by `brainstorm.md` (step 2.5), `bug.md` (step 2.5), and `plan.md` (step 3.5).

### Matching Algorithm

1. Read `docs/LEARNINGS.md`. Filter to entries with `status: active`.
2. Match each learning against the current context using:
   - **Category match:** Compare learning `category` against the affected domain (e.g., a bug in the pipeline executor matches `architecture` and `patterns` learnings)
   - **Keyword overlap:** Check for keyword overlap between the learning title/body and the current topic description (bug description, design doc title, planned modules)
   - **File path match:** If the learning body names specific file paths, check for overlap with the files/modules relevant to the current task
3. Rank by relevance (prefer learnings that match on multiple criteria).
4. Surface the **top 3** most relevant learnings.
5. If LEARNINGS.md doesn't exist or has no active learnings matching the context, skip silently.

### Output Format

When surfacing learnings, use this format:

```
## Relevant Past Learnings

Based on past work in this project:
- **{L-NNN}**: {one-line summary} — {recommendation}
- **{L-NNN}**: {one-line summary} — {recommendation}

These learnings will inform the current task.
```

### Recurrence Detection

When consulting learnings during `/harness:bug`, also check for recurrence: if a learning's recommendation directly addresses the class of bug being investigated, note this explicitly:
- "L-012 recommended always checking X, but this bug is exactly that class — the learning failed to prevent recurrence."
This signals that the learning may need strengthening or that additional guardrails are needed beyond documentation.
```

- [ ] **Step 2: Update brainstorm.md to reference the shared pattern**

In `plugins/harness/commands/brainstorm.md`, replace the inline matching description at step 2.5 with a reference to the shared format. Change lines 24-39 from the full inline description to:

```markdown
2.5. **Surface past learnings** (if available):
   - Follow the consultation pattern defined in `_learnings-format.md` § "Consulting Learnings"
   - Match learnings against the brainstorm topic
   - Surface the top 3 most relevant learnings before starting the brainstorm dialogue (using the output format from `_learnings-format.md`)
   - Record the IDs of consulted learnings for inclusion in the design doc frontmatter (step 3's HARNESS_OVERRIDES `consulted-learnings` field)
   - If LEARNINGS.md doesn't exist or has no active learnings, skip silently
```

- [ ] **Step 3: Commit**

```bash
git add plugins/harness/commands/_learnings-format.md plugins/harness/commands/brainstorm.md
git commit -m "feat(harness): add shared learnings consultation pattern to _learnings-format.md"
```

---

### Task 2: Create references/architecture-review-prompt.md

**Files:**
- Create: `plugins/harness/references/architecture-review-prompt.md`

LLM-facing prompt loaded by bug.md step 4.5. Same pattern as the existing `references/adversarial-review-prompt.md`.

- [ ] **Step 1: Create the reference file**

Create `plugins/harness/references/architecture-review-prompt.md` with this content:

```markdown
# Architecture Review Prompt — Post-Root-Cause Analysis

This prompt is loaded by `/harness:bug` step 4.5 after root cause has been confirmed. It frames the review around one question: **"Why was it possible for this bug to be written, and how do we prevent it in the future?"**

The reviewing agent has access to:
- The confirmed root cause (from systematic debugging)
- The bug analysis document (symptoms, reproduction, evidence, impact)
- The full codebase (via Grep, Glob, Read tools)
- The harness documentation (CLAUDE.md, docs/*.md)

## Instructions

With the root cause confirmed, step back from the specific bug and conduct a systematic review across four dimensions. For each dimension, either produce actionable findings or explicitly state "nothing systemic." Do not force findings where none exist — use the provided "None" templates when a dimension is clean.

### Dimension 1: Systemic Spread

Search the codebase for analogous patterns — the same API misuse, the same incorrect assumption, the same copy-paste lineage that produced this bug. These are instances where the same bug class likely exists but hasn't been reported yet.

**How to search:**
- Grep for the specific pattern that caused the bug (function call, API usage, assumption)
- Check for copy-paste siblings — code that was likely duplicated from the buggy code
- Search for similar control flow patterns in related modules

**Output:** List every instance found with `file:line` references. These become additional fix targets in the plan.

**If nothing found:** `"None — isolated to this call site"`

### Dimension 2: Design Gap

Determine whether the root cause is a symptom of a deeper design problem. A design gap means the system's structure made this bug easy to write — not just that someone made a mistake.

**Indicators of a design gap:**
- Missing abstraction (same logic implemented differently in multiple places)
- Implicit contract (callers must "just know" something that isn't enforced by types or interfaces)
- Wrong layer of responsibility (validation happening in the wrong place)
- Lack of type safety (stringly-typed data where an enum or struct would prevent misuse)
- Missing validation at a boundary (data crosses a trust boundary unchecked)

**Output:** Name the specific design weakness and describe what a better design would look like.

**If design is sound:** `"None — implementation error within sound design"`

### Dimension 3: Testing Gaps

Two sub-dimensions:

**3a. Missing test cases:** What specific test, with what specific input, would have caught this exact bug before it shipped? Be concrete — name the test file, describe the test scenario, specify the assertion.

**3b. Testing infrastructure gaps:** Are there missing test *categories* for the affected area? This is about structural gaps, not individual missing tests. Examples:
- "No integration tests exist for the pipeline executor — only unit tests with mocked dependencies"
- "No table-driven tests covering the validation boundary — each case is tested ad-hoc"
- "No tests exercise the error path in this module at all"
- "No fuzz testing for parser inputs despite accepting user-provided data"

**Output for 3a:** Concrete test descriptions.
**Output for 3b:** Infrastructure gaps, or `"Test coverage for this area is adequate — this was a gap in a specific case, not a structural gap"`

### Dimension 4: Harness Context Gaps

Check whether the harness documentation (`CLAUDE.md`, `docs/ARCHITECTURE.md`, `docs/DESIGN.md`, `docs/LEARNINGS.md`, and other docs referenced in the Documentation Map) accurately describes the affected area.

**What to check:**
- Does CLAUDE.md mention the affected module's key patterns or contracts?
- Does ARCHITECTURE.md describe the module boundaries relevant to this bug?
- Does DESIGN.md cover the design decisions that relate to the root cause?
- Does LEARNINGS.md have prior learnings that should have prevented this bug?
- Are any docs actively misleading about the affected area?

**Output:** Flag which docs are missing, outdated, or misleading and what's wrong. Do NOT fix the docs — just flag them. The fix plan and `/harness:reflect` handle remediation.

**If docs are accurate:** `"Docs accurately describe this area"`

## Output Template

Append findings to the bug analysis document as a new section:

```
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

## Scope Note

This review expands the scope of the fix plan. Every finding is a potential task:
- Systemic spread instances → fix each one
- Design gaps → refactor to close the gap
- Testing gaps → add the missing tests and infrastructure
- Harness context gaps → doc updates (flagged for plan + /harness:reflect)

The architecture review directly shapes how big the fix plan is. A bug that reveals a design gap doesn't just get a patch — it gets a plan that addresses the structural problem.
```

- [ ] **Step 2: Commit**

```bash
git add plugins/harness/references/architecture-review-prompt.md
git commit -m "feat(harness): add architecture review prompt reference for bug flow"
```

---

### Task 3: Create agents/learnings-reviewer.md

**Files:**
- Create: `plugins/harness/agents/learnings-reviewer.md`

Agent definition for the learnings enforcement reviewer. Follow the same pattern as `agents/harness-pruner.md`.

- [ ] **Step 1: Create the agent file**

Create `plugins/harness/agents/learnings-reviewer.md`:

```markdown
---
name: learnings-reviewer
description: Use during /harness:review Phase 4 to check code changes against active learnings from docs/LEARNINGS.md for violations
---

# Learnings Reviewer

You enforce compliance with the project's accumulated learnings. When code changes violate recommendations from past bug investigations, design reviews, or workflow corrections, you flag them.

## Context

`docs/LEARNINGS.md` contains actionable recommendations captured from past sessions — bug root causes, architecture decisions, testing patterns, and workflow corrections. Each learning has an ID (L-NNN), a category, and a forward-looking recommendation.

Your job is to check whether the current diff follows or violates these recommendations.

## Process

1. **Read learnings:** Read `docs/LEARNINGS.md`. Filter to entries with `status: active`. If the file doesn't exist or has no active learnings, return PASS immediately.

2. **Match learnings against the diff using a two-gate filter:**

   **Gate 1 — File relevance:** The learning must reference file paths, modules, or packages that overlap with the diff's changed files. If the learning names no specific paths, match by category against the diff's affected domains:
   - `architecture` learnings match changes to module boundaries, interfaces, data flow
   - `testing` learnings match changes to test files or testable code paths
   - `patterns` learnings match changes using the pattern described in the learning
   - `debugging` learnings match changes in the area where the original bug occurred
   - `performance` learnings match changes to hot paths or resource-sensitive code
   - `workflow` learnings match changes to CI, build, or process files
   - `review-escape` learnings match changes in the area where the original escape occurred

   **Gate 2 — Semantic relevance:** The learning's recommendation must be about the *kind* of change being made, not just the same files. A learning about "always add migration rollbacks" is irrelevant to a comment fix in a migration file. A learning about "update mocks when modifying the executor" is relevant to executor changes that don't update mocks.

3. **Check compliance:** For each learning that passes both gates, determine whether the diff follows or violates the recommendation.

4. **Report findings:** For each violation, report:
   - The learning ID and its recommendation
   - How the diff violates it (specific files and changes)
   - Suggested fix (concrete, not vague)

5. **Verdict:**
   - **PASS:** No violations found
   - **FAIL:** One or more learnings violated — list all violations

## Conservatism Principle

Only report clear violations. If you're unsure whether a learning applies, it doesn't. A learning about database migrations should not fire on unrelated SQL changes. A learning about a specific module should not fire on a different module that happens to share a keyword.

The goal is zero false positives at the cost of occasional false negatives. Noisy enforcement erodes trust faster than missed violations.

## Output Format

```
## Learnings Review

**Learnings checked:** {N active learnings}
**Matched to diff:** {N learnings passed both gates}
**Violations found:** {N}

### Violations

**[L-NNN] {learning title}**
- Recommendation: {what the learning says to do}
- Violation: {how the diff violates it, with file:line references}
- Fix: {concrete suggestion}

### Verdict: {PASS | FAIL}
```
```

- [ ] **Step 2: Commit**

```bash
git add plugins/harness/agents/learnings-reviewer.md
git commit -m "feat(harness): add learnings-reviewer agent for enforcement during review"
```

---

### Task 4: Modify commands/bug.md (steps 2.5, 4.5, 4.7)

**Files:**
- Modify: `plugins/harness/commands/bug.md`

Three changes: add step 2.5 (check prior learnings), add step 4.5 (architecture review), replace step 4.5 with enhanced step 4.7 (multi-dimensional learnings), update the bug analysis doc template, fix the LEARNINGS.md scaffold to include `review-escape`, and update the closing note.

- [ ] **Step 1: Add step 2.5 — Check prior learnings**

After step 2 (line 22, after "Read `docs/bug-analyses/index.md`...") and before step 3, insert:

```markdown

2.5. **Check prior learnings** (if available):
   - Follow the consultation pattern defined in `_learnings-format.md` § "Consulting Learnings"
   - Match learnings against the bug's affected area and symptoms
   - If relevant learnings are found, surface them before starting systematic debugging — they may accelerate diagnosis
   - Also check for recurrence per the "Recurrence Detection" section in `_learnings-format.md`: if a prior learning's recommendation directly addresses this bug class, note it explicitly
   - If LEARNINGS.md doesn't exist or has no matches, skip silently

```

- [ ] **Step 2: Add Architecture Review section to bug analysis template**

In step 4's bug analysis template (the markdown code block), after the `## Recommended Fix Direction` line and before the closing triple backticks, add:

```markdown

    ## Architecture Review

    _Populated by step 4.5 — see below._
```

- [ ] **Step 3: Add step 4.5 — Architecture review**

After step 4's closing triple backticks (line 52) and before the current step 4.5, insert:

```markdown

4.5. **Architecture review** — With root cause confirmed, step back and answer: *"Why was it possible for this bug to be written, and how do we prevent it in the future?"*
   - Read `references/architecture-review-prompt.md` for the detailed review dimensions and instructions
   - Conduct the review across all four dimensions: systemic spread, design gap, testing gaps, harness context gaps
   - Append findings to the bug analysis document, replacing the placeholder `## Architecture Review` section with the completed findings
   - Each dimension produces actionable findings or an explicit "nothing systemic" signal — no forced output, but the section is always written (use "None" templates when clean)
   - These findings directly expand the scope of the fix plan created by `/harness:plan`

```

- [ ] **Step 4: Replace step 4.5 (learnings) with enhanced step 4.7**

Replace the entire current step 4.5 (lines 54-81, from "4.5. **Write learning from root cause:**" through "- The learning must be actionable...") with:

```markdown
4.7. **Write learnings from root cause + architecture review:**
   - Produce one learning per dimension that has actionable findings, using the categories below. Follow the `_learnings-format.md` spec for format, IDs, and scaffold.
   - Check if `docs/LEARNINGS.md` exists. If not, create it with the scaffold from `_learnings-format.md` § "LEARNINGS.md Scaffold".
   - Determine the next learning ID by scanning existing `### L-NNN` headers.

   | Finding dimension | Learning category |
   |-------------------|-------------------|
   | Systemic spread | `patterns` |
   | Design gap | `architecture` |
   | Testing gaps | `testing` |
   | Root cause itself | `debugging` |
   | Harness context gaps | No learning (flagged for the plan) |

   - Only write a learning if the finding is actionable. "None — isolated to this call site" produces no learning for that dimension.
   - `/harness:bug` intentionally does not produce `review-escape` category learnings — review escapes are detected by `/harness:reflect`'s Review Escape Mining phase.
   - Each learning must be forward-looking: "When doing X, always check Y because Z." Not just "X was broken because of Y."
   - Source field: `/harness:bug {YYYY-MM-DD}`
```

- [ ] **Step 5: Update the closing note**

Replace the final line:
```
**IMPORTANT:** Do NOT attempt to fix the bug during investigation. The bug command produces a diagnosis; `/harness:plan` turns it into an executable fix plan.
```

With:
```
**IMPORTANT:** Do NOT attempt to fix the bug during investigation. The bug command produces a diagnosis + architecture review; `/harness:plan` turns it into an executable fix plan that addresses the instance, systemic spread, and missing guardrails.
```

- [ ] **Step 6: Commit**

```bash
git add plugins/harness/commands/bug.md
git commit -m "feat(harness): add architecture review step and multi-dimensional learnings to bug flow"
```

---

### Task 5: Modify commands/plan.md (step 3.5)

**Files:**
- Modify: `plugins/harness/commands/plan.md`

Add step 3.5 — learnings consultation before planning begins. Also add `Consulted Learnings` to the plan header template.

- [ ] **Step 1: Add step 3.5 — Consult learnings**

After step 3 (ends around line 36, after the refactor scope bullet) and before step 4 ("Invoke superpowers:writing-plans"), insert:

```markdown

3.5. **Consult learnings** (if available):
   - Follow the consultation pattern defined in `_learnings-format.md` § "Consulting Learnings"
   - Match learnings against the modules and areas being planned (extracted from the design document in step 3)
   - Surface relevant learnings so the plan can incorporate their recommendations
   - Record consulted learning IDs for inclusion in the plan header (step 5)
   - If LEARNINGS.md doesn't exist or has no matches, skip silently

```

- [ ] **Step 2: Add Consulted Learnings to plan header template**

In step 5's plan template (the markdown code block), after the `> **Design Doc**:` line, add:

```markdown
   > **Consulted Learnings**: {L-NNN, L-NNN from step 3.5, or "None"}
```

- [ ] **Step 3: Commit**

```bash
git add plugins/harness/commands/plan.md
git commit -m "feat(harness): add learnings consultation step to plan flow"
```

---

### Task 6: Modify commands/review.md (add 6th agent)

**Files:**
- Modify: `plugins/harness/commands/review.md`

Add the learnings-reviewer as a 6th agent in Phase 4, update the agent count references, and update the report template.

- [ ] **Step 1: Add learnings-reviewer to the agent table**

In Phase 4 step 14a (line 160, after the Comment Analyzer row), add a new row:

```markdown
   | Learnings Reviewer | `harness:learnings-reviewer` | Checks diff against active learnings for violations |
```

- [ ] **Step 2: Update agent count in Phase 4**

Change line 148 from:
```
13. Set `cycle = 1`, `max_cycles = 3`, `failing_agents = all 5 review agents`.
```
To:
```
13. Set `cycle = 1`, `max_cycles = 3`, `failing_agents = all 6 review agents`.
```

- [ ] **Step 3: Update the report template**

Change line 236 from:
```
    **Agents:** {passed}/5 passed
```
To:
```
    **Agents:** {passed}/6 passed
```

Add a new row to the Per-Agent Results table (after line 255, the comment-analyzer row):

```markdown
    | learnings-reviewer | pass | 0 | 0 |
```

- [ ] **Step 4: Commit**

```bash
git add plugins/harness/commands/review.md
git commit -m "feat(harness): add learnings-reviewer as 6th agent in review loop"
```

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
- Parallel agent dispatch for independent tasks (1-3 and 4-6) was efficient
- DRY extraction from eng review caught the learnings consultation duplication early
- Cross-reference verification caught the plugin.json registration gap

**What didn't:**
- Initial one-shot implementation was reverted — should have brainstormed first

**Learnings to codify:**
- Plugin version sync (CLAUDE.md key pattern) applies when adding agents, not just modifying code
- Markdown prompt changes still need cross-reference verification even though they can't break tests
