---
description: Use when implementation is done and code needs quality review, when user says "review the code", "check the code", or after /harness:orchestrate completes all tasks
---

# Review

Multi-agent review loop on local changes using pr-review-toolkit agents. Runs all review agents in parallel, fixes issues, and re-runs failing agents until all pass or max cycles reached. Run after `/harness:orchestrate`, before `/harness:complete`.

## Usage

```
/harness:review                    # Review changes since active plan creation
/harness:review HEAD~5             # Review last 5 commits
```

## Prerequisites

This command requires the **pr-review-toolkit** plugin. If not installed, STOP and print:
```
ERROR: Missing required plugin: pr-review-toolkit

Install it:
  /plugins add pr-review-toolkit
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

### Phase 3: Review Loop

4. Set `cycle = 1`, `max_cycles = 3`, `failing_agents = all 5 review agents`.

5. **Review cycle loop** — repeat until all pass or `cycle > max_cycles`:

   a. Spawn each agent in `failing_agents` **in parallel** via the Agent tool, passing each the git diff from Phase 1. Use these pr-review-toolkit agent types:

   | Agent | `subagent_type` | Focus |
   |-------|----------------|-------|
   | Code Reviewer | `pr-review-toolkit:code-reviewer` | Code quality, bugs, logic errors, CLAUDE.md adherence |
   | Silent Failure Hunter | `pr-review-toolkit:silent-failure-hunter` | Silent failures, error handling, inappropriate fallbacks |
   | Test Analyzer | `pr-review-toolkit:pr-test-analyzer` | Test coverage completeness, missing edge cases |
   | Type Design Analyzer | `pr-review-toolkit:type-design-analyzer` | Type design, encapsulation, invariant expression |
   | Comment Analyzer | `pr-review-toolkit:comment-analyzer` | Comment accuracy, staleness, maintainability |

   Each agent prompt should include:
   - The full diff (from Phase 1 scope)
   - The changed file list
   - Instruction: "Review these local changes (not a PR). Return your findings in your standard format. End with a verdict: PASS (no critical/important issues) or FAIL (critical/important issues found)."

   b. Wait for all agents to complete. Collect results.

   c. Determine pass/fail for each agent:
      - **PASS**: No critical or important issues reported
      - **FAIL**: Critical or important issues found (code-reviewer confidence ≥80, test-analyzer criticality ≥7, type-design-analyzer ratings ≤4, silent-failure-hunter CRITICAL/HIGH, comment-analyzer Critical Issues)

   d. If all pass → exit loop.

   e. If any fail:
      - Present findings grouped by agent
      - Fix all reported issues inline (edit files directly)
      - Re-run verification (tests/build must still pass after fixes)
      - Commit fixes: `git commit -am "fix: address review findings (cycle {cycle})"`
      - Set `failing_agents = only agents that failed`
      - Increment `cycle`

6. After loop exits:
   - If all passed: review is green
   - If max cycles reached with failures remaining: note unresolved issues

### Phase 4: Code Simplification

7. Spawn the `pr-review-toolkit:code-simplifier` agent scoped to the changed files. This agent modifies code directly — let it make improvements.

8. If the simplifier made changes, commit and re-run verification:
   ```bash
   git add {changed files}
   git commit -m "refactor: simplify code from review"
   ```

### Phase 5: ADR Compliance

9. Check if `docs/ARCHITECTURE.md` exists in the project.

10. **If it exists:**
    - Run `/adr:review` against the diff from Phase 1
    - Report any CRITICAL or WARNING violations
    - If the diff introduces new architectural patterns that aren't covered by existing ADRs, note them for `/harness:reflect` — do NOT create new ADRs during review. The bar for flagging is high: only note patterns that represent genuinely new architectural decisions, not routine implementation choices.

11. **If it does not exist:** Skip silently.

### Phase 6: Resolution

12. If unresolved issues remain after max cycles:
    - Present options:
      ```
      ## Review: {N} unresolved issues after {max_cycles} cycles

      Options:
      1. Fix now — address remaining findings inline
      2. `/harness:orchestrate` — create tasks for significant fixes
      3. Defer — proceed with findings noted

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
    **Agents:** {passed}/5 passed
    **Simplification:** {changes made | no changes needed}
    **ADR compliance:** {N violations | compliant | skipped}

    ### Per-Agent Results
    | Agent | Status | Issues Found | Issues Resolved |
    |-------|--------|-------------|-----------------|
    | code-reviewer | pass | 2 | 2 |
    | silent-failure-hunter | pass | 1 | 1 |
    | pr-test-analyzer | pass | 0 | 0 |
    | type-design-analyzer | pass | 0 | 0 |
    | comment-analyzer | fail | 3 | 1 |

    ### Unresolved Issues
    - {any remaining issues, or "None"}

    ## Next Step

    Run `/harness:complete` to archive the plan and commit all changes.
    ```
