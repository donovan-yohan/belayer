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

[ -z "$HARNESS_DIR" ] && { echo "Error: --harness-dir required" >&2; exit 1; }
[ -z "$PHASE" ] && { echo "Error: --phase required" >&2; exit 1; }

# Validate phase against known values
VALID_PHASES="brainstorm plan orchestrate review reflect complete evolve"
PHASE_VALID=false
for p in $VALID_PHASES; do
  [ "$PHASE" = "$p" ] && PHASE_VALID=true && break
done
if [ "$PHASE_VALID" != "true" ]; then
  echo "Error: --phase '$PHASE' is not valid. Known phases: $VALID_PHASES" >&2
  exit 1
fi

STATE_FILE="$HARNESS_DIR/run-state.json"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
BRANCH_VAL="${BRANCH:-$(git branch --show-current 2>/dev/null || echo "")}"

STATE_FILE="$STATE_FILE" PHASE="$PHASE" NOW="$NOW" PLAN="$PLAN" DESIGN_DOC="$DESIGN_DOC" BRANCH_VAL="$BRANCH_VAL" \
python3 - <<'PYEOF'
import json, os, os.path, sys

state_file = os.environ['STATE_FILE']
phase = os.environ['PHASE']
now = os.environ['NOW']
plan = os.environ.get('PLAN', '')
design_doc = os.environ.get('DESIGN_DOC', '')
branch = os.environ.get('BRANCH_VAL', '')

if os.path.isfile(state_file):
    try:
        with open(state_file) as f:
            state = json.load(f)
    except json.JSONDecodeError as e:
        print(f"Error: failed to parse {state_file}: {e}", file=sys.stderr)
        sys.exit(1)
    state['phase'] = phase
    state['last_updated'] = now
    if plan:
        state['plan'] = plan
    if design_doc:
        state['design_doc'] = design_doc
    if branch:
        state['branch'] = branch
    # On re-run of the same phase: update the existing entry's timestamp instead of deduplicating silently
    completed_phases = state.setdefault('completed_phases', [])
    existing = next((p for p in completed_phases if p['name'] == phase), None)
    if existing:
        existing['completed_at'] = now
    else:
        completed_phases.append({'name': phase, 'completed_at': now})
else:
    state = {
        'schema_version': 1,
        'plan': plan,
        'design_doc': design_doc,
        'branch': branch,
        'phase': phase,
        'completed_phases': [{'name': phase, 'completed_at': now}],
        'started_at': now,
        'last_updated': now,
    }

# Atomic write: write to temp file then rename to avoid truncation on failure
tmp_file = state_file + '.tmp'
try:
    with open(tmp_file, 'w') as f:
        json.dump(state, f, indent=2)
    os.rename(tmp_file, state_file)
except OSError as e:
    print(f"Error: failed to write {state_file}: {e}", file=sys.stderr)
    try:
        os.unlink(tmp_file)
    except OSError:
        pass
    sys.exit(1)
PYEOF

echo "Updated run-state: phase=$PHASE"
