---
description: Use when running the full implementation cycle autonomously after a brainstorm/bug/refactor session, when user says "loop", "run it all", "implement end to end", or "take it from here"
---

# Loop

Autonomous end-to-end execution: plan, orchestrate, review, fix, reflect, and complete — with no user checkpoints. Surfaces a decision summary at the end.

**Prerequisite:** A design document must already exist (from `/harness:brainstorm`, `/harness:bug`, or `/harness:refactor`).

**"Execute inline"** means: follow that command's instructions directly within this conversation, applying any LOOP_OVERRIDES specified here. Do not spawn a separate invocation of the command.

## Usage

```
/harness:loop                                    # Loop from most recent design doc
/harness:loop docs/design-docs/{file}.md         # Loop from specific design doc
```

## Invocation

**IMMEDIATELY execute this workflow:**

### Phase 1: Locate Design & Initialize Decision Log

1. Locate the design document:
   - If a path argument was provided, use it
   - Otherwise, search for the most recently modified context document across:
     - `docs/design-docs/*-design.md`
     - `docs/bug-analyses/*-bug-analysis.md`
     - `docs/refactor-scopes/*-refactor-scope.md`
   - If no context document found, STOP and tell the user to run `/harness:brainstorm`, `/harness:bug`, or `/harness:refactor` first

2. Read the design document. Verify it has a **goal**, **approach**, and **key decisions** section. If any are missing, STOP and tell the user the design doc appears incomplete.

2.5. **Detect multi-goal mode:** If the located document contains a `## Goals` section with a dependency table, it is a refactor scope doc. Switch to multi-goal mode:
   - Extract the goal dependency graph from the Goals table
   - Read each individual goal design doc from the scope's sub-directory (`docs/refactor-scopes/{scope-name}/`)
   - Determine the default branch: `git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's|refs/remotes/origin/||'` — fall back to checking for `main` then `master` if that fails
   - Skip to "Multi-Goal Execution" (below Phase 9)

3. Derive a **slug** from the design doc filename for use in all loop artifacts. Strip the suffix (`-design.md`, `-bug-analysis.md`, `-refactor-scope.md`) to get the slug. For example, `docs/design-docs/auth-refactor-design.md` → slug `auth-refactor`.

4. Create the **Loop Decision Log** file at `docs/exec-plans/active/.loop-decision-log-{slug}.md`:
   ```markdown
   # Loop Decision Log: {slug}
   | Phase | Decision | Rationale | Alternatives Considered |
   |-------|----------|-----------|------------------------|
   ```
   Append to this file after every autonomous decision throughout the loop. This persists across context and provides recovery state if the loop crashes. The slug-based path ensures multiple concurrent loops do not collide.

### Phase 2: Plan

5. Execute `/harness:plan` inline, passing the design doc path. Plan executes autonomously by default and requires no overrides.

6. Append to decision log: which design doc was used, any ambiguities resolved during planning.

7. Note the plan file path in `docs/exec-plans/active/` for subsequent phases.

### Phase 3: Orchestrate (Autonomous)

