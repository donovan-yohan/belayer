# Crag Architecture Review Fixes

> **Status**: Completed | **Created**: 2026-03-10 | **Completed**: 2026-03-10
> **Design Doc**: `docs/design-docs/2026-03-10-crag-architecture-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-10 | Review | Group fixes into 3 parallel tasks by concern | Maximize parallelism while avoiding conflicts |

## Progress

- [x] Task 1: Rename completion (JSON tags, internal fields, comments, log prefixes, dead code) _(completed 2026-03-10)_
- [x] Task 2: Error handling fixes (unchecked errors, silent failures) _(completed 2026-03-10)_
- [x] Task 3: Robustness (crash recovery state, env injection test) _(completed 2026-03-10)_

## Surprises & Discoveries

| Date | What was unexpected | Impact | Action taken |
|------|---------------------|--------|-------------|
| 2026-03-10 | Task 1 renamed QueuedClimb.Goal in taskrunner.go but not belayer.go/setter_test.go | Task 3 worker had to fix stale references to compile | Fixed .Goal → .Climb references in belayer.go and setter_test.go |

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

### Task 1: Rename Completion

Complete the naming overhaul that was missed in the initial rename pass.

**Files:**
- `internal/climbctx/climbctx.go` — Fix JSON tags
- `internal/anchor/verdict.go` — Fix JSON tag
- `internal/belayer/taskrunner.go` — Rename QueuedClimb.Goal→Climb, writeProfiles param goalID→climbID
- `internal/belayer/dag.go` — Rename internal goals field/params to climbs, remove dead MarkSpotting
- `internal/belayer/belayer.go` — Fix stale "setter:" log prefix
- `internal/model/types.go` — Update event type string constants
- `internal/defaults/claudemd/lead.md` — Fix spotter mail address format
- `internal/defaults/claudemd/spotter.md` — Fix spotter mail address format
- `internal/defaults/claudemd/anchor.md` — Fix spotter mail address format, fix anchor prompt climbs reference

**Steps:**
- [ ] Fix JSON tags in `climbctx.go`: `"task_spec"` → `"problem_spec"`, `"goal_id"` → `"climb_id"` in LeadClimb, AnchorClimb, ClimbSummary
- [ ] Fix `RepoVerdict.Goals` JSON tag from `"goals"` to `"climbs"` in `anchor/verdict.go`
- [ ] Rename `QueuedClimb.Goal` field to `QueuedClimb.Climb` and update all references in taskrunner.go
- [ ] Rename DAG internal `goals` field and method params from `goals` to `climbs` in dag.go
- [ ] Remove dead `MarkSpotting` method from dag.go
- [ ] Fix stale `"setter: "` log prefix to `"belayer: "` in belayer.go
- [ ] Rename `writeProfiles` param `goalID` to `climbID` in taskrunner.go
- [ ] Update stale `CheckCompletions` doc comment to reference "per-repo spotter activation"
- [ ] Update event type string constants in model/types.go (`"task_created"` → `"problem_created"`, `"goal_started"` → `"climb_started"`, etc.)
- [ ] Fix spotter mail address format in default prompts — remove `/<climbID>` suffix from spotter address in lead.md, spotter.md, anchor.md
- [ ] Fix anchor prompt to reference `"climbs"` key matching the Go struct
- [ ] Run `go build ./...` and `go test ./...` to verify
- [ ] Commit changes

### Task 2: Error Handling Fixes

Fix unchecked errors and silent failures identified in review.

**Files:**
- `internal/belayer/taskrunner.go` — Lines ~145, ~792, CheckRepoSpotResults, HandleApproval

**Steps:**
- [ ] Check `os.MkdirAll(tr.problemDir)` error at ~line 145 in Init()
- [ ] Check `json.Unmarshal` error at ~line 792 in ActivateSpotter
- [ ] Check store errors in `CheckRepoSpotResults` failure path
- [ ] Make `HandleApproval` propagate PR creation failures instead of always returning nil
- [ ] Fix `windowExists` to distinguish tmux errors from "window doesn't exist" in taskrunner.go
- [ ] Run `go build ./...` and `go test ./...` to verify
- [ ] Commit changes

### Task 3: Robustness Improvements

Fix crash recovery gap and add test coverage.

**Files:**
- `internal/belayer/taskrunner.go` — recover() function
- `internal/lead/spawner_test.go` — New test file or add to existing

**Steps:**
- [ ] Update `recover()` in taskrunner.go to restore `repoSpotterActivated` and `repoSpotterAttempts` from SQLite state
- [ ] Add test for env injection in `ClaudeSpawner.Spawn` — verify `export KEY=VALUE` prefix is built correctly
- [ ] Run `go build ./...` and `go test ./...` to verify
- [ ] Commit changes

---

## Outcomes & Retrospective

**What worked:**
- All 3 tasks ran fully in parallel — no blocking dependencies
- Workers self-healed cross-task conflicts (workers 2 and 3 independently fixed Goal→Climb references Task 1 missed)

**What didn't:**
- Task 1's rename scope was incomplete — missed belayer.go and setter_test.go references

**Learnings to codify:**
- Struct field renames need a full codebase grep, not just the defining file
