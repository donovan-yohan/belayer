#!/bin/bash
#
# Belayer SessionStart Hook — Sets belayer context env vars for the session.
#
# These env vars are used by the Stop hook (stop-insurance.sh) and
# Notification hook (notification-router.sh) to identify the session.
#
# The variables are passed by the worker when spawning the session,
# but this hook ensures they're in the Claude Code env file for
# hooks and tools to access.
#
# Environment variables (set by worker at spawn time):
#   BELAYER_TASK_ID       — Temporal workflow ID
#   BELAYER_ROLE          — Role name
#   BELAYER_REPO          — Repo name (multi-repo only)
#   BELAYER_CHANNEL_PORT  — This session's channel HTTP port
#   BELAYER_OBSERVER_PORT — Observer session's channel HTTP port
#

if [ -n "$CLAUDE_ENV_FILE" ]; then
  [ -n "$BELAYER_TASK_ID" ]       && echo "export BELAYER_TASK_ID='$BELAYER_TASK_ID'" >> "$CLAUDE_ENV_FILE"
  [ -n "$BELAYER_ROLE" ]          && echo "export BELAYER_ROLE='$BELAYER_ROLE'" >> "$CLAUDE_ENV_FILE"
  [ -n "$BELAYER_REPO" ]          && echo "export BELAYER_REPO='$BELAYER_REPO'" >> "$CLAUDE_ENV_FILE"
  [ -n "$BELAYER_CHANNEL_PORT" ]  && echo "export BELAYER_CHANNEL_PORT='$BELAYER_CHANNEL_PORT'" >> "$CLAUDE_ENV_FILE"
  [ -n "$BELAYER_OBSERVER_PORT" ] && echo "export BELAYER_OBSERVER_PORT='$BELAYER_OBSERVER_PORT'" >> "$CLAUDE_ENV_FILE"
fi
exit 0
