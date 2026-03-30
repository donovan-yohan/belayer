#!/usr/bin/env bash
# Resolve .harness/ or ~/.harness/<repo-slug>/ storage tier.
# Outputs the resolved path to stdout, or empty string if neither exists.
# Usage: harness-resolve-dir.sh [--repo-root <path>]
set -euo pipefail

REPO_ROOT="."
while [[ $# -gt 0 ]]; do
  case $1 in
    --repo-root) REPO_ROOT="$2"; shift 2 ;;
    *) echo "Usage: $0 [--repo-root <path>]" >&2; exit 1 ;;
  esac
done

if [ ! -d "$REPO_ROOT" ]; then
  echo "Error: --repo-root '$REPO_ROOT' is not a directory" >&2
  exit 1
fi

REPO_SLUG=$(basename "$(cd "$REPO_ROOT" && git rev-parse --show-toplevel 2>/dev/null || echo "$REPO_ROOT")")

# Tier 1: repo-local
if [ -d "$REPO_ROOT/.harness" ]; then
  echo "$REPO_ROOT/.harness"
  exit 0
fi

# Tier 2: global
GLOBAL_DIR="$HOME/.harness/$REPO_SLUG"
if [ -d "$GLOBAL_DIR" ]; then
  echo "$GLOBAL_DIR"
  exit 0
fi

# Neither exists — output empty
echo ""
