# Bug Analysis: Stale Timeout Race + Recovery Failures

> **Status**: Confirmed | **Date**: 2026-03-17
> **Severity**: Critical
> **Affected Area**: `internal/belayer/belayer.go` (tick loop, recovery), `internal/belayer/taskrunner.go` (CheckStaleClimbs, SpawnClimb)

## Symptoms
- Climbs that completed successfully (wrote TOP.json) were marked failed by the stale timeout check
- After daemon restart, recovery tried to respawn climbs into a tmux session that no longer existed
- Recovery didn't detect that climbs had already completed (TOP.json present)
- The 30m stale timeout is a blunt instrument — doesn't distinguish between an idle agent and one actively working

## Reproduction Steps
1. Submit a problem with climbs that take ~30 minutes (e.g., codex provider)
2. Climbs spawn at T+0, write TOP.json near T+30m
3. Stale timeout fires at exactly T+30m, marks climbs failed
4. Stop and restart daemon — recovery fails to recreate tmux session, can't respawn

## Root Cause

Three compounding bugs:

### Bug 1: Stale timeout race with completion check (same-tick race)

In `belayer.go:tick()`, `CheckCompletions` (line 145) runs before `CheckStaleClimbs` (line 167). If a climb writes TOP.json between these two calls in the same tick, `CheckCompletions` misses it and `CheckStaleClimbs` fires the timeout. The TOP.json guard in `CheckStaleClimbs` (taskrunner.go:510-513) mitigates this but is subject to filesystem timing — if the file write hasn't flushed or the stat call races with the write, the guard can miss it.

More fundamentally: the stale timeout is purely elapsed-time-based (`now.Sub(startedAt) > staleTimeout`). It doesn't check whether the agent is actively producing output. An agent that has been writing to its log file continuously for 30 minutes gets the same treatment as one that died silently after 5 minutes.

### Bug 2: Recovery doesn't recreate tmux sessions

In `belayer.go:recover()` (line 407-491), the recovery path sets `runner.tmuxSession` (line 428) but never creates the tmux session. It assumes the session still exists from before the crash. But `belayer stop` kills the tmux session during cleanup. When recovery queues ready climbs (line 478-481) and `processLeadQueue` tries to spawn them (line 397), `SpawnClimb` calls `tmux.NewWindow(tr.tmuxSession, ...)` on a non-existent session, which fails.

### Bug 3: Recovery respawns already-completed climbs

Recovery calls `CheckCompletions` (line 437) which handles Running climbs. But climbs that were marked Failed by the stale timeout (and subsequently completed) have status Pending or Failed in the database. `CheckCompletions` only checks Running climbs (taskrunner.go:382). So TOP.json files from completed-but-failed climbs are never detected. Recovery then queues these climbs for respawning (line 478-481), wasting resources redoing completed work.

## Evidence
- `belayer.go:145,167` — CheckCompletions before CheckStaleClimbs, same tick
- `taskrunner.go:462-463` — stale check: `now.Sub(startedAt) > staleTimeout`, no activity check
- `belayer.go:428` — recovery sets tmuxSession name but never creates it
- `taskrunner.go:253` — SpawnClimb assumes tmux session exists
- `belayer.go:437` — recovery CheckCompletions only handles Running climbs
- `taskrunner.go:382` — CheckCompletions skips non-Running climbs
- User observation: climbs spawned at 19:29:45, timed out at 19:59:50 (exactly 30m)
- User observation: completed climbs had TOP.json but daemon didn't detect them after restart

## Impact Assessment
- **Data loss**: completed work is discarded and redone unnecessarily
- **Stuck state**: daemon can't recover after stop+start, requires manual reset
- **Resource waste**: 30m of codex execution wasted per false timeout
- **Scaling blocker**: as lead execution times vary (codex ~30m, claude ~15m), a single static timeout can't accommodate all providers

## Recommended Fix Direction

### Bug 1: Activity-aware stale timeout
Replace the pure elapsed-time timeout with an activity-aware check:
1. Track last log file modification time per climb (already captured in silence detection)
2. Only trigger stale timeout if **both** conditions are true: elapsed > staleTimeout AND log file hasn't been modified in > silenceThreshold (2 min)
3. If the agent is actively producing output (log file recently modified), extend the deadline — it's not stale, it's just slow
4. Keep the hard timeout as an absolute ceiling (e.g., configurable, default 2h) to prevent infinite hangs

### Bug 2: Recovery must ensure tmux session exists
In `recover()`, after setting `runner.tmuxSession`, check if the session exists with `tmux.HasSession()`. If not, create it with `tmux.NewSession()` (same pattern as `Init()` at taskrunner.go:192-198). Also pre-create spotter/anchor windows as Init does.

### Bug 3: Recovery must check TOP.json for all non-complete climbs
Before queuing ready climbs during recovery, scan **all** climbs (not just Running) for existing TOP.json files. For any climb with a TOP.json that's still in Pending/Failed state, mark it complete in the store and DAG. This handles the case where a climb completed but the daemon was stopped before it could process the completion.
