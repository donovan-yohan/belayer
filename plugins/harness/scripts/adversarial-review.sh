#!/usr/bin/env bash
# adversarial-review.sh — Context-isolated adversarial production review
#
# Wraps `claude -p` as a completely separate process with no conversation
# context, no plugins, no hooks. The reviewer sees only the diff and a targeted
# adversarial prompt. Output is structured JSON validated against a schema.
#
# Usage:
#   adversarial-review.sh --prompt-file <path> --diff-file <path> [options]
#   cat diff.patch | adversarial-review.sh --prompt-file <path> [options]
#
# Exit codes:
#   0 — PASS (no production failure modes found)
#   1 — FAIL with CRITICAL findings (must block)
#   2 — FAIL without CRITICAL findings (HIGH/MEDIUM only)
#   3 — Inconclusive (could not parse output or schema validation failed)
#   4 — Error (claude binary not found, timeout, empty diff, etc.)
#
# Output: JSON to stdout with verdict, findings, and summary.

set -euo pipefail

# ─── Defaults ────────────────────────────────────────────────────────────────

PROMPT_FILE=""
DIFF_FILE=""
MODEL="sonnet"
EFFORT="max"
MAX_TURNS=3
MAX_BUDGET="1.00"
TIMEOUT=300  # seconds
VERBOSE=false

# ─── Parse arguments ─────────────────────────────────────────────────────────

usage() {
  cat <<'USAGE'
Usage: adversarial-review.sh --prompt-file <path> [--diff-file <path>] [options]

Required:
  --prompt-file <path>    System prompt file for the adversarial reviewer

Input (one of):
  --diff-file <path>      Diff file to review (or pipe via stdin)

Options:
  --model <name>          Model alias (default: sonnet)
  --effort <level>        Effort level: low|medium|high|max (default: max)
  --max-turns <n>         Max agentic turns (default: 3)
  --max-budget <usd>      Max budget in USD (default: 1.00)
  --timeout <seconds>     Timeout in seconds (default: 300)
  --verbose               Show raw claude output on stderr
  -h, --help              Show this help
USAGE
  exit 4
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prompt-file)  PROMPT_FILE="$2"; shift 2 ;;
    --diff-file)    DIFF_FILE="$2"; shift 2 ;;
    --model)        MODEL="$2"; shift 2 ;;
    --effort)       EFFORT="$2"; shift 2 ;;
    --max-turns)    MAX_TURNS="$2"; shift 2 ;;
    --max-budget)   MAX_BUDGET="$2"; shift 2 ;;
    --timeout)      TIMEOUT="$2"; shift 2 ;;
    --verbose)      VERBOSE=true; shift ;;
    -h|--help)      usage ;;
    *)              echo "Unknown option: $1" >&2; usage ;;
  esac
done

# ─── Validate inputs ─────────────────────────────────────────────────────────

if [[ -z "$PROMPT_FILE" ]]; then
  echo '{"error":"--prompt-file is required","exit_code":4}' >&2
  exit 4
fi

if [[ ! -f "$PROMPT_FILE" ]]; then
  echo "{\"error\":\"prompt file not found: $PROMPT_FILE\",\"exit_code\":4}" >&2
  exit 4
fi

# Check for claude binary
CLAUDE_BIN=$(which claude 2>/dev/null || echo "")
if [[ -z "$CLAUDE_BIN" ]]; then
  echo '{"error":"claude binary not found in PATH","exit_code":4}' >&2
  exit 4
fi

# Handle diff input: file arg or stdin
DIFF_TEMP=""
if [[ -n "$DIFF_FILE" ]]; then
  if [[ ! -f "$DIFF_FILE" ]]; then
    echo "{\"error\":\"diff file not found: $DIFF_FILE\",\"exit_code\":4}" >&2
    exit 4
  fi
elif [[ ! -t 0 ]]; then
  # stdin is piped
  DIFF_TEMP=$(mktemp /tmp/harness-adv-diff-XXXXXX.patch)
  cat > "$DIFF_TEMP"
  DIFF_FILE="$DIFF_TEMP"
