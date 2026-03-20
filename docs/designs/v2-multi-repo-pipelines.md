---
status: implemented
implemented-by: docs/exec-plans/completed/2026-03-20-v2-multi-repo-pipelines.md
created: 2026-03-20
branch: master
---
# Design: Multi-Repo Pipelines — CWD Context, Repo Registry, Fan-Out/Fan-In

## Summary

Add multi-repo support to belayer v2. Users register repos globally, create named pipelines scoped to repo subsets, and belayer handles fan-out (parallel per-repo execution) and fan-in (cross-repo validation). The CLI is context-aware: commands resolve the current crag from CWD. Each pipeline run creates isolated git worktrees per repo to prevent concurrent edit conflicts.

## Goal

Support workflows like "build a feature across extend-app, extend-android, extend-ios, and extend-api" without requiring an LLM to discover which repos exist. Users declare repo sets upfront. The decomposer decides which repos need changes per task, but the candidates are a known set.

## Approach

### Global Repo Registry

Belayer maintains a global config at `~/.belayer/config.json`:

```json
{
  "repos": {
    "extend-app": { "path": "/Users/me/projects/extend-app" },
    "extend-android": { "path": "/Users/me/projects/extend-android" },
    "extend-ios": { "path": "/Users/me/projects/extend-ios" },
    "extend-api": { "path": "/Users/me/projects/extend-api" }
  },
  "crags": {
    "extend-platform": "/Users/me/projects/extend-platform"
  }
}
```

CLI: `belayer repo add extend-app ~/projects/extend-app`
CLI: `belayer repo list`
CLI: `belayer repo remove extend-app`

### Crag = Directory with Pipeline(s)

A crag is a directory. It contains one or more pipeline YAML files, each scoped to a subset of registered repos:

```
~/projects/extend-platform/          ← crag directory
  belayer-pipeline.yaml              ← default pipeline (full-stack)
  belayer-pipeline-backend.yaml      ← backend-only pipeline
  belayer-pipeline-frontend.yaml     ← frontend-only pipeline
  .belayer/                          ← runtime state
    worktrees/                       ← per-run worktrees (auto-managed)
```

Each pipeline declares which repos from the global registry it operates on:

```yaml
# belayer-pipeline.yaml (full-stack)
name: full-stack
repos: [extend-app, extend-android, extend-ios, extend-api]
phases:
  - phase: ascent
    roles:
      - name: decomposer
        fan_out: repos
      - name: lead
        per: repo
      - name: spotter
        per: repo
      - name: anchor
        fan_in: repos
```

```yaml
# belayer-pipeline-backend.yaml
name: backend
extends: belayer-pipeline.yaml
repos: [extend-api]
```

Running a specific pipeline: `belayer run --pipeline belayer-pipeline-backend.yaml "add rate limiting"`

### Per-Run Worktree Isolation

Each pipeline run creates **git worktrees** per repo, not editing live checkouts. This ensures concurrent runs don't conflict and provides clean branch/cleanup lifecycle.

```
.belayer/worktrees/
  <run-id>/                        ← per pipeline run
    extend-api/                    ← git worktree (branch: belayer/<run-id>)
    extend-app/                    ← git worktree
```

Lifecycle:
1. **Before fan-out:** For each needed repo, create a git worktree from the registered repo path: `git -C <repo-path> worktree add <worktree-path> -b belayer/<run-id>`
2. **Lead sessions run in the worktree**, committing to the `belayer/<run-id>` branch
3. **On pipeline completion:** Push branch to remote (`git push origin belayer/<run-id>`), create PRs from the pushed branch, then remove worktree. Code is safe on the remote before any cleanup happens.
4. **On pipeline failure/cancellation:** Check for unpushed commits. If unpushed work exists → **preserve the worktree and warn** ("worktree preserved at .belayer/worktrees/<run-id>/<repo> — has unpushed commits"). If no unpushed commits → safe to remove. Never silently delete work.
5. **Manual recovery:** User can `cd .belayer/worktrees/<run-id>/<repo>` to inspect preserved worktrees, push manually, or resume work

Single-repo fallback: if no `repos` field, the lead runs in CWD (no worktree — same as current behavior). Worktrees are only created for multi-repo pipelines.

### CWD Context Awareness

When you run `belayer` from a crag directory:
- `belayer run "..."` uses `belayer-pipeline.yaml` by default
- `belayer status` shows only runs started from this directory
- `belayer attach setter` shows only sessions from this crag

The workflow stores the crag directory path as a Temporal custom search attribute for filtering. The tmux session name includes the crag name for `attach` scoping.

### Named Crags + `belayer cd`

Users can name crags for quick navigation:

```bash
belayer crag init --name extend-platform    # Registers CWD as a named crag
belayer crag list                           # Shows all named crags
belayer cd extend-platform                  # Outputs the path (for shell alias)
```

Shell alias for convenience: `bcd() { cd "$(belayer cd "$1")" }`

### Fan-Out / Fan-In in the Pipeline DSL

Three role annotations:

- **`fan_out: repos`** — Role output creates per-repo tasks. The decomposer receives the full repo list and problem spec, outputs a map of `repo_name → task_spec`. Repos the decomposer deems unnecessary are skipped (but the anchor still receives the skip justifications for cross-repo validation).
- **`per: repo`** — Role runs once per repo from the fan-out. Each instance gets a worktree path as WorkDir and its specific task spec.
- **`fan_in: repos`** — Role receives all per-repo results AND the decomposer's skip justifications as combined input. The anchor can flag if a skipped repo was actually needed.

