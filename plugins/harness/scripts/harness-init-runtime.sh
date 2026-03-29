#!/usr/bin/env bash
# Scaffold .harness/ directory with default configuration and empty metrics.
# Usage: harness-init-runtime.sh --harness-dir <path> [--repo-name <owner/repo>] [--force]
set -euo pipefail

HARNESS_DIR=""
REPO_NAME=""
FORCE=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --harness-dir) HARNESS_DIR="$2"; shift 2 ;;
    --repo-name) REPO_NAME="$2"; shift 2 ;;
    --force) FORCE=true; shift ;;
    *) echo "Usage: $0 --harness-dir <path> [--repo-name <owner/repo>] [--force]" >&2; exit 1 ;;
  esac
done

[ -z "$HARNESS_DIR" ] && { echo "Error: --harness-dir required" >&2; exit 1; }

if [ -f "$HARNESS_DIR/manifest.yaml" ] && [ "$FORCE" != "true" ]; then
  echo "Warning: $HARNESS_DIR already initialized (use --force to overwrite)" >&2
  exit 0
fi

mkdir -p "$HARNESS_DIR"/{agents,metrics,memory,proposals,handoffs,runs}

# Determine storage tier from path
STORAGE_TIER="repo"
case "$HARNESS_DIR" in
  "$HOME"/.harness/*) STORAGE_TIER="global" ;;
esac

# manifest.yaml
cat > "$HARNESS_DIR/manifest.yaml" <<EOF
protocol_version: "1.0.0"
created: "$(date +%Y-%m-%d)"
repo: "$REPO_NAME"
storage_tier: "$STORAGE_TIER"
features:
  evolve: true
  test: false
  document: false
  contribute: false
EOF

# config.yaml
cat > "$HARNESS_DIR/config.yaml" <<EOF
agents:
  code-reviewer: true
  silent-failure-hunter: true
  pr-test-analyzer: true
  type-design-analyzer: true
  comment-analyzer: true
  learnings-reviewer: true

evolve:
  auto_apply: true
  min_runs_for_auto: 5
  contribute: false
  contribute_repo: "$REPO_NAME"

review:
  max_cycles: 3
  adversarial: true
  evaluator: true
EOF

# Empty metrics files
for metric in review-effectiveness plan-accuracy learning-efficacy phase-costs; do
  cat > "$HARNESS_DIR/metrics/$metric.json" <<METRICEOF
{
  "schema_version": 1,
  "last_updated": null
}
METRICEOF
done

# IMPROVEMENTS.md
cat > "$HARNESS_DIR/memory/IMPROVEMENTS.md" <<'EOF'
# Harness Improvements

Audit trail of self-modifications to agent definitions. Append-only.

---
EOF

# .gitignore
cat > "$HARNESS_DIR/.gitignore" <<'EOF'
handoffs/
memory/session-history.json
review-results.json
phase-timing.tmp
run-state.json
EOF

echo "Scaffolded $HARNESS_DIR"
