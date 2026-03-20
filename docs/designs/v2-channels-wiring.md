---
status: current
created: 2026-03-20
branch: master
---
# Design: Channel-Aware Spawner + Observer Session Wiring

## Summary

Wire the belayer channel server and hooks into the session spawn flow and `belayer run` command. This connects the channel/hooks infrastructure (already built) to the actual Claude Code sessions.

## Goal

After this, `belayer run "description"` opens an observer Claude Code session with the belayer channel, and worker sessions (setter, lead, etc.) spawn in tmux with channels + hooks configured. Pipeline events flow from Temporal → HTTP → channel → Claude Code session.

## Approach

### Task 3: Channel-aware session spawner

Update `ClaudeSessionSpawner.Spawn` to:
1. Accept `ChannelPort` and `ObserverPort` in `SessionOpts`
2. When `ChannelPort > 0`: add `--dangerously-load-development-channels server:belayer-channel` to the claude command
3. Set env vars in the tmux command: `BELAYER_CHANNEL_PORT`, `BELAYER_TASK_ID`, `BELAYER_ROLE`, `BELAYER_REPO`, `BELAYER_OBSERVER_PORT`
4. Write a per-session `.mcp.json` in the WorkDir so Claude Code finds the channel server
5. Configure hooks in the session's settings by writing a `.claude/settings.local.json` with Stop + Notification + SessionStart hooks pointing to the hook scripts

When `ChannelPort == 0`: no channels, no hooks — original tmux-only behavior (Codex/Cursor fallback).

### Task 5: Observer session in `belayer run`

Update `belayer run` to:
1. Assign port 8790 to the observer
2. Write a temp `.mcp.json` with the belayer-channel server configured at that port
3. Instead of just starting the Temporal workflow and printing the ID, exec into a Claude Code session:
   - `claude --dangerously-load-development-channels server:belayer-channel --append-system-prompt "You are the belayer observer..."`
4. The workflow starts in the background (via a goroutine that connects to Temporal)
5. Pipeline events arrive in the observer session as `<channel>` tags
6. Add `--detach` flag to preserve the old fire-and-forget behavior

### Worker session port assignment

The worker assigns sequential ports starting at 8791 for worker sessions:
- Observer: 8790
- Setter: 8791
- Lead (repo-a): 8792
- Lead (repo-b): 8793
- etc.

Port is passed via `BELAYER_CHANNEL_PORT` env var to each session's channel server.

## Key Decisions

- Use `--dangerously-load-development-channels` during research preview (not `--channels`)
- Per-session `.mcp.json` written to WorkDir so each session finds its own channel server
- Hook scripts referenced by absolute path from the belayer repo's `channel/hooks/` directory
- Observer runs in foreground (user's terminal). Workers run in tmux.
- `--detach` flag on `belayer run` for fire-and-forget mode (starts workflow, prints ID, exits)
