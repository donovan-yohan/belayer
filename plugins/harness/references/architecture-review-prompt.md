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
