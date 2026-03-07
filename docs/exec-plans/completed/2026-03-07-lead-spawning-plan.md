# Execution Plan: Lead Spawning (Goal 3)

**Design Doc**: [lead-spawning-design](../../design-docs/2026-03-07-lead-spawning-design.md)
**PRD Goal**: 3 — Lead spawning — AgentSpawner interface and per-goal sessions

## Steps

### 1. Create `internal/lead/spawner.go` — AgentSpawner interface
- [x] Define `AgentSpawner` interface with `Spawn(ctx, SpawnOpts) error`
- [x] Define `SpawnOpts` struct
- File: `internal/lead/spawner.go`

### 2. Create `internal/lead/prompt.go` — Prompt template builder
- [x] Define `PromptData` struct with Spec, GoalID, RepoName, Description fields
- [x] Define `BuildPrompt(data PromptData) (string, error)` using `text/template`
- [x] Include spec content, goal description, harness instructions, DONE.json format
- File: `internal/lead/prompt.go`

### 3. Create `internal/lead/claude.go` — Claude Code spawner
- [x] Implement `ClaudeSpawner` struct with `tmux.TmuxManager` dependency
- [x] Implement `Spawn` method: builds `claude -p` command, sends to tmux window
- [x] Shell-quote the prompt to prevent injection
- File: `internal/lead/claude.go`

### 4. Integrate AgentSpawner into TaskRunner
- [x] Add `spawner lead.AgentSpawner` field to `TaskRunner`
- [x] Update `NewTaskRunner` to accept `AgentSpawner` parameter
- [x] Update `SpawnGoal` to use `spawner.Spawn()` with built prompt
- [x] Update setter.go `New` to accept and pass `AgentSpawner`
- [x] Update cli/setter.go to create `ClaudeSpawner` and pass to `setter.New`
- Files: `internal/setter/taskrunner.go`, `internal/setter/setter.go`, `internal/cli/setter.go`

### 5. Write tests
- [x] `internal/lead/prompt_test.go` — template renders correctly
- [x] `internal/lead/claude_test.go` — verifies correct command construction via mock tmux
- [x] Update `internal/setter/setter_test.go` — update all test setup to pass mock spawner
- Files: `internal/lead/*_test.go`, `internal/setter/setter_test.go`

### 6. Run tests
- [x] `go test ./...` passes
- [x] `go build -o belayer ./cmd/belayer` succeeds

### 7. Verify acceptance criteria
- [x] AgentSpawner interface has Spawn method
- [x] Claude implementation spawns `claude -p` in tmux window
- [x] Prompt includes spec.md, goal description, DONE.json instructions
- [x] DONE.json contains structured output format
- [x] Per-goal tmux windows named `<repo>-<goal-id>`
- [x] Vendor abstraction: no Claude-specific code outside `lead/claude.go`
