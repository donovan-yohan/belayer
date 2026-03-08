# Interactive Lead Sessions

**Date:** 2026-03-08
**Status:** Approved

## Problem

Leads currently run as `claude -p` one-shot sessions with prompt templates piped via stdin. This sidesteps the entire harness engineering stack: leads can't read CLAUDE.md, use skills, access MCP tools (Chrome DevTools, etc.), spawn agent teams, or maintain documentation through the harness reflect/complete cycle. The quality and knowledge persistence gains from the harness plugin are lost because the lead never enters the full Claude Code environment.

The prompt template approach tries to replicate what the environment already provides — context, instructions, workflow — but in a flat, one-shot format that can't adapt, discover, or persist knowledge.

## Solution

Replace `claude -p < prompt.md` with full interactive Claude Code sessions for all agent types (lead, spotter, anchor). Each agent runs in the real Claude Code environment with CLAUDE.md, skills, MCP tools, and harness workflow access.

## Core Principle

From the OpenAI harness engineering post: "Repository knowledge is the system of record." The lead should discover context progressively through CLAUDE.md and docs/, not receive a flat prompt dump. The harness plugin handles plan → orchestrate → complete. Belayer's job is to prepare the environment and supervise.

## Architecture

### Environment Preparation

Before spawning any agent, the setter prepares the worktree with two files:

**`.claude/CLAUDE.md`** — Auto-loaded by Claude Code, not committed to git. Contains:
- Role identification (lead / spotter / anchor)
- Pointer to `.lead/GOAL.json` for assignment context
- Signal file contracts (DONE.json / SPOT.json / VERDICT.json)
- Autonomous operation mandate — no questions, no waiting for input
- Harness workflow guidance (plan → orchestrate → complete)

**`.lead/GOAL.json`** — The goal context, structured per role:

```json
{
  "role": "lead",
  "task_spec": "Full task specification from the user...",
  "goal_id": "frontend-1",
  "repo_name": "vct-fantasy-league",
  "description": "Implement the draft page with player cards",
  "attempt": 1,
  "spotter_feedback": null
}
```

For spotters: adds `project_type`, `profiles` (validation checklists), `done_json` content.
For anchors: adds `repo_diffs`, `goal_summaries`.

### Spawner Change

Old:
```bash
cd <workdir> && claude -p --dangerously-skip-permissions < .belayer-prompt.md 2>&1; echo 'Claude session exited'
```

New:
```bash
cd <workdir> && claude --dangerously-skip-permissions "You are a belayer lead. Read .lead/GOAL.json for your assignment. Operate fully autonomously." 2>&1; echo 'Claude session exited'
```

