# Execution Plan: Task Intake & Decomposition (Goal 5)

## Steps

| # | Step | Files | Status |
|---|------|-------|--------|
| 1 | Add `sufficiency_checked` column via migration 003 | `internal/db/migrations/003_task_intake.sql` | complete |
| 2 | Update model types for sufficiency tracking | `internal/model/types.go` | complete |
| 3 | Create intake package with Pipeline struct | `internal/intake/intake.go` | complete |
| 4 | Create intake tests | `internal/intake/intake_test.go` | complete |
| 5 | Update coordinator store: add sufficiency_checked read/write | `internal/coordinator/store.go` | complete |
| 6 | Make coordinator decomposition instance-aware + skip sufficiency when pre-checked | `internal/coordinator/coordinator.go` | complete |
| 7 | Update CLI task create to use intake pipeline | `internal/cli/task.go` | complete |
| 8 | Update mock claude for brainstorm prompts | `internal/coordinator/agentic_test.go` | complete |
| 9 | Add coordinator tests for instance-aware decomposition | `internal/coordinator/coordinator_test.go` | complete |
| 10 | Update db_test for migration 003 | `internal/db/db_test.go` | complete |
| 11 | Run full test suite | - | complete |
| 12 | Update docs (ARCHITECTURE.md, DESIGN.md) | `docs/` | complete |

## Step Details

### Step 1: Migration 003
Add `sufficiency_checked` boolean column to tasks table.

### Step 2: Model types
Add `SufficiencyChecked` field to `Task` struct.

### Step 3: Intake package
- `Pipeline` struct with `Run(ctx, config) -> (enrichedDescription, error)`
- `PipelineConfig`: description, jiraTickets, instanceRepos, agenticModel, stdin/stdout for brainstorm
- `AgenticExecutor` interface for testability (decouples from real claude)
- Text parsing: take description as-is
- Jira parsing: format comma-separated IDs into structured description
- Sufficiency check: call agentic node, parse response
- Brainstorm: if insufficient, loop through gaps asking user for answers
- Returns enriched description

### Step 4: Intake tests
- Test text input passthrough
- Test Jira multi-ticket formatting
- Test sufficiency check (sufficient → no brainstorm)
- Test sufficiency check (insufficient → brainstorm enrichment)
- Test --no-brainstorm flag skips brainstorm

### Step 5: Store updates
- `InsertTask` and `GetTask` include `sufficiency_checked`
- `SetTaskSufficiencyChecked(taskID)` method

### Step 6: Coordinator changes
- `startDecomposition`: skip sufficiency if task.SufficiencyChecked
- Decomposition prompt includes available repo names from instance config
- Pass repo names through coordinator config or fetch from instance

### Step 7: CLI task create
- Parse `--jira` as comma-separated
- Create intake.Pipeline and run it
- Add `--no-brainstorm` flag
- Set sufficiency_checked on task after pipeline completes

### Steps 8-12: Tests and docs
- Ensure all existing tests pass
- Add new tests for new behavior
- Update architecture docs with intake module
