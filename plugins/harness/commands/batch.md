---
description: Use when executing a living plan via Claude Code's /batch tool for worktree-isolated parallel execution, or when user says "batch", "batch execute", "run batch", "parallel batch", "run the plan in parallel", "execute without checkpoints", "hands-off execution", or "use batch mode"
---

# Batch

Execute a living plan by delegating to Claude Code's built-in `/batch` tool for parallel, worktree-isolated agent execution. Alternative to `/harness:orchestrate` — trades per-task checkpoints and user approval gates for faster, fully parallel execution with PR-per-unit output.

## Usage

```
/harness:batch
/harness:batch docs/exec-plans/active/{file}.md
```

## When to use `/harness:batch` vs `/harness:orchestrate`

| Aspect | `/harness:batch` | `/harness:orchestrate` |
|---|---|---|
| Execution | All independent units in parallel via `/batch` | Sequential with per-task user checkpoints |
| Isolation | Git worktree per unit | In-process agents |
| Output | PR per unit | Commits on current branch |
| User interaction | Approve batch plan once, then hands-off | Approve after each task |
| Best for | Large plans with many independent tasks | Plans with complex dependencies or need for tight control |

## Architecture

- Orchestrator (main agent, Opus) owns coordination, never writes code
- `/batch` handles parallel agent dispatch, git worktree isolation, and PR creation
- Orchestrator manages plan lifecycle: loading, batch instruction construction, plan updates, and post-batch review

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

### Phase 2: Construct Batch Instruction

4. Build a `/batch` instruction from the plan's remaining tasks. The instruction should:
   - Summarize the overall goal from the plan
   - Include the absolute project path so agents can orient themselves
   - List each remaining task with its full specification and acceptance criteria
   - Reference relevant files, architectural docs, and codebase patterns from the plan context
   - Include outputs or context from any already-completed tasks that subsequent tasks depend on

   ```
   /batch Implement the following tasks from {plan title}:

   Project: {absolute path to project}
   Architecture: see docs/ARCHITECTURE.md, docs/DESIGN.md
   Previously completed: {list of completed tasks and their key outputs, or "none"}

   Task {N}: {task name}
   {full task specification from plan}
   Acceptance criteria: {criteria}
   Key files: {relevant files and modules}

   Task {M}: {task name}
   ...

   Context:
   - Follow existing patterns in the codebase
   - Run tests/build/lint to verify each change
   - Commit changes with descriptive messages
   ```

5. `/batch` will research the codebase, decompose the work into independent units (5-30), and present a plan for your review. Review the proposed decomposition against your plan's tasks:
   - Verify all plan tasks are covered
   - Confirm the decomposition respects task dependencies (dependent tasks should be in the same unit)
   - Approve when satisfied, or provide corrections

### Phase 3: Monitor Batch Execution

6. `/batch` spawns one background agent per unit, each in an isolated git worktree. Each agent implements its unit, runs tests, and opens a PR. Wait for `/batch` to report completion before proceeding to Phase 4.

7. As units complete and PRs are created, collect:
   - What each unit implemented
   - Any surprises or deviations from the plan
   - PR links for each unit

### Phase 4: Update Living Plan

8. After batch completes, update the plan file:

   **a) Update Progress** — check off completed tasks with timestamp:
   ```markdown
   - [x] Task {N}: {name} _(completed {YYYY-MM-DD})_
   ```

   **b) Add Surprises row** if anything unexpected occurred:
   ```markdown
   | {date} | {what was unexpected} | {how it affects the plan} | {what was done} |
   ```

   **c) Add Plan Drift row** for any deviations:
   ```markdown
   | Task {N} | {what the plan said} | {what actually happened} | {why} |
   ```

   **d) Add Decision Log row** for non-trivial decisions:
   ```markdown
   | {date} | Implementation | {decision} | {rationale} |
   ```

   **e) Update Last Updated** timestamp in the plan's status line.

   **f) If the plan's source is a refactor scope doc** (check for `Refactor Scope:` in the plan header), also update the scope doc's step statuses. When batch finishes all planned steps and hits an async gate, update the scope doc and output the gate info:
   ```
   Step {N} ({name}): completed
   Step {M} ({name}): in_progress — {status}

   Gate before Step {X}: {gate condition}

   This refactor is paused at a gate. Return in a new session and run:
     /harness:refactor-status
   ```

### Phase 5: Micro-Review

9. Review the PRs created by `/batch`:

   a) For each PR, check the diff for obvious issues:
   ```bash
   gh pr diff {pr-number}
   ```

   b) Scan for:
   - Stale docs contradicted by the diff (new modules not in ARCHITECTURE.md, changed patterns not in DESIGN.md)
   - Obvious code issues (unused imports, debug logging left in, TODO comments that should be resolved)
   - Test coverage gaps (new code paths without corresponding tests)
   - Cross-PR consistency (changes in one PR that should be reflected in another)

   c) Fix any stale docs immediately while context is fresh.

   d) Note any code issues in the checkpoint report — they'll be caught thoroughly by `/harness:review` later.

### Phase 6: Checkpoint

10. Report final status with PR links:

    ```
    ## Batch Execution Complete

    **Progress:** {M} of {total} tasks done
    **PRs created:** {list of PR links}
    **Surprises:** {any entries, or "none"}
    **Drift:** {any deviations, or "none"}
    **Docs:** {what was updated, or "current"}
    **Code notes:** {any issues spotted, or "clean"}
    ```

11. If any tasks remain (due to dependency chains or batch limitations), ask the user whether to run another `/batch` round for the remaining tasks.

### Phase 7: Completion

12. When all tasks are complete, run a final integration check using the project's build and test commands (check CLAUDE.md or the plan for the correct commands):

    ```bash
    # Example for Go projects:
    go test ./...
    go build ./...
    ```

13. Report final status:

    ```
    ## All Tasks Complete

    **Progress:** {total} of {total} tasks done
    **PRs to merge:** {list of PR links}
    **Total surprises:** {N}
    **Total drift entries:** {N}
    **Decisions logged:** {N}

    ## Next Step

    Merge the PRs, then run `/harness:review` to:
    - Simplify code
    - Run multi-perspective code review (6 agents)
    - Fix or defer findings

    Then `/harness:reflect` → `/harness:complete`.
    ```

## Orchestrator Rules

**DO:**
- Write a detailed `/batch` instruction that includes full task specs and context from the plan
- Review `/batch`'s proposed decomposition before approving execution
- Ensure dependent tasks land in the same batch unit (they must not be split across parallel agents)
- Update living plan after batch completes (progress, surprises, drift, decisions)
- Run micro-review on all PRs to catch stale docs and obvious issues
- Run integration checks yourself after all PRs are merged

**DON'T:**
- Write code yourself — pure control plane only
- Skip living plan updates after batch execution
- Skip micro-reviews on batch PRs
- Approve a batch decomposition that splits dependent tasks across units
- Mark the project done without running a final build/test check yourself
