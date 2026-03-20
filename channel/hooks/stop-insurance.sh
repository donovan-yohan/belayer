#!/bin/bash
#
# Belayer Stop Hook — Insurance for session completion signaling.
#
# The agent is prompted to call `belayer <role> finish` explicitly.
# This hook fires when the session ends — if finish was already called,
# the CLI is idempotent (second call is a no-op). If the agent forgot,
# this catches it.
#
# Environment variables (set by session-start.sh):
#   BELAYER_TASK_ID  — Temporal workflow ID
#   BELAYER_ROLE     — Role name (setter, lead, etc.)
#   BELAYER_REPO     — Repo name (for multi-repo, empty for single)
#

TASK_ID="${BELAYER_TASK_ID:-}"
ROLE="${BELAYER_ROLE:-}"
REPO="${BELAYER_REPO:-}"

if [ -z "$TASK_ID" ] || [ -z "$ROLE" ]; then
  # Not a belayer-managed session. Skip.
  exit 0
fi

REPO_FLAG=""
if [ -n "$REPO" ]; then
  REPO_FLAG="--repo $REPO"
fi

# Call finish as insurance. Idempotent — safe to call even if agent already did.
belayer "$ROLE" finish --task-id "$TASK_ID" $REPO_FLAG 2>/dev/null || true
exit 0
