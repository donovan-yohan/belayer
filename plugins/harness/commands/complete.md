---
description: Use when ready to archive the plan and create the PR, when user says "we're done", "complete this", "wrap up", or after /harness:reflect finishes
---

# Complete

Plan archival, prune health check, and PR creation. Run after `/harness:review` and `/harness:reflect`.

## Usage

```
/harness:complete                # Complete most recent active plan
```

## Invocation

**IMMEDIATELY execute this workflow:**

### Phase 1: Identify Plan

1. List files in `docs/exec-plans/active/`.
2. If multiple plans exist, ask user which to complete. If only one, confirm it.
3. Read the full plan — verify it has:
   - Progress section with tasks checked off
   - Outcomes & Retrospective section filled in (by `/harness:reflect`)
   - If retrospective is empty, suggest running `/harness:reflect` first

### Phase 2: Verification Gate

4. **Apply `superpowers:verification-before-completion`** — run the project's verification commands (tests, build, lint, typecheck). All must pass before proceeding. Do not archive or create a PR with failing verification.

### Phase 3: Plan Archival

5. Change plan status from Active to Completed:
   ```markdown
   > **Status**: Completed | **Created**: {date} | **Completed**: {date}
   ```

6. Add completion entry to Decision Log:
   ```markdown
   | {date} | Retrospective | Plan completed | {summary} |
   ```

7. Move the plan file:
   ```bash
   mv docs/exec-plans/active/{file} docs/exec-plans/completed/{file}
   ```

### Phase 4: Deleted-Code Audit

8. Check if any modules/directories were deleted during this plan's lifetime:
   ```bash
   git diff $(git log --diff-filter=A --format=%H -- docs/exec-plans/active/{file} | tail -1)...HEAD --diff-filter=D --name-only
   ```
   For each deleted path, verify no Tier 2/3 doc still describes it as existing code. Fix any stale references before archiving — confident docs about nonexistent code is the most dangerous staleness.

### Phase 5: Tier 2 Summary Updates

9. Update `docs/PLANS.md`:
   - Move entry from Active Plans table to Completed Plans table
   - Add completion date

10. Update `docs/DESIGN.md` if any new patterns or key decisions were established.

11. Update `docs/ARCHITECTURE.md` if any new modules were created or boundaries changed.

### Phase 6: Prune Health Check

12. Execute `/harness:prune` inline to verify docs health after all updates. Report any issues.

### Phase 7: PR Creation

13. Create the pull request:
    - If the `pr:author` command is available, invoke it
    - Otherwise, use `gh pr create` directly with:
      - Title derived from plan title
      - Body summarizing: goal, tasks completed, key decisions, surprises
      - Link to the completed plan in the PR description

### Report

14. Output:
    ```
    ## Complete

    ### Plan
    - Archived: docs/exec-plans/completed/{file}
    - Tasks: {M} completed, {D} deviated, {K} surprises

    ### Tier 2 Updates
    - PLANS.md: updated
    - DESIGN.md: {updated | no changes}
    - ARCHITECTURE.md: {updated | no changes}

    ### Prune Health: {HEALTHY | NEEDS ATTENTION}

    ### PR: #{number} — {url}
    ```