The prompt is a positional argument (not `--initial-prompt`, which doesn't exist). No stdin piping. The agent starts, reads CLAUDE.md naturally, discovers its goal, and works using the full Claude Code toolkit.

### .claude/CLAUDE.md Template

```markdown
# Belayer {{.Role}}

You are operating as an autonomous {{.Role}} agent managed by belayer.

## Your Assignment

Read `.lead/GOAL.json` for your full assignment context including task spec, goal description, and any feedback from previous attempts.

## Autonomous Operation

You MUST operate fully autonomously:
- NEVER ask questions or wait for user input
- NEVER request clarification — make your best judgment and proceed
- If you encounter ambiguity, document your decision and move forward
- Use available skills, MCP tools, and harness commands as needed

## Workflow

1. Read `.lead/GOAL.json` to understand your assignment
2. Use `/harness:plan` to create an implementation plan for your goal
3. Use `/harness:orchestrate` to execute with agent teams if beneficial
4. Implement, test, commit, and push your changes
5. Use `/harness:reflect` to update documentation
6. Write `DONE.json` when complete (see format below)

## DONE.json Contract

When finished, write `DONE.json` in the working directory:

{{`{
  "status": "complete",
  "summary": "Brief description of what was done",
  "files_changed": ["list", "of", "files"],
  "notes": "Any context for reviewers"
}`}}

If you cannot complete the goal, write DONE.json with `"status": "failed"` and explain what blocked you.

IMPORTANT: You MUST commit, push, and write DONE.json before your session ends.
```

Spotter and anchor variants follow the same structure with role-specific contracts (SPOT.json, VERDICT.json).

### Tmux Window Configuration

Each window is configured with:
```
tmux set-option -t <session>:<window> remain-on-exit on
```

This keeps the pane open after claude exits, allowing the setter to inspect exit status via `#{pane_dead}` and `#{pane_dead_status}` before cleanup.

## Stuck Session Detection

No `tmux send-keys` nudging — Claude Code's Ink TUI makes programmatic input fragile (known issues: GitHub #15553, #23513, #31739). Instead: prevent stalls and restart on failure.

### Prevention

The `.claude/CLAUDE.md` is emphatic about autonomous operation. `--dangerously-skip-permissions` eliminates permission prompts. The remaining stall sources are:
- Clarification questions (prevented by CLAUDE.md instructions)
- Edge cases in skills/plugins (prevented by the autonomous mandate)
- Unexpected errors causing the TUI to hang (detected by silence monitoring)

### Detection

The setter monitors for stuck sessions using **log file modification time** (simpler and more reliable than tmux hooks):

1. Each tick, check `os.Stat(logFile).ModTime()` for running goals
2. If no output for the silence threshold (configurable, default 2 minutes):
   - Capture pane content via `tmux capture-pane -t <session>:<window> -p -S -30`
   - Check if the last lines show an input prompt pattern (`>` at end of output)
   - Also check `#{pane_dead}` — if the process exited without DONE.json, it crashed
3. If confirmed stuck/crashed: kill window, mark goal failed, retry with incremented attempt

### Recovery

Same as existing stale detection — the goal is re-queued with incremented attempt count. If spotter feedback exists from a prior attempt, it's included in `.lead/GOAL.json` for the retry.

Maximum attempts (default 3) before the goal is marked stuck, same as today.

## What Gets Removed

- `internal/defaults/prompts/lead.md` — replaced by `.claude/CLAUDE.md` template
- `internal/defaults/prompts/spotter.md` — replaced by spotter CLAUDE.md variant
- `internal/defaults/prompts/anchor.md` — replaced by anchor CLAUDE.md variant
- `lead.BuildPrompt()` / `lead.BuildPromptDefault()` — no longer needed
- `spotter.BuildSpotterPrompt()` / `spotter.BuildSpotterPromptDefault()` — no longer needed
- `anchor.BuildAnchorPrompt()` / `anchor.BuildAnchorPromptDefault()` — no longer needed
- `.belayer-prompt.md` temp file writing in ClaudeSpawner — eliminated

## What Stays the Same

- **DONE.json / SPOT.json / VERDICT.json** polling — coordination protocol unchanged
- **Tmux session/window lifecycle** — same creation, logging, cleanup
- **DAG execution, retry logic, spotter feedback flow** — all unchanged
- **Config system** (belayer.toml, validation profiles) — stays, but profiles written to `.lead/` for agent discovery
- **belayerconfig resolution chain** — still used for CLAUDE.md template resolution

## What Changes

- `ClaudeSpawner.Spawn()` — positional arg instead of stdin piping
- `TaskRunner.SpawnGoal()` — writes `.claude/CLAUDE.md` + `.lead/GOAL.json` instead of building prompt strings
- `TaskRunner.SpawnSpotter()` — same pattern, spotter-specific CLAUDE.md + GOAL.json
- `TaskRunner.SpawnAnchor()` — same pattern, anchor-specific CLAUDE.md + GOAL.json
- Stale detection — enhanced with log mtime silence checking
- Tmux windows — `remain-on-exit on` for exit status inspection

## Error Handling

- **Agent never writes DONE.json**: Existing stale timeout (default 30m) applies. After timeout, goal marked failed, retry.
- **Agent exits unexpectedly**: Setter detects `#{pane_dead}` without DONE.json. Mark failed, retry.
- **CLAUDE.md conflicts**: If the target repo already has `.claude/CLAUDE.md`, prepend belayer instructions to existing content.
- **Harness plugin not installed**: CLAUDE.md instructions work standalone — plan, implement, test, commit, write DONE.json. Harness just makes it better. No hard dependency.

## Key Decisions

- **Positional arg, not `--initial-prompt`**: The `--initial-prompt` flag doesn't exist in Claude Code. Positional argument achieves the same thing.
- **No send-keys nudging**: Claude Code's Ink TUI makes `tmux send-keys` fragile. Prevent stalls via instructions; restart on failure.
- **Log mtime over tmux hooks**: Checking log file modification time is simpler and more reliable than `monitor-silence` tmux hooks for Go integration.
- **`.claude/CLAUDE.md` over project CLAUDE.md**: Keeps belayer instructions out of git history while still being auto-loaded by Claude Code.
- **Assume harness installed**: No need to ship a custom harness plugin. The user's existing harness plugin provides the workflow. Belayer prepares the environment.