Temporal mapping:
- `fan_out` → workflow dispatches decomposer activity, parses output into per-repo map
- `per: repo` → workflow creates worktrees, then spawns parallel `workflow.Go()` goroutines per needed repo
- `fan_in` → workflow collects all parallel results + skip reasons, dispatches anchor activity

### Decomposer Contract for Multi-Repo

The decomposer's input includes the known repo list from the pipeline YAML. Its output is a map:

```json
{
  "repos": {
    "extend-api": {
      "needed": true,
      "spec": "Create /api/v2/auth endpoint with JWT validation...",
      "depends_on": []
    },
    "extend-app": {
      "needed": true,
      "spec": "Add login page, connect to /api/v2/auth...",
      "depends_on": ["extend-api"]
    },
    "extend-android": {
      "needed": false,
      "reason": "No Android changes needed for this feature"
    },
    "extend-ios": {
      "needed": false,
      "reason": "No iOS changes needed for this feature"
    }
  }
}
```

The workflow skips repos marked `needed: false` for lead/spotter execution, but passes the skip reasons to the anchor for cross-repo validation. The decomposer makes the judgment call, but the anchor can override: if changes in extend-api require corresponding changes in a skipped repo, the anchor flags it and the workflow can loop back.

### Decomposer Override via Risk Gate

The decomposer's output passes through a risk gate before fan-out begins. If the risk score is above threshold (e.g., the decomposer skipped 3 of 4 repos), the pipeline pauses for human review. The user can:
- **Approve** — proceed with the decomposer's plan
- **Override** — mark additional repos as `needed: true` and provide specs
- **Reject** — loop back to the setter for re-scoping

This prevents false negatives from silently skipping repos.

### Single-Repo Fallback

If a pipeline has no `repos` field, or has exactly one repo, fan-out/fan-in is skipped entirely. Lead runs in CWD (no worktree creation). No decomposer needed — the setter output goes directly to lead. This is a unified code path: single-repo is a fan-out of 1 with synthetic input.

This means `belayer run "fix the login bug"` from within a single repo just works, no pipeline YAML needed.

### Pipeline Inheritance

Pipelines can extend a parent using `extends:`:

```yaml
# belayer-pipeline-backend.yaml
name: backend
extends: belayer-pipeline.yaml    # Inherits all phases/roles
repos: [extend-api]               # Overrides repo scope only
```

Resolution rules:
- `extends:` is relative to the same directory (no absolute paths, no `../`)
- Child fields override parent fields (shallow merge)
- `repos`, `name`, and `safety` are fully replaced if present in child
- Phases are inherited unless the child declares its own
- Circular extends detected at parse time via visited-set DFS

## Engineering Review Findings (2026-03-20)

### Must-Fix: Signal Routing for Multi-Repo
Add `Repo` field to `RoleSignal` and `--repo` flag to `belayer <role> finish/flare/fail`. Multi-repo Type B roles (leads running in parallel) need Role+Repo matching to route signals correctly. Single-repo defaults (--repo optional).

### Architecture Decision: Unified Fan-Out
Use `workflow.Go()` goroutines within one workflow (not child workflows). Single-repo = fan-out of 1, same code path. No branching for "is this multi-repo?" — the fan-out count just varies.

### Window Naming
Multi-repo leads: `lead-<repo>-<taskid[:8]>` (e.g., `lead-extend-api-019d0b95`). Enables `belayer attach lead --repo extend-api`.

### Partial Flare Behavior
If one lead flares, other leads continue. The anchor waits for all to complete/resolve. If one lead fails, the workflow loops back to the decomposer.

## Codex Review Findings (2026-03-20)

### Fixed: WorkDir Isolation (Codex #2)
Added per-run worktree creation. Leads run in isolated git worktrees, not live checkouts. Prevents concurrent run conflicts and provides clean branch lifecycle.

### Fixed: Decomposer Veto Override (Codex #5)
Added risk gate on decomposer output + anchor receives skip justifications. Prevents false negatives from silently excluding needed repos.

### Fixed: Signal Collision (Codex #1)
Same as eng review finding — `--repo` flag on signals.

### Acknowledged: CWD Context Specifics (Codex #6)
CWD resolution uses `filepath.Abs()` + walks up looking for `belayer-pipeline.yaml`. Tmux sessions named per-crag for attach scoping. Details resolved at implementation.

### Rejected: Overbuilt Concern (Codex #7)
The modes are progressive and opt-in: zero-config (CWD) → pipeline file → multi-repo → named crags. Each layer adds capability without requiring the previous. This is the correct design for a tool that serves both solo developers and multi-repo teams.

## Key Decisions

- Single `~/.belayer/config.json` with `repos{}` and `crags{}` sections
- Repos registered globally, referenced by name from any pipeline
- Pipelines declare repo subsets — users control scope, not the LLM
- Multiple pipelines per crag via `extends:` inheritance
- Per-run git worktrees for multi-repo isolation (single-repo uses CWD)
- Decomposer decides which repos need changes, from fixed candidate set
- Anchor receives skip justifications to catch false negatives
- Risk gate on decomposer output for human override of repo selection
- Signal routing: Role+Repo matching with `--repo` flag
- Fan-out via `workflow.Go()` goroutines (unified code path for 1 or N repos)
- Partial flare: other leads continue; fail: loop back to decomposer
- Dependency ordering via `depends_on` in decomposer output
- CWD context via Temporal search attributes + tmux session naming
- Named crags are optional sugar for navigation
