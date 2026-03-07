#!/usr/bin/env bash
set -uo pipefail

# Lead execution loop for belayer.
# Runs in a git worktree, executes goals via claude -p, and emits structured
# JSON events to stdout for the Go runner to parse.
#
# Environment variables:
#   LEAD_DIR         - Path to .lead/ directory (default: .lead)
#   MAX_ATTEMPTS     - Max retry attempts per goal (default: 3)
#   EXECUTE_MODEL    - Claude model for implementation (default: claude-sonnet-4-6)
#   REVIEW_MODEL     - Claude model for review (default: claude-sonnet-4-6)

LEAD_DIR="${LEAD_DIR:-.lead}"
MAX_ATTEMPTS="${MAX_ATTEMPTS:-3}"
EXECUTE_MODEL="${EXECUTE_MODEL:-claude-sonnet-4-6}"
REVIEW_MODEL="${REVIEW_MODEL:-claude-sonnet-4-6}"
OUTPUT_DIR="${LEAD_DIR}/output"

# Emit a structured JSON event to stdout.
# Usage: emit "type" '{"key":"value"}'
emit() {
    local event_type="$1"
    local extra="${2:-}"
    if [ -n "$extra" ]; then
        echo "{\"type\":\"${event_type}\",${extra}}"
    else
        echo "{\"type\":\"${event_type}\"}"
    fi
}

# Check required files exist
if [ ! -f "${LEAD_DIR}/spec.md" ]; then
    emit "error" "\"error\":\"spec.md not found in ${LEAD_DIR}\""
    exit 1
fi

if [ ! -f "${LEAD_DIR}/goals.json" ]; then
    emit "error" "\"error\":\"goals.json not found in ${LEAD_DIR}\""
    exit 1
fi

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Read goals count and descriptions using python3 (available on macOS/Linux)
GOALS_COUNT=$(python3 -c "import json; goals=json.load(open('${LEAD_DIR}/goals.json')); print(len(goals))" 2>/dev/null)
if [ -z "$GOALS_COUNT" ] || [ "$GOALS_COUNT" -eq 0 ]; then
    emit "error" "\"error\":\"no goals found in goals.json\""
    exit 1
fi

SPEC=$(cat "${LEAD_DIR}/spec.md")

emit "started"

# Track overall success
ALL_PASSED=true

# Process each goal
for goal_idx in $(seq 0 $((GOALS_COUNT - 1))); do
    GOAL_DESC=$(python3 -c "import json; goals=json.load(open('${LEAD_DIR}/goals.json')); print(goals[${goal_idx}]['description'])" 2>/dev/null)
    emit "goal_started" "\"goal\":${goal_idx},\"attempt\":1,\"description\":$(python3 -c "import json; print(json.dumps('${GOAL_DESC}'))" 2>/dev/null || echo "\"${GOAL_DESC}\"")"

    GOAL_PASSED=false
    PREVIOUS_FEEDBACK=""

    for attempt in $(seq 1 "$MAX_ATTEMPTS"); do
        emit "goal_executing" "\"goal\":${goal_idx},\"attempt\":${attempt}"

        # Build execution prompt
        EXEC_PROMPT="You are working in $(pwd). Implement the following goal:

Goal: ${GOAL_DESC}

Full spec for context:
${SPEC}"

        if [ -n "$PREVIOUS_FEEDBACK" ]; then
            EXEC_PROMPT="${EXEC_PROMPT}

Previous review feedback (fix these issues):
${PREVIOUS_FEEDBACK}"
        fi

        # Execute implementation
        if claude -p --model "$EXECUTE_MODEL" "$EXEC_PROMPT" > "${OUTPUT_DIR}/goal-${goal_idx}-attempt-${attempt}-execute.txt" 2>&1; then
            : # success
        else
            emit "goal_verdict" "\"goal\":${goal_idx},\"attempt\":${attempt},\"pass\":false,\"summary\":\"claude execution failed with exit code $?\""
            continue
        fi

        emit "goal_reviewing" "\"goal\":${goal_idx},\"attempt\":${attempt}"

        # Remove previous verdict
        rm -f "${LEAD_DIR}/verdict.json"

        # Build review prompt
        REVIEW_PROMPT="Review the implementation in $(pwd) against this goal:

Goal: ${GOAL_DESC}

Full spec for context:
${SPEC}

Evaluate whether the goal has been correctly implemented. Write your verdict as JSON to the file ${LEAD_DIR}/verdict.json with exactly this format:
{
  \"pass\": true or false,
  \"summary\": \"brief description of the review result\",
  \"issues\": [\"issue 1\", \"issue 2\"]
}

The 'pass' field must be true only if the goal is fully and correctly implemented."

        # Execute review
        claude -p --model "$REVIEW_MODEL" "$REVIEW_PROMPT" > "${OUTPUT_DIR}/goal-${goal_idx}-attempt-${attempt}-review.txt" 2>&1 || true

        # Parse verdict
        if [ -f "${LEAD_DIR}/verdict.json" ]; then
            PASS=$(python3 -c "import json; v=json.load(open('${LEAD_DIR}/verdict.json')); print(str(v.get('pass', False)).lower())" 2>/dev/null || echo "false")
            SUMMARY=$(python3 -c "import json; v=json.load(open('${LEAD_DIR}/verdict.json')); print(v.get('summary', 'no summary'))" 2>/dev/null || echo "no summary")
            ISSUES=$(python3 -c "import json; v=json.load(open('${LEAD_DIR}/verdict.json')); print('; '.join(v.get('issues', [])))" 2>/dev/null || echo "")

            emit "goal_verdict" "\"goal\":${goal_idx},\"attempt\":${attempt},\"pass\":${PASS},\"summary\":$(python3 -c "import json; print(json.dumps('${SUMMARY}'))" 2>/dev/null || echo "\"${SUMMARY}\"")"

            if [ "$PASS" = "true" ]; then
                GOAL_PASSED=true
                emit "goal_complete" "\"goal\":${goal_idx}"
                break
            else
                PREVIOUS_FEEDBACK="${SUMMARY}"
                if [ -n "$ISSUES" ]; then
                    PREVIOUS_FEEDBACK="${PREVIOUS_FEEDBACK}. Issues: ${ISSUES}"
                fi
            fi
        else
            emit "goal_verdict" "\"goal\":${goal_idx},\"attempt\":${attempt},\"pass\":false,\"summary\":\"no verdict.json produced by review\""
        fi
    done

    if [ "$GOAL_PASSED" = false ]; then
        ALL_PASSED=false
        emit "goal_stuck" "\"goal\":${goal_idx},\"attempts\":${MAX_ATTEMPTS}"
    fi
done

if [ "$ALL_PASSED" = true ]; then
    emit "complete"
    exit 0
else
    emit "stuck"
    exit 1
fi