else
  echo '{"error":"no diff provided — use --diff-file or pipe via stdin","exit_code":4}' >&2
  exit 4
fi

# Check diff is non-empty
if [[ ! -s "$DIFF_FILE" ]]; then
  echo '{"verdict":"SKIP","reason":"empty diff","findings":[],"summary":"No changes to review."}'
  [[ -n "$DIFF_TEMP" ]] && rm -f "$DIFF_TEMP"
  exit 4
fi

# ─── JSON Schema for structured output ────────────────────────────────────────

# The schema enforces the verdict/findings structure so we don't need to parse
# free-text output. Claude validates conformance at the API level.
SCHEMA_FILE=$(mktemp /tmp/harness-adv-schema-XXXXXX.json)
cat > "$SCHEMA_FILE" <<'SCHEMA'
{
  "type": "object",
  "required": ["verdict", "findings", "summary"],
  "additionalProperties": false,
  "properties": {
    "verdict": {
      "type": "string",
      "enum": ["PASS", "FAIL"]
    },
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["severity", "title", "location", "scenario", "impact", "fix"],
        "additionalProperties": false,
        "properties": {
          "severity": {
            "type": "string",
            "enum": ["CRITICAL", "HIGH", "MEDIUM", "LOW"]
          },
          "title": {
            "type": "string",
            "description": "Short title for the finding"
          },
          "location": {
            "type": "string",
            "description": "Function name or file context from the diff"
          },
          "scenario": {
            "type": "string",
            "description": "What triggers this failure"
          },
          "impact": {
            "type": "string",
            "description": "What happens in production"
          },
          "fix": {
            "type": "string",
            "description": "Concrete code change recommendation"
          }
        }
      }
    },
    "summary": {
      "type": "string",
      "description": "One-paragraph summary of the review"
    }
  }
}
SCHEMA

# ─── Build the combined input ─────────────────────────────────────────────────

# Combine the diff into the stdin so the system prompt stays clean
COMBINED_INPUT=$(mktemp /tmp/harness-adv-input-XXXXXX.md)
cat > "$COMBINED_INPUT" <<HEREDOC
Review the following diff. Respond with structured JSON per the schema.

