# Setter Research And Draft Guidance

> **Status**: Completed | **Created**: 2026-03-17 | **Last Updated**: 2026-03-17
> **Context**: `.lead/belayer-8/GOAL.json`
> **For Codex:** Use the harness-orchestrate workflow to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-17 | Planning | Use `.lead/belayer-8/GOAL.json` as the source context instead of waiting for a dedicated design doc | The climb description already scopes the setter-specific work precisely and the repository has no narrower belayer-8 design artifact |
| 2026-03-17 | Planning | Treat harness initialization and review-persona setup as already satisfied | The repo already has `docs/`, `docs/exec-plans/`, `CLAUDE.md`, and `review-personas.toml`, so belayer-8 only needs a new active plan |
| 2026-03-17 | Planning | Start from the existing staged explorer/research/draft branch state rather than restaging that adjacent work | The worktree already contains the larger shared-command rollout; belayer-8 should close the setter-runtime gaps without undoing or re-splitting the integrated change set |
| 2026-03-17 | Execution | Treat setter workspace prep as "refresh all embedded commands and prune stale generated legacy files" instead of maintaining a second hard-coded setter command list | Setter relaunches must keep the full shared research and draft toolkit while removing renamed legacy commands from reused crags, and deriving the allowed set from embedded defaults avoids command-list drift over time |
| 2026-03-17 | Completion | Archive belayer-8 after green verification, doc-health checks, and no remaining local review findings | The setter-session slice landed with the requested workflow guidance plus the reuse fix needed to keep existing crags aligned with the `blr-` command surface |

## Progress

- [x] Task 1: Audit the staged setter-session surface against the belayer-8 contract and capture drift _(completed 2026-03-17)_
- [x] Task 2: Tighten setter guidance and deterministic command publication for reused crag sessions _(completed 2026-03-17)_
- [x] Task 3: Run verification, review, reflect, archive, commit, and write `.lead/belayer-8/TOP.json` _(completed 2026-03-17; commit + TOP follow this archival step)_

## Surprises & Discoveries

- The branch already includes shared `/blr-research*`, `/blr-phase-plan`, and `/blr-draft-*` command assets plus explorer bootstrap work, so this climb starts from integration review rather than greenfield implementation.
- `PrepareManageDir` originally copied the current embedded commands into setter sessions but did not remove renamed legacy command files on reuse, so the belayer-8 fix had to treat command publication as a prune-and-refresh step instead of a blind copy.

## Plan Drift

- The final implementation tightened reuse behavior more than the original climb description stated by explicitly pruning legacy generated setter commands (`problem-create.md`, `research.md`, and peers). This was required to make the `blr-` command rename actually stick for existing crags instead of only for fresh workspaces.

---

## Task 1: Audit the staged setter-session surface

### Goal

Confirm which parts of the belayer-8 setter contract are already satisfied and identify any remaining runtime or prompt gaps.

### Files

- `.lead/belayer-8/GOAL.json`
- `internal/defaults/claudemd/setter.md`
- `internal/manage/prompt.go`
- `internal/manage/prompt_test.go`
- `internal/defaults/defaults_test.go`

### Steps

1. Compare the staged setter template and copied command surface with the belayer-8 requirements around research workflows, shared command references, draft consumption, and operating-principles enforcement.
2. Inspect `PrepareManageDir` behavior for existing crags, especially around stale generated commands after the `blr-` rename.
3. Record any non-obvious implementation decision before editing.

### Verification

- `rg -n "Operating Principles|blr-research|blr-draft-list|blr-draft-review|PrepareManageDir" internal/defaults/claudemd/setter.md internal/manage/prompt.go internal/manage/prompt_test.go internal/defaults/defaults_test.go`

### Acceptance Criteria

- Remaining belayer-8 gaps are concrete and traceable to files.
- Any discovered drift from the broader explorer/research rollout is captured in this plan.

---

## Task 2: Tighten setter guidance and command publication

### Goal

