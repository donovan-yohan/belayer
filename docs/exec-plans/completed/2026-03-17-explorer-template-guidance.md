# Explorer Template Guidance

> **Status**: Completed | **Created**: 2026-03-17 | **Last Updated**: 2026-03-17
> **Context**: `.lead/belayer-4/GOAL.json`
> **For Codex:** Use the harness-orchestrate workflow to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-17 | Planning | Use `.lead/belayer-4/GOAL.json` as the source context instead of waiting for a separate design doc | The climb description already scopes the template slice precisely, and no narrower design doc exists for this artifact alone |
| 2026-03-17 | Planning | Treat `harness:init` as already satisfied by the existing `CLAUDE.md`, `docs/`, and exec-plan structure | The repository already has the harness layout, so belayer-4 only needs a new active plan |
| 2026-03-17 | Planning | Focus execution on explorer template content quality, tests, and current-surface docs instead of redoing the explorer bootstrap plumbing | Adjacent branch work already introduced `ExplorerPromptData`, `PrepareExplorerDir`, and baseline explorer coverage; belayer-4 should own the prompt contract and draft-quality guidance |
| 2026-03-17 | Execution | Teach the `spec.md` and `climbs.json` quality bar directly in `explorer.md` instead of assuming future slash commands will convey it | The belayer-4 goal is about the prompt contract itself, so the template must stay informative even before the draft command suite is fully available |
| 2026-03-17 | Completion | Archive belayer-4 after green verification, no remaining local review findings, and a healthy prune pass | The template slice landed with the intended prompt contract, tests, and current-surface docs aligned |

## Progress

- [x] Task 1: Audit the current explorer template against the belayer-4 contract and capture any drift _(completed 2026-03-17)_
- [x] Task 2: Refine the explorer template and regression coverage so the draft workflow teaches belayer's problem/climb model clearly _(completed 2026-03-17)_
- [x] Task 3: Run verification, review, reflect, archive, commit, and write `.lead/belayer-4/TOP.json` _(completed 2026-03-17)_

## Surprises & Discoveries

- The branch already contains a baseline `internal/defaults/claudemd/explorer.md` and related explorer bootstrap code from an adjacent climb, so this plan starts from refinement rather than greenfield template creation.
- The baseline explorer template already had the five phase headings and draft-workflow command names, but it did not explain the `spec.md`/`climbs.json` artifact model well enough to satisfy the belayer-4 contract.

## Plan Drift

_None yet._

---

## Task 1: Audit the current explorer template

### Goal

Confirm which parts of the belayer-4 template contract are already satisfied and which still need explicit guidance.

### Files

- `.lead/belayer-4/GOAL.json`
- `internal/defaults/claudemd/explorer.md`
- `internal/manage/prompt_test.go`
- `docs/DESIGN.md`

### Steps

1. Compare the current explorer template with the requested identity, five-phase workflow, operating-principles enforcement, workflow documentation, and draft-quality guidance.
2. Check whether the template already teaches the `spec.md` and `climbs.json` model well enough for draft authoring quality.
3. Record any non-obvious decision or deviation before editing.

### Verification

- `rg -n "Operating Principles|Five-Phase Workflow|Draft Problems|climbs.json|spec.md" internal/defaults/claudemd/explorer.md internal/manage/prompt_test.go docs/DESIGN.md`

### Acceptance Criteria

- Gaps between the current template and the belayer-4 goal are identified.
- Any necessary drift from the broader explorer epic is documented in the plan.

---

## Task 2: Refine explorer guidance and coverage

### Goal

Make the explorer template a strong source of truth for pre-crag sessions, especially around producing high-quality belayer problem drafts.

### Files

- `internal/defaults/claudemd/explorer.md`
- `internal/manage/prompt_test.go`
- `docs/DESIGN.md`

### Steps

1. Add or tighten guidance for explorer identity, five phases, operating-principles enforcement, and handoff expectations.
2. Add explicit draft-quality guidance covering `spec.md`, `climbs.json`, repo scoping, climb IDs, and `depends_on` semantics.
3. Extend focused tests and current-surface docs only where they materially prove the template contract.

### Verification

- `go test ./internal/manage ./internal/defaults`

### Acceptance Criteria

- The explorer template explicitly teaches the problem/climb model instead of only naming the draft commands.
- Tests cover the new template guidance at a useful level.
- Current docs do not under-describe the explorer session contract.

---

## Task 3: Complete the harness cycle

### Goal

Carry the belayer-4 slice through verification, review, reflection, archival, commit, and completion signaling.

### Files

- `docs/exec-plans/active/2026-03-17-explorer-template-guidance.md`
- `docs/PLANS.md`
- `.lead/belayer-4/TOP.json`

### Steps

1. Run targeted and full verification relevant to the change.
2. Perform harness review, fix any inline findings, and refresh docs/plan retrospective.
3. Archive the plan under `docs/exec-plans/completed/`.
4. Commit all changes and write `.lead/belayer-4/TOP.json`.

### Verification

- `go test ./...`
- `go build -o /tmp/belayer ./cmd/belayer`

### Acceptance Criteria

- Verification is green or any residual issue is captured in `TOP.json`.
- The plan is archived and removed from the active-plan index.
- The repo ends with a commit containing the belayer-4 work and `.lead/belayer-4/TOP.json`.

---

## Outcomes & Retrospective

- Refined `internal/defaults/claudemd/explorer.md` so the explorer prompt now teaches the five-phase workflow, reinforces the stay-in-explorer-until-handoff boundary, and explains the `spec.md` plus repo-scoped `climbs.json` quality bar directly in the template.
- Expanded `internal/manage/prompt_test.go` with explorer-specific assertions for the phase headings, operating-principles section, handoff boundary, and draft-artifact guidance so the prompt contract is enforced in tests.
- Updated `docs/DESIGN.md` and `docs/PLANS.md` so the current-surface docs describe the explorer template as a workflow-and-draft-model guide rather than only a rendered workspace artifact.
- Verification passed with `go test ./internal/manage ./internal/defaults`, `go test ./...`, `go build -o /tmp/belayer ./cmd/belayer`, and `git diff --check`.
- Local review found no remaining belayer-4 issues after loosening the new template assertions to check key concepts instead of brittle full-sentence wording.
