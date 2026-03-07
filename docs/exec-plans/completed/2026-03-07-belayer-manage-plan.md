# Execution Plan: Belayer Manage

**Goal**: 5 — Belayer manage — interactive agent session for task creation
**Design Doc**: `docs/design-docs/2026-03-07-belayer-manage-design.md`

## Steps

| # | Step | Status | Files |
|---|------|--------|-------|
| 1 | Create manage prompt template | pending | `internal/manage/prompt.go` |
| 2 | Create manage prompt tests | pending | `internal/manage/prompt_test.go` |
| 3 | Create manage CLI command | pending | `internal/cli/manage.go` |
| 4 | Register manage command in root | pending | `internal/cli/root.go` |
| 5 | Run tests | pending | - |
| 6 | Verify acceptance criteria | pending | - |

## Step Details

### Step 1: Create manage prompt template
- Create `internal/manage/prompt.go`
- Define `PromptData` struct with instance name and repo names
- Write prompt template that teaches the agent belayer CLI usage
- Include spec.md format guide and goals.json schema
- Include workflow descriptions (brainstorm, Jira, ready files)
- `BuildPrompt(data PromptData) (string, error)` function

### Step 2: Create manage prompt tests
- Create `internal/manage/prompt_test.go`
- Test template renders with instance context
- Test repo names appear in output
- Test all required sections are present

### Step 3: Create manage CLI command
- Create `internal/cli/manage.go`
- `belayer manage --instance <name>` command
- Load instance config to get repo names
- Build prompt from template
- Exec `claude -p <prompt> --allowedTools '*'` replacing current process

### Step 4: Register manage command in root
- Add `newManageCmd()` to `root.go`'s `cmd.AddCommand(...)`

### Step 5: Run tests
- `go test ./...`

### Step 6: Verify acceptance criteria
- Check all criteria from PRD Goal 5
