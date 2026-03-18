# Bug Analysis: Lead Work Lost When Worktree/Branch Cleaned Up

> **Status**: Confirmed | **Date**: 2026-03-17
> **Severity**: Critical
> **Affected Area**: `internal/belayer/taskrunner.go` (HandleApproval), `internal/crag/crag.go` (CleanupProblemWorktrees), `internal/repo/repo.go` (WorktreeRemove)

## Symptoms
- After manually recovering from a stuck problem (resetting status, deleting worktrees/branches), all lead commits are permanently lost
- ~3,300 lines of work across 56 files nearly lost in one incident
- Commits only recoverable via `git fsck --unreachable` on the bare repo

## Reproduction Steps
1. Submit a problem; leads begin working and committing to worktree branches
2. Problem gets stuck (e.g., daemon crash, tmux session lost, stale timeout race)
3. Attempt manual recovery: reset problem status in SQLite, delete worktree branches
4. Lead commits become dangling objects — no remote backup exists
5. Daemon re-initializes the problem with fresh worktrees from master

## Root Cause

Leads never push their branches to the remote during work. The **only** push to origin occurs in `HandleApproval` (`taskrunner.go:1290`), which runs only when the problem transitions to PR creation after anchor approval.

### Evidence chain:

1. **No incremental push**: `internal/lead/` contains zero `git push` calls. Leads commit locally to worktree branches in the bare repo but never push to remote.

2. **Single push point**: `HandleApproval` at `taskrunner.go:1290`:
   ```go
   tr.git.Run(worktreePath, "push", "-u", "origin", "HEAD:"+branchName)
   ```
   This is the first and only push. It runs only when the problem reaches PR creation.

3. **Force removal on cleanup**: `WorktreeRemove` at `repo.go:141`:
   ```go
   cmd := exec.Command("git", "-C", bareRepoDir, "worktree", "remove", "--force", worktreePath)
   ```
   `--force` removes worktrees regardless of uncommitted changes or unpushed state.

4. **No pre-cleanup safety check**: `CleanupProblemWorktrees` (`crag.go:243-260`) iterates all repos and calls `WorktreeRemove` without checking if branches have unpushed commits.

5. **Cleanup called from multiple paths**: `DestroyEnv` → `env.Destroy()` → `CleanupProblemWorktrees()` is called from:
   - Init error rollback (`taskrunner.go:159, 166, 171`)
   - Problem completion cleanup (`taskrunner.go:873`)
   - Manual `belayer env destroy` commands

### Normal flow vs. failure flow:

In the **normal flow**, worktrees are preserved across daemon restarts and only cleaned up after HandleApproval has pushed. But in any **abnormal flow** (stuck problem, manual recovery, init rollback), cleanup destroys unpushed work permanently.

## Impact Assessment
- **Data loss**: Hours of lead compute silently destroyed during recovery
- **No safety net**: No remote backup until the very end of the pipeline
- **Manual recovery is destructive**: The natural recovery action (reset + clean up) is the worst thing to do
- **Scales with problem complexity**: Multi-repo problems with many files lose the most work

## Recommended Fix Direction

### Primary: Push-early strategy
Leads should push their branch to origin after each commit (or at least periodically). This creates a remote backup throughout the execution lifecycle, not just at PR creation time. The push could happen:
- In the lead spawner command (add `git push` to the lead's post-commit hook or CLAUDE.md instructions)
- In the daemon's tick loop (periodic push check for active climbs)
- In HandleApproval (already done, but too late)

### Secondary: Never delete branches with unpushed work
`CleanupProblemWorktrees` should check if each branch has commits ahead of origin before force-removing. If unpushed work exists:
- Push it first, then remove
- Or refuse to remove and warn the operator

### Tertiary: Recovery should reuse existing worktrees
When re-initializing a problem, check if worktrees already exist with work in them. If so, reuse them instead of creating fresh ones from master.
