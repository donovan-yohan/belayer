#!/usr/bin/env bash
# Write a timestamped run record to .harness/runs/
# Usage: harness-write-run.sh --harness-dir <path> --phase <phase> [--branch <branch>] [--data-file <path>]
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

# Compute timestamp once to ensure TIMESTAMP and NOW refer to the same instant
EPOCH=$(date +%s)
TIMESTAMP=$(date -u -r "$EPOCH" +%Y-%m-%dT%H%M%SZ 2>/dev/null || date -u -d "@$EPOCH" +%Y-%m-%dT%H%M%SZ)
NOW=$(date -u -r "$EPOCH" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "@$EPOCH" +%Y-%m-%dT%H:%M:%SZ)
RUN_FILE="$RUNS_DIR/${TIMESTAMP}-${PHASE}.json"

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
tmp_file = os.environ['RUN_FILE'] + '.tmp'
try:
    with open(tmp_file, 'w') as f:
        json.dump(record, f, indent=2)
    os.rename(tmp_file, os.environ['RUN_FILE'])
except OSError as e:
    print(f"Error: failed to write run record: {e}", file=sys.stderr)
    try: os.unlink(tmp_file)
    except OSError: pass
    sys.exit(1)
PYEOF

echo "$RUN_FILE"
