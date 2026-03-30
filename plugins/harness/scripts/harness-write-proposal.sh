#!/usr/bin/env bash
# Create a proposal markdown file in .harness/proposals/
# Usage: harness-write-proposal.sh --harness-dir <path> --slug <slug> --scope <repo|universal> --signal <signal> --agent <agent> --current-file <path> --proposed-file <path> --reasoning-file <path>
# Note: --current-file, --proposed-file, --reasoning-file are paths to temp files containing the content (avoids shell escaping issues with long markdown)
set -euo pipefail

HARNESS_DIR=""
SLUG=""
SCOPE="repo"
SIGNAL=""
AGENT=""
CURRENT_FILE=""
PROPOSED_FILE=""
REASONING_FILE=""

while [[ $# -gt 0 ]]; do
  case $1 in
    --harness-dir) HARNESS_DIR="$2"; shift 2 ;;
    --slug) SLUG="$2"; shift 2 ;;
    --scope) SCOPE="$2"; shift 2 ;;
    --signal) SIGNAL="$2"; shift 2 ;;
    --agent) AGENT="$2"; shift 2 ;;
    --current-file) CURRENT_FILE="$2"; shift 2 ;;
    --proposed-file) PROPOSED_FILE="$2"; shift 2 ;;
    --reasoning-file) REASONING_FILE="$2"; shift 2 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

[ -z "$HARNESS_DIR" ] && { echo "Error: --harness-dir required" >&2; exit 1; }
[ -z "$SLUG" ] && { echo "Error: --slug required" >&2; exit 1; }

# Validate --scope against allowed values
case "$SCOPE" in
  repo|universal) ;;
  *) echo "Error: --scope '$SCOPE' is not valid. Allowed: repo, universal" >&2; exit 1 ;;
esac

DATE=$(date +%Y-%m-%d)
mkdir -p "$HARNESS_DIR/proposals"
PROPOSAL_FILE="$HARNESS_DIR/proposals/${DATE}-${SLUG}.md"

{
  echo "# Proposal: $SLUG"
  echo ""
  echo "- **Date:** $DATE"
  echo "- **Signal:** $SIGNAL"
  echo "- **Agent:** $AGENT"
  echo "- **Scope:** $SCOPE"
  echo "- **Status:** pending"
  echo ""
  echo "## Current"
  echo ""
  if [ -f "$CURRENT_FILE" ]; then
    cat "$CURRENT_FILE"
  elif [ -n "$CURRENT_FILE" ]; then
    echo "Warning: --current-file '$CURRENT_FILE' not found, using placeholder" >&2
    echo "(no current text provided)"
  else
    echo "(no current text provided)"
  fi
  echo ""
  echo "## Proposed"
  echo ""
  if [ -f "$PROPOSED_FILE" ]; then
    cat "$PROPOSED_FILE"
  elif [ -n "$PROPOSED_FILE" ]; then
    echo "Warning: --proposed-file '$PROPOSED_FILE' not found, using placeholder" >&2
    echo "(no proposed text provided)"
  else
    echo "(no proposed text provided)"
  fi
  echo ""
  echo "## Reasoning"
  echo ""
  if [ -f "$REASONING_FILE" ]; then
    cat "$REASONING_FILE"
  elif [ -n "$REASONING_FILE" ]; then
    echo "Warning: --reasoning-file '$REASONING_FILE' not found, using placeholder" >&2
    echo "(no reasoning provided)"
  else
    echo "(no reasoning provided)"
  fi
} > "$PROPOSAL_FILE"

echo "$PROPOSAL_FILE"
