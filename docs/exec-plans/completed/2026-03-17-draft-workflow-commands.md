# Draft Workflow Commands

> **Status**: Completed | **Created**: 2026-03-17 | **Last Updated**: 2026-03-17
> **Context**: `.lead/belayer-7/GOAL.json`
> **For Codex:** Use the harness-orchestrate workflow to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-17 | Planning | Use `.lead/belayer-7/GOAL.json` as the source context instead of retrofitting an older design doc | No existing design doc cleanly scopes just the draft-workflow command slice, while the climb description already defines the required behavior precisely |
| 2026-03-17 | Planning | Treat `harness-init` as already satisfied by the existing `CLAUDE.md` map, Tier 2 docs, and exec-plan layout | The repo already has the expected harness structure, so this climb only needs normalization during reflection rather than doc bootstrap |
| 2026-03-17 | Planning | Keep implementation scoped to command assets, regression tests, and current-surface docs unless execution reveals a real code gap | `PrepareManageDir` already copies every embedded command dynamically, so the likely work is content and coverage, not new Go plumbing |
| 2026-03-17 | Execution | Use the zero-padded draft directory name as `draft_id` | Keeping `draft_id` aligned with the `problems/<nnn>/` directory makes `depends_on` references stable across creation, listing, and review without inventing a second identifier |
| 2026-03-17 | Execution | Build the draft workflow on top of concurrent explorer and research session changes already present in the worktree | `PrepareExplorerDir`, the research commands, and related docs/tests already referenced the draft workflow surface, so integrating with them was safer than trying to peel the branch back apart |
| 2026-03-17 | Completion | Archive only the belayer-7 plan and leave the other active harness plans in place | The repository still contains separate active plans for adjacent climbs, and belayer-7 should close without pretending to own their archival state |

## Progress

- [x] Task 1: Define the draft-workflow command contract, including context detection and draft metadata expectations
- [x] Task 2: Add the new command assets and expand regression coverage for embedded and copied command surfaces
- [x] Task 3: Reconcile docs, run verification and review, then reflect/archive/commit the completed work

## Surprises & Discoveries

- Parallel explorer and research workflow changes appeared in the same worktree during execution, including `PrepareExplorerDir`, shared research command assets, and additional docs/test coverage.
- The explorer command-subset test originally rejected unexpected files but did not prove that every expected explorer command was copied, so this climb expanded the assertion to check the full set.
- The runtime session surface needed template updates as well as new command files, because the draft commands rely on concrete `phases.md` and `~/.belayer/drafts/<crag>/problems/` locations being taught in session context.

## Plan Drift

- Execution expanded slightly beyond just adding four command files: it also tightened setter/explorer template guidance and explorer command-copy assertions so the shared research-plus-draft workflow stayed coherent.

---

## Task 1: Define the draft-workflow command contract

### Goal

Translate the climb spec into concrete command-asset behavior for `/blr-phase-plan`, `/blr-draft-create`, `/blr-draft-list`, and `/blr-draft-review`.

### Files

- `.lead/belayer-7/GOAL.json`
- `internal/defaults/claudemd/setter.md`
- `internal/defaults/commands/*.md`

### Steps

1. Confirm how setter sessions currently resolve crag context and copy commands.
2. Define how the commands should distinguish explorer workspace context from setter/crag-docs context.
3. Capture any non-obvious decisions in the Decision Log before implementation.

### Verification

- `rg -n "BELAYER_CRAG|commandsDir|problem create" internal/manage internal/cli internal/defaults`

### Acceptance Criteria

- The command requirements are translated into concrete paths and behaviors that can be encoded in static command markdown.
- Any deviations from the broader feature spec are documented in the plan.

---

## Task 2: Add the new command assets and coverage

### Goal

Create the four draft-system command files and make the embedded/copied command test surface prove they exist and remain internally consistent.

### Files

- `internal/defaults/commands/blr-phase-plan.md`
- `internal/defaults/commands/blr-draft-create.md`
- `internal/defaults/commands/blr-draft-list.md`
- `internal/defaults/commands/blr-draft-review.md`
- `internal/defaults/defaults_test.go`
- `internal/manage/prompt_test.go`
- `internal/defaults/claudemd/setter.md`

### Steps

1. Author the new command markdown files with context detection instructions, file-writing expectations, and explicit next-step footers.
2. Update the setter template only as needed so the runtime command surface remains discoverable and coherent.
3. Extend embedded-file and copied-command tests to cover the new assets and their key invariants.

### Verification

- `go test ./internal/defaults ./internal/manage`

### Acceptance Criteria

- All four command files are embedded under `internal/defaults/commands/`.
- Setter workspaces copy the new files into `.claude/commands/`.
- Tests assert the new command assets exist and cover the draft workflow contract at a useful level.

---

## Task 3: Reconcile docs and complete the harness cycle

### Goal

Review the implemented diff, update current-surface docs, and carry the work through verification, reflection, archival, commit, and `TOP.json`.

### Files

- `docs/DESIGN.md`
- `docs/PLANS.md`
- `docs/exec-plans/active/2026-03-17-draft-workflow-commands.md`
- `.lead/belayer-7/TOP.json`

### Steps

1. Run targeted and full verification relevant to the change.
2. Fix any review findings inline when they are low-risk and clearly within scope.
3. Refresh plan progress, retrospective, and any stale current-surface docs.
4. Archive the plan, commit the work, and write `.lead/belayer-7/TOP.json`.

### Verification

- `go test ./...`
- `go build -o /tmp/belayer ./cmd/belayer`

### Acceptance Criteria

- Review and verification are green, or any blocker is documented in `TOP.json`.
- The plan is archived under `docs/exec-plans/completed/`.
- The repo ends in a committed state with `.lead/belayer-7/TOP.json` written.

---

## Outcomes & Retrospective

- Added `/blr-phase-plan`, `/blr-draft-create`, `/blr-draft-list`, and `/blr-draft-review` as embedded command assets with explicit context detection, draft metadata rules, and `Next Steps` footers.
- Updated the setter and explorer runtime templates so sessions now teach the draft queue layout, `phases.md` location, and the research-to-draft workflow directly in `.claude/CLAUDE.md`.
- Strengthened defaults/manage regression coverage so tests now prove the new commands are embedded, copied into setter workspaces, and included in the explorer command subset rather than merely allowing them.
- Verification passed with `go test ./internal/defaults ./internal/manage`, `go test ./...`, and `go build -o /tmp/belayer ./cmd/belayer`.
