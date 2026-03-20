#!/bin/bash
#
# Belayer Notification Hook — Routes permission prompts to the observer session.
#
# When a worker session hits a permission prompt (e.g., Claude needs approval
# to run a command), this hook POSTs a notification to the observer's channel
# so the user knows which session needs attention.
#
# Environment variables:
#   BELAYER_OBSERVER_PORT — HTTP port of the observer's channel server
#   BELAYER_ROLE          — Role name of this session
#   BELAYER_REPO          — Repo name (for multi-repo)
#

INPUT=$(cat)
NOTIFICATION_TYPE=$(echo "$INPUT" | jq -r '.notification_type // empty')
OBSERVER_PORT="${BELAYER_OBSERVER_PORT:-}"

# Only route permission_prompt and idle_prompt notifications.
if [ "$NOTIFICATION_TYPE" != "permission_prompt" ] && [ "$NOTIFICATION_TYPE" != "idle_prompt" ]; then
  exit 0
fi

# If no observer port, we can't route. Skip silently.
if [ -z "$OBSERVER_PORT" ]; then
  exit 0
fi

MESSAGE=$(echo "$INPUT" | jq -r '.message // "Permission needed"')
ROLE="${BELAYER_ROLE:-unknown}"
REPO="${BELAYER_REPO:-}"

# POST to observer's channel server.
curl -s -X POST "http://127.0.0.1:${OBSERVER_PORT}" \
  -H "Content-Type: application/json" \
  -d "{
    \"event\": \"permission_needed\",
    \"content\": \"${ROLE}${REPO:+ ($REPO)}: ${MESSAGE}\",
    \"meta\": {
      \"event\": \"permission_needed\",
      \"role\": \"${ROLE}\",
      \"repo\": \"${REPO}\",
      \"notification_type\": \"${NOTIFICATION_TYPE}\"
    }
  }" 2>/dev/null || true

exit 0
