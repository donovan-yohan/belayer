#!/usr/bin/env bash
# Test suite for harness persistence scripts.
# Run from repo root: bash plugins/harness/scripts/test-harness-scripts.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEST_DIR=$(mktemp -d)
PASS=0
FAIL=0

cleanup() { rm -rf "$TEST_DIR"; }
trap cleanup EXIT

assert_eq() {
  local desc="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    echo "  PASS: $desc"
    PASS=$((PASS+1))
  else
    echo "  FAIL: $desc"
    echo "    expected: $expected"
    echo "    actual:   $actual"
    FAIL=$((FAIL+1))
  fi
}

assert_file_exists() {
  local desc="$1" path="$2"
  if [ -f "$path" ]; then
    echo "  PASS: $desc"
    PASS=$((PASS+1))
  else
    echo "  FAIL: $desc — file not found: $path"
    FAIL=$((FAIL+1))
  fi
}

assert_dir_exists() {
  local desc="$1" path="$2"
  if [ -d "$path" ]; then
    echo "  PASS: $desc"
    PASS=$((PASS+1))
  else
    echo "  FAIL: $desc — dir not found: $path"
    FAIL=$((FAIL+1))
  fi
}

assert_contains() {
  local desc="$1" file="$2" pattern="$3"
  if grep -q "$pattern" "$file" 2>/dev/null; then
    echo "  PASS: $desc"
    PASS=$((PASS+1))
  else
    echo "  FAIL: $desc — pattern '$pattern' not found in $file"
    FAIL=$((FAIL+1))
  fi
}

run_script() {
  local desc="$1"; shift
  if "$@" >/dev/null 2>&1; then
    return 0
  else
    echo "  FAIL (setup): $desc — script exited non-zero"
    FAIL=$((FAIL+1))
    return 1
  fi
}

# ─── Test: harness-resolve-dir.sh ───
echo "Testing harness-resolve-dir.sh"

RESOLVE_DIR="$TEST_DIR/resolve-test"
mkdir -p "$RESOLVE_DIR"
cd "$RESOLVE_DIR" && git init -q && cd - >/dev/null

# No .harness/ → empty output
RESULT=$("$SCRIPT_DIR/harness-resolve-dir.sh" --repo-root "$RESOLVE_DIR")
assert_eq "no .harness/ returns empty" "" "$RESULT"

# With .harness/ → returns path
mkdir -p "$RESOLVE_DIR/.harness"
RESULT=$("$SCRIPT_DIR/harness-resolve-dir.sh" --repo-root "$RESOLVE_DIR")
assert_eq "with .harness/ returns path" "$RESOLVE_DIR/.harness" "$RESULT"

# Item 18: global storage fallback tier (~/.harness/repo-slug/)
# The slug is derived from the basename of the git repo root.
# Name the temp repo dir to get a predictable slug.
GLOBAL_SLUG="harness-test-global-$$"
GLOBAL_RESOLVE_DIR="$TEST_DIR/$GLOBAL_SLUG"
GLOBAL_HARNESS_DIR="$HOME/.harness/$GLOBAL_SLUG"
mkdir -p "$GLOBAL_RESOLVE_DIR"
(cd "$GLOBAL_RESOLVE_DIR" && git init -q)
mkdir -p "$GLOBAL_HARNESS_DIR"
RESULT=$("$SCRIPT_DIR/harness-resolve-dir.sh" --repo-root "$GLOBAL_RESOLVE_DIR")
assert_eq "global storage fallback returns ~/.harness/<slug>" "$GLOBAL_HARNESS_DIR" "$RESULT"
rm -rf "$GLOBAL_HARNESS_DIR"

# ─── Test: harness-init-runtime.sh ───
echo "Testing harness-init-runtime.sh"

INIT_DIR="$TEST_DIR/init-test/.harness"
run_script "init harness dir" "$SCRIPT_DIR/harness-init-runtime.sh" --harness-dir "$INIT_DIR" --repo-name "test/repo" || { echo "Skipping remaining init tests"; }

