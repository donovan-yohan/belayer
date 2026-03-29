#!/usr/bin/env bash
# Write a timestamped run record to .harness/runs/
# Usage: harness-write-run.sh --harness-dir <path> --phase <phase> --branch <branch> [--data-file <path>]
set -euo pipefail

HARNESS_DIR=""
PHASE=""
BRANCH=""
DATA_FILE=""

while [[ $# -gt 0 ]]; do
  case $1 in
    --harness-dir) HARNESS_DIR="$2"; shift 2 ;;
    --phase) PHASE="$2"; shift 2 ;;
    --branch) BRANCH="$2"; shift 2 ;;
    --data-file) DATA_FILE="$2"; shift 2 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

[ -z "$HARNESS_DIR" ] && { echo "Error: --harness-dir required" >&2; exit 1; }
[ -z "$PHASE" ] && { echo "Error: --phase required" >&2; exit 1; }

RUNS_DIR="$HARNESS_DIR/runs"
mkdir -p "$RUNS_DIR"

TIMESTAMP=$(date -u +%Y-%m-%dT%H%M%SZ)
RUN_FILE="$RUNS_DIR/${TIMESTAMP}-${PHASE}.json"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

DATA_FILE="$DATA_FILE" PHASE="$PHASE" BRANCH="${BRANCH:-unknown}" NOW="$NOW" RUN_FILE="$RUN_FILE" \
python3 - <<'PYEOF'
import json, os, sys

data = {}
data_file = os.environ.get('DATA_FILE', '')
if data_file:
    try:
        with open(data_file) as f:
            data = json.load(f)
    except FileNotFoundError:
        print(f"Error: --data-file '{data_file}' not found", file=sys.stderr)
        sys.exit(1)
    except json.JSONDecodeError as e:
        print(f"Error: --data-file '{data_file}' is not valid JSON: {e}", file=sys.stderr)
        sys.exit(1)

record = {
    'phase': os.environ['PHASE'],
    'branch': os.environ.get('BRANCH', 'unknown'),
    'timestamp': os.environ['NOW'],
    'data': data
}
with open(os.environ['RUN_FILE'], 'w') as f:
    json.dump(record, f, indent=2)
PYEOF

echo "$RUN_FILE"
