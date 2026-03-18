# Implementation Plan: Trust Dialog Blocking Fix

> **Status**: Complete | **Created**: 2026-03-17 | **Completed**: 2026-03-17
> **Source**: `docs/bug-analyses/2026-03-17-trust-dialog-blocking-bug-analysis.md`
> **Branch**: `fix/trust-dialog-blocking`

## Goal

Prevent agent trust-folder dialogs from blocking tmux sessions and wasting climb attempts. Two-pronged: prevent dialogs via pre-spawn setup, and detect+resolve them as defense-in-depth.

## Progress

- [x] Task 1: Add `looksLikeTrustDialog()` detection function
- [x] Task 2: Add post-spawn readiness checker with trust dialog auto-resolution
- [x] Task 3: Integrate readiness checker into `CheckStaleClimbs` with early detection
- [x] Task 4: Tests for trust dialog detection and readiness checking

## Drift Log

_No drift yet._

---

### Task 1: Add `looksLikeTrustDialog()` detection function

**File:** `internal/belayer/taskrunner.go`

Add a `looksLikeTrustDialog(content string) bool` function that detects trust/workspace prompts in captured pane content. Patterns to match:
- "Do you trust" (Claude Code)
- "Trust this folder" / "trust the files" (Claude Code)
- "allow this" / "Allow access" (generic)
- Lines ending with `(y/N)`, `(Y/n)`, `[y/N]`, `[Y/n]` (confirmation prompts)
- "Yes, proceed" / "Continue?" patterns

Also update `looksLikeInputPrompt()` to NOT match when `looksLikeTrustDialog()` matches — trust dialogs should take a different code path than generic input prompts.

**Acceptance:** Function correctly identifies trust dialog content in unit tests.

---

### Task 2: Add post-spawn readiness checker with trust dialog auto-resolution

**File:** `internal/belayer/taskrunner.go`

Add a `checkAndResolveTrustDialog(windowName string) (resolved bool, err error)` method on `ProblemRunner` that:
1. Calls `tr.tmux.CapturePaneContent(tr.tmuxSession, windowName, 30)`
2. Checks with `looksLikeTrustDialog(content)`
3. If trust dialog detected: sends `Enter` via `tr.tmux.SendKeysRaw(target, "Enter")` to accept
4. Logs the resolution: `log.Printf("trust dialog auto-resolved for window %s", windowName)`
5. Returns `(true, nil)` if resolved, `(false, nil)` if no dialog found

Also modify `CheckStaleClimbs()`:
- Before marking a silent climb as failed with reason "waiting for input", first call `checkAndResolveTrustDialog()`
- If resolved, reset the silence tracking (update log file mtime or skip the failure) and continue — do NOT mark failed or consume an attempt
- If not a trust dialog, proceed with existing behavior

**Acceptance:** Trust dialogs are auto-resolved without consuming a climb attempt.

---

### Task 3: Integrate readiness checker into spawn flows

**File:** `internal/belayer/taskrunner.go`

After `spawners.Lead.Spawn()` in `SpawnClimb` (and similarly in `ActivateSpotter` and `SpawnAnchor`), the session needs time to start. Rather than adding a sleep, we rely on the existing `CheckStaleClimbs` path in the tick loop which already captures pane content after silence. The integration from Task 2 handles this.

Additionally, reduce the silence threshold for the first 30 seconds after spawn — add a `spawnedAt` timestamp per window to `startedAt` map (already tracked). In `CheckStaleClimbs`, if a climb has been running for < 30 seconds AND is silent, do an early pane capture to check for trust dialog. This catches the dialog faster than the 2-minute silence threshold.

**Acceptance:** Trust dialogs are detected within ~30 seconds of spawn, not 2 minutes.

---

### Task 4: Tests for trust dialog detection and readiness checking

**Files:** `internal/belayer/taskrunner_test.go` (or a new `internal/belayer/trust_test.go`)

Add table-driven tests:

1. **`TestLooksLikeTrustDialog`**: Various pane content strings — trust dialogs from Claude and Codex, normal working output, `>` prompts, empty content. Assert correct classification.

2. **`TestLooksLikeInputPrompt_NotTrustDialog`**: Verify `looksLikeInputPrompt` returns false when content is a trust dialog (trust dialogs take priority).

3. **`TestCheckStaleClimbs_TrustDialogResolved`**: Mock tmux that returns trust dialog content on `CapturePaneContent`. Verify the climb is NOT marked failed. Verify `SendKeysRaw` was called with "Enter". Verify no attempt is consumed.

4. **`TestCheckStaleClimbs_EarlyTrustDetection`**: A climb spawned <30s ago with silent log — verify early pane capture is triggered.

**Acceptance:** All tests pass. `go test ./internal/belayer/...` green.
