# Spotter: Cross-Repo Review with Redistribution

**Date**: 2026-03-07
**Status**: Proposed
**Goal**: PRD Goal 4

## Problem Statement

When all leads complete their goals for a task, the system needs to validate cross-repo alignment before creating PRs. The spotter is an agent session that reviews all diffs against the original spec, produces a verdict (approve/reject), and on rejection specifies correction goals for failing repos. Max 2 review cycles prevent infinite loops.

## Architecture

The spotter follows the same pattern as leads: an agent session spawned in a tmux window via the `AgentSpawner` interface. The key differences are:

1. **Prompt content**: spec + git diffs from all worktrees + goal completion summaries
2. **Output file**: `VERDICT.json` (not `DONE.json`)
3. **Lifecycle**: spawned by setter when all goals complete; setter reads verdict and acts on it

```
All goals complete
    |
    v
Setter: transition task to "reviewing"
    |
    v
Setter: gather diffs + summaries, build spotter prompt
    |
    v
Setter: spawn spotter agent in tmux window (task dir as workdir)
    |
    v
Spotter agent: review diffs against spec
    |
    v
Spotter agent: write VERDICT.json
    |
    v
Setter: read VERDICT.json
    |
    +-- approve -> create PRs -> task complete
    |
    +-- reject (attempt < 2) -> create correction goals -> task running -> spawn leads
    |
    +-- reject (attempt >= 2) -> task stuck
```

## New Package: `internal/spotter/`

### prompt.go

```go
type SpotterPromptData struct {
    Spec       string
    RepoDiffs  []RepoDiff
    Summaries  []GoalSummary
}

type RepoDiff struct {
    RepoName string
    DiffStat string
    Diff     string
}

type GoalSummary struct {
    GoalID      string
    RepoName    string
    Description string
    Status      string
    Summary     string
    Notes       string
}
```

Template instructs the agent to:
1. Review all diffs against the spec
2. Check cross-repo alignment (API contracts, shared types, etc.)
3. Write VERDICT.json with approve/reject per repo

### verdict.go

```go
type VerdictJSON struct {
    Verdict string                `json:"verdict"` // "approve" or "reject"
    Repos   map[string]RepoVerdict `json:"repos"`
}

type RepoVerdict struct {
    Status string   `json:"status"` // "pass" or "fail"
    Goals  []string `json:"goals"`  // correction goal descriptions (if fail)
}
```

## Changes to Existing Code

### TaskRunner (taskrunner.go)

New fields:
- `spotterAttempt int` - current review cycle (0, 1, 2)
- `spotterRunning bool` - whether spotter is currently active
- `taskDir string` - path for VERDICT.json

New methods:
- `GatherDiffs() ([]RepoDiff, error)` - runs `git diff` in each worktree
- `GatherSummaries() []GoalSummary` - reads DONE.json from each worktree
- `SpawnSpotter() error` - builds prompt, spawns agent
- `CheckSpotterVerdict() (*VerdictJSON, error)` - reads VERDICT.json if present
- `HandleApproval() error` - creates PRs for all repos
- `HandleRejection(verdict *VerdictJSON) ([]QueuedGoal, error)` - creates correction goals
- `CreatePRs() error` - runs git push + gh pr create per repo

### Setter (setter.go)

- Replace auto-complete placeholder with spotter spawning
- Add `reviewing` state handling in tick():
  - If task is reviewing and spotter not running: spawn spotter
  - If task is reviewing and spotter running: check for verdict
  - On verdict: handle approve/reject

### DAG

- `AddGoals(goals []model.Goal)` - adds new goals to existing DAG (for correction goals)

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Verdict file name | VERDICT.json | Distinct from lead's DONE.json; avoids confusion |
| Verdict location | Task directory | Spotter reviews all repos, not repo-specific |
| Diff gathering | Go exec in worktrees | Include diffs in prompt for deterministic context |
| PR creation | Direct exec (git push + gh pr create) | One-shot operation, no need for tmux |
| Correction goal IDs | `<repo>-corr-<attempt>-<index>` | Unique, traceable |
| DONE.json cleanup | Remove before spawning correction leads | Prevents false completion detection |
| Spotter workdir | Task directory | Natural location for VERDICT.json |
| Max reviews | 2 (from PRD) | Prevents infinite loops |

## Edge Cases

1. **Empty diff**: Repo had goals but no changes — spotter should flag this
2. **Spotter crash**: Window dies without VERDICT.json — treated like a failed goal, retried
3. **Partial rejection**: Some repos pass, others fail — only failing repos get correction goals
4. **Git errors**: Worktree deleted or corrupted — log error, mark repo as stuck
5. **PR creation failure**: Log error, don't mark task complete — user can retry manually
