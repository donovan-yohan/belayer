# Review Deferred Items: Test Coverage & Type Safety

> **Status**: Completed | **Created**: 2026-03-16 | **Last Updated**: 2026-03-16
> **Design Doc**: `docs/design-docs/2026-03-16-review-deferred-items-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-16 | Plan | 7 tasks: 4 test, 1 type safety, 1 pure fn test, 1 bug fix | Direct from review findings |

## Progress

- [x] Task 1: Learning store CRUD tests (8 tests)
- [x] Task 2: GetActiveProblems recovery tests (3 tests)
- [x] Task 3: createSpotterCorrectionClimbs tests (3 tests)
- [x] Task 4: CheckRepoSpotResults needs_human escalation tests (2 tests)
- [x] Task 5: LearningCategory/LearningSeverity typed enums
- [x] Task 6: extractTestContract tests (4 tests)
- [x] Task 7: HandleApproval failure → stuck

## Surprises & Discoveries

_None yet._

## Plan Drift

_None yet._

---

### Task 1: Learning store CRUD tests

**File:** `internal/store/store_test.go` (or new `internal/store/learning_test.go`)
**What:** Test all 5 learning store methods.

Tests:
- `TestInsertAndGetLearning` — insert with all fields, get by ID, verify round-trip
- `TestInsertLearning_GeneratesUUID` — insert with empty ID, verify UUID generated
- `TestInsertLearning_NullProblemID` — insert with empty ProblemID, verify stored as NULL
- `TestListLearnings_ActiveOnly` — insert 2 (1 resolved, 1 active), list with activeOnly=true, verify only active returned
- `TestListLearnings_CategoryFilter` — insert 2 different categories, filter by one
- `TestResolveLearning` — insert, resolve, verify resolved=true on get
- `TestIncrementLearningAccess` — insert, increment twice, verify count=2
- `TestGetLearning_NotFound` — get non-existent ID, verify error

Follow existing test patterns in store_test.go. Use `db.Open(":memory:")` or tmpdir pattern.

**Verify:** `go test ./internal/store/...`

---

### Task 2: GetActiveProblems recovery tests

**File:** `internal/store/store_test.go`
**What:** Test that `GetActiveProblems` returns problems in `spotting` and `reflecting` states.

Tests:
- `TestGetActiveProblems_IncludesSpotting` — insert problem with status `spotting`, verify returned
- `TestGetActiveProblems_IncludesReflecting` — insert problem with status `reflecting`, verify returned
- `TestGetActiveProblems_ExcludesNeedsHuman` — insert problem with status `needs_human`, verify NOT returned

**Verify:** `go test ./internal/store/...`

---

### Task 3: createSpotterCorrectionClimbs tests

**File:** `internal/belayer/setter_test.go`
**What:** Test the correction climb creation path.

Tests:
- `TestCreateSpotterCorrectionClimbs_CreatesClimbs` — set up runner with a repo, call with SpotJSON containing 2 correction climbs, verify:
  - 2 new climbs in store
  - 2 new climbs in DAG
  - EventCorrectionClimbCreated events emitted
  - Returned QueuedClimbs have spotter feedback populated
- `TestCreateSpotterCorrectionClimbs_EmptyList` — call with empty correction_climbs, verify nil return
- `TestCreateSpotterCorrectionClimbs_TOPJsonCleanup` — verify old TOP.json files removed before new climbs added to DAG

Follow existing test patterns (setupTestEnv, insertTestTask, manual ProblemRunner construction).

**Verify:** `go test ./internal/belayer/...`

---

### Task 4: CheckRepoSpotResults needs_human escalation tests

**File:** `internal/belayer/setter_test.go`
**What:** Test the max spotter cycles → needs_human transition.

Tests:
- `TestCheckRepoSpotResults_MaxCyclesNeedsHuman` — set up runner with `repoSpotterAttempts[repo] = 2`, `reviewLoop.MaxSpotterCycles = 2`, write a failing SPOT.json, call CheckRepoSpotResults, verify:
  - `task.Status == ProblemStatusNeedsHuman`
  - EventNeedsHuman event emitted
  - `repoSpotterActivated[repo] == false`
- `TestCheckRepoSpotResults_DefaultMaxCycles` — set `reviewLoop.MaxSpotterCycles = 0`, verify fallback to 2

**Verify:** `go test ./internal/belayer/...`

---

### Task 5: LearningCategory/LearningSeverity typed enums

**File:** `internal/model/types.go`
**What:** Add typed string enums for learning category and severity.

```go
type LearningCategory string
const (
    LearningCategoryTestGap       LearningCategory = "test_gap"
    LearningCategorySpecAmbiguity LearningCategory = "spec_ambiguity"
    LearningCategoryInfraIssue    LearningCategory = "infra_issue"
    LearningCategoryReviewMiss    LearningCategory = "review_miss"
    LearningCategoryPattern       LearningCategory = "pattern"
)

type LearningSeverity string
const (
    LearningSeverityHigh   LearningSeverity = "high"
    LearningSeverityMedium LearningSeverity = "medium"
    LearningSeverityLow    LearningSeverity = "low"
)
```

Update `Learning` struct fields `Category` and `Severity` to use these types.
Update `internal/cli/learnings.go` validation to use typed constants.
Update `internal/agentic/reflect.go` `ReflectLearning` fields to use these types.

**Verify:** `go build ./...` and `go test ./...`

---

### Task 6: extractTestContract tests

**File:** `internal/belayer/taskrunner_test.go` (or in setter_test.go)
**What:** Test the pure function `extractTestContract`.

Tests:
- `TestExtractTestContract_Found` — spec with `## Test Contract` section followed by another `##`
- `TestExtractTestContract_AtEnd` — spec with `## Test Contract` as last section
- `TestExtractTestContract_NotFound` — spec without the section, verify empty string
- `TestExtractTestContract_Empty` — spec with `## Test Contract` but no content before next heading

**Verify:** `go test ./internal/belayer/...`

---

### Task 7: HandleApproval failure → stuck

**File:** `internal/belayer/belayer.go`
**What:** When `HandleApproval` fails, transition to `stuck` instead of cleaning up.

Find the single-repo and multi-repo paths where HandleApproval is called. On error:
```go
if err := runner.HandleApproval(ctx); err != nil {
    log.Printf("belayer: error creating PRs for %s: %v", taskID, err)
    if stErr := s.store.UpdateProblemStatus(taskID, model.ProblemStatusStuck); stErr != nil {
        log.Printf("belayer: error updating status to stuck: %v", stErr)
    }
    runner.task.Status = model.ProblemStatusStuck
    continue
}
```

Do NOT call `runner.Cleanup()` or `delete(s.problems, taskID)` on HandleApproval failure.

**Verify:** `go build ./...` and `go test ./internal/belayer/...`

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
- All 7 tasks were independent — full parallel dispatch worked perfectly
- Typed enums caught no existing bugs but improved discoverability and IDE support
- 20 new tests covering all critical gaps identified by the review

**What didn't:**
- Nothing significant — focused scope kept this clean

**Learnings to codify:**
- Always write store CRUD tests when adding a new table — the migration/test gap was avoidable
- HandleApproval failure silently cleaning up was a pre-existing bug that survived multiple review cycles — highlights importance of silent-failure-hunter agent
