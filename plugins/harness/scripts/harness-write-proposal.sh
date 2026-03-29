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

[ -z "$HARNESS_DIR" ] || [ -z "$SLUG" ] && { echo "Error: --harness-dir and --slug required" >&2; exit 1; }

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
  [ -f "$CURRENT_FILE" ] && cat "$CURRENT_FILE" || echo "(no current text provided)"
  echo ""
  echo "## Proposed"
  echo ""
  [ -f "$PROPOSED_FILE" ] && cat "$PROPOSED_FILE" || echo "(no proposed text provided)"
  echo ""
  echo "## Reasoning"
  echo ""
  [ -f "$REASONING_FILE" ] && cat "$REASONING_FILE" || echo "(no reasoning provided)"
} > "$PROPOSAL_FILE"

echo "$PROPOSAL_FILE"
