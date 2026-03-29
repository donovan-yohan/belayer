#!/usr/bin/env bash
# Increment metric counters for a review agent or phase.
# Usage: harness-write-metrics.sh --harness-dir <path> --metric <type> [--agent <name>] [--findings <n>] [--false-pos <n>] [--unique <n>] [--plan-slug <slug>] [--tasks-planned <n>] [--tasks-completed <n>] [--drift <n>] [--surprises <n>]
set -euo pipefail

HARNESS_DIR=""
METRIC=""
AGENT=""
FINDINGS=0
FALSE_POS=0
UNIQUE=0
PLAN_SLUG=""
TASKS_PLANNED=0
TASKS_COMPLETED=0
DRIFT=0
SURPRISES=0

while [[ $# -gt 0 ]]; do
  case $1 in
    --harness-dir) HARNESS_DIR="$2"; shift 2 ;;
    --metric) METRIC="$2"; shift 2 ;;
    --agent) AGENT="$2"; shift 2 ;;
    --findings) FINDINGS="$2"; shift 2 ;;
    --false-pos) FALSE_POS="$2"; shift 2 ;;
    --unique) UNIQUE="$2"; shift 2 ;;
    --plan-slug) PLAN_SLUG="$2"; shift 2 ;;
    --tasks-planned) TASKS_PLANNED="$2"; shift 2 ;;
    --tasks-completed) TASKS_COMPLETED="$2"; shift 2 ;;
    --drift) DRIFT="$2"; shift 2 ;;
    --surprises) SURPRISES="$2"; shift 2 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

[ -z "$HARNESS_DIR" ] || [ -z "$METRIC" ] && { echo "Error: --harness-dir and --metric required" >&2; exit 1; }

METRICS_FILE="$HARNESS_DIR/metrics/$METRIC.json"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

[ -f "$METRICS_FILE" ] || { echo "Error: $METRICS_FILE not found" >&2; exit 1; }

python3 -c "
import json

with open('$METRICS_FILE') as f:
    data = json.load(f)

metric_type = '$METRIC'
now = '$NOW'

if metric_type == 'review-effectiveness' and '$AGENT':
    agents = data.setdefault('agents', {})
    agent = agents.setdefault('$AGENT', {
        'runs': 0, 'findings': 0, 'false_positives': 0,
        'unique_catches': 0, 'last_run': None, 'disabled': False, 'disable_reason': None
    })
    agent['runs'] += 1
    agent['findings'] += $FINDINGS
    agent['false_positives'] += $FALSE_POS
    agent['unique_catches'] += $UNIQUE
    agent['last_run'] = now

elif metric_type == 'plan-accuracy' and '$PLAN_SLUG':
    plans = data.setdefault('plans', {})
    plan = plans.setdefault('$PLAN_SLUG', {
        'tasks_planned': 0, 'tasks_completed': 0,
        'drift_entries': 0, 'surprise_entries': 0, 'completion_date': None
    })
    if $TASKS_PLANNED > 0:
        plan['tasks_planned'] = $TASKS_PLANNED
    if $TASKS_COMPLETED > 0:
        plan['tasks_completed'] = $TASKS_COMPLETED
    plan['drift_entries'] += $DRIFT
    plan['surprise_entries'] += $SURPRISES

elif metric_type == 'phase-costs':
    # Phase costs updated by harness-update-state.sh timing, not here
    pass

data['last_updated'] = now

with open('$METRICS_FILE', 'w') as f:
    json.dump(data, f, indent=2)
"

echo "Updated $METRIC"