assert_dir_exists "creates agents/" "$INIT_DIR/agents"
assert_dir_exists "creates metrics/" "$INIT_DIR/metrics"
assert_dir_exists "creates memory/" "$INIT_DIR/memory"
assert_dir_exists "creates proposals/" "$INIT_DIR/proposals"
assert_dir_exists "creates runs/" "$INIT_DIR/runs"
assert_file_exists "creates manifest.yaml" "$INIT_DIR/manifest.yaml"
assert_file_exists "creates config.yaml" "$INIT_DIR/config.yaml"
assert_file_exists "creates .gitignore" "$INIT_DIR/.gitignore"
assert_file_exists "creates IMPROVEMENTS.md" "$INIT_DIR/memory/IMPROVEMENTS.md"
assert_file_exists "creates review-effectiveness.json" "$INIT_DIR/metrics/review-effectiveness.json"
assert_file_exists "creates plan-accuracy.json" "$INIT_DIR/metrics/plan-accuracy.json"
assert_file_exists "creates learning-efficacy.json" "$INIT_DIR/metrics/learning-efficacy.json"
assert_file_exists "creates phase-costs.json" "$INIT_DIR/metrics/phase-costs.json"
assert_contains "manifest has repo name" "$INIT_DIR/manifest.yaml" "test/repo"
assert_contains "config has evolve settings" "$INIT_DIR/config.yaml" "auto_apply: true"

# Item 19a: metrics files have type-specific keys
assert_contains "review-effectiveness has agents key" "$INIT_DIR/metrics/review-effectiveness.json" '"agents"'
assert_contains "plan-accuracy has plans key" "$INIT_DIR/metrics/plan-accuracy.json" '"plans"'
assert_contains "learning-efficacy has learnings key" "$INIT_DIR/metrics/learning-efficacy.json" '"learnings"'
assert_contains "phase-costs has phases key" "$INIT_DIR/metrics/phase-costs.json" '"phases"'

# Item 19b: idempotency guard — re-running without --force should exit 1
if "$SCRIPT_DIR/harness-init-runtime.sh" --harness-dir "$INIT_DIR" --repo-name "test/repo" 2>/dev/null; then
  echo "  FAIL: re-init without --force should exit 1"
  FAIL=$((FAIL+1))
else
  echo "  PASS: re-init without --force exits non-zero"
  PASS=$((PASS+1))
fi

# Item 19c: --force overwrites and creates a backup
ORIG_CREATED=$(grep "^created:" "$INIT_DIR/manifest.yaml")
run_script "re-init with --force" "$SCRIPT_DIR/harness-init-runtime.sh" --harness-dir "$INIT_DIR" --repo-name "test/repo" --force
BACKUP_COUNT=$(ls "$INIT_DIR"/manifest.yaml.bak.* 2>/dev/null | wc -l | tr -d ' ')
assert_eq "--force creates a manifest backup" "1" "$BACKUP_COUNT"
assert_file_exists "manifest still exists after --force" "$INIT_DIR/manifest.yaml"

# Item 19d: storage tier detection — repo tier for path not under ~/.harness/
assert_contains "repo tier set in manifest" "$INIT_DIR/manifest.yaml" 'storage_tier: "repo"'

# Item 19e: global tier — init path under ~/.harness/ sets storage_tier: global
GLOBAL_INIT_DIR="$HOME/.harness/harness-test-init-$$"
run_script "global tier init" "$SCRIPT_DIR/harness-init-runtime.sh" --harness-dir "$GLOBAL_INIT_DIR"
assert_contains "global tier set in manifest" "$GLOBAL_INIT_DIR/manifest.yaml" 'storage_tier: "global"'
rm -rf "$GLOBAL_INIT_DIR"

# ─── Test: harness-read-state.sh / harness-update-state.sh ───
echo "Testing harness-read-state.sh and harness-update-state.sh"

STATE_DIR="$TEST_DIR/state-test"
mkdir -p "$STATE_DIR"

# Read nonexistent → sentinel JSON
RESULT=$("$SCRIPT_DIR/harness-read-state.sh" --harness-dir "$STATE_DIR")
assert_eq "read nonexistent state returns sentinel" '{"phase": "none"}' "$RESULT"

# Create state
run_script "create state" "$SCRIPT_DIR/harness-update-state.sh" --harness-dir "$STATE_DIR" --phase "brainstorm" --plan "test-plan.md" --branch "test-branch"
assert_file_exists "creates run-state.json" "$STATE_DIR/run-state.json"
assert_contains "state has phase" "$STATE_DIR/run-state.json" '"phase": "brainstorm"'
assert_contains "state has plan" "$STATE_DIR/run-state.json" "test-plan.md"

