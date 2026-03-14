---
description: Use when resuming an in-progress refactoring, checking goal progress, or when user says "refactor status", "where are we", or "continue refactoring"
---

# Refactor Status

Resume checkpoint for multi-session refactoring. Reads the active refactor scope document, derives goal status from git and GitHub state, and guides the user to the next executable goal.

## Usage

```
/harness:refactor-status                                        # Check most recent refactor
/harness:refactor-status docs/refactor-scopes/{file}.md         # Check specific refactor
```

## Invocation

**IMMEDIATELY execute this workflow:**

1. Locate the active refactor scope document:
   - If a path argument was provided (`$ARGUMENTS`), use it
   - Otherwise, find the most recently modified `*-refactor-scope.md` in `docs/refactor-scopes/`
   - If no scope doc found, suggest running `/harness:refactor` first

2. Read the scope document. Extract:
   - Title and refactor type
   - The Goals table (goal names, dependencies, production safety strategy)

3. Derive goal status from git/GitHub state. For each goal:
   - Check if a branch exists: `git branch --list "*{goal-name}*" --all`
   - Check if a PR exists: `gh pr list --search "{goal-name}" --state all --json number,state,title,url 2>/dev/null`
   - Derive status:
     - `complete` = PR merged (state=MERGED)
     - `in_progress` = branch exists or PR open (state=OPEN)
     - `blocked` = dependencies not complete
     - `pending` = not started (no branch, no PR)

4. Display goal status:

   ```
   ## Refactor Status: {Title}

   **Type**: {refactor type}
   **Target**: {target being refactored}

   | # | Goal | Status | PR |
   |---|------|--------|----|
   | 1 | {goal name} | complete | #142 |
   | 2 | {goal name} | in_progress | #145 (open) |
   | 3 | {goal name} | blocked (depends on 2) | — |
   | 4 | {goal name} | pending | — |

   **Progress**: {completed}/{total} goals complete
   ```

5. Show which goals are currently unblocked and ready to execute:

   ```
   ### Ready to Execute
   - Goal {N}: {name} — all dependencies met
   ```

   If no goals are ready (all blocked or complete), say so.

6. Guide user to next action:

   ```
   ## Next Steps

   Run `/harness:loop docs/refactor-scopes/{scope}/{goal}-design.md` to execute the next goal.
   Or run `/harness:loop docs/refactor-scopes/{date}-{scope}-refactor-scope.md` to execute all remaining goals.
   ```
