# Execution Plan: Cross-Repo Integration & Alignment (Goal 6)

## Steps

### Step 1: Add git diff/stat functions to repo package
**Files:** `internal/repo/repo.go`, `internal/repo/repo_test.go`
**Status:** complete

### Step 2: Add EventPRsCreated to model types
**Files:** `internal/model/types.go`
**Status:** complete

### Step 3: Add CountAlignmentAttempts to coordinator store
**Files:** `internal/coordinator/store.go`, `internal/coordinator/store_test.go`
**Status:** complete

### Step 4: Add DiffCollector and PRCreator interfaces to coordinator
**Files:** `internal/coordinator/coordinator.go`
**Status:** complete

### Step 5: Enhance AlignmentOutput and alignment prompt
**Files:** `internal/coordinator/coordinator.go`
**Status:** complete

### Step 6: Implement re-dispatch on alignment failure
**Files:** `internal/coordinator/coordinator.go`
**Status:** complete

### Step 7: Implement PR creation on alignment pass
**Files:** `internal/coordinator/coordinator.go`
**Status:** complete

### Step 8: Update mock claude for enhanced alignment output
**Files:** `internal/coordinator/agentic_test.go`
**Status:** complete

### Step 9: Add tests for enhanced alignment flow
**Files:** `internal/coordinator/coordinator_test.go`
**Status:** complete

### Step 10: Run full test suite and fix issues
**Status:** complete
