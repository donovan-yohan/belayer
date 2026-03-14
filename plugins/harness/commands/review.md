---
description: Use when implementation is done and code needs quality review, when user says "review the code", "check the code", or after /harness:orchestrate completes all tasks
---

# Review

Code simplification and multi-perspective review on local changes. Run after `/harness:orchestrate`, before `/harness:reflect`.

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

### Phase 3: Code Simplification

4. Spawn a `code-simplifier:code-simplifier` agent (or invoke `/simplify` if available) scoped to the changed files. Let it make improvements directly.

5. If the simplifier made changes, commit them and re-run verification:
   ```bash
   git add {changed files}
   git commit -m "refactor: simplify code from review"
   ```
   Verification must still pass after simplification.

### Phase 4: Multi-Perspective Code Review

6. Spawn all of the following agents **concurrently** via the Task tool, passing each the full diff and the list of changed files:

   | Agent | Focus |
   |-------|-------|
   | `pr-review-toolkit:code-reviewer` | Code quality, bugs, logic errors, style guide adherence |
   | `pr-review-toolkit:silent-failure-hunter` | Silent failures, inadequate error handling, inappropriate fallbacks |
   | `pr-review-toolkit:pr-test-analyzer` | Test coverage completeness, missing edge cases |
   | `pr-review-toolkit:type-design-analyzer` | Type design quality, encapsulation, invariant expression |
   | `pr-review-toolkit:comment-analyzer` | Comment accuracy, staleness, long-term maintainability |
   | `pr-review-toolkit:code-simplifier` | Unnecessary complexity, DRY violations, code smells |

   **Important:** These agents review the **local diff**, not a remote PR. Do not use `gh pr view` or PR numbers.

   Wait for all agents to complete before proceeding.

7. Aggregate findings and present a consolidated review summary:
   ```
   ## Code Review Findings

   **Agents run:** {completed}/{total} completed
   **Total findings:** {total_findings}

   ### [agent-name] ({agent_findings} findings)
   - **file:line** — description
   ...
   ```

### Phase 5: Resolution

8. If findings need action, present options:
   ```
   ## Review found {N} issues

   Options:
   1. Fix now — address findings inline (for minor issues)
   2. `/harness:orchestrate` — create tasks for significant fixes
   3. Defer — proceed to `/harness:reflect` with findings noted

   Which approach?
   ```

9. If user chooses to fix now, apply fixes. **Re-run verification after fixes** — do not claim fixes are complete without fresh evidence.

### Report

10. Output:
    ```
    ## Review Complete

    **Scope:** {diff description}
    **Verification:** {passing — with evidence}
    **Simplification:** {changes made | no changes needed}
    **Review findings:** {N total across all agents}
    **Resolved:** {N fixed | N deferred}

    ## Next Step

    Run `/harness:reflect` to capture learnings, update docs, and run the retrospective.
    ```
