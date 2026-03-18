# Bug Analysis: PR Creation Fails Silently — stderr Not Captured

> **Status**: Confirmed | **Date**: 2026-03-17
> **Severity**: Medium
> **Affected Area**: `internal/scm/github/github.go` (runInDir, CreatePR)

## Symptoms
- PR creation failures log only `exit status 1` with no diagnostic information
- Actual error message from `gh` (e.g., "No commits between master and branch") is lost
- Debugging PR failures requires manual reproduction outside belayer

## Reproduction Steps
1. Submit a problem where a lead's branch has no commits ahead of base (e.g., due to Bug 3 work loss)
2. Problem reaches anchor approval → HandleApproval calls `scm.CreatePR()`
3. `gh pr create` fails with a descriptive stderr message
4. Daemon logs: `failed to create PR for <repo>: gh pr create: exit status 1`
5. The descriptive error is never captured or shown

## Root Cause

The GitHub SCM provider's `runInDir` helper uses `cmd.Output()` which only captures stdout. stderr — where `gh` writes its error messages — is silently discarded.

### Evidence:

1. **`github.go:170-173`** — `runInDir` uses `cmd.Output()`:
   ```go
   func runInDir(ctx context.Context, dir string, name string, args ...string) ([]byte, error) {
       cmd := exec.CommandContext(ctx, name, args...)
       cmd.Dir = dir
       return cmd.Output()  // stdout only, stderr discarded
   }
   ```

2. **`github.go:188-190`** — error wrapping only includes exit code:
   ```go
   if err != nil {
       return nil, fmt.Errorf("gh pr create: %w", err)
   }
   ```

3. **Contrast with `repo.go:207`** — the local git helper correctly uses `CombinedOutput()`:
   ```go
   output, err := cmd.CombinedOutput()  // captures both stdout and stderr
   ```

4. **Same issue at `github.go:259`** — `ReplyToComment` also uses `cmd.Output()`, losing stderr from `gh api` failures.

### No pre-flight validation:

`CreatePR` at `github.go:176-206` immediately calls `gh pr create` without verifying:
- Branch exists on remote
- Branch has commits ahead of base
- Worktree is intact and has the expected branch checked out

These checks would catch common failure modes and provide clear diagnostic messages before attempting the `gh` call.

## Impact Assessment
- **Debugging friction**: Operators must manually reproduce `gh pr create` to see the real error
- **Cascading confusion**: When combined with Bug 3 (work loss), the error "No commits between master and branch" would directly explain the situation — but it's never shown
- **Applies to other gh calls**: `ReplyToComment` (`github.go:259`) has the same issue

## Recommended Fix Direction

### Primary: Capture stderr
Change `runInDir` to use `cmd.CombinedOutput()` instead of `cmd.Output()`, matching the pattern already used in `repo.go:207`. Include the output in error messages.

### Secondary: Pre-flight checks in CreatePR
Before calling `gh pr create`:
1. Verify the branch exists on remote (`git ls-remote --heads origin <branch>`)
2. Verify the branch has commits ahead of base (`git rev-list --count base..branch`)
3. If either check fails, return a clear error: "branch X has no commits ahead of Y — leads may not have pushed their work"
