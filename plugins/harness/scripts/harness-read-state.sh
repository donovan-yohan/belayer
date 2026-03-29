#!/usr/bin/env bash
# Read run-state.json to stdout. Empty output if file doesn't exist.
# Usage: harness-read-state.sh --harness-dir <path>
set -euo pipefail

HARNESS_DIR=""
while [[ $# -gt 0 ]]; do
  case $1 in
    --harness-dir) HARNESS_DIR="$2"; shift 2 ;;
    *) echo "Usage: $0 --harness-dir <path>" >&2; exit 1 ;;
  esac
done

[ -z "$HARNESS_DIR" ] && { echo "Error: --harness-dir required" >&2; exit 1; }

STATE_FILE="$HARNESS_DIR/run-state.json"

if [ -f "$STATE_FILE" ]; then
  cat "$STATE_FILE"
else
  echo '{"phase": "none"}'
fi