# Update state
run_script "update state" "$SCRIPT_DIR/harness-update-state.sh" --harness-dir "$STATE_DIR" --phase "plan"
assert_contains "updated phase" "$STATE_DIR/run-state.json" '"phase": "plan"'

# Verify both phases in completed_phases
COMPLETED_COUNT=$(python3 -c "import json; d=json.load(open('$STATE_DIR/run-state.json')); print(len(d['completed_phases']))")
assert_eq "two completed phases" "2" "$COMPLETED_COUNT"

# ─── Test: harness-write-metrics.sh ───
echo "Testing harness-write-metrics.sh"

# Use the init-test .harness dir which has metrics files
run_script "write review metrics" "$SCRIPT_DIR/harness-write-metrics.sh" --harness-dir "$INIT_DIR" --metric "review-effectiveness" --agent "code-reviewer" --findings 3 --false-pos 1 --unique 2
assert_contains "metrics updated" "$INIT_DIR/metrics/review-effectiveness.json" '"code-reviewer"'

RUNS=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/review-effectiveness.json')); print(d['agents']['code-reviewer']['runs'])")
assert_eq "agent runs incremented" "1" "$RUNS"

# plan-accuracy metric path
run_script "write plan-accuracy metrics" "$SCRIPT_DIR/harness-write-metrics.sh" --harness-dir "$INIT_DIR" --metric "plan-accuracy" --plan-slug "test-plan" --tasks-planned 10 --tasks-completed 7 --drift 2 --surprises 1
assert_contains "plan entry created" "$INIT_DIR/metrics/plan-accuracy.json" '"test-plan"'
PLANNED=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/plan-accuracy.json')); print(d['plans']['test-plan']['tasks_planned'])")
assert_eq "tasks_planned recorded" "10" "$PLANNED"
DRIFT=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/plan-accuracy.json')); print(d['plans']['test-plan']['drift_entries'])")
assert_eq "drift recorded" "2" "$DRIFT"

# plan-accuracy validation: missing --plan-slug should fail
if "$SCRIPT_DIR/harness-write-metrics.sh" --harness-dir "$INIT_DIR" --metric "plan-accuracy" 2>/dev/null; then
  echo "  FAIL: plan-accuracy without --plan-slug should exit non-zero"
  FAIL=$((FAIL+1))
else
  echo "  PASS: plan-accuracy without --plan-slug rejected"
  PASS=$((PASS+1))
fi

# Item 16: metrics accumulation — call review-effectiveness twice, verify counters accumulate
run_script "write review metrics second call" "$SCRIPT_DIR/harness-write-metrics.sh" --harness-dir "$INIT_DIR" --metric "review-effectiveness" --agent "code-reviewer" --findings 5 --false-pos 2 --unique 3
RUNS2=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/review-effectiveness.json')); print(d['agents']['code-reviewer']['runs'])")
assert_eq "runs accumulates to 2 after two calls" "2" "$RUNS2"
FINDINGS2=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/review-effectiveness.json')); print(d['agents']['code-reviewer']['findings'])")
assert_eq "findings accumulates (3+5=8)" "8" "$FINDINGS2"
FP2=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/review-effectiveness.json')); print(d['agents']['code-reviewer']['false_positives'])")
assert_eq "false_positives accumulates (1+2=3)" "3" "$FP2"
UNIQUE2=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/review-effectiveness.json')); print(d['agents']['code-reviewer']['unique_catches'])")
assert_eq "unique_catches accumulates (2+3=5)" "5" "$UNIQUE2"

# ─── Test: harness-write-proposal.sh ───
echo "Testing harness-write-proposal.sh"

CURRENT_TMP=$(mktemp)
echo "Check for null pointer errors" > "$CURRENT_TMP"
PROPOSED_TMP=$(mktemp)
echo "Check for null pointer AND connection pool exhaustion errors" > "$PROPOSED_TMP"
REASONING_TMP=$(mktemp)
echo "Connection pool exhaustion escaped review in the March 28 session" > "$REASONING_TMP"

