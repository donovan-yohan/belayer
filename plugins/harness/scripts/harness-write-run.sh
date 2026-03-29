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

[ -z "$HARNESS_DIR" ] || [ -z "$PHASE" ] && { echo "Error: --harness-dir and --phase required" >&2; exit 1; }

RUNS_DIR="$HARNESS_DIR/runs"
mkdir -p "$RUNS_DIR"

TIMESTAMP=$(date -u +%Y-%m-%dT%H%M%SZ)
RUN_FILE="$RUNS_DIR/${TIMESTAMP}-${PHASE}.json"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

python3 -c "
import json

data = {}
data_file = '$DATA_FILE'
if data_file:
    try:
        with open(data_file) as f:
            data = json.load(f)
    except (FileNotFoundError, json.JSONDecodeError):
        pass

record = {
    'phase': '$PHASE',
    'branch': '${BRANCH:-unknown}',
    'timestamp': '$NOW',
    'data': data
}
with open('$RUN_FILE', 'w') as f:
    json.dump(record, f, indent=2)
"

echo "$RUN_FILE"
