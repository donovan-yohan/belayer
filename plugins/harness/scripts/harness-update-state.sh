#!/usr/bin/env bash
# Update run-state.json with phase completion.
# Creates the file if it doesn't exist.
# Usage: harness-update-state.sh --harness-dir <path> --phase <phase> [--plan <path>] [--design-doc <path>] [--branch <branch>]
set -euo pipefail

HARNESS_DIR=""
PHASE=""
PLAN=""
DESIGN_DOC=""
BRANCH=""

while [[ $# -gt 0 ]]; do
  case $1 in
    --harness-dir) HARNESS_DIR="$2"; shift 2 ;;
    --phase) PHASE="$2"; shift 2 ;;
    --plan) PLAN="$2"; shift 2 ;;
    --design-doc) DESIGN_DOC="$2"; shift 2 ;;
    --branch) BRANCH="$2"; shift 2 ;;
    *) echo "Usage: $0 --harness-dir <path> --phase <phase> [--plan <path>] [--design-doc <path>] [--branch <branch>]" >&2; exit 1 ;;
  esac
done

[ -z "$HARNESS_DIR" ] || [ -z "$PHASE" ] && { echo "Error: --harness-dir and --phase required" >&2; exit 1; }

STATE_FILE="$HARNESS_DIR/run-state.json"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

if [ -f "$STATE_FILE" ]; then
  STATE_FILE="$STATE_FILE" PHASE="$PHASE" NOW="$NOW" PLAN="$PLAN" DESIGN_DOC="$DESIGN_DOC" BRANCH="$BRANCH" \
  python3 - <<'PYEOF'
import json, os
state_file = os.environ['STATE_FILE']
phase = os.environ['PHASE']
now = os.environ['NOW']
plan = os.environ.get('PLAN', '')
design_doc = os.environ.get('DESIGN_DOC', '')
branch = os.environ.get('BRANCH', '')

with open(state_file) as f:
    state = json.load(f)
state['phase'] = phase
state['last_updated'] = now
if plan:
    state['plan'] = plan
if design_doc:
    state['design_doc'] = design_doc
if branch:
    state['branch'] = branch
completed_names = [p['name'] for p in state.get('completed_phases', [])]
if phase not in completed_names:
    state.setdefault('completed_phases', []).append({'name': phase, 'completed_at': now})
with open(state_file, 'w') as f:
    json.dump(state, f, indent=2)
PYEOF
else
  BRANCH_VAL="${BRANCH:-$(git branch --show-current 2>/dev/null || echo "")}"
  STATE_FILE="$STATE_FILE" PHASE="$PHASE" NOW="$NOW" PLAN="$PLAN" DESIGN_DOC="$DESIGN_DOC" BRANCH_VAL="$BRANCH_VAL" \
  python3 - <<'PYEOF'
import json, os
state = {
    'schema_version': 1,
    'plan': os.environ.get('PLAN', ''),
    'design_doc': os.environ.get('DESIGN_DOC', ''),
    'branch': os.environ.get('BRANCH_VAL', ''),
    'phase': os.environ['PHASE'],
    'completed_phases': [{'name': os.environ['PHASE'], 'completed_at': os.environ['NOW']}],
    'started_at': os.environ['NOW'],
    'last_updated': os.environ['NOW']
}
with open(os.environ['STATE_FILE'], 'w') as f:
    json.dump(state, f, indent=2)
PYEOF
fi

echo "Updated run-state: phase=$PHASE"
