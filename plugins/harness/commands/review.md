---
description: Use when implementation is done and code needs quality review, when user says "review the code", "check the code", or after /harness:orchestrate completes all tasks
---

# Review

Multi-persona review loop on local changes. Runs review personas in parallel, fixes issues, and re-runs failing personas until all pass or max cycles reached. Run after `/harness:orchestrate`, before `/harness:complete`.

## Usage

```
/harness:review                    # Review changes since active plan creation
/harness:review HEAD~5             # Review last 5 commits
```

## Invocation

**IMMEDIATELY execute this workflow:**

### Phase 1: Determine Scope

1. Determine the diff scope:
   - If a commit reference was provided, use it
   - Otherwise, check for an active plan in `docs/exec-plans/active/`. If one exists, diff from its creation date.
   - Fallback: diff the last 5 commits

2. Gather the full diff and changed file list:
   ```bash
   git diff HEAD~{N} --stat
   git diff HEAD~{N} --name-only
   ```

### Phase 2: Verification Gate

3. **Apply `superpowers:verification-before-completion`** — run the project's verification commands (tests, build, lint, typecheck) BEFORE starting review. If verification fails, STOP and fix first. Do not review broken code.

### Phase 3: Persona Discovery

4. Look for `review-personas.toml` in the repo root. This file defines the review personas for this repo.

5. If `review-personas.toml` is missing:
   - Detect repo type:
     - `go.mod` present → backend
     - `package.json` with React/Vue/Next/Svelte → frontend
     - `package.json` with `bin` field → CLI
     - `go.mod` with no `main` package → library
     - Default → backend
   - Read the matching template from `internal/defaults/personas/{type}.toml` (if in belayer repo) or use the built-in defaults below
   - Generate `review-personas.toml` in the repo root and commit it:
     ```bash
     git add review-personas.toml
     git commit -m "chore: add review personas config"
     ```

6. Parse the personas file. Each persona has: `description`, `focus` (list of areas), `docs` (list of doc paths to read).

### Phase 4: Multi-Persona Review Loop

7. Set `cycle = 1`, `max_cycles = 3` (or read from belayer.toml `[review_loop].max_review_cycles` if available), `failing_personas = all_personas`.

8. **Review cycle loop** — repeat until all pass or `cycle > max_cycles`:

   a. For each persona in `failing_personas`, spawn a Claude Code subagent (via Agent tool) **in parallel**. Each subagent receives:
      - The persona's description and focus areas
      - The git diff (from Phase 1 scope)
      - The contents of any docs listed in the persona's `docs` field
      - The test contract from the active plan's design doc (if available)
      - Instruction: "Review this diff from the perspective of {persona.description}. Focus on: {persona.focus}. Return JSON: `{ \"pass\": true/false, \"issues\": [{ \"file\": \"...\", \"line\": N, \"description\": \"...\", \"severity\": \"error|warning\" }] }`"

   b. Wait for all subagents to complete. Collect results.

   c. Separate passing and failing personas.

   d. If all pass → exit loop.

   e. If any fail:
      - Present findings grouped by persona
      - Fix all reported issues inline (edit files directly)
      - Re-run verification (tests/build must still pass after fixes)
      - Commit fixes: `git commit -am "fix: address {persona} review findings (cycle {cycle})"`
      - Set `failing_personas = only personas that failed`
      - Increment `cycle`

9. After loop exits:
   - If all passed: review is green
   - If max cycles reached with failures remaining: note unresolved issues

### Phase 5: Code Simplification

10. After the persona loop, spawn a `code-simplifier:code-simplifier` agent (or invoke `/simplify`) scoped to the changed files. Let it make improvements.

11. If the simplifier made changes, commit and re-run verification:
    ```bash
    git add {changed files}
    git commit -m "refactor: simplify code from review"
    ```

### Phase 6: Resolution

12. If unresolved persona issues remain after max cycles:
    - Present options:
      ```
      ## Review: {N} unresolved issues after {max_cycles} cycles

      Options:
      1. Fix now — address remaining findings inline
      2. `/harness:orchestrate` — create tasks for significant fixes
      3. Defer — proceed with findings noted (TOP.json will be marked review_incomplete)

      Which approach?
      ```

13. If user chooses to fix now, apply fixes and re-run verification.

### Report

14. Output:
    ```
    ## Review Complete

    **Scope:** {diff description}
    **Verification:** {passing — with evidence}
    **Review cycles:** {N} of {max_cycles}
    **Personas:** {passed}/{total} passed
    **Simplification:** {changes made | no changes needed}

    ### Per-Persona Results
    | Persona | Status | Issues Found | Issues Resolved |
    |---------|--------|-------------|-----------------|
    | architect | pass | 2 | 2 |
    | test-engineer | pass | 1 | 1 |
    | domain-expert | pass | 0 | 0 |
    | code-quality | fail | 3 | 1 |

    ### Unresolved Issues
    - {any remaining issues, or "None"}

    ## Next Step

    Run `/harness:complete` to archive the plan and commit all changes.
    ```

## Built-in Default Personas

If no template is available and the repo type cannot be determined, use these defaults:

```toml
[personas.test-engineer]
description = "Reviews test coverage, test quality, and test contract compliance"
focus = ["test coverage", "edge cases", "test contract satisfaction", "test isolation"]
docs = []

[personas.domain-expert]
description = "Reviews business logic correctness and spec compliance"
focus = ["acceptance criteria", "edge cases from spec", "domain invariants"]
docs = []

[personas.code-quality]
description = "Reviews code style, performance, and maintainability"
focus = ["naming", "complexity", "performance", "error handling"]
docs = []
```
