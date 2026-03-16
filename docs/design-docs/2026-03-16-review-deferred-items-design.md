# Review Deferred Items: Test Coverage & Type Safety

**Date:** 2026-03-16
**Status:** Accepted

## Problem Statement

The review-loops-test-infra implementation introduced significant new code paths that lack test coverage, and several type design weaknesses were identified. These need to be addressed before the work is considered complete.

## Design Goals

| Goal | Success Criteria |
|------|-----------------|
| Critical test coverage | Tests for correction climbs, needs_human escalation, store CRUD, recovery with new statuses |
| Type safety | Typed enums for Learning category/severity, validation at deserialization boundaries |
| Pre-existing bug fix | `HandleApproval` failure no longer cleans up problem silently |

## Architecture

### 1. Test Coverage (4 critical gaps)

#### 1a. `createSpotterCorrectionClimbs` tests

Test that:
- Correction climbs are created from SPOT.json `correction_climbs` entries — verify DAG, store, and events
- Empty `correction_climbs` list returns nil
- Store insertion failure propagates error

#### 1b. `CheckRepoSpotResults` max cycles / needs_human escalation

Test that:
- When `repoSpotterAttempts[repo] >= maxCycles`, status transitions to `needs_human`
- Event is emitted, spotter is deactivated
- Default fallback of `maxCycles = 2` when config is zero

#### 1c. Store learning CRUD

Test round-trip for all 5 store methods:
- `InsertLearning` + `GetLearning` round-trip
- `ListLearnings` with `activeOnly=true` filters resolved
- `ListLearnings` with category filter
- `ResolveLearning` marks as resolved
- `IncrementLearningAccess` increments counter
- `GetLearning` for non-existent ID returns error

#### 1d. `GetActiveProblems` with new statuses

Test that problems in `spotting` and `reflecting` states are returned by `GetActiveProblems` (recovery scenario).

### 2. Type Safety

#### 2a. Typed enums for Learning category and severity

Add `LearningCategory` and `LearningSeverity` typed string constants to `internal/model/types.go`, following the existing `ProblemStatus`/`ClimbStatus` pattern:

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

Update `Learning` struct to use these types. Update CLI validation and agentic output processing to use the constants.

#### 2b. `extractTestContract` tests

Pure function, easy to test. Cover: no section, section at end, section with next heading, empty section.

### 3. HandleApproval Failure Fix

In `belayer.go`, when `HandleApproval` fails, do NOT clean up or delete. Transition to `stuck` state instead:

```go
if err := runner.HandleApproval(ctx); err != nil {
    log.Printf("belayer: error creating PRs for %s: %v", taskID, err)
    s.store.UpdateProblemStatus(taskID, model.ProblemStatusStuck)
    runner.task.Status = model.ProblemStatusStuck
    continue
}
```

## Changes Required

| Area | Change |
|------|--------|
| `internal/belayer/setter_test.go` | Tests for correction climbs, needs_human escalation, extractTestContract |
| `internal/store/store_test.go` | Learning CRUD tests, GetActiveProblems with new statuses |
| `internal/model/types.go` | `LearningCategory`, `LearningSeverity` typed enums |
| `internal/model/types.go` | Update `Learning` struct to use typed fields |
| `internal/cli/learnings.go` | Use typed constants for validation |
| `internal/belayer/belayer.go` | HandleApproval failure → stuck instead of cleanup |

## Resolved Decisions

| Decision | Resolution | Rationale |
|----------|-----------|-----------|
| Scope | Tests + type safety + HandleApproval fix only | Architecture items (ProblemStatus.IsActive, shared LearningCore, WriteClimbJSON type safety) are valuable but lower priority — can be separate work |