Make setter sessions explicitly enforce belayer operating principles and ensure reused crag sessions receive the intended research and draft command set without stale generated files.

### Files

- `internal/defaults/claudemd/setter.md`
- `internal/manage/prompt.go`
- `internal/manage/prompt_test.go`
- `internal/defaults/defaults_test.go`
- `docs/DESIGN.md`
- `docs/ARCHITECTURE.md`

### Steps

1. Add an explicit `## Operating Principles` section to the setter template that keeps users inside belayer problem-creation workflows unless they clearly override.
2. Make `PrepareManageDir` treat the embedded command directory as the canonical setter command set, while still pruning stale generated commands in reused crag workspaces.
3. Extend focused tests and any current-surface docs needed to prove the setter runtime contract without duplicating adjacent climb docs.

### Verification

- `go test ./internal/manage ./internal/defaults ./internal/cli`

### Acceptance Criteria

- Setter guidance documents the research and draft queue workflows plus the operating-principles boundary.
- Re-running setter workspace preparation removes stale generated legacy commands while preserving the intended `blr-` command set.
- Tests prove the research and draft command assets are embedded and copied into setter sessions.

---

## Task 3: Complete the harness cycle

### Goal

Carry belayer-8 through verification, review, reflection, archival, commit, and completion signaling.

### Files

- `docs/exec-plans/active/2026-03-17-setter-research-draft-guidance.md`
- `docs/PLANS.md`
- `.lead/belayer-8/TOP.json`

### Steps

1. Run targeted and full verification relevant to the branch state.
2. Perform harness review, fix any inline findings, and refresh docs plus plan retrospective.
3. Archive the plan under `docs/exec-plans/completed/`.
4. Commit the worktree and write `.lead/belayer-8/TOP.json`.

### Verification

- `go test ./...`
- `go build -o /tmp/belayer ./cmd/belayer`

### Acceptance Criteria

- Verification is green or any residual issue is captured in `TOP.json`.
- The plan is archived and removed from the active-plan index.
- The repo ends with a commit containing the belayer-8 work and `.lead/belayer-8/TOP.json`.

---

## Outcomes & Retrospective

- Added an explicit `## Operating Principles` section to [`internal/defaults/claudemd/setter.md`](/Users/donovanyohan/.belayer/crags/belayer/tasks/problem-1773788643787517000/belayer/internal/defaults/claudemd/setter.md) so setter sessions now enforce belayer-first routing, point users toward the shared research and draft workflow, and treat explorer-produced drafts as the normal handoff into publication.
- Tightened [`internal/manage/prompt.go`](/Users/donovanyohan/.belayer/crags/belayer/tasks/problem-1773788643787517000/belayer/internal/manage/prompt.go) so `PrepareManageDir` refreshes the full embedded setter command surface and prunes renamed legacy generated commands on reused crags instead of only copying current files into place.
- Extended [`internal/manage/prompt_test.go`](/Users/donovanyohan/.belayer/crags/belayer/tasks/problem-1773788643787517000/belayer/internal/manage/prompt_test.go), along with the already-expanded defaults coverage, to prove the setter prompt contract and the reused-workspace cleanup path.
- Refreshed [`docs/DESIGN.md`](/Users/donovanyohan/.belayer/crags/belayer/tasks/problem-1773788643787517000/belayer/docs/DESIGN.md) and [`docs/ARCHITECTURE.md`](/Users/donovanyohan/.belayer/crags/belayer/tasks/problem-1773788643787517000/belayer/docs/ARCHITECTURE.md) so the current docs describe setter launches as a refresh-and-prune operation rather than a one-time temp-workspace copy.
- Verification passed with `go test ./internal/manage ./internal/defaults ./internal/cli`, `go test ./...`, `go build -o /tmp/belayer ./cmd/belayer`, `git diff --check`, and `go run ./cmd/belayer mail read` (`No unread messages.`).
- Local review found no remaining belayer-8 issues after the setter command refresh path, legacy-command cleanup test, and operating-principles wording landed.
