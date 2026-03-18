# Crag Create Local Paths

> **Status**: Completed | **Created**: 2026-03-17 | **Last Updated**: 2026-03-17
> **Context**: `.lead/belayer-2/GOAL.json`
> **For Codex:** Use the harness-orchestrate workflow to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-17 | Planning | Treat this climb as a targeted `crag create` contract change rather than a broader explorer feature batch. | The assigned climb description narrows the work to `--local-paths` support in `internal/cli/crag.go` and config persistence. |
| 2026-03-17 | Execute | Gate filesystem sources at the CLI with `--local-paths` instead of changing the lower-level `crag.Create` API contract. | The user-facing requirement is specifically a CLI flag, and `crag.Create` already clones local repos correctly once the input is permitted. |
| 2026-03-17 | Execute | Keep the existing `crag.json` `url` field and clarify it as a generic clone source in code comments. | This preserves on-disk compatibility while still storing local paths verbatim as required. |
| 2026-03-17 | Review | Add clearer GitHub tracker errors for local-path crags instead of silently widening tracker support in this climb. | The review surfaced a downstream `owner/repo` assumption; clearer guardrails are low risk and avoid turning this focused CLI change into a broader tracker feature. |
| 2026-03-17 | Complete | Archive the plan after verification, review, and doc reconciliation all passed. | The implementation and documentation are aligned, and no additional open findings remain for this climb. |

## Progress

- [x] Task 1: Add the active plan and required review persona config. _(completed 2026-03-17)_
- [x] Task 2: Implement `belayer crag create --local-paths` semantics in CLI and crag creation flow. _(completed 2026-03-17)_
- [x] Task 3: Add regression coverage and update user-facing docs for local path usage. _(completed 2026-03-17)_
- [x] Task 4: Run verification, review, reflection, and archival closeout. _(completed 2026-03-17)_

## Surprises & Discoveries

- Existing `crag.Create` logic already clones from local git repository paths and persists the original source string in `crag.json`; the gap is an explicit CLI flag and contract validation around path inputs.
- GitHub tracker integration still assumes the first crag repo source can be converted into `owner/repo`; local-path crags therefore remain incompatible with that workflow until a follow-up change broadens tracker source handling.
- POSIX paths containing `:` can look like SSH clone strings; the final validation/name-extraction logic now prefers real on-disk directories when `--local-paths` is enabled.

## Plan Drift

- Added explicit tracker-side error messaging and documentation for local-path crags using GitHub tracker integration.

---

## Task 1: Plan And Review Persona Setup

### Goal

Create the living execution plan for this climb and add the required `review-personas.toml` file before the review phase.

### Files

- `docs/exec-plans/active/2026-03-17-crag-local-paths.md`
- `docs/PLANS.md`
- `review-personas.toml`

### Steps

1. Add this active plan document.
2. Register the plan in `docs/PLANS.md`.
3. Add a backend-oriented `review-personas.toml` with at least `test-engineer`, `domain-expert`, and `code-quality`.
4. Commit the review persona config separately as required by the climb instructions.

### Verification

- `git diff -- docs/PLANS.md review-personas.toml docs/exec-plans/active/2026-03-17-crag-local-paths.md`

### Acceptance Criteria

- The repo has one active plan entry for this climb.
- `review-personas.toml` exists at the repo root.
- The review persona config is committed separately before the review phase.

---

## Task 2: CLI And Crag Semantics

### Goal

Make `belayer crag create` explicitly support local repository paths via `--local-paths` while keeping remote URL behavior intact and persisting the chosen source values into `crag.json`.

### Files

- `internal/cli/crag.go`
- `internal/crag/crag.go`
- `internal/repo/repo.go`

### Steps

1. Inspect current CLI argument handling for `crag create`.
2. Add a `--local-paths` flag and align help/error text with the new contract.
3. If needed, add validation helpers so remote URL mode and local-path mode fail clearly.
4. Keep `crag.json` persistence storing the original repo source strings used to create the crag.

### Verification

- `go test ./internal/cli ./internal/crag ./internal/repo`
- Result: pass

### Acceptance Criteria

- `belayer crag create <name> --repos <path> --local-paths` succeeds for local git repos.
- Remote URL-based creation still works.
- `crag.json` preserves the provided local path strings.

---

## Task 3: Regression Coverage And Docs

### Goal

Capture the new local-path contract in automated tests and concise user-facing docs.

### Files

- `internal/crag/crag_test.go`
- `internal/repo/repo_test.go`
- `README.md`
- `docs/DESIGN.md`

### Steps

1. Add tests for local path name extraction and/or CLI validation as needed.
2. Extend crag tests to assert local path persistence under the new flag semantics.
3. Update the README and design doc examples to mention `--local-paths`.

### Verification

- `go test ./internal/cli ./internal/crag ./internal/repo`
- Result: pass

### Acceptance Criteria

- Tests cover the local-path behavior and any new validation rules.
- User-facing docs mention the local-path workflow without breaking existing remote URL guidance.

---

## Task 4: Verification And Closeout

### Goal

Finish the harness cycle with verification, review, reflection, archival, and completion signaling.

### Files

- `docs/exec-plans/active/2026-03-17-crag-local-paths.md`
- `docs/PLANS.md`
- `.lead/belayer-2/TOP.json`

### Steps

1. Run targeted and full verification appropriate to the change.
2. Perform a harness-style review pass and fix low-risk findings inline.
3. Reflect any doc drift or durable learnings into the active plan and repo docs.
4. Mark the plan completed, archive it, commit the changes, and write `TOP.json`.

### Verification

- `go test ./...`
- `git status --short`

### Acceptance Criteria

- Verification is green.
- The plan is archived under `docs/exec-plans/completed/`.
- `TOP.json` summarizes the completed climb.

---

## Outcomes & Retrospective

- Added `--local-paths` gating to `belayer crag create`, preserved local sources in `crag.json`, and clarified repo-source semantics in CLI and crag code.
- Added CLI regression tests for local-path success/failure and `file://` URL success, plus repo-source validation tests for missing paths, non-directory inputs, and colon-containing local paths.
- Updated README and `docs/DESIGN.md` to document the local-path workflow and the GitHub tracker limitation for remote-only owner/repo resolution.
- Verification: `go test ./internal/cli ./internal/crag ./internal/repo ./internal/belayer` and `go test ./...` both passed on 2026-03-17.
