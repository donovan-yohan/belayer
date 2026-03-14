---
description: Use when capturing learnings and updating docs after review, when user says "reflect", "retrospective", "update docs", or after /harness:review completes
---

# Reflect

Full documentation reconciliation, conversation mining, and retrospective. Run after `/harness:review`, before `/harness:complete`.

## Usage

```
/harness:reflect                    # Diff from active plan creation or last 5 commits
/harness:reflect HEAD~3             # Diff last 3 commits
```

## Invocation

**IMMEDIATELY execute this workflow:**

### Phase 1: Diff-First Discovery

1. Determine the diff scope:
   - If a commit reference was provided, use it: `git diff {ref}...HEAD`
   - Otherwise, check for an active plan in `docs/exec-plans/active/`. If one exists, diff from its creation date.
   - Fallback: diff the last 5 commits: `git diff HEAD~5...HEAD`

2. Build a **change profile** from the diff:
   ```bash
   git diff {ref}...HEAD --stat
   git diff {ref}...HEAD --name-only
   ```
   Extract:
   - Directories and modules touched
   - Types of changes (new files, modified files, deleted files)
   - Domains affected (testing, API, auth, build, frontend, etc.)

### Phase 2: Selective Doc Loading

3. Read the Documentation Map from CLAUDE.md (just the table — not the whole file).

4. Match the change profile against Documentation Map categories:
   - Changes to module code → load `docs/ARCHITECTURE.md`
   - Changes to patterns/conventions → load `docs/DESIGN.md`
   - Changes matching discovered categories → load the relevant `docs/{DOMAIN}.md`

5. Read ONLY the matched Tier 2 files. Scan their "Deep Docs" tables. If any Tier 3 files are relevant to the diff, read those too (on-demand).

6. If `docs/adrs/` exists, list ADR filenames and read title lines. Load any ADRs whose topic overlaps with the change profile.

### Phase 3: Staleness Check & Fix

7. For each loaded doc, check if the diff contradicts or obsoletes anything:
   - Does the diff add a new module not mentioned in ARCHITECTURE.md?
   - Does the diff **delete** a module/directory still listed in ARCHITECTURE.md Code Map or DESIGN.md?
   - Does the diff change a pattern described in DESIGN.md?
   - Does the diff affect something described in a Tier 3 doc?

8. **Deleted-code audit**: Extract deleted files/directories from the diff (`--diff-filter=D`). For each:
   - Search all loaded Tier 2/3 docs for references to the deleted path
   - Flag any doc that describes the deleted code as if it still exists
   - This is the highest-priority staleness fix — confident docs about nonexistent code actively mislead

9. For each stale finding, fix it in-place:
   - Add new modules to ARCHITECTURE.md Code Map
   - **Remove or mark as removed** deleted modules from ARCHITECTURE.md Code Map
   - Update DESIGN.md Current State bullets
   - **Remove or update** DESIGN.md sections that describe deleted functionality
   - Update Tier 3 docs that reference changed code
   - If a dedicated doc exists for a deleted module (e.g., `docs/TUI.md`), delete the file or replace contents with a tombstone pointing to what replaced it

10. If an active plan exists in `docs/exec-plans/active/`:
   - Check if any surprises or drift should be recorded
   - Update the plan's Surprises & Discoveries table if something unexpected was found
   - Update the plan's Plan Drift table if a doc fix implies the implementation deviated

### Phase 4: Plan vs. Actual

11. Read the active plan fully. Compare the plan's Progress/Drift tables against the actual diff. The living plan already has most of this data from orchestration. Fill any gaps:
    - Tasks that were completed but not checked off
    - Drift entries that should have been recorded
    - Surprises discovered but not logged

### Phase 5: Conversation Mining

12. Scan the current conversation history for:
    - **User corrections** — instances where the user corrected approach or output
    - **Rejected approaches** — things proposed that the user pushed back on
    - **Incorrect assumptions** — things assumed about the codebase that turned out false
    - **New patterns** — approaches discovered that worked well and should be documented
    - **Process feedback** — meta-feedback about the workflow

13. Categorize each finding as:
    - **doc-update** — corrects or adds to an existing Tier 2/3 doc
    - **adr-candidate** — represents a significant architectural decision
    - **no-action** — interesting but one-off

14. Codify findings:
    - For each **doc-update**: edit the relevant doc in place
    - For each **adr-candidate**: if `docs/adrs/` exists, create a new ADR with status Proposed. If not, inform user and suggest `/adr:init`.
    - If ADRs were created, execute `/adr:update` inline.

### Phase 6: Outcomes & Retrospective

15. Ask user for their perspective:
    ```
    The plan had {N} tasks. {M} completed, {D} deviated, {K} surprises logged.

    What worked well? What didn't? Any learnings to capture?
    ```

16. Fill in the plan's Outcomes & Retrospective section with:
    - User's input
    - Conversation-mined learnings
    - Summary of surprises and drift

### Phase 7: Design Doc Archival

17. If `docs/design-docs/index.md` exists, check whether the diff supersedes any earlier design docs:
    - If a newer design doc covers the same topic as an older one, add a `Superseded by` column or marker to the older entry
    - Separate the index into **Current Designs** and **Archived** sections if not already structured that way
    - Archived entries should note which doc superseded them

### Phase 8: Tier 2 Summary Updates

18. Update `docs/PLANS.md`:
    - Update plan status (not yet archived — that's `/harness:complete`)

19. Update `docs/DESIGN.md` if any new patterns or key decisions were established.

20. Update `docs/ARCHITECTURE.md` if any new modules were created or boundaries changed.

### Report

21. Output:
    ```
    ## Reflect Complete

    **Scope:** {diff description, e.g., "3 commits, 12 files changed"}
    **Docs loaded:** {list of Tier 2/3 files checked}

    ### Updates Made
    - {list of doc files modified with one-line description of what changed}
    - {or "No updates needed — docs are current"}

    ### Deleted-Code Cleanup
    - {list of stale references removed, or "No deleted modules detected"}

    ### Plan Updates
    - {surprises/drift added, or "No active plan" or "No drift detected"}

    ### Conversation Learnings
    - Doc updates: {N}
    - ADR candidates: {N}
    - No-action: {N}

    ### Retrospective
    - Captured in plan's Outcomes & Retrospective section

    ## Next Step

    Run `/harness:complete` to archive the plan and create the PR.
    ```
