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
  python3 -c "
import json, sys
with open('$STATE_FILE') as f:
    state = json.load(f)
state['phase'] = '$PHASE'
state['last_updated'] = '$NOW'
if '$PLAN':
    state['plan'] = '$PLAN'
if '$DESIGN_DOC':
    state['design_doc'] = '$DESIGN_DOC'
if '$BRANCH':
    state['branch'] = '$BRANCH'
completed_names = [p['name'] for p in state.get('completed_phases', [])]
if '$PHASE' not in completed_names:
    state.setdefault('completed_phases', []).append({'name': '$PHASE', 'completed_at': '$NOW'})
with open('$STATE_FILE', 'w') as f:
    json.dump(state, f, indent=2)
"
else
  BRANCH_VAL="${BRANCH:-$(git branch --show-current 2>/dev/null || echo "")}"
  python3 -c "
import json
state = {
    'schema_version': 1,
    'plan': '$PLAN',
    'design_doc': '$DESIGN_DOC',
    'branch': '$BRANCH_VAL',
    'phase': '$PHASE',
    'completed_phases': [{'name': '$PHASE', 'completed_at': '$NOW'}],
    'started_at': '$NOW',
    'last_updated': '$NOW'
}
with open('$STATE_FILE', 'w') as f:
    json.dump(state, f, indent=2)
"
fi

echo "Updated run-state: phase=$PHASE"
