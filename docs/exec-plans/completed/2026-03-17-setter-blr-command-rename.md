# Setter BLR Command Rename

> **Status**: Completed | **Created**: 2026-03-17 | **Last Updated**: 2026-03-17
> **Context**: `.lead/belayer-1/GOAL.json`
> **For Codex:** Use the harness-orchestrate workflow to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-17 | Planning | Scope this climb to setter command asset renaming, setter template updates, and manage-dir copy/test coverage | The assigned climb description is narrower than the full explorer feature in `GOAL.json`; finishing this slice cleanly is the defensible unit of work |
| 2026-03-17 | Execution | Reuse the existing committed `review-personas.toml` rather than rewriting it | A repo-level persona file appeared on this branch during execution and already satisfies the review gate; replacing it would risk trampling adjacent work |
| 2026-03-17 | Review | Remove `--crag {{.CragName}}` placeholders from static command files instead of templating commands | Setter sessions already export `BELAYER_CRAG`, so the commands can stay static and the copied `.claude/commands/` surface becomes self-consistent |

## Progress

- [x] Task 1: Create review personas config required for the review loop _(satisfied by existing committed `review-personas.toml` on the branch)_
- [x] Task 2: Rename embedded setter command files to the `blr-` namespace and update copied workspace expectations _(completed 2026-03-17)_
- [x] Task 3: Update the setter session template and command content to use `blr-` command names consistently _(completed 2026-03-17)_
- [x] Task 4: Reconcile targeted docs, run verification, and complete review/reflect/archive _(completed 2026-03-17)_

## Surprises & Discoveries

- The repository already uses harness-style docs with `CLAUDE.md` acting as the Tier 1 map; no `harness:init` rebuild is needed for this climb.
- Existing planning docs describe `/belayer:` command names as documentation-only, but the current goal now requires real command-file renames to the `blr-` namespace.
- `review-personas.toml` was missing during the initial scan but existed by execution time as a separate committed branch change; this climb reused it instead of rewriting a shared repo-level file.
- Review surfaced a pre-existing inconsistency: several static command files still contained literal `{{.CragName}}` placeholders even though `PrepareManageDir` copies them without template rendering.

## Plan Drift

- Review expanded the fix slightly beyond pure renaming: the command contents now rely on `BELAYER_CRAG` context instead of unresolved `--crag {{.CragName}}` placeholders, and the tests now verify namespace/content correctness rather than filename presence alone.

---

## Task 1: Create review personas config

### Goal

Add the required `review-personas.toml` file before the review phase and commit it in isolation per the autonomous lead instructions.

### Files

- `review-personas.toml`

### Steps

1. Detect the repo type from the belayer instructions.
2. Write a backend-oriented persona set with at least `test-engineer`, `domain-expert`, and `code-quality`.
3. Commit only the new persona file with the required commit message.

### Verification

- `git diff -- review-personas.toml`
- `git status --short`

### Acceptance Criteria

- `review-personas.toml` exists at the repo root.
- The file is committed as `chore: add review personas config`.

---

## Task 2: Rename embedded setter command files

### Goal

Rename the embedded command assets under `internal/defaults/commands/` to `blr-*.md` and ensure `PrepareManageDir` copies the renamed files into `.claude/commands/`.

### Files

- `internal/defaults/commands/*.md`
- `internal/manage/prompt.go`
- `internal/manage/prompt_test.go`
- `internal/defaults/defaults_test.go`

### Steps

1. Rename the command markdown files to the new `blr-` prefixed names.
2. Update any tests that assert specific command filenames.
3. Confirm the embedded filesystem and `PrepareManageDir` continue to use the command directory without extra rename logic.

### Verification

- `go test ./internal/defaults ./internal/manage`

### Acceptance Criteria

- Embedded command files use `blr-` names.
- Manage workspace preparation writes `.claude/commands/blr-*.md`.
- Tests cover the renamed files.

---

## Task 3: Update setter command references

### Goal

Make the setter CLAUDE template and command copy self-consistent around the `blr-` namespace and the expected problem-routing workflow.

### Files

- `internal/defaults/claudemd/setter.md`
- `internal/defaults/commands/blr-*.md`

### Steps

1. Replace stale `/problem-*` and `/belayer:*` references with `/blr-*`.
2. Keep the CLI invocations unchanged; only the session command namespace should move.
3. Preserve the setter workflow constraint that execution routes through belayer problem creation rather than local harness orchestration.

### Verification

- `rg -n "/problem-|/belayer:" internal/defaults/claudemd/setter.md internal/defaults/commands`

### Acceptance Criteria

- Setter guidance names only `/blr-*` commands for this surface.
- Command bodies and next-step text align with the renamed files.

---

## Task 4: Reconcile docs and complete the harness cycle

### Goal

Review the diff, update any stale repo docs that describe the old command namespace, verify the change, and archive the plan.

### Files

- `docs/PLANS.md`
- `docs/DESIGN.md`
- `CLAUDE.md`
- `docs/exec-plans/active/2026-03-17-setter-blr-command-rename.md`

### Steps

1. Run targeted and then full-repo verification relevant to this change.
2. Update docs that still describe the old command namespace on the changed surface.
3. Fill the retrospective, archive the plan, and commit the completed work.
4. Write `.lead/belayer-1/TOP.json`.

### Verification

- `go test ./...`
- `go build -o /tmp/belayer ./cmd/belayer`

### Acceptance Criteria

- Review passes or is documented in `TOP.json` if not fully green.
- The plan is archived under `docs/exec-plans/completed/`.
- Final repo state is committed and `.lead/belayer-1/TOP.json` is written.

---

## Outcomes & Retrospective

- Renamed all embedded setter command files under `internal/defaults/commands/` to the `blr-` namespace and kept `PrepareManageDir` working without production code changes.
- Updated setter-facing guidance to use `/blr-*` command names across the runtime template and current design docs, while leaving historical manage-era docs in their original command namespace to avoid mixed-state guidance.
- Strengthened regression coverage so tests now prove three things: only `blr-` filenames are embedded/copied, command contents do not retain old namespace strings, and rendered `CLAUDE.md` no longer exposes `/problem-*` or `/belayer:*` command names.
- Full verification passed with `go test ./...` and `go build -o /tmp/belayer ./cmd/belayer`.