\`\`\`diff
$(cat "$DIFF_FILE")
\`\`\`
HEREDOC

# ─── Invoke claude as a separate process ──────────────────────────────────────

STDERR_FILE=$(mktemp /tmp/harness-adv-stderr-XXXXXX.txt)
RAW_OUTPUT=""
EXIT_CODE=0

# Strip CLAUDECODE env var to avoid nesting detection
# Use --no-session-persistence so nothing persists
# Use --json-schema for structured output validation
# Use --allowedTools "Read" for read-only (no writes)
if [[ "$VERBOSE" == "true" ]]; then
  echo "--- adversarial-review: invoking claude -p ---" >&2
  echo "    model: $MODEL | effort: $EFFORT | turns: $MAX_TURNS | budget: \$$MAX_BUDGET" >&2
fi

RAW_OUTPUT=$(
  timeout "${TIMEOUT}" env -u CLAUDECODE \
    "$CLAUDE_BIN" \
    -p "$(cat "$COMBINED_INPUT")" \
    --system-prompt-file "$PROMPT_FILE" \
    --output-format json \
    --json-schema "$(cat "$SCHEMA_FILE")" \
    --model "$MODEL" \
    --effort "$EFFORT" \
    --max-turns "$MAX_TURNS" \
    --max-budget-usd "$MAX_BUDGET" \
    --allowedTools "Read" \
    --no-session-persistence \
    2>"$STDERR_FILE"
) || EXIT_CODE=$?

# ─── Handle errors ────────────────────────────────────────────────────────────

if [[ $EXIT_CODE -eq 124 ]]; then
  # timeout(1) returns 124 on timeout
  echo "{\"verdict\":\"SKIP\",\"reason\":\"timeout after ${TIMEOUT}s\",\"findings\":[],\"summary\":\"Adversarial review timed out.\"}"
  rm -f "$SCHEMA_FILE" "$COMBINED_INPUT" "$STDERR_FILE" "$DIFF_TEMP" 2>/dev/null
  exit 4
fi

if [[ $EXIT_CODE -ne 0 ]]; then
  STDERR_CONTENT=$(cat "$STDERR_FILE" 2>/dev/null || echo "unknown error")
  # Escape for JSON
  STDERR_ESCAPED=$(echo "$STDERR_CONTENT" | head -5 | tr '\n' ' ' | sed 's/"/\\"/g')
  echo "{\"verdict\":\"SKIP\",\"reason\":\"claude exited with code $EXIT_CODE: $STDERR_ESCAPED\",\"findings\":[],\"summary\":\"Adversarial review failed.\"}"
  rm -f "$SCHEMA_FILE" "$COMBINED_INPUT" "$STDERR_FILE" "$DIFF_TEMP" 2>/dev/null
  exit 4
fi

if [[ -z "$RAW_OUTPUT" ]]; then
  echo '{"verdict":"SKIP","reason":"empty output from claude","findings":[],"summary":"No output received."}'
  rm -f "$SCHEMA_FILE" "$COMBINED_INPUT" "$STDERR_FILE" "$DIFF_TEMP" 2>/dev/null
  exit 3
fi

# ─── Extract structured output from claude JSON envelope ──────────────────────

# claude --output-format json wraps the result in an envelope with .result
# The structured_output (from --json-schema) is in .result.structured_output
# or the text result may be in .result
STRUCTURED=$(echo "$RAW_OUTPUT" | jq -r '.result // empty' 2>/dev/null)

if [[ -z "$STRUCTURED" ]]; then
  # Try raw output as JSON directly
  STRUCTURED="$RAW_OUTPUT"
fi

# Validate it has the expected shape
VERDICT=$(echo "$STRUCTURED" | jq -r '.verdict // empty' 2>/dev/null)

if [[ -z "$VERDICT" ]]; then
  # Schema validation may have failed — output raw for debugging
  if [[ "$VERBOSE" == "true" ]]; then
    echo "--- raw claude output ---" >&2
    echo "$RAW_OUTPUT" >&2
    echo "--- end raw output ---" >&2
  fi
  echo "{\"verdict\":\"SKIP\",\"reason\":\"could not parse verdict from output\",\"findings\":[],\"summary\":\"Adversarial review produced unparseable output.\",\"raw_output\":$(echo "$RAW_OUTPUT" | jq -Rs .)}"
  rm -f "$SCHEMA_FILE" "$COMBINED_INPUT" "$STDERR_FILE" "$DIFF_TEMP" 2>/dev/null
  exit 3
fi

# ─── Output the structured result ─────────────────────────────────────────────

# Pass through the structured JSON
echo "$STRUCTURED" | jq .

# ─── Determine exit code from findings ────────────────────────────────────────

CRITICAL_COUNT=$(echo "$STRUCTURED" | jq '[.findings[] | select(.severity == "CRITICAL")] | length' 2>/dev/null || echo 0)
HIGH_COUNT=$(echo "$STRUCTURED" | jq '[.findings[] | select(.severity == "HIGH")] | length' 2>/dev/null || echo 0)

if [[ "$VERBOSE" == "true" ]]; then
  echo "--- verdict: $VERDICT | critical: $CRITICAL_COUNT | high: $HIGH_COUNT ---" >&2
  STDERR_CONTENT=$(cat "$STDERR_FILE" 2>/dev/null || echo "")
  if [[ -n "$STDERR_CONTENT" ]]; then
    echo "--- claude stderr ---" >&2
    echo "$STDERR_CONTENT" >&2
  fi
fi

# ─── Cleanup ──────────────────────────────────────────────────────────────────

rm -f "$SCHEMA_FILE" "$COMBINED_INPUT" "$STDERR_FILE" "$DIFF_TEMP" 2>/dev/null

# ─── Exit with semantic code ──────────────────────────────────────────────────

if [[ "$VERDICT" == "PASS" ]]; then
  exit 0
elif [[ "$CRITICAL_COUNT" -gt 0 ]]; then
  exit 1
else
  exit 2
fi
