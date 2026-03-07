# Design: Cross-Repo Integration & Alignment (Goal 6)

## Overview

Enhance the existing alignment agentic node to collect git diffs from completed worktrees, produce structured per-criterion verdicts, re-dispatch leads on failure with alignment feedback, and create PRs on success.

## Current State

The coordinator already:
- Detects when all leads for a task are complete (`checkLeadProgress`)
- Transitions task to `aligning` status
- Runs an alignment agentic node via `startAlignment()`
- Emits alignment events (started/passed/failed)

**Gaps in current implementation:**
- Alignment prompt only includes repo specs and worktree paths (no actual diffs)
- `AlignmentOutput` is `{pass, feedback}` — no per-criterion breakdown
- On failure: task is marked `failed` (no re-dispatch)
- On pass: task is marked `complete` (no PR creation)

## Design

### 1. Git Diff Collection (`internal/repo/`)

Add a `WorktreeDiff` function to the repo package:

```go
func WorktreeDiff(worktreePath string) (string, error)
```

Runs `git diff HEAD` in the worktree to capture all committed changes on the branch vs the base. Uses `git diff main...HEAD` (three-dot diff) to capture only changes on the belayer branch relative to the base branch.

Also add `WorktreeDiffStat` for a summary view:

```go
func WorktreeDiffStat(worktreePath string) (string, error)
```

### 2. Enhanced Alignment Output

Replace the simple `AlignmentOutput` with a richer structure:

```go
type AlignmentOutput struct {
    Pass     bool              `json:"pass"`
    Feedback string            `json:"feedback"`
    Criteria []AlignmentCriterion `json:"criteria"`
    MisalignedRepos []string   `json:"misaligned_repos,omitempty"`
}

type AlignmentCriterion struct {
    Name    string `json:"name"`
    Pass    bool   `json:"pass"`
    Details string `json:"details"`
}
```

The alignment prompt instructs Claude to evaluate:
- API contract consistency (endpoints, request/response shapes)
- Shared type compatibility (data models, enums)
- Feature parity (all repos implement their part)
- Integration points (correct service URLs, event names)

### 3. Enhanced Alignment Prompt

The alignment prompt now includes:
- Task description
- Per-repo specs
- Per-repo git diffs (the actual code changes)
- Per-repo diff stats (summary of files changed)
- Instructions to evaluate specific criteria and produce structured output

If diffs are too large (>50KB combined), fall back to diff stats only with a note that full diffs were too large.

### 4. Re-dispatch on Alignment Failure

When alignment fails:
1. Track alignment attempt count on the task (new field or via events)
2. If under max alignment attempts (default: 2), re-dispatch:
   - For each misaligned repo identified, create a new lead with alignment feedback appended to the spec
   - Only re-dispatch repos that failed criteria (not all repos)
   - Transition task back to `running` status
3. If max alignment attempts exceeded, fail the task

This requires a new task field `alignment_attempt` or we can count alignment events to derive the attempt number. Decision: count alignment events (simpler, no schema change needed).

### 5. PR Creation on Alignment Pass

When alignment passes, for each repo worktree:
1. Run `git push` to push the belayer branch to the remote
2. Create a PR via `gh pr create` (GitHub CLI)
3. Record PR URLs in an event

PR creation is best-effort: if it fails for a repo, log the error and record it in the event but still mark the task as complete (the code changes are already committed in worktrees).

Add PR creation functions to `internal/repo/`:

```go
func PushBranch(worktreePath string) error
func CreatePR(worktreePath, title, body string) (string, error)
```

### 6. Alignment Attempt Limiting

Rather than adding a schema migration, derive alignment attempts by counting `alignment_started` events for the task. Add a store method:

```go
func (s *Store) CountAlignmentAttempts(taskID string) (int, error)
```

Max alignment attempts configurable in `CoordinatorConfig` (default: 2).

### 7. DiffCollector Interface

For testability, introduce a `DiffCollector` interface:

```go
type DiffCollector interface {
    CollectDiff(worktreePath string) (string, error)
    CollectDiffStat(worktreePath string) (string, error)
}
```

The coordinator uses this interface. Default implementation calls `repo.WorktreeDiff`/`repo.WorktreeDiffStat`. Tests provide a mock.

### 8. PRCreator Interface

For testability, introduce a `PRCreator` interface:

```go
type PRCreator interface {
    PushAndCreatePR(worktreePath, title, body string) (prURL string, err error)
}
```

Default implementation calls `repo.PushBranch` then `repo.CreatePR`. Tests provide a mock.

## Decisions

1. **No schema migration** — alignment attempts derived from events, PR URLs stored as event payloads
2. **Re-dispatch only misaligned repos** — efficient; non-misaligned repos keep their completed state
3. **Best-effort PR creation** — task completes even if PR creation fails; user can manually create PRs from branches
4. **Diff size cap at 50KB** — prevents overwhelming the agentic node; falls back to stat-only
5. **Max 2 alignment attempts** — prevents infinite re-dispatch loops; configurable
6. **Three-dot diff** — `main...HEAD` captures only branch changes, not upstream drift

## Files Changed

| File | Change |
|------|--------|
| `internal/repo/repo.go` | Add `WorktreeDiff`, `WorktreeDiffStat`, `PushBranch`, `CreatePR` |
| `internal/repo/repo_test.go` | Tests for new repo functions |
| `internal/coordinator/coordinator.go` | Enhance `startAlignment`, add re-dispatch logic, PR creation, new interfaces |
| `internal/coordinator/coordinator_test.go` | Tests for enhanced alignment, re-dispatch, PR creation |
| `internal/coordinator/agentic_test.go` | Update mock claude to return per-criterion alignment output |
| `internal/coordinator/store.go` | Add `CountAlignmentAttempts` |
| `internal/coordinator/store_test.go` | Test for `CountAlignmentAttempts` |
| `internal/model/types.go` | Add `EventPRsCreated` event type |