PROPOSAL=$("$SCRIPT_DIR/harness-write-proposal.sh" \
  --harness-dir "$INIT_DIR" \
  --slug "add-pool-check" \
  --scope "universal" \
  --signal "escape-20260328" \
  --agent "code-reviewer" \
  --current-file "$CURRENT_TMP" \
  --proposed-file "$PROPOSED_TMP" \
  --reasoning-file "$REASONING_TMP")

assert_file_exists "proposal file created" "$PROPOSAL"
assert_contains "proposal has scope" "$PROPOSAL" "universal"
assert_contains "proposal has signal" "$PROPOSAL" "escape-20260328"
rm -f "$CURRENT_TMP" "$PROPOSED_TMP" "$REASONING_TMP"

# Item 17c: write-proposal with missing content file paths → uses fallback text
PROPOSAL_MISSING=$("$SCRIPT_DIR/harness-write-proposal.sh" \
  --harness-dir "$INIT_DIR" \
  --slug "missing-files-test" \
  --scope "repo" \
  --signal "test-signal" \
  --agent "test-agent")
assert_file_exists "proposal with missing files created" "$PROPOSAL_MISSING"
assert_contains "missing current-file shows fallback" "$PROPOSAL_MISSING" "(no current text provided)"
assert_contains "missing proposed-file shows fallback" "$PROPOSAL_MISSING" "(no proposed text provided)"
assert_contains "missing reasoning-file shows fallback" "$PROPOSAL_MISSING" "(no reasoning provided)"

# Item 17a: learning-efficacy with dedicated --learning-id, --recurrence, --prevented flags
run_script "write learning-efficacy with dedicated flags" "$SCRIPT_DIR/harness-write-metrics.sh" \
  --harness-dir "$INIT_DIR" --metric "learning-efficacy" \
  --learning-id "lesson-null-checks" --recurrence 3 --prevented 2
RECURRENCE=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/learning-efficacy.json')); print(d['learnings']['lesson-null-checks']['recurrence_count'])")
assert_eq "learning-efficacy recurrence_count set" "3" "$RECURRENCE"
PREVENTED=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/learning-efficacy.json')); print(d['learnings']['lesson-null-checks']['prevented_count'])")
assert_eq "learning-efficacy prevented_count set" "2" "$PREVENTED"

# Also verify backward compat via --plan-slug/--drift/--surprises
run_script "write learning-efficacy backward compat" "$SCRIPT_DIR/harness-write-metrics.sh" \
  --harness-dir "$INIT_DIR" --metric "learning-efficacy" \
  --plan-slug "lesson-old-compat" --drift 1 --surprises 4
COMPAT_RECUR=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/learning-efficacy.json')); print(d['learnings']['lesson-old-compat']['recurrence_count'])")
assert_eq "learning-efficacy backward compat recurrence" "1" "$COMPAT_RECUR"

# ─── Test: harness-write-run.sh ───
echo "Testing harness-write-run.sh"

RUN_FILE=$("$SCRIPT_DIR/harness-write-run.sh" --harness-dir "$INIT_DIR" --phase "review" --branch "test-branch")
assert_file_exists "run file created" "$RUN_FILE"
assert_contains "run has phase" "$RUN_FILE" '"phase": "review"'

# Item 17b: write-run with --data-file parameter
DATA_TMP=$(mktemp)
echo '{"score": 95, "notes": "all green"}' > "$DATA_TMP"
RUN_FILE_DATA=$("$SCRIPT_DIR/harness-write-run.sh" --harness-dir "$INIT_DIR" --phase "review" --branch "test-branch" --data-file "$DATA_TMP")
assert_file_exists "run file with data-file created" "$RUN_FILE_DATA"
assert_contains "run data-file score preserved" "$RUN_FILE_DATA" '"score": 95'
rm -f "$DATA_TMP"

# Item 17b: write-run with missing --data-file should exit non-zero
if "$SCRIPT_DIR/harness-write-run.sh" --harness-dir "$INIT_DIR" --phase "review" --data-file "/nonexistent/data.json" 2>/dev/null; then
  echo "  FAIL: write-run with missing --data-file should exit non-zero"
  FAIL=$((FAIL+1))
else
  echo "  PASS: write-run with missing --data-file rejected"
  PASS=$((PASS+1))
fi

# ─── Summary ───
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
