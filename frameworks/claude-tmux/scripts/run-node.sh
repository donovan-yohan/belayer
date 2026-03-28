#!/usr/bin/env bash
set -euo pipefail

# Belayer claude-tmux framework: node runner
# Reads context from env vars + node-context.json, writes Claude Code Stop hook, opens Claude in tmux.

# Dependency checks.
command -v jq >/dev/null 2>&1 || { echo "ERROR: jq is required but not installed. Install with: brew install jq" >&2; exit 1; }
command -v tmux >/dev/null 2>&1 || { echo "ERROR: tmux is required but not installed. Install with: brew install tmux" >&2; exit 1; }
command -v claude >/dev/null 2>&1 || { echo "ERROR: claude CLI is required but not installed." >&2; exit 1; }

TASK_ID="${BELAYER_TASK_ID:?}"
NODE="${BELAYER_NODE:?}"
ATTEMPT="${BELAYER_ATTEMPT:?}"
WORK_DIR="${BELAYER_WORK_DIR:?}"

CONTEXT_FILE="$WORK_DIR/.belayer/.internal/input/node-context.json"
[ -f "$CONTEXT_FILE" ] || { echo "ERROR: node-context.json not found at $CONTEXT_FILE" >&2; exit 1; }

DESCRIPTION=$(jq -r '.description // empty' "$CONTEXT_FILE")
[ -n "$DESCRIPTION" ] || { echo "ERROR: description is empty in $CONTEXT_FILE" >&2; exit 1; }
INPUT_PROMPT=$(jq -r '.input_prompt // empty' "$CONTEXT_FILE")

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
