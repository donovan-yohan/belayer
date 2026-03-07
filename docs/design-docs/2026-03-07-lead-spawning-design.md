# Lead Spawning: AgentSpawner Interface and Per-Goal Sessions

**Date**: 2026-03-07
**Status**: Proposed
**Parent**: [Agent-Friendly Architecture Design](2026-03-07-agent-friendly-architecture-design.md)
**PRD Goal**: 3 — Lead spawning

## Problem Statement

The setter daemon (Goal 2) currently uses a placeholder command when spawning goals in tmux windows. Goal 3 replaces this with a real `AgentSpawner` interface that launches agent sessions (Claude Code initially) in tmux windows with properly constructed prompts, and a `DoneSignaler` that watches for DONE.json completion files.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Interface location | `internal/lead/` package | Clean separation from setter; lead package owns agent abstraction |
| Prompt template | Go `text/template` | Simple, no external deps, sufficient for structured prompts |
| Claude invocation | `claude -p "<prompt>" --allowedTools ...` | Standard Claude Code CLI invocation in non-interactive mode |
| DoneSignaler | Filesystem polling (reuse existing pattern) | Already proven in TaskRunner.CheckCompletions; no need for separate watcher |
| DONE.json path | `<worktree>/DONE.json` | Consistent with existing convention from Goal 2 |
| Vendor boundary | Interface in `lead/spawner.go`, impl in `lead/claude.go` | No Claude-specific code leaks outside `claude.go` |
| Prompt content | spec.md + goal description + harness instructions + DONE.json instructions | Per design doc section 5 |

## Architecture

### Package Layout

```
internal/lead/
  spawner.go      # AgentSpawner interface + AgentHandle
  claude.go       # ClaudeSpawner implementation
  prompt.go       # Prompt template builder
  spawner_test.go # Tests for spawner
  claude_test.go  # Tests for Claude spawner
  prompt_test.go  # Tests for prompt builder
```

### Interfaces

```go
// AgentSpawner launches agent sessions in tmux windows.
type AgentSpawner interface {
    // Spawn launches an agent in the given tmux window with the given prompt.
    // The agent runs in the specified working directory.
    Spawn(ctx context.Context, opts SpawnOpts) error
}

// SpawnOpts contains everything needed to spawn a lead session.
type SpawnOpts struct {
    TmuxSession string
    WindowName  string
    WorkDir     string
    Prompt      string
}
```

Note: The design doc's `AgentHandle` and `DoneSignaler` interfaces are not needed as separate abstractions. The setter already handles completion detection via DONE.json polling in `TaskRunner.CheckCompletions()`, and tmux window lifecycle is managed by the setter via `TmuxManager`. Adding handle/channel abstractions would duplicate existing functionality. The `AgentSpawner` interface only needs `Spawn` — the setter manages everything else.

### Claude Spawner Implementation

```go
type ClaudeSpawner struct {
    tmux tmux.TmuxManager
}

func (c *ClaudeSpawner) Spawn(ctx context.Context, opts SpawnOpts) error {
    // Build the claude command
    cmd := fmt.Sprintf("cd %s && claude -p %s --allowedTools '*'",
        shellQuote(opts.WorkDir),
        shellQuote(opts.Prompt))

    // Send to tmux window
    return c.tmux.SendKeys(opts.TmuxSession, opts.WindowName, cmd)
}
```

### Prompt Template

The prompt provides the agent with:
1. The full spec.md content
2. The specific goal description
3. Instructions to follow the harness flow
4. Instructions to write DONE.json when complete

```
You are a lead agent working on a specific goal within a larger task.

## Task Specification

{{.Spec}}

## Your Goal

**Goal ID**: {{.GoalID}}
**Repository**: {{.RepoName}}
**Description**: {{.Description}}

## Instructions

1. Read the task specification above carefully
2. Focus ONLY on your specific goal
3. Plan your approach, then implement it
4. Run tests to verify your work: `go test ./...`
5. When complete, write a DONE.json file in the current directory

## DONE.json Format

When you have completed your goal, create a file called `DONE.json` in the current directory with this exact format:

{
  "status": "complete",
  "summary": "Brief description of what you did",
  "files_changed": ["list", "of", "files", "you", "modified"],
  "notes": "Any additional context for reviewers"
}

If you cannot complete the goal, write DONE.json with status "failed":

{
  "status": "failed",
  "summary": "Why you could not complete the goal",
  "files_changed": [],
  "notes": "What blocked you"
}

IMPORTANT: You MUST write DONE.json before exiting. This is how the system knows you are finished.
```

### Integration with TaskRunner

The `TaskRunner.SpawnGoal` method currently has a placeholder. It will be updated to:

1. Accept an `AgentSpawner` (injected via `TaskRunner` constructor)
2. Build the prompt using the prompt template
3. Call `spawner.Spawn()` instead of sending a placeholder echo command

The task's spec content is available from `tr.task.Spec`.

## Testing Strategy

- **Unit tests**: Mock `TmuxManager` to verify correct command construction
- **Prompt tests**: Verify template rendering with various inputs
- **Integration point**: Verify `TaskRunner.SpawnGoal` calls the spawner correctly

No real Claude sessions are spawned in tests — the mock tmux captures the command sent to `SendKeys`.
