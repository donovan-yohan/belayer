# claude-tmux Framework

Interactive Claude Code sessions in tmux windows. Each pipeline node opens a new tmux window running Claude Code with the node's description as a system prompt.

## Prerequisites

- tmux
- claude (Claude Code CLI, authenticated)
- jq (JSON processor)
- belayer (on PATH, for the Stop hook)

## How it works

1. `belayer climb` starts a pipeline run
2. For each node, belayer writes `node-context.json` and execs the node's command
3. The script reads context, configures a Claude Code Stop hook, and opens a tmux window
4. Claude does its work and exits
5. The Stop hook calls `belayer node-complete`, which writes a completion file
6. Belayer detects the completion file and routes to the next node

## Customization

- Edit `pipeline.yaml` to change node descriptions, add/remove nodes, adjust gate thresholds
- Edit scripts to change how Claude is invoked (different flags, different agents)
- Create new scripts for different node types
