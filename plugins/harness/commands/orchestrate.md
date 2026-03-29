---
description: Use when executing a living plan with agent teams, or when user says "orchestrate", "execute the plan", "start building", or "run the plan"
---

# Orchestrate

Execute a living plan using subagent-driven development with per-task living plan updates, micro-reviews, and user checkpoints.

## Usage

```
/harness:orchestrate
/harness:orchestrate docs/exec-plans/active/{file}.md
```

## Invocation

**IMMEDIATELY execute this workflow:**

### Phase 1: Load Plan

0.1. **Read run-state** (if `.harness/` runtime exists):
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-read-state.sh --harness-dir "$HARNESS_DIR"
    ```
    Use the run-state to auto-detect the active plan if no argument was provided.

1. Locate the plan:
   - If a path argument was provided, use it
   - Otherwise, list `docs/exec-plans/active/` and use the most recently modified file
   - If no active plans exist, suggest running `/harness:brainstorm` → `/harness:plan` first

2. Read the full plan. Extract:
   - Task list (from `### Task N:` headers or Progress checklist)
   - Progress section (which tasks are already completed)
   - Current state of Surprises, Drift, and Decision Log tables

3. If any tasks are already marked complete in Progress, skip them. Resume from the first incomplete task.

### Phase 2: Execute with Subagent-Driven Development

4. **Invoke `superpowers:subagent-driven-development`** using the Skill tool: `Skill("superpowers:subagent-driven-development")`. Then follow the loaded skill's full process (dispatch implementer subagents per task, spec compliance review, code quality review, handle implementer status). You MUST use the Skill tool — do not replicate the SDD methodology from memory.

   <HARNESS_OVERRIDES>
   The following overrides REPLACE conflicting instructions from superpowers:subagent-driven-development.
   These take ABSOLUTE PRECEDENCE over any path, handoff, or workflow instruction in that skill:

   - **Plan location:** The plan is in `docs/exec-plans/active/`, NOT `docs/superpowers/plans/`. Provide the full task text from the plan to implementer subagents — do not make them read the plan file.
   - **No worktree setup:** Do NOT invoke `superpowers:using-git-worktrees`. Harness manages its own workflow context.
   - **No final code reviewer or finishing-a-development-branch:** Do NOT dispatch the "final code reviewer subagent for entire implementation" step, and do NOT invoke `superpowers:finishing-a-development-branch`. Harness uses `/harness:review` for holistic review instead. After all tasks complete (each having passed their individual two-stage reviews), proceed directly to Phase 3 below.
   - **Parallel dispatch for independent tasks:** SDD normally processes tasks sequentially. Override this when the plan contains independent tasks (no shared files, no data dependencies). For independent tasks, use `superpowers:dispatching-parallel-agents` to run them concurrently. If agent teams are available (TeamCreate, SendMessage), use them for coordination — create a named team, dispatch named workers, and collect results via SendMessage. After all parallel workers complete, run the two-stage review (spec + quality) on each task's changes before proceeding. Parallel dispatch is an optimization, not a requirement — fall back to sequential if uncertain about independence.
   - **Living plan updates after EVERY task:** After each task completes (after both reviews pass), update the plan file immediately:
     - **Progress:** Check off the completed task: `- [x] Task {N}: {name} _(completed {YYYY-MM-DD})_`
     - **Surprises:** If the implementer reported anything unexpected, add a row: `| {date} | {what} | {plan impact} | {action taken} |`
     - **Drift:** If the implementer deviated from the plan, add a row: `| Task {N} | {plan said} | {actually happened} | {why} |`
     - **Decision Log:** If a non-trivial decision was made, add a row: `| {date} | Implementation | {decision} | {rationale} |`
     - **Last Updated:** Update the timestamp in the plan's status line.
   - **Micro-review after EVERY task:** After updating the plan, check the task's diff (`git diff HEAD~1`) for:
     - Stale docs contradicted by the diff (new modules not in ARCHITECTURE.md, changed patterns not in DESIGN.md)
     - Obvious code issues (unused imports, debug logging, unresolved TODOs)
     - Test coverage gaps
     Fix any stale docs immediately. Note code issues for `/harness:review`.
   - **Checkpoint after EVERY task:** After the micro-review, report status to the user and wait for feedback:
     ```
     ## Task {N} Complete

     **Progress:** {M} of {total} tasks done
     **Surprises:** {any new entries, or "none"}
     **Drift:** {any deviations, or "none"}
     **Docs:** {what was updated, or "current"}
     **Code notes:** {any issues spotted, or "clean"}

     Ready for next task. Continue? (y/n)
     ```
     Apply any user corrections before proceeding to the next task.
   - **Refactor scope awareness:** If the plan header contains `Refactor Scope:`, also update the scope doc's step statuses after each task. When orchestrate finishes all planned steps and hits an async gate, output the gate info:
     ```
     Step {N} ({name}): completed
     Step {M} ({name}): in_progress — {status}

     Gate before Step {X}: {gate condition}

     This refactor is paused at a gate. Return in a new session and run:
       /harness:refactor-status
     ```
   </HARNESS_OVERRIDES>

### Phase 3: Completion

5. When all tasks are complete, report final status:

   ```
   ## All Tasks Complete

   **Progress:** {total} of {total} tasks done
   **Total surprises:** {N}
   **Total drift entries:** {N}
   **Decisions logged:** {N}

   ## Next Step

   Run `/harness:review` to:
   - Simplify code
   - Run multi-perspective code review
   - Fix or defer findings

   Then `/harness:reflect` → `/harness:complete`.
   ```

6. **Update run-state** (if `.harness/` runtime exists):
    ```bash
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-update-state.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "orchestrate"
    ```
