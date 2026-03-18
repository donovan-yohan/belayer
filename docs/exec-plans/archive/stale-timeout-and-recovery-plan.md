# Implementation Plan: Stale Timeout + Recovery Fixes

> **Status**: Complete | **Created**: 2026-03-17 | **Completed**: 2026-03-17
> **Source**: `docs/bug-analyses/2026-03-17-stale-timeout-and-recovery-bug-analysis.md`
> **Branch**: `fix/stale-timeout-and-recovery`

## Goal

Fix three bugs: (1) stale timeout ignores agent activity, (2) recovery doesn't recreate tmux sessions, (3) recovery misses completed climbs.

## Progress

- [x] Task 1: Activity-aware stale timeout
- [x] Task 2: Recovery ensures tmux session exists
- [x] Task 3: Recovery checks TOP.json for all non-complete climbs
- [x] Task 4: Tests for all three fixes

## Drift Log

_No drift yet._

---

### Task 1: Activity-aware stale timeout

**File:** `internal/belayer/taskrunner.go` — `CheckStaleClimbs()`

Current behavior: `timedOut := tracked && now.Sub(startedAt) > staleTimeout` — pure elapsed time.

Change to: only time out if the agent is also **silent** (log file not modified recently). If the log file has been updated within the silence threshold, the agent is actively working — skip the timeout.

```go
// Replace the timedOut condition:
timedOut := tracked && now.Sub(startedAt) > staleTimeout

// With activity-aware check:
timedOut := false
if tracked && now.Sub(startedAt) > staleTimeout {
    logPath := tr.logMgr.LogPath(tr.task.ID, climb.ID)
    if info, err := os.Stat(logPath); err == nil {
        // Only time out if log file is also silent
        timedOut = now.Sub(info.ModTime()) > 2*time.Minute
    } else {
        // No log file at all — definitely stale
        timedOut = true
    }
}
```

This means a climb that has been running for 30m but still producing output will NOT be timed out. Only climbs that have exceeded the timeout AND gone silent for 2+ minutes are considered stale.

**Acceptance:** An actively-writing agent at 30m+ is not timed out; a silent agent at 30m+ is.

---

### Task 2: Recovery ensures tmux session exists

**File:** `internal/belayer/belayer.go` — `recover()`

After `runner.tmuxSession = fmt.Sprintf(...)` (line 428), add a tmux session existence check:

```go
// Ensure tmux session exists (may have been killed during previous stop)
if !s.tmux.HasSession(runner.tmuxSession) {
    if err := s.tmux.NewSession(runner.tmuxSession); err != nil {
        log.Printf("belayer: error recreating tmux session for %s: %v", task.ID, err)
        continue
    }
}
```

Also pre-create spotter and anchor windows (same as Init does at taskrunner.go:200-213) so that ActivateSpotter/SpawnAnchor don't fail.

**Acceptance:** After daemon stop + start, recovery creates the tmux session and windows before trying to spawn.

---

### Task 3: Recovery checks TOP.json for all non-complete climbs

**File:** `internal/belayer/belayer.go` — `recover()`

Currently, `runner.CheckCompletions()` (line 437) only checks Running climbs. Add a scan of **all non-complete climbs** for TOP.json before queuing ready climbs:

```go
// Check for TOP.json on ANY non-complete climb (not just Running)
for _, climb := range climbs {
    if climb.Status == model.ClimbStatusComplete {
        continue
    }
    worktreePath := runner.worktrees[climb.RepoName]
    topPath := filepath.Join(worktreePath, ".lead", climb.ID, "TOP.json")
    if _, err := os.Stat(topPath); err == nil {
        // Climb completed while we were down — mark it
        runner.dag.MarkComplete(climb.ID)
        s.store.UpdateClimbStatus(climb.ID, model.ClimbStatusComplete)
        log.Printf("belayer: recovery found TOP.json for climb %s — marking complete", climb.ID)
    }
}
```

This goes before the existing `CheckCompletions` call and the ready-climb queuing.

**Acceptance:** Climbs with TOP.json in Pending/Failed state are detected during recovery and not respawned.

---

### Task 4: Tests

**Files:** `internal/belayer/taskrunner_test.go`, `internal/belayer/belayer_test.go` (or setter_test.go)

1. **`TestCheckStaleClimbs_ActiveAgentNotTimedOut`**: Mock a climb running > staleTimeout with a recently-modified log file. Verify it is NOT timed out.

2. **`TestCheckStaleClimbs_SilentAgentTimedOut`**: Mock a climb running > staleTimeout with a stale log file (>2min old). Verify it IS timed out.

3. **`TestRecover_RecreatesTmuxSession`**: Recovery with no existing tmux session. Verify `NewSession` is called.

4. **`TestRecover_DetectsCompletedClimbs`**: Recovery where a Failed climb has a TOP.json. Verify it's marked complete and not queued for respawn.

**Acceptance:** All tests pass. `go test ./internal/belayer/...` green.
