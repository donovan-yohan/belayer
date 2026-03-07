# Execution Plan: Bundled Lead Execution Loop (Goal 3)

**Date**: 2026-03-06
**Design Doc**: [lead-execution-loop-design](../../design-docs/2026-03-06-lead-execution-loop-design.md)

## Steps

### 1. Add model types
- [x] Add `LeadGoalStatus` enum, `LeadGoal` struct, `Verdict` struct to `internal/model/types.go`
- [x] Add `LeadEvent` struct for parsing shell script output

### 2. Add database migration
- [x] Create `internal/db/migrations/002_lead_execution.sql` with `lead_goals` table

### 3. Create lead store
- [x] Create `internal/lead/store.go` with DB operations:
  - `UpdateLeadStatus(db, leadID, status, output)`
  - `InsertLeadGoal(db, goal)`
  - `UpdateLeadGoalStatus(db, goalID, status, attempt, output, verdictJSON)`
  - `InsertEvent(db, event)`
- [x] Create `internal/lead/store_test.go` with in-memory SQLite tests

### 4. Create lead shell script
- [x] Create `internal/lead/scripts/lead.sh`
  - Read spec and goals from .lead/ directory
  - Execute->review->verdict cycle per goal
  - Emit structured JSON events to stdout
  - Handle retry logic (MAX_ATTEMPTS)
  - Handle stuck/complete states

### 5. Create lead runner
- [x] Create `internal/lead/runner.go`:
  - `Runner` struct with config
  - `Run(ctx, worktreePath, leadID, spec, goals)` method
  - Extract and write lead.sh to worktree
  - Launch process, parse stdout events
  - Update DB on each event
  - Handle process lifecycle (cancel, timeout)
- [x] Event parsing function `parseLeadEvent(line string) (*LeadEvent, error)`

### 6. Write runner tests
- [x] Create `internal/lead/runner_test.go`:
  - Test event parsing
  - Test runner with mock claude command
  - Test script extraction

### 7. Verify build and tests
- [x] Run `go build ./...` to verify compilation
- [x] Run `go test ./...` to verify all tests pass

### 8. Update docs
- [x] Update `docs/ARCHITECTURE.md` code map with lead module
- [x] Update `docs/design-docs/index.md` with new design doc
