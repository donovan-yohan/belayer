# Explorer Session Bootstrap

> **Status**: Completed | **Created**: 2026-03-17 | **Last Updated**: 2026-03-17
> **Context**: `.lead/belayer-3/GOAL.json`
> **For Codex:** Use the harness-orchestrate workflow to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-17 | Planning | Scope this climb to explorer-session bootstrapping: CLI entrypoint, workspace prep, template rendering, and command-subset deployment | The assigned climb description is narrower than the full explorer plus research-toolkit spec, and other climbs already own command-content work |
| 2026-03-17 | Planning | Treat explorer slash-command deployment as a curated subset that only copies explorer-safe shared command assets when they exist | This keeps the workspace prep code correct without pulling setter-only commands into pre-crag sessions or overlapping the dedicated command-asset climbs |
| 2026-03-17 | Execution | Resolve `--prd` to an absolute path before entering the explorer workspace | Explorer sessions `chdir` into `~/.belayer/explorer/...`, so relative PRD paths would otherwise break as soon as Claude starts |
| 2026-03-17 | Execution | Clear inherited `BELAYER_CRAG` and `BELAYER_INSTANCE` values for all interactive Claude launches, then re-add `BELAYER_CRAG` only for setter sessions | Explorer sessions must not inherit stale crag context, and the launcher behavior is safer and easier to reason about when the environment cleanup is centralized |
| 2026-03-17 | Completion | Archive the plan after review, reflection, and full verification passed | The explorer bootstrap slice landed with green tests/builds and the targeted docs now describe both setter and explorer session context |

## Progress

- [x] Task 1: Add explorer prompt assets and workspace-preparation helpers in `internal/manage` with coverage for named, unnamed, and PRD-backed sessions _(completed 2026-03-17)_
- [x] Task 2: Wire a new `belayer explorer` CLI command to the workspace-preparation flow and Claude launcher, including absolute PRD-path handling _(completed 2026-03-17)_
- [x] Task 3: Reconcile targeted docs, run verification, and complete review/reflect/archive for the explorer bootstrap slice _(completed 2026-03-17)_

## Surprises & Discoveries

- The branch already contained adjacent research-toolkit work in `internal/defaults/commands/blr-research*.md`, `internal/defaults/claudemd/setter.md`, and the active-plan index; the explorer bootstrap changes preserved and reused those assets instead of trying to partition them back out.
- The existing setter-only `.claude` deployment code was small enough that extracting a shared workspace helper reduced duplication without forcing a broader manage-package refactor.

## Plan Drift

- The full problem spec calls for an interactive resume-or-restart decision when an explorer workspace already exists. This climb stops at safe workspace reuse for named sessions and timestamped creation for unnamed sessions, which matches the assigned slice but leaves the richer prompt-driven resume UX for a follow-up climb.

---

## Task 1: Add explorer prompt assets and workspace-preparation helpers

### Goal

Create the explorer-specific prompt data, template rendering, and workspace directory logic alongside the existing setter manage helpers.

### Files

- `internal/manage/prompt.go`
- `internal/manage/prompt_test.go`
- `internal/defaults/claudemd/explorer.md`
- `internal/defaults/defaults_test.go`

### Steps

1. Add an `ExplorerPromptData` struct carrying the project name and PRD path.
2. Add `PrepareExplorerDir` to create the explorer workspace, choose `_unnamed-<timestamp>` directories when needed, render `explorer.md`, and copy only explorer-safe command assets.
3. Add regression tests covering named sessions, unnamed sessions, template rendering, and command-subset behavior.

### Verification

- `go test ./internal/manage ./internal/defaults`

### Acceptance Criteria

- Explorer workspace prep creates `.claude/CLAUDE.md` from an explorer template.
- Empty names produce `_unnamed-<timestamp>` workspace directories.
- Explorer prep does not copy setter-only command files into `.claude/commands/`.

---

## Task 2: Wire the explorer CLI command

### Goal

Expose the new explorer session bootstrap through the CLI and make sure PRD paths remain valid after the process changes into the explorer workspace.

### Files

- `internal/cli/root.go`
- `internal/cli/explorer_cmd.go`
- `internal/cli/explorer_cmd_test.go`
- `internal/cli/setter_cmd.go`

### Steps

1. Add `belayer explorer [--prd <path>] [--name <project-name>] [--yolo]`.
2. Resolve `--prd` to an absolute path before entering the workspace.
3. Reuse the Claude launcher with explorer-safe environment handling and add focused CLI tests.

### Verification

- `go test ./internal/cli`

### Acceptance Criteria

- The root command exposes `belayer explorer`.
- Named and unnamed explorer launches prepare the expected workspace paths.
- Relative PRD paths are rendered as usable absolute paths in the generated prompt.

---

## Task 3: Review, reflect, and complete the harness cycle

### Goal

Verify the explorer bootstrap slice, update any stale docs, and archive the plan with a final retrospective.

### Files

- `docs/PLANS.md`
- `CLAUDE.md`
- `docs/exec-plans/completed/2026-03-17-explorer-session-bootstrap.md`
- `.lead/belayer-3/TOP.json`

### Steps

1. Run targeted and full verification.
2. Reconcile docs only where the explorer bootstrap slice changes the documented surface.
3. Fill the retrospective, archive the plan, commit the work, and write `TOP.json`.

### Verification

- `go test ./...`
- `go build -o /tmp/belayer ./cmd/belayer`

### Acceptance Criteria

- Verification is green or any residual issue is documented in `TOP.json`.
- The completed plan is archived under `docs/exec-plans/completed/`.
- The branch ends with a commit containing the code, docs, archived plan, and `TOP.json`.

---

## Outcomes & Retrospective

- Added a new `belayer explorer` CLI command that prepares `~/.belayer/explorer/<workspace>/`, resolves `--prd` to an absolute file path, and launches Claude without inherited crag state.
- Added `ExplorerPromptData`, `PrepareExplorerDir`, and a shared `.claude` workspace-preparation helper so setter and explorer sessions both render their templates and deploy command files through the same path.
- Added the embedded `internal/defaults/claudemd/explorer.md` template with explicit operating principles, the five-phase explorer workflow, and a default redirect from implementation requests into belayer problem creation.
- Expanded regression coverage across `internal/manage`, `internal/defaults`, and `internal/cli` for named and unnamed explorer workspaces, absolute PRD-path rendering, command-subset filtering, and invalid directory-valued `--prd` input.
- Verification passed with `go test ./internal/manage ./internal/defaults ./internal/cli`, `go test ./...`, and `go build -o /tmp/belayer ./cmd/belayer`.
