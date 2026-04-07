#!/usr/bin/env bash
set -euo pipefail

# Belayer gstack framework: trigger contract
# Checks for APPROVED design docs in the gstack projects directory.
# Returns exit 0 + artifact path on stdout if ready, exit 1 if not.

APPROVED_DIR=".belayer/design-docs/approved"
[ -d "$APPROVED_DIR" ] || exit 1

for doc in $(ls -t "$APPROVED_DIR"/*.md 2>/dev/null); do
  if grep -q "^Status: APPROVED" "$doc" 2>/dev/null && grep -q "^Pipeline: ready" "$doc" 2>/dev/null; then
    echo "$doc"
    exit 0
  fi
done

exit 1
