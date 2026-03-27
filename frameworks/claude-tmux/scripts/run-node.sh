#!/usr/bin/env bash
set -euo pipefail

# Belayer claude-tmux framework: node runner
# Reads context from env vars + node-context.json, opens Claude in tmux.

TASK_ID="${BELAYER_TASK_ID:?}"
NODE="${BELAYER_NODE:?}"
ATTEMPT="${BELAYER_ATTEMPT:?}"
WORK_DIR="${BELAYER_WORK_DIR:?}"

CONTEXT_FILE="$WORK_DIR/.belayer/.internal/input/node-context.json"
DESCRIPTION=$(jq -r '.description' "$CONTEXT_FILE")
INPUT_PROMPT=$(jq -r '.input_prompt' "$CONTEXT_FILE")

# Write Claude Code Stop hook to call belayer node-complete.
HOOKS_DIR="$WORK_DIR/.belayer/.internal"
mkdir -p "$HOOKS_DIR"
HOOK_CMD="belayer node-complete --task-id ${TASK_ID} --node ${NODE} --attempt ${ATTEMPT}"
jq -n --arg cmd "$HOOK_CMD" '{
  hooks: {
    Stop: [{ hooks: [{ type: "command", command: $cmd }] }]
  }
}' > "$HOOKS_DIR/hooks.json"

# Ensure tmux session exists.
SESSION="belayer-v3"
tmux has-session -t "$SESSION" 2>/dev/null || tmux new-session -d -s "$SESSION"

# Create window and launch Claude.
WINDOW="${NODE}-${TASK_ID:0:8}"
tmux new-window -t "$SESSION" -n "$WINDOW"
tmux send-keys -t "$SESSION:$WINDOW" \
  "cd $(printf '%q' "$WORK_DIR") && claude --dangerously-skip-permissions --settings $(printf '%q' "$HOOKS_DIR/hooks.json") $(printf '%q' "$DESCRIPTION") $(printf '%q' "$INPUT_PROMPT")" Enter
