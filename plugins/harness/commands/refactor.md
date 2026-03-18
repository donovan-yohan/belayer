---
description: Use when planning an incremental refactoring, extracting responsibilities from a large class, or when user says "refactor", "extract", "strangler fig", or "decompose"
---

# Refactor

Scope an incremental refactoring using the Strangler Fig pattern, saved as a versioned refactor scope document with a goal dependency graph and individual goal design docs for multi-session execution.

## Usage

```
/harness:refactor                                    # Start refactoring scoping
/harness:refactor extract auth from UserController   # With initial target
```

## Invocation

**IMMEDIATELY execute this workflow:**

1. Verify the project has been initialized (check for "Documentation Map" with "When to look here" column in CLAUDE.md). If not, suggest running `/harness:init` first.

2. Read `docs/refactor-scopes/index.md` to understand prior refactoring efforts. If the directory doesn't exist, create it with an empty index.

3. **Invoke the `harness:strangler-fig` skill** with the user's refactoring target. Follow the full interactive scoping dialogue through all three phases: identify extraction target, dependency & seam analysis, and map the strangler fig steps.

4. When scoping is complete, save the refactor scope doc to `docs/refactor-scopes/{YYYY-MM-DD}-{kebab-name}-refactor-scope.md`:

   ```markdown
   # Refactor Scope: {Title}

   > **Status**: Scoped | **Date**: {date}
   > **Refactor Type**: {backend extraction / frontend extraction / pure code / mixed}
   > **Target**: {the code being refactored}

   ## Current State
   - {what the target does today, its responsibilities}
   - {why it needs refactoring — size, complexity, coupling}

   ## Extraction Targets
   - {responsibility 1} → {new module/component}
   - {responsibility 2} → {new module/component}

   ## Dependency Map
   - **Consumers**: {who calls/imports the target}
   - **Dependencies**: {what the target depends on}
   - **Seams identified**: {natural boundaries for coexistence}

   ## Risk Assessment
   - {blast radius}
   - {what could break during transition}
   - {mitigation strategies}

   ## Goals

   | # | Goal | Description | Depends On | Production Safety |
   |---|------|-------------|------------|-------------------|
   | 1 | {kebab-name} | {one-line summary} | — | {feature flag / unconsumed scaffolding / additive-only / safe deletion} |
   | 2 | {kebab-name} | {one-line summary} | 1 | {strategy} |
   | 3 | {kebab-name} | {one-line summary} | 1 | {strategy} |
   | 4 | {kebab-name} | {one-line summary} | 2, 3 | {strategy} |

   ### Execution Order
   - {describe which goals can run in parallel and which are sequential}
   - {e.g., "Goals 2 and 3 can run in parallel (both depend only on goal 1)"}

   ### Merge Criteria
   {Criteria the reviewer should verify before merging each goal's PR. These are advisory — the agent writes all the code upfront.}
   ```

5. Create individual goal design docs in `docs/refactor-scopes/{scope-name}/` (where `{scope-name}` is the kebab name used in the scope file). Each goal gets its own design doc named `{N}-{goal-kebab-name}-design.md`:

   ```markdown
   # {Goal Title}

   > **Status**: Ready | **Date**: {date}
   > **Refactor Scope**: [{scope name}](../{date}-{scope-name}-refactor-scope.md)
   > **Goal**: {N} of {total}
   > **Depends On**: {goal numbers or "none"}
   > **Branch Base**: {default branch / goal-N branch name}

   ## Goal
   {What this goal accomplishes — one production-safe unit of work}

   ## Production Safety
   {How this PR is safe to merge: feature flag, unconsumed scaffolding, additive-only, etc.}

   ## Approach
   {Implementation details — files to create/modify, patterns to follow}

   ## Rollback
   {How to reverse this goal's changes if needed}

   ## Merge Criteria
   {Conditions to verify before merging this PR — tests pass, no regressions, etc. Advisory documentation for the human reviewer.}

   ## Key Decisions
   | Decision | Rationale |
   |----------|-----------|
   | {decision} | {why} |
   ```

   For the **Branch Base** field: Goal 1 uses "default branch". Each subsequent goal that depends on a prior goal uses that prior goal's branch name as its base.

6. Update `docs/refactor-scopes/index.md` — add a row for the new scope:
   ```markdown
   | [{name}-refactor-scope.md]({date}-{name}-refactor-scope.md) | {one-line summary} | {date} |
   ```

7. Guide the user to next steps:
   ```
   Refactor scope saved to: docs/refactor-scopes/{filename}.md
   Goal design docs saved to: docs/refactor-scopes/{scope-name}/

   ## Next Steps

   1. `/harness:loop docs/refactor-scopes/{date}-{scope}-refactor-scope.md` — Execute all goals with full pipeline per goal
   2. `/harness:loop docs/refactor-scopes/{scope}/{goal}-design.md` — Execute a single goal
   3. `/harness:refactor-status` — Check progress between sessions

   Run `/harness:loop docs/refactor-scopes/{date}-{scope}-refactor-scope.md` to begin.
   ```

**IMPORTANT:** When the strangler-fig skill transitions to output, do NOT invoke writing-plans directly. Instead, output the Next Steps above and let the user invoke `/harness:loop` or `/harness:plan`.
