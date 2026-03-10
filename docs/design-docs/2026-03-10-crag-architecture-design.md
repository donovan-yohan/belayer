# Crag Architecture: Naming Overhaul + Per-Role Window Layout

## Problem Statement

Belayer's naming doesn't fully embrace its climbing metaphor, and the tmux architecture has practical issues:

1. **Naming gaps**: "instance", "task", "goal", "setter" (daemon) don't map to climbing terminology. The manage session is the human-facing interface but isn't named as such.
2. **Spotter timing**: Spotters are spawned after leads finish, meaning they can't receive mail during lead execution. Both should be alive simultaneously.
3. **Spotter granularity**: Per-goal spotting is redundant now that leads run the full harness review pipeline internally. Spotting should happen per-repo after all climbs complete.
4. **Session isolation**: All agents share one tmux session, which works, but window layout isn't intentional. Pre-creating role windows at task init gives clear structure.

## Naming Map

| Current | New | Climbing Rationale |
|---------|-----|--------------------|
| Instance | **Crag** | A persistent climbing area with its own problems |
| Task | **Problem** | The human setter defines problems on the wall |
| Goal | **Climb** | Leads climb the problems |
| Setter (daemon) | **Belayer** | Manages the ropes, sends leads up |
| Manage (user session) | **Setter** | The human sets problems and routes |
| DONE.json | **TOP.json** | Topping out = completing a climb |
| Flash | **Flash** | Completing all climbs without spotter rejection |

Unchanged: **Lead**, **Spotter**, **Anchor**, **SPOT.json**, **VERDICT.json**.

### Full Metaphor Flow

> The **setter** defines **problems** at the **crag**. The **belayer** sends **leads** up their **climbs**. When they **top** out, the **spotter** validates. If no retries were needed, it was **flashed**.

## Architecture: Per-Role Window Layout

### Window Layout Per Task Session

```
belayer-problem-{problemID}
├── window 0: spot-{repo}           (pre-created, activated when all climbs top)
├── window 1: {repo}-{climbID}      (lead 1)
├── window 2: {repo}-{climbID}      (lead 2)
├── ...
├── window N: {repo}-{climbID}      (lead N, up to max_leads)
└── anchor                          (pre-created, multi-repo only)
```

### Window Lifecycle

| Window | Created At | Activated At | Purpose |
|--------|-----------|--------------|---------|
| `spot-{repo}` | Problem init | All climbs for repo top | Per-repo PRD validation |
| `{repo}-{climbID}` | Climb spawn | Immediately | Lead implementation |
| `anchor` | Problem init (multi-repo) | All repos pass spotting | Cross-repo alignment |

**Deferred activation**: Pre-created windows are empty tmux windows. The belayer activates them by writing GOAL.json and using `SendKeys` to launch Claude. Zero cost while idle, but the window exists as a valid tmux target for mail delivery from the start.

### Signal Files

| File | Writer | Location | When |
|------|--------|----------|------|
| `TOP.json` | Lead | `.lead/{climbID}/TOP.json` | Climb topped |
| `SPOT.json` | Spotter | `.lead/{climbID}/SPOT.json` | Repo validated (written per-climb for traceability) |
| `VERDICT.json` | Anchor | `tasks/{problemID}/VERDICT.json` | Cross-repo alignment reviewed |

### Mail Addresses

| Role | Address |
|------|---------|
| Lead | `problem/{problemID}/lead/{repo}/{climbID}` |
| Spotter | `problem/{problemID}/spotter/{repo}` |
| Anchor | `problem/{problemID}/anchor` |
| Belayer (daemon) | `belayer` |

## Flow

1. **Problem init**: Create tmux session `belayer-problem-{problemID}`. Pre-create empty `spot-{repo}` windows (one per unique repo) and `anchor` window (if multi-repo). Create worktrees.

2. **Climb spawn**: Create `{repo}-{climbID}` windows for unblocked climbs. Launch leads with full harness pipeline (plan → orchestrate → review → reflect → complete). Multiple unblocked climbs run in parallel up to `max_leads`.

3. **All climbs for a repo top**: Belayer detects all climbs for repo X have `TOP.json`. Writes spotter GOAL.json with all TOP.json summaries + original PRD. Activates `spot-{repo}` via `SendKeys`.

4. **Spotter validates**: Checks repo's work against the PRD. Writes SPOT.json. On failure, belayer creates new correction climbs for that repo.

5. **All repos pass spotting**: Belayer activates `anchor` window (multi-repo only). Anchor reviews cross-repo alignment, writes VERDICT.json. Can send mail to belayer to redistribute work.

6. **Flash detection**: If all climbs have `attempt == 1` and spotter passes first try, report as flashed.

7. **PR creation + cleanup**: Create PRs per repo. Kill all windows, then session. Clean up mail.

## Spotter Changes

### Current (per-goal)
- SpawnSpotter called per-goal when DONE.json appears
- Kills lead window, creates new spotter window
- SpotterGoal contains single goal's DONE.json
- SPOT.json written per goal

### New (per-repo)
- Spotter window pre-created at problem init
- Activated when ALL climbs for a repo top
- SpotterGoal contains all TOP.json summaries for the repo + original PRD
- Validates the repo's complete body of work, not individual climbs
- CheckCompletions tracks per-repo completion instead of transitioning individual climbs

### New Completion Tracking

The belayer needs to know when all climbs for a given repo have topped. This requires:
- Grouping climbs by repo in the DAG
- Tracking repo-level completion state (not just climb-level)
- New status: repo transitions through `pending → climbing → spotting → complete`

## Rename Scope

