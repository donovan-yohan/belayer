#!/usr/bin/env bash
# Increment metric counters for a review agent or phase.
# Usage: harness-write-metrics.sh --harness-dir <path> --metric <type> [--agent <name>] [--findings <n>] [--false-pos <n>] [--unique <n>] [--plan-slug <slug>] [--tasks-planned <n>] [--tasks-completed <n>] [--drift <n>] [--surprises <n>] [--learning-id <id>] [--recurrence <n>] [--prevented <n>]
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
LEARNING_ID=""
RECURRENCE=""
PREVENTED=""

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
    --learning-id) LEARNING_ID="$2"; shift 2 ;;
    --recurrence) RECURRENCE="$2"; shift 2 ;;
    --prevented) PREVENTED="$2"; shift 2 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

[ -z "$HARNESS_DIR" ] && { echo "Error: --harness-dir required" >&2; exit 1; }
[ -z "$METRIC" ] && { echo "Error: --metric required" >&2; exit 1; }

# Validate metric type and required secondary args
case "$METRIC" in
  review-effectiveness)
    [ -z "$AGENT" ] && { echo "Error: --metric review-effectiveness requires --agent" >&2; exit 1; }
    ;;
  plan-accuracy)
    [ -z "$PLAN_SLUG" ] && { echo "Error: --metric plan-accuracy requires --plan-slug" >&2; exit 1; }
    ;;
  learning-efficacy)
    # Prefer dedicated --learning-id; fall back to --plan-slug for backward compat
    if [ -z "$LEARNING_ID" ] && [ -z "$PLAN_SLUG" ]; then
      echo "Error: --metric learning-efficacy requires --learning-id (or --plan-slug for backward compat)" >&2
      exit 1
    fi
    ;;
  phase-costs)
    echo "Error: phase-costs metric collection not yet implemented" >&2
    exit 1
    ;;
  *)
    echo "Error: unknown --metric '$METRIC'. Valid: review-effectiveness, plan-accuracy, learning-efficacy, phase-costs" >&2
    exit 1
    ;;
esac

METRICS_FILE="$HARNESS_DIR/metrics/$METRIC.json"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

[ -f "$METRICS_FILE" ] || { echo "Error: $METRICS_FILE not found" >&2; exit 1; }

METRICS_FILE="$METRICS_FILE" METRIC="$METRIC" NOW="$NOW" AGENT="$AGENT" \
FINDINGS="$FINDINGS" FALSE_POS="$FALSE_POS" UNIQUE="$UNIQUE" \
PLAN_SLUG="$PLAN_SLUG" TASKS_PLANNED="$TASKS_PLANNED" TASKS_COMPLETED="$TASKS_COMPLETED" \
DRIFT="$DRIFT" SURPRISES="$SURPRISES" \
LEARNING_ID="$LEARNING_ID" RECURRENCE="$RECURRENCE" PREVENTED="$PREVENTED" \
python3 - <<'PYEOF'
import json, os, sys, tempfile

metrics_file = os.environ['METRICS_FILE']
metric_type = os.environ['METRIC']
now = os.environ['NOW']

try:
    with open(metrics_file) as f:
        data = json.load(f)
except json.JSONDecodeError as e:
    print(f"Error: failed to parse {metrics_file}: {e}", file=sys.stderr)
    sys.exit(1)
except OSError as e:
    print(f"Error: failed to read {metrics_file}: {e}", file=sys.stderr)
    sys.exit(1)

if metric_type == 'review-effectiveness':
    agent_name = os.environ['AGENT']
    agents = data.setdefault('agents', {})
    agent = agents.setdefault(agent_name, {
        'runs': 0, 'findings': 0, 'false_positives': 0,
        'unique_catches': 0, 'last_run': None
    })
    # All fields below are ACCUMULATIVE (incremented on each call)
    agent['runs'] += 1
    agent['findings'] += int(os.environ.get('FINDINGS', '0'))
    agent['false_positives'] += int(os.environ.get('FALSE_POS', '0'))
    agent['unique_catches'] += int(os.environ.get('UNIQUE', '0'))
    agent['last_run'] = now

elif metric_type == 'plan-accuracy':
    plan_slug = os.environ['PLAN_SLUG']
    plans = data.setdefault('plans', {})
    plan = plans.setdefault(plan_slug, {
        'tasks_planned': 0, 'tasks_completed': 0,
        'drift_entries': 0, 'surprise_entries': 0, 'completion_date': None
    })
    # tasks_planned / tasks_completed are IDEMPOTENT (SET when non-zero, not incremented)
    tasks_planned = int(os.environ.get('TASKS_PLANNED', '0'))
    tasks_completed = int(os.environ.get('TASKS_COMPLETED', '0'))
    if tasks_planned > 0:
        plan['tasks_planned'] = tasks_planned
    if tasks_completed > 0:
        plan['tasks_completed'] = tasks_completed
    # drift_entries / surprise_entries are ACCUMULATIVE (incremented on each call)
    plan['drift_entries'] += int(os.environ.get('DRIFT', '0'))
    plan['surprise_entries'] += int(os.environ.get('SURPRISES', '0'))

elif metric_type == 'learning-efficacy':
    # Prefer dedicated --learning-id; fall back to --plan-slug for backward compat
    learning_id = os.environ.get('LEARNING_ID') or os.environ.get('PLAN_SLUG', '')
    # Prefer dedicated --recurrence/--prevented; fall back to --drift/--surprises for backward compat
    recurrence_raw = os.environ.get('RECURRENCE') or os.environ.get('DRIFT', '0')
    prevented_raw = os.environ.get('PREVENTED') or os.environ.get('SURPRISES', '0')
    learnings = data.setdefault('learnings', {})
    learning = learnings.setdefault(learning_id, {
        'recurrence_count': 0, 'prevented_count': 0, 'scope': 'repo', 'category': ''
    })
    # Both fields are ACCUMULATIVE (incremented on each call)
    learning['recurrence_count'] += int(recurrence_raw)
    learning['prevented_count'] += int(prevented_raw)

data['last_updated'] = now

# Atomic write: write to temp file then rename to avoid truncation on failure
tmp_file = metrics_file + '.tmp'
try:
    with open(tmp_file, 'w') as f:
        json.dump(data, f, indent=2)
    os.rename(tmp_file, metrics_file)
except OSError as e:
    print(f"Error: failed to write {metrics_file}: {e}", file=sys.stderr)
    # Clean up temp file if it exists
    try:
        os.unlink(tmp_file)
    except OSError:
        pass
    sys.exit(1)
PYEOF

echo "Updated $METRIC"
