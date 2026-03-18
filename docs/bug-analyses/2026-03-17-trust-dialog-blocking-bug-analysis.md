# Bug Analysis: Agent Trust-Folder Dialogs Block Tmux Sessions

> **Status**: Confirmed | **Date**: 2026-03-17
> **Severity**: High
> **Affected Area**: lead spawning, stale detection (`internal/belayer/taskrunner.go`, `internal/lead/claude.go`, `internal/lead/codex.go`)

## Symptoms
- Leads spawned in git worktrees hang silently for 2+ minutes before being detected as stale
- Climbs are marked failed with reason "waiting for input" and retried, wasting attempts
- Both `claude` and `codex` can show a "trust this folder?" dialog when opened in a directory they haven't seen before, even with `--dangerously-skip-permissions` / `--dangerously-bypass-approvals-and-sandbox`

## Reproduction Steps
1. Create a crag with repos that haven't been opened by claude/codex before
2. Run `belayer daemon start` and submit a problem
3. Belayer creates worktrees (new paths) and spawns agents via `tmux send-keys`
4. Agent shows trust dialog → session blocks → 2min silence threshold → detected as stale → climb fails

## Root Cause

Three compounding issues:

### 1. No pre-spawn trust handling
When belayer spawns agents in worktrees, the directory path is brand new. Both Claude Code and Codex may present a workspace trust confirmation dialog before entering their interactive REPL. Neither `--dangerously-skip-permissions` (Claude) nor `--dangerously-bypass-approvals-and-sandbox` (Codex) fully suppress this trust prompt — they skip tool-use permissions, not workspace trust.

The setter session already works around this for its own use case (`internal/cli/setter_cmd.go:39` — `PrepareManageDir` writes `.claude/` context into the crag directory to avoid the popup), but no equivalent exists for lead/spotter/anchor worktrees.

### 2. Detection is too slow (2-minute silence threshold)
The trust dialog is only caught after the silence threshold (`2 * time.Minute`) in `CheckStaleClimbs` (taskrunner.go:469-470). During this time the climb appears to be "running" but is actually blocked.

### 3. Detection is destructive (fails the climb instead of resolving the dialog)
When `looksLikeInputPrompt()` matches, the climb is marked **failed** and retried (up to 3 attempts). A trust dialog could burn all 3 attempts without any real work being done. The function also only checks for `>` / `> ` suffixes — it does not recognize trust dialog patterns.

## Evidence
- `looksLikeInputPrompt()` at taskrunner.go:1382-1392 — only checks for `>` prompt, not trust dialogs
- `CheckStaleClimbs()` at taskrunner.go:466-485 — 2min silence before any check, then marks failed
- `claude.go:37` / `codex.go:37` — agents launched with dangerous flags but no trust pre-approval
- `setter_cmd.go:39-40` — setter already has a workaround pattern (`PrepareManageDir`) proving the issue is known

## Impact Assessment
- Every new worktree path risks hitting the trust dialog
- Each occurrence wastes 2+ minutes before detection plus a climb attempt
- If all 3 attempts hit the dialog, the climb permanently fails with no real work done
- Multi-repo problems are especially affected (more worktrees = more novel paths)

## Recommended Fix Direction

Two-pronged approach — **prevent** trust dialogs and **handle** them when they occur:

### Prevention (preferred)
1. **Claude Code**: Before spawning, write a `.claude/settings.json` or equivalent trust marker into each worktree directory (similar to `PrepareManageDir` pattern already used by setter). Alternatively, set `CLAUDE_TRUST_DIRECTORY=1` or equivalent env var if supported.
2. **Codex**: Investigate Codex's trust mechanism — may support `--trust-dir` flag or config file approach.
3. **Shared**: Add a `PrepareTrustContext(worktreePath)` step in `SpawnClimb` before calling `spawners.Lead.Spawn()`.

### Detection + Recovery (defense in depth)
1. **Add `looksLikeTrustDialog(content string) bool`**: Detect trust-specific patterns in pane content (e.g., "trust", "Do you trust", "allow this directory", "y/N", "Yes/No").
2. **Early detection**: Add a post-spawn readiness check — capture pane content ~5-10 seconds after spawning and check for trust dialogs before the 2-minute silence threshold.
3. **Auto-resolve or prompt user**: When a trust dialog is detected:
   - **Option A (auto)**: Send `Enter` / `y` + `Enter` via `tmux send-keys` to accept the trust prompt automatically, since belayer already runs agents with full permissions.
   - **Option B (interactive)**: Log a warning and prompt the belayer user (via daemon status/events) to manually accept, though this breaks the autonomous model.
   - **Recommended**: Option A with a config flag (default: auto-accept), since `--dangerously-skip-permissions` already implies full trust.
4. **Don't waste attempts**: If a trust dialog is detected and resolved, don't count it as a failed attempt — reset the silence timer and continue monitoring.