### Phase 1: Naming (mechanical)

**Package renames:**
- `internal/setter/` → `internal/belayer/` (the daemon package)
- `internal/goalctx/` → `internal/climbctx/` (or rename types within)
- `internal/instance/` — keep package name but rename exported types/functions
- CLI: `manage` command → `setter` command
- CLI: `setter` command → `belayer` command (the daemon start/stop/status)
- CLI: `instance` command → `crag` command
- CLI: `task` command → `problem` command

**Type/field renames:**
- `Goal` → `Climb` (model, goalctx, DAG, store)
- `GoalStatus*` → `ClimbStatus*` enums
- `Task` → `Problem` (model, store, CLI)
- `TaskRunner` → `ProblemRunner`
- `SpawnGoal` → `SpawnClimb`
- `SpawnSpotter` → `ActivateSpotter`
- `SpawnAnchor` → `ActivateAnchor`
- `DONE.json` → `TOP.json` (all references)
- `DoneJSON` struct → `TopJSON`
- `GOAL.json` → consider keeping or renaming to `CLIMB.json`
- `LeadGoal` / `SpotterGoal` / `AnchorGoal` → `LeadClimb` / `SpotterClimb` / `AnchorClimb`
- Event constants: `EventGoalStarted` → `EventClimbStarted`, etc.

**SQLite migration:**
- New migration renaming tables: `tasks` → `problems`, `task_repos` → `problem_repos`
- Column renames where feasible, or new migration that creates new tables + migrates data

**CLI help text (cobra Use/Short/Long fields):**
- `internal/cli/instance.go` — `Use: "instance"` → `"crag"`, all help strings
- `internal/cli/task.go` — `Use: "task"` → `"problem"`, all help strings
- `internal/cli/manage.go` — `Use: "manage"` → `"setter"`, all help strings
- `internal/cli/setter.go` — `Use: "setter"` → `"belayer"`, all help strings
- `internal/cli/status.go` — "task and goal status" → "problem and climb status"
- `internal/cli/logs.go` — task/goal references in help text
- `internal/cli/message.go` — address format documentation
- `internal/cli/mail.go` — any task/goal references
- `internal/cli/root.go` — AddCommand wiring

**Default prompts (embedded templates):**
- `claudemd/manage.md` → `claudemd/setter.md` (rename + update content)
- `claudemd/lead.md` — DONE.json→TOP.json, goal→climb, task→problem, address formats
- `claudemd/spotter.md` — goal→climb, task→problem, address formats
- `claudemd/anchor.md` — goal→climb, task→problem, address formats

**Slash commands (internal/defaults/commands/):**
- `task-create.md` → `problem-create.md` (rename + update content)
- `task-list.md` → `problem-list.md` (rename + update content)
- `status.md` — task/goal → problem/climb
- `logs.md` — task/goal references
- `message.md` — address format documentation
- `mail.md` — any task/goal references

**Core Go files (log messages, error messages, comments):**
- `internal/setter/taskrunner.go` — extensive log.Printf and fmt.Errorf with old terms
- `internal/setter/setter.go` — state machine comments, log messages
- `internal/store/store.go` — query comments, function names
- `internal/instance/instance.go` — function names, comments
- `internal/mail/*.go` — address format references, message type docs
- `internal/logmgr/logmgr.go` — task/goal parameter names
- `internal/lead/spawner.go` — comments about goals
- `internal/lead/claude.go` — comments
- `internal/db/db.go` — migration comments

**Test files:**
- `internal/setter/setter_test.go` — DONE.json, task/goal in test names and assertions
- `internal/goalctx/goalctx_test.go` — type names, test function names
- `internal/lead/claude_test.go` — mock names, comments
- All other `*_test.go` with task/goal references

**Top-level docs:**
- `README.md` — full terminology overhaul (How It Works, Quickstart, CLI Reference, Architecture)
- `CLAUDE.md` — Key Patterns, Documentation Map, Quick Reference
- `docs/ARCHITECTURE.md` — Bird's Eye View, Code Map, Data Flow, Directory Layout
- `docs/DESIGN.md` — all sections (schema, validation pipeline, signal files, mail, etc.)
- `docs/QUALITY.md` — test descriptions
- `docs/TUI.md` — task/goal/instance terminology
- `docs/PLANS.md` — plan descriptions

**Historical docs (update for consistency but lower priority):**
- `docs/design-docs/index.md` — table entries
- `docs/design-docs/*.md` — old design docs (could add note at top: "terminology updated")
- `docs/exec-plans/completed/*.md` — historical plans
- `docs/plans/PRD-*.md` — if any exist

### Phase 2: Architecture (structural)

**Window layout:**
- Pre-create spotter and anchor windows at problem init
- Remove `SpawnSpotter` (create new window) → `ActivateSpotter` (SendKeys to existing window)
- Same for anchor

**Spotter per-repo:**
- New repo-level completion tracking in DAG
- SpotterGoal struct gets list of TOP.json summaries instead of single DONE.json
- CheckCompletions groups by repo instead of per-climb transitions

**Flash detection:**
- Track per-climb attempt counts
- Track per-repo spotter attempt counts
- Report flash when all attempts == 1

**Deferred activation pattern:**
- Extract into helper: `activateWindow(session, window, goalJSON, appendPrompt, initialPrompt, env)`
- Reused by both spotter and anchor activation

## Testing Strategy

- Update all existing tests with new names (Phase 1)
- Add test for repo-level completion detection (Phase 2)
- Add test for deferred window activation (Phase 2)
- Add test for flash detection (Phase 2)
- Integration: verify pre-created windows exist but are empty, then activated correctly