8. Execute `/harness:orchestrate` inline with the following overrides:

   <LOOP_OVERRIDES>
   These overrides REPLACE conflicting instructions from /harness:orchestrate:

   - **No checkpoints:** Do not output checkpoint status blocks (Phase 6 steps 10-11 of orchestrate). Do not wait for user input between tasks. This overrides the Orchestrator Rule "Report progress at checkpoints and wait for user feedback." Proceed immediately to the next task after updating the living plan.
   - **Auto-resolve blockers:** If a worker reports a surprise or blocker, append it to the decision log and make a judgment call:
     - **Fundamental** (would require changing the design doc's approach or affects 3+ unstarted tasks): STOP the loop via Emergency Stop
     - **Tactical** (unexpected API shape, missing test fixture, localized workaround): resolve and continue
   - **Decision logging:** For every non-trivial decision, append to the Loop Decision Log file.
   </LOOP_OVERRIDES>

9. When orchestrate completes, append summary to decision log: tasks completed, surprises, drift entries.

### Phase 4: Review

10. Execute `/harness:review` inline with the following overrides:

   <LOOP_OVERRIDES>
   These overrides REPLACE conflicting instructions from /harness:review:

   - **No resolution prompt:** Do not present the Phase 5 "Which approach?" options (review.md steps 8-9). Do not wait for user selection. Findings are auto-triaged as described below.
   </LOOP_OVERRIDES>

11. **Auto-triage findings** using this rule:
    - **Minor** (affects only readability, maintainability, or style — e.g., naming, comments, simplification, formatting): fix inline immediately
    - **Significant** (could cause incorrect behavior at runtime or in production — e.g., logic bugs, missing tests, security concerns, silent failures, inadequate error handling): collect into a fix task list
    - **When in doubt, treat as significant.**
    - Append each triage decision to the decision log with rationale

12. If significant issues exist, proceed to Phase 5. Otherwise, skip to Phase 6.

### Phase 5: Fix Cycle (max 2 iterations)

13. Set `fix_iteration = 1`. Create a lightweight fix plan at `docs/exec-plans/active/.loop-fix-plan-{slug}.md`:
    ```markdown
    # Fix Plan (Loop Iteration {fix_iteration})

    > **Status**: Active | **Created**: {date}
    > **Source**: Review findings from /harness:loop

    ## Progress

    - [ ] Fix 1: {finding description}
    - [ ] Fix 2: {finding description}
    ...

    ---

    ### Task 1: {finding description}
    **File:** {file path}
    **Finding:** {what the reviewer found}
    **Fix:** {what needs to change}

    ### Task 2: ...
    ```

14. Execute `/harness:orchestrate` inline with:
    - The fix plan path (not the original plan)
    - Same `LOOP_OVERRIDES` as Phase 3 (no checkpoints, auto-resolve, decision logging)

15. Re-run `/harness:review` inline (with Phase 4 LOOP_OVERRIDES) scoped to the fix commits only.

16. If new significant issues are found AND `fix_iteration == 1`:
    - Set `fix_iteration = 2`
    - Append to decision log: "Entering fix cycle iteration 2"
    - Repeat steps 13-15

17. If issues remain after iteration 2:
    - Append all unresolved issues to the decision log as "deferred"
    - Do NOT enter a third cycle — proceed to Phase 6

18. Clean up: delete `docs/exec-plans/active/.loop-fix-plan-{slug}.md`

### Phase 6: Reflect (Autonomous)

19. Execute `/harness:reflect` inline with the following overrides:

    <LOOP_OVERRIDES>
    These overrides REPLACE conflicting instructions from /harness:reflect:

    - **Skip user retrospective prompt:** Do not ask "What worked well? What didn't?" (reflect Phase 6 step 15). Instead, auto-fill the Outcomes & Retrospective section using:
      - The Loop Decision Log file
      - Conversation-mined learnings
      - Summary of review findings and how they were resolved
    - **Auto-approve doc fixes:** Apply all staleness fixes and doc updates without asking for confirmation. For file deletions: add a tombstone note to the file AND append the path to the decision log — do not delete the file.
    </LOOP_OVERRIDES>

20. Check if prune is warranted:
    - Did reflect detect deleted-code staleness?
    - Has the project not been pruned in 30+ days? Check: `git log --all --oneline --grep="prune" --since="30 days ago"`
    - If either condition is true, execute `/harness:prune --fix` inline
    - Append decision to log: whether prune was run and why

### Phase 7: Complete (Autonomous)

21. Execute `/harness:complete` inline with the following overrides:

    <LOOP_OVERRIDES>
    These overrides REPLACE conflicting instructions from /harness:complete:

    - **No plan selection prompt:** Do not prompt for plan selection (complete Phase 1 step 2). Use the plan created in Phase 2 at the noted path. If other active plans exist, ignore them.
    - **Auto-proceed past reflect check:** Reflect was already run in Phase 6 — skip the "suggest running /harness:reflect" gate (complete Phase 1 step 3)
    - **Skip prune health check:** Do not run Phase 6 of complete (prune was already handled in this loop's Phase 6 step 19)
    </LOOP_OVERRIDES>

### Phase 8: PR Decision

22. Determine whether to open a PR by checking:
    - **Remote exists?** `git remote -v` — if no remote, skip PR
    - **Not on main/master?** If on main/master, skip PR (work is already on trunk)
    - **PR workflow present?** Check for `.github/` directory or CLAUDE.md mentions of PR requirements
    - **Project convention?** Check if CLAUDE.md has PR instructions

23. If conditions favor a PR:
    - Try invoking `/pr:author`. If the command is not recognized, fall back to `gh pr create`
    - Append to decision log: "Opened PR — {rationale}"

24. If conditions are unclear or unfavorable:
    - Append to decision log: "Skipped PR — {rationale}"
    - Include recommendation in the final summary

### Phase 9: Final Summary

**Note:** If in multi-goal mode, skip this section — use the Multi-Goal Summary above instead.

25. Read the full Loop Decision Log file. Output the complete summary:

    ```
    ## Loop Complete

    **Design:** {design doc path}
    **Plan:** {archived plan path}

    ### Execution Summary
    - Tasks: {completed} completed, {deviated} deviated
    - Review cycles: {1 or 2}
    - Review findings: {total} total, {resolved} resolved, {deferred} deferred
    - Prune: {ran — health status | skipped}

    ### Decisions Made ({total} total)

    {Full decision log table, grouped by phase}

    ### Deferred Items
    - {any unresolved review findings}
    - {any file deletions that were logged instead of executed}
    - {or "None"}

    ### PR
    - {PR #{number} — {url}}
    - {or "Skipped — {reason}. Run `/pr:author` to create manually."}
    ```

26. Clean up: delete `docs/exec-plans/active/.loop-decision-log-{slug}.md`

## Multi-Goal Execution

When loop receives a refactor scope doc (detected by `## Goals` table), it executes each goal through the full pipeline independently.

### Goal Graph Traversal

1. **Determine goal readiness:** For each goal in the dependency graph:
   - Check if all dependencies are complete (merged PR or completed in this session)
   - Goals with no unmet dependencies are "ready"

2. **Execute ready goals:**
   - If multiple goals are ready simultaneously (parallel), execute them concurrently:
     - Each goal runs in a separate worktree (via EnterWorktree)
     - Worktrees branch from the default branch, or from the prior goal's branch if sequential
   - If only one goal is ready, execute it in the current worktree

3. **Per-goal pipeline:** For each goal, run the full loop pipeline:
   - Phase 2: Plan (from the goal's individual design doc)
   - Phase 3: Orchestrate (with LOOP_OVERRIDES — no checkpoints, auto-resolve)
   - Phase 4: Review (with LOOP_OVERRIDES — auto-triage)
   - Phase 5: Fix cycle (if needed)
   - Phase 6: Reflect (with LOOP_OVERRIDES — auto-approve doc fixes)
   - Phase 7: Complete
   - Phase 8: PR (create PR for this goal)

4. **After each goal completes:**
   - Update the refactor scope doc: mark the goal as complete in a new `## Goal Status` section
   - Re-evaluate the dependency graph for newly unblocked goals
   - Continue until all goals are complete or a fundamental blocker is hit

### Branch Strategy

- **Default branch detection:** `git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's|refs/remotes/origin/||'` with fallback to `main` then `master`
- **Independent goals (parallel):** Each worktree branches from the default branch
- **Sequential goals:** Branch from the prior goal's branch
  ```
  Goal 1: branches from default branch
  Goal 2 (depends on 1): branches from goal-1's branch
  Goal 3 (depends on 1): branches from goal-1's branch (parallel with goal 2)
  Goal 4 (depends on 2, 3): branches from the default branch after all dependencies are merged
  ```

### Multi-Goal Summary

When all goals complete (or the loop stops), output:

```
## Refactor Loop Complete

**Scope:** {scope doc path}

### Goals
| # | Goal | Status | PR |
|---|------|--------|----|
| 1 | {name} | complete | #{number} |
| 2 | {name} | complete | #{number} |
| 3 | {name} | blocked | — |

### Execution Summary
- Goals: {completed}/{total} complete
- Total review cycles: {N}
- Total decisions: {N}

### Decisions Made
{Full decision log table}

### Deferred Items
- {any unresolved items}
- {or "None"}
```

## Emergency Stop

If at ANY point during the loop:
- Verification fails and cannot be auto-fixed after 2 attempts
- A worker reports a fundamental blocker (would require changing the design approach or affects 3+ unstarted tasks)
- The fix cycle produces more issues than it resolves
- A required file (plan, design doc) cannot be read or is corrupted
- A tool call returns an unrecoverable error (permission denied, authentication failure)

Then STOP the loop immediately and output:

```
## Loop Stopped

**Phase:** {which phase failed}
**Reason:** {what went wrong}
**State:** {what has been completed so far}
**Decision Log:** {read and include full contents of .loop-decision-log-{slug}.md}

The plan remains active at: docs/exec-plans/active/{file}

To resume manually:
- Fix the issue
- Run `/harness:orchestrate` to continue execution
- Then `/harness:review` → `/harness:reflect` → `/harness:complete`
```
