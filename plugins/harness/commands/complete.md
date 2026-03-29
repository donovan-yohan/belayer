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

4. **Apply `superpowers:verification-before-completion`** using the Skill tool: `Skill("superpowers:verification-before-completion")`. Follow the loaded skill to run the project's verification commands (tests, build, lint, typecheck). All must pass before proceeding. Do not archive or create a PR with failing verification.

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

7.5. **Archive the source design doc:**
   - Read the plan's header to find the `Design Doc:` path (or `Bug Analysis:` or `Refactor Scope:`)
   - If a source design doc is found, update its YAML frontmatter:
     - Change `status:` to `implemented`
     - Set `implemented-by: docs/exec-plans/completed/{file}`
   - If the design doc has no YAML frontmatter (legacy), add one:
     ```yaml
     ---
     status: implemented
     created: {infer from filename date prefix, or use file creation date}
     branch: {current branch}
     implemented-by: docs/exec-plans/completed/{file}
     ---
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

11.5. **Update design-docs index with archival:**
   - Read `docs/design-docs/index.md`
   - If the index does NOT have `## Current Designs` and `## Archived` sections:
     - Restructure the index: add `## Current Designs` header above the existing table
     - Add a `Status` column to the table
     - Add `## Archived` section below with its own table (same columns)
     - Move all design docs with frontmatter `status: implemented` or `status: superseded` to the Archived table
     - Keep docs with `status: current` or no frontmatter in the Current Designs table
   - If the index already has Current/Archived sections:
     - Move the just-archived design doc from Current to Archived
     - Update its Status column to "Implemented"
   - Status badge vocabulary for the index: `Current` | `Implemented` | `Superseded` | `Stale`

### Phase 5.7: Evolution Summary

11.6. Resolve the harness runtime directory:
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    ```

11.7. If `HARNESS_DIR` is not empty, check for proposals from this session:
    - List files in `$HARNESS_DIR/proposals/` matching today's date prefix
    - For each proposal, read its status, scope, signal, and agent

11.8. If proposals exist, include them in the PR description (Phase 7). Add a "Harness Evolution" section to the PR body:
    ```markdown
    ## Harness Evolution

    | Proposal | Agent | Signal | Scope | Status |
    |----------|-------|--------|-------|--------|
    | {slug} | {agent} | {signal} | {scope} | {applied/pending} |

    **Auto-applied ({N}):** These proposals met all safety criteria (escape-sourced, additive, 5+ agent runs) and were applied automatically.

    **Pending review ({N}):** These proposals need human review before applying.
    ```

11.9. If no proposals or no `.harness/`: skip. The PR is created normally.

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

15. **Update run-state** (if `.harness/` runtime exists):
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-update-state.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "complete"
    ```
