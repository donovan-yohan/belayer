# Explorer Session Resume Flow

> **Status**: Active | **Created**: 2026-03-17 | **Last Updated**: 2026-03-17
> **Context**: `.lead/belayer-5/GOAL.json`
> **For Codex:** Use the harness-orchestrate workflow to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-17 | Planning | Treat belayer-5 as the follow-up explorer CLI slice: interrupted-session detection and resume-or-start-fresh behavior on top of the already-landed explorer bootstrap work | The worktree already contains the baseline `belayer explorer` command, template rendering, and launcher wiring from adjacent climbs, and the archived explorer bootstrap plan explicitly deferred this UX gap |
| 2026-03-17 | Planning | Implement the resume decision in the CLI layer with Cobra I/O instead of adding a new prompting dependency | The behavior is interactive but small, and keeping it in the command layer makes it testable while avoiding a broader manage-package UX framework |
| 2026-03-17 | Planning | Scope the conflict choice to `resume` versus `start fresh` for launch-time named-workspace collisions and record the broader rename/delete options as remaining drift | The climb description and acceptance test T-4 require resume-or-start-fresh handling, while the richer rename/delete UX applies to later in-session naming conflicts from the broader problem spec |

## Progress

- [x] Task 1: Add named explorer workspace conflict detection with resume-or-start-fresh prompting in the CLI flow _(completed 2026-03-17)_
- [x] Task 2: Cover the new prompt and fresh-reset behavior with focused CLI/manage tests _(completed 2026-03-17)_
- [ ] Task 3: Reconcile docs, run verification, review/reflect the slice, archive the plan, commit, and write `.lead/belayer-5/TOP.json`

## Surprises & Discoveries

- The current worktree already includes the explorer bootstrap implementation and adjacent research-command/template changes, but the named-workspace flow still silently reused existing directories until this slice.
- Another active plan already exists in `docs/exec-plans/active/`; belayer-5 needs to coexist cleanly in the plan index instead of assuming exclusive ownership of the active-plan table.

## Plan Drift

- The broader problem spec still mentions rename/delete handling for mid-session naming conflicts. This slice only adds the launch-time `resume` versus `start fresh` decision required by the climb description and T-4.

---

## Task 1: Add interrupted-session handling for named explorer workspaces

### Goal

Detect when `belayer explorer --name <project>` is pointed at an existing workspace, ask whether to resume or start fresh, and then launch Claude in the selected workspace.

### Files

- `internal/cli/explorer_cmd.go`
- `internal/manage/prompt.go`

### Steps

1. Add a stable way to derive the sanitized named explorer workspace path before preparing the workspace.
2. When a named workspace already exists, prompt through Cobra I/O for `resume` or `start fresh`.
3. Preserve resume behavior for existing workspaces and delete/recreate the workspace only when the user explicitly chooses a fresh start.

### Verification

- `go test ./internal/cli ./internal/manage`

### Acceptance Criteria

- Named explorer relaunches no longer silently reuse existing workspaces.
- The prompt supports resuming the prior workspace without deleting it.
- Choosing a fresh start removes the previous workspace contents before re-preparing `.claude/`.

---

## Task 2: Add regression coverage for the interrupted-session flow

### Goal

Prove the new prompt flow, resume behavior, and fresh-reset path with deterministic tests.

### Files

- `internal/cli/explorer_cmd_test.go`
- `internal/manage/prompt_test.go`

### Steps

1. Add CLI tests for resuming and starting fresh on an existing named workspace.
2. Keep the existing unnamed-workspace behavior covered and ensure prompt-free launches still work.
3. Add or adjust manage tests only where needed for the sanitized workspace-path contract.

### Verification

- `go test ./internal/cli ./internal/manage`

### Acceptance Criteria

- Tests prove both resume and fresh branches.
- Tests verify stale workspace contents survive resume and are removed on fresh start.

---

## Task 3: Verify, reflect, and complete the harness cycle

### Goal

Close the belayer-5 slice with current docs, review, archival, a commit, and a `TOP.json` handoff artifact.

### Files

- `docs/PLANS.md`
- `docs/DESIGN.md`
- `docs/ARCHITECTURE.md`
- `docs/exec-plans/active/2026-03-17-explorer-session-resume.md`
- `.lead/belayer-5/TOP.json`

### Steps

1. Run targeted and repo-wide verification.
2. Update plan progress, surprises, drift, and retrospective with the final behavior.
3. Reconcile any stale docs describing silent explorer workspace reuse.
4. Archive the plan, commit the worktree, and write `TOP.json`.

### Verification

- `go test ./...`
- `go build -o /tmp/belayer ./cmd/belayer`

### Acceptance Criteria

- Verification passes or any failure is documented in `TOP.json`.
- The archived plan and summary docs match the final explorer behavior.
- The repository ends with a commit containing the completed belayer-5 work and `TOP.json`.

---

## Outcomes & Retrospective

_Filled or refreshed by harness-reflect before archival._
