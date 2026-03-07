# Design: Bundled Lead Execution Loop (Goal 3)

**Date**: 2026-03-06
**Goal**: Implement the bundled lead execution loop that runs in worktrees, executes goals via `claude -p`, writes progress to SQLite, and handles retry/stuck/complete states.

## Overview

The lead execution loop is belayer's per-repo agent. It receives a spec (what to implement), runs in an isolated git worktree, and uses `claude -p` to implement and review changes. Progress is tracked in SQLite, events are emitted on state transitions, and verdict.json files determine pass/fail for each attempt.

## Architecture

```
Go Runner (internal/lead/)
  |
  |-- Writes .lead/spec.md, .lead/goals.json to worktree
  |-- Launches lead.sh via os/exec
  |-- Reads structured JSON events from stdout (line-delimited)
  |-- Updates SQLite on each event
  |-- Emits events to the events table
  |
  v
Shell Script (lead.sh, embedded via embed.FS)
  |-- Reads .lead/spec.md and .lead/goals.json
  |-- For each goal:
  |     |-- Execute: claude -p (implements the goal)
  |     |-- Review: claude -p (writes .lead/verdict.json)
  |     |-- Parse verdict -> pass/fail
  |     |-- Retry on fail (up to MAX_ATTEMPTS)
  |-- Outputs structured JSON event lines to stdout
  |-- Exits 0 on success, 1 on stuck/failure
```

## Design Decisions

### 1. Shell script with Go runner (not pure Go)

The shell script is the execution unit that runs in the worktree. Go orchestrates launching it, monitoring stdout, and persisting state. This separation keeps the script simple and the Go code testable.

The shell script is embedded via `//go:embed scripts/lead.sh` and written to `.lead/lead.sh` in the worktree before execution.

### 2. Structured stdout for progress (not file polling)

The shell script emits JSON event lines to stdout. The Go runner reads them in real-time via bufio.Scanner. This avoids polling delays and filesystem race conditions.

Event format (one JSON object per line):
```json
{"type":"started"}
{"type":"goal_started","goal":0,"attempt":1,"description":"Implement the feature"}
{"type":"goal_executing","goal":0,"attempt":1}
{"type":"goal_reviewing","goal":0,"attempt":1}
{"type":"goal_verdict","goal":0,"attempt":1,"pass":true,"summary":"All changes correct"}
{"type":"goal_complete","goal":0}
{"type":"complete"}
```

### 3. Goals as sub-tasks within a lead

A lead can have one or more goals. Each goal goes through the execute->review->verdict cycle independently. Goals are defined in `.lead/goals.json` (written by Go before launching). For the initial implementation, a single goal covering the full spec is typical.

### 4. Verdict.json format

```json
{
  "pass": true,
  "summary": "Implementation matches spec",
  "issues": []
}
```

Written by `claude -p` during the review step. The Go runner reads it after each verdict event and stores it in SQLite.

### 5. Model split: Opus for execute, Sonnet for review

Following the PRD's research influences (Anthropic: "Opus for lead / Sonnet for review"). Configurable via environment variables `LEAD_EXECUTE_MODEL` and `LEAD_REVIEW_MODEL`.

### 6. Retry logic

Each goal retries up to `MAX_ATTEMPTS` (default 3). On each retry, the review feedback from the previous attempt is included in the next execution prompt. After exhausting retries, the goal is marked as stuck and the lead exits with status "stuck".

### 7. Process management

The Go runner:
- Starts the shell script as a child process
- Sets working directory to the worktree
- Passes configuration via environment variables
- Reads stdout for events, stderr for debug logging
- Handles context cancellation (sends SIGTERM, then SIGKILL after timeout)
- Captures exit code to determine final status

## Package Structure

```
internal/lead/
  runner.go          # LeadRunner: starts/monitors the lead process
  store.go           # SQLite operations for leads and lead_goals
  scripts/
    lead.sh          # Embedded shell script
  runner_test.go     # Tests for event parsing and runner logic
  store_test.go      # Tests for DB operations
```

## Database Changes

New migration `002_lead_execution.sql`:

```sql
CREATE TABLE IF NOT EXISTS lead_goals (
    id TEXT PRIMARY KEY,
    lead_id TEXT NOT NULL REFERENCES leads(id),
    goal_index INTEGER NOT NULL,
    description TEXT DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    attempt INTEGER NOT NULL DEFAULT 0,
    output TEXT DEFAULT '',
    verdict_json TEXT DEFAULT '',
    started_at TIMESTAMP,
    finished_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_lead_goals_lead ON lead_goals(lead_id);
CREATE INDEX IF NOT EXISTS idx_lead_goals_status ON lead_goals(status);
```

## .lead/ Directory Structure

```
.lead/
  lead.sh            # Extracted shell script (executable)
  spec.md            # Task spec (written by Go before launch)
  goals.json         # Goal list (written by Go before launch)
  verdict.json       # Latest verdict (written by claude during review)
  output/            # Output capture directory
    goal-0-attempt-1-execute.txt
    goal-0-attempt-1-review.txt
```

## Error Handling

- **claude -p fails**: Script captures exit code, emits error event, retries
- **No verdict.json produced**: Treated as a failed verdict (will retry)
- **Process killed**: Go runner detects non-zero exit, marks lead as failed
- **Context cancelled**: Go runner sends SIGTERM, waits for graceful shutdown

## Testing Strategy

- **Event parsing**: Unit tests for parsing JSON event lines from stdout
- **Store operations**: Unit tests with in-memory SQLite
- **Runner integration**: Mock `claude` command (shell script that writes verdict.json) to test the full cycle without real AI calls
- **Script extraction**: Verify the embedded script is written correctly and made executable
