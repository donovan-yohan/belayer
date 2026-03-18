---
description: Use when executing a living plan with agent teams, or when user says "orchestrate", "execute the plan", "start building", or "run the plan"
---

# Orchestrate

Execute a living plan using agent teams with per-task micro-reflects and living plan updates.

## Usage

```
/harness:orchestrate
/harness:orchestrate docs/exec-plans/active/{file}.md
```

## Architecture

- Orchestrator (main agent, Opus) owns coordination, never writes code
- Workers (Sonnet 4.6) implement tasks
- Workers report back, orchestrator updates plan and runs micro-reflects

## Invocation

**IMMEDIATELY execute this workflow:**

### Phase 1: Load Plan

1. Locate the plan:
   - If a path argument was provided, use it
   - Otherwise, list `docs/exec-plans/active/` and use the most recently modified file
   - If no active plans exist, suggest running `/harness:brainstorm` → `/harness:plan` first

2. Read the full plan. Extract:
   - Task list (from `### Task N:` headers or Progress checklist)
   - Progress section (which tasks are already completed)
   - Current state of Surprises, Drift, and Decision Log tables

3. If any tasks are already marked complete in Progress, skip them. Resume from the first incomplete task.

### Phase 2: Team & Task Setup

4. Create the agent team:

   ```typescript
   TeamCreate({
     team_name: "{plan-kebab-name}",
     description: "Executing: {plan title}"
   })
   ```

5. Create tasks for each remaining plan task:

   ```typescript
   TaskCreate({
     subject: "Task {N}: {task name}",
     description: `
       {full task specification from plan}

       Acceptance criteria:
       {acceptance criteria from plan}

       MANDATORY COMPLETION REQUIREMENTS:
       1. Verify implementation: run tests/build/lint as appropriate
       2. Commit changes with descriptive message
       3. Report back: what was done, any surprises, any deviations from plan
     `,
     activeForm: "Implementing Task {N}"
   })
   ```

6. Set up dependencies between tasks based on plan structure:

   ```typescript
   TaskUpdate({ taskId: "2", addBlockedBy: ["1"] })
   ```

### Phase 3: Dispatch Workers

7. For each unblocked task (or batch of independent tasks), spawn a Sonnet 4.6 worker:

   ```typescript
   // Mark task in progress
   TaskUpdate({ taskId: "N", status: "in_progress" })

   // Spawn worker
   Task({
     description: "Task {N}: {task name}",
     prompt: `You are a team member working on: {task name}.

   PROJECT: {absolute path to project}
   TEAM: {team-name}

   ## Task Specification

   {paste the full ### Task N section from the plan}

   ## Context
   - Plan: {plan path}
   - Previous tasks completed: {list}
   - Known surprises: {from plan's Surprises table, or "none"}

   ## Implementation Approach

   Implement directly using your tools (Read, Edit, Write, Bash).
   Follow existing patterns in the codebase.

   ## MANDATORY Before Claiming Complete

   1. Run relevant tests/build to verify implementation works
   2. Commit changes: git add {files} && git commit -m "{descriptive message}"
   3. Report back:
      - What was done (summary)
      - Any surprises (unexpected findings, blockers encountered, different from plan)
      - Any deviations from plan (what the plan said vs what you actually did, and why)
   4. Mark task complete via: TaskUpdate({ taskId: "{N}", status: "completed" })
   `,
     subagent_type: "general-purpose",
     model: "sonnet",
     mode: "bypassPermissions",
     team_name: "{team-name}",
     name: "worker-{N}",
     run_in_background: true
   })
   ```

   **Independent tasks dispatched in parallel** — send multiple Task tool calls in one message. Sequential (blocked) tasks wait for their blockers to complete.

### Phase 4: Monitor & Update Living Plan

8. After each task completes, update the plan file immediately:

   **a) Update Progress** — check off the completed task with timestamp:
   ```markdown
   - [x] Task {N}: {name} _(completed {YYYY-MM-DD})_
   ```

   **b) Add Surprises row** if worker reported anything unexpected:
   ```markdown
   | {date} | {what was unexpected} | {how it affects the plan} | {what was done} |
   ```

   **c) Add Plan Drift row** if worker deviated from the plan:
   ```markdown
   | Task {N} | {what the plan said} | {what actually happened} | {why} |
   ```

   **d) Add Decision Log row** if a non-trivial decision was made:
   ```markdown
   | {date} | Implementation | {decision} | {rationale} |
   ```

   **e) Update Last Updated** timestamp in the plan's status line.

   **f) If the plan's source is a refactor scope doc** (check for `Refactor Scope:` in the plan header), also update the scope doc's step statuses. When orchestrate finishes all planned steps and hits an async gate, update the scope doc and output the gate info:
   ```
   Step {N} ({name}): completed
   Step {M} ({name}): in_progress — {status}

   Gate before Step {X}: {gate condition}

   This refactor is paused at a gate. Return in a new session and run:
     /harness:refactor-status
   ```

### Phase 5: Micro-Review

9. After updating the plan, run a quick review scoped to the task's changes:

   a) Check the task's diff for obvious issues:
   ```bash
   git diff HEAD~1 --stat
   git diff HEAD~1
   ```

   b) Scan for:
   - Stale docs contradicted by the diff (new modules not in ARCHITECTURE.md, changed patterns not in DESIGN.md)
   - Obvious code issues (unused imports, debug logging left in, TODO comments that should be resolved)
   - Test coverage gaps (new code paths without corresponding tests)

   c) Fix any stale docs immediately while context is fresh.

   d) If code issues are found, note them in the checkpoint report — they'll be caught thoroughly by `/harness:review` later.

### Phase 6: Checkpoint

10. After each task (or batch of parallel tasks), report status:

    ```
    ## Task {N} Complete

    **Progress:** {M} of {total} tasks done
    **Surprises:** {any new entries, or "none"}
    **Drift:** {any deviations, or "none"}
    **Docs:** {what was updated, or "current"}
    **Code notes:** {any issues spotted, or "clean"}

    Ready for next task. Continue? (y/n)
    ```

11. Wait for user feedback. Apply any corrections before proceeding to next task.

### Phase 7: Completion

12. When all tasks are complete, dispatch team shutdown:

    ```typescript
    SendMessage({ type: "shutdown_request", recipient: "worker-N", content: "All tasks complete." })
    // Repeat for each active teammate
    TeamDelete()
    ```

13. Report final status:

    ```
    ## All Tasks Complete

    **Progress:** {total} of {total} tasks done
    **Total surprises:** {N}
    **Total drift entries:** {N}
    **Decisions logged:** {N}

    ## Next Step

    Run `/harness:review` to:
    - Simplify code
    - Run multi-perspective code review (6 agents)
    - Fix or defer findings

    Then `/harness:reflect` → `/harness:complete`.
    ```

## Orchestrator Rules

**DO:**
- Write detailed worker prompts with full task spec, project path, and team name
- Spawn workers with `model: "sonnet"` (Sonnet 4.6) for cost-effective implementation
- Update living plan after EVERY task (progress, surprises, drift, decisions)
- Run micro-review after EVERY task to catch stale docs and obvious issues
- Report progress at checkpoints and wait for user feedback
- Run integration checks yourself (never delegate to workers)

**DON'T:**
- Write code yourself — pure control plane only
- Skip living plan updates after any task
- Skip micro-reviews after any task
- Dispatch dependent tasks in parallel (check blockedBy before spawning)
- Mark the project done without running a final build/test check yourself
