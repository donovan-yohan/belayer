# Research Toolkit Commands

> **Status**: Completed | **Created**: 2026-03-17 | **Last Updated**: 2026-03-17
> **Context**: `.lead/belayer-6/GOAL.json`
> **For Codex:** Use the harness-orchestrate workflow to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-17 | Planning | Scope this climb to the three shared research command assets plus the setter guidance, tests, and docs they require | The assigned climb description is narrower than the full explorer and draft-system epic in `GOAL.json`; finishing this reusable command slice cleanly is the defensible unit of work |
| 2026-03-17 | Execution | Make the research commands prefer session-provided paths, then fall back to `BELAYER_CRAG` and workspace inspection | The command markdown is static, so path resolution must work in setter today while staying reusable for explorer sessions once that runtime exists |
| 2026-03-17 | Review | Align shared research command handoffs with the phase/draft workflow and explorer-safe command subset | The branch’s session surfaces now converge on `/blr-phase-plan` plus the draft queue, so the shared `Next Steps` text cannot point at commands missing from explorer or bypass the draft flow |
| 2026-03-17 | Completion | Carry the already-present explorer bootstrap and draft-workflow slices through verification and doc reconciliation alongside the research toolkit | The branch state already coupled shared research commands, explorer workspace prep, draft command assets, and the active/completed plan index; finishing them in a consistent verified state was safer than leaving split documentation/runtime surfaces |

## Progress

- [x] Task 1: Add shared research command assets and setter workflow/context guidance _(completed 2026-03-17)_
- [x] Task 2: Extend embedded/defaults regression coverage and reconcile design docs _(completed 2026-03-17)_
- [x] Task 3: Run verification, review, reflect, archive, and write `.lead/belayer-6/TOP.json` _(completed 2026-03-17)_

## Surprises & Discoveries

- The branch already contained adjacent explorer bootstrap and draft-workflow work, so the research-toolkit slice had to reconcile against a live session surface that included explorer-only command subsets, draft queues, and multiple active/completed plan artifacts.
- Static command markdown cannot rely on template rendering, which made session-guidance-first path detection necessary instead of direct inline placeholders.
- Reused named explorer workspaces can drift unless stale generated command files are pruned before re-copying the explorer-safe subset.

## Plan Drift

- The closeout expanded beyond the original three command assets: review fixes required shared explorer/root publication (`Workspace Root`), explorer workspace pruning, direct env-scrubbing unit coverage, corrected draft-review status handoff text, and archival/docs reconciliation across the already-present explorer bootstrap and draft-workflow slices.

---

## Task 1: Add shared research command assets and setter workflow/context guidance

### Goal

Create `/blr-research`, `/blr-research-url`, and `/blr-research-summarize` as embedded command assets and expose the research workflow clearly in the setter runtime template.

### Files

- `internal/defaults/commands/blr-research.md`
- `internal/defaults/commands/blr-research-url.md`
- `internal/defaults/commands/blr-research-summarize.md`
- `internal/defaults/claudemd/setter.md`

### Steps

1. Author the three command files with shared context-detection instructions, the incremental `research-notes.md` append model, and explicit `Next Steps` footers.
2. Update the setter CLAUDE template to document the research workflow, the crag-docs research location, and how the commands chain together.
3. Keep the guidance compatible with future explorer sessions by making the commands prefer template/session-provided context first and documented fallbacks second.

### Verification

- `rg -n "blr-research|research-notes|research.md|Next Steps" internal/defaults/commands internal/defaults/claudemd/setter.md`

### Acceptance Criteria

- Three new `blr-` research command files exist under `internal/defaults/commands/`.
- The commands describe where research files live and how to append/summarize them.
- The setter template teaches the research workflow and names the new commands.

---

## Task 2: Extend embedded/defaults regression coverage and reconcile design docs

### Goal

Make the embedded command surface, copied setter workspace, and current docs reflect the expanded research toolkit.

### Files

- `internal/defaults/defaults_test.go`
- `internal/manage/prompt_test.go`
- `docs/DESIGN.md`
- `docs/PLANS.md`

### Steps

1. Extend defaults and manage-dir tests to assert the new command files are embedded and copied.
2. Add content assertions for the research workflow where helpful.
3. Update current design docs so the setter command inventory and runtime behavior describe the new research toolkit.

### Verification

- `go test ./internal/defaults ./internal/manage`

### Acceptance Criteria

- Tests prove the new commands are embedded and copied into `.claude/commands/`.
- Current design docs mention the research toolkit without stale command counts.

---

## Task 3: Run verification, review, reflect, archive, and write TOP.json

### Goal

Complete the full harness cycle for this climb and leave a reviewable, archived result.

### Files

- `docs/exec-plans/active/2026-03-17-research-command-toolkit.md`
- `docs/PLANS.md`
- `.lead/belayer-6/TOP.json`

### Steps

1. Run targeted and full verification relevant to the change.
2. Perform the review pass, fix any inline findings, and reflect doc/plan updates.
3. Archive the plan under `docs/exec-plans/completed/`.
4. Commit all changes and write `.lead/belayer-6/TOP.json`.

### Verification

- `go test ./...`
- `go build -o /tmp/belayer ./cmd/belayer`

### Acceptance Criteria

- Verification passes or any residual failure is captured in `TOP.json`.
- The plan is archived and no active-plan entry remains for this climb.
- The repo state is committed and `.lead/belayer-6/TOP.json` exists.

---

## Outcomes & Retrospective

- Added `/blr-research`, `/blr-research-url`, and `/blr-research-summarize` as embedded command assets with shared research-root resolution, append-only `research-notes.md` guidance, `research.md` compilation guidance, and phase/draft-oriented `Next Steps` footers that work in both setter and explorer sessions.
- Updated setter runtime guidance, the explorer template, README copy, CLI help text, and current design docs so the interactive session surfaces now advertise research-first workflows, explicit research roots, and the draft queue before publication.
- Extended regression coverage across defaults/manage/cli to prove the new research commands are embedded and copied, explorer workspaces publish a stable root, stale explorer command files are pruned on reuse, invalid dot-style names become unnamed workspaces, and BELAYER context variables are scrubbed/reapplied correctly for interactive session launches.
- Carried the already-present explorer bootstrap and draft-workflow slices through review and verification so the branch ends with consistent session docs, plan indexes, and command surfaces instead of split partial states.
- Verification passed with `go test ./internal/defaults ./internal/manage ./internal/cli`, `go test ./...`, `go build -o /tmp/belayer ./cmd/belayer`, `git diff --check`, and `belayer mail read` (`No unread messages.`).
