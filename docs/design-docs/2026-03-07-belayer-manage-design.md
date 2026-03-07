# Belayer Manage: Interactive Agent Session for Task Creation

**Date**: 2026-03-07
**Status**: Proposed
**Goal**: 5 — Belayer manage — interactive agent session for task creation

## Problem Statement

Users need a way to interactively create tasks for belayer. Currently, `belayer task create --spec spec.md --goals goals.json` requires pre-generated files. The `belayer manage` command bridges this gap by spawning an interactive agent session (Claude Code) that can brainstorm, fetch Jira tickets, generate spec.md and goals.json, and invoke the CLI to publish tasks.

Key principle: **No LLM inference happens in the CLI itself** — all inference occurs in the manage session.

## Design

### Command

```bash
belayer manage --instance <name>
```

Starts an interactive Claude Code session in the current terminal (not in tmux — this is a user-facing command). The agent receives a system prompt that trains it on belayer CLI usage and available workflows.

### Architecture

```
User terminal
  |
  belayer manage --instance foo
  |
  exec claude -p <manage-prompt>
  |
  Claude Code session (interactive)
    |-- can run: belayer task create --instance foo --spec spec.md --goals goals.json
    |-- can run: brainstorm skill to generate spec.md + goals.json
    |-- can run: fetch Jira tickets and convert to task format
    |-- has: full belayer CLI docs in prompt
```

### Key Decision: `exec` vs `subprocess`

The manage command uses `os/exec` to replace the current process with `claude`. This is the simplest approach:
- No need to proxy stdin/stdout
- Claude gets full terminal control (colors, cursor, etc.)
- User interacts directly with the agent
- When the agent exits, the belayer process exits

Alternative considered: running Claude as a subprocess and capturing output. Rejected because:
- Adds complexity for no benefit (the user wants to interact with the agent directly)
- Would need to handle terminal passthrough (pty forwarding)

### Prompt Design

The manage prompt teaches the agent how to use belayer. It includes:

1. **Role description**: You are a belayer — an interactive assistant for creating tasks
2. **Instance context**: Which instance is active, what repos it has
3. **CLI reference**: Full usage for `belayer task create`, flags, and input formats
4. **Spec.md format guide**: What a good spec looks like
5. **Goals.json schema**: The exact JSON format with examples
6. **Workflows**:
   - "I have a Jira ticket" → fetch and convert to spec + goals
   - "I have an idea" → brainstorm, then generate spec + goals
   - "I have files ready" → just run task create

The prompt does NOT include:
- Internal belayer architecture details
- Setter/spotter/lead mechanics
- SQLite schema

### Package Structure

```
internal/
  manage/
    prompt.go       # Prompt template and data types
    prompt_test.go  # Template rendering tests
  cli/
    manage.go       # cobra command that execs claude with prompt
```

### Instance Context

The manage command loads the instance config to provide repo context in the prompt. The agent needs to know:
- Instance name
- Repo names (so it can generate valid goals.json with correct repo references)

### No Jira SDK

The acceptance criteria mention "can fetch and convert Jira tickets to task format." Rather than building a Jira client into belayer, the manage prompt instructs the agent to use available MCP tools or `curl`/`gh` to fetch ticket details. This keeps the CLI pure (no API keys, no HTTP clients) and leverages the agent's existing capabilities.

### Testing Strategy

- **prompt_test.go**: Verify template renders correctly with instance data
- **manage_test.go**: Verify cobra command setup (flags, validation). Cannot integration-test the `exec` call, but can test prompt generation and flag parsing.

## Decisions Made

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Process model | `exec` (replace process) | Simplest; user gets direct terminal access |
| Prompt scope | CLI usage + workflows only | Agent doesn't need internal architecture knowledge |
| Jira integration | Via agent capabilities (MCP/curl) | Keeps CLI pure; no API clients in Go code |
| Package location | `internal/manage/` | Consistent with `internal/lead/`, `internal/setter/` |
| Terminal mode | User's terminal, not tmux | This is a user-facing interactive command |
