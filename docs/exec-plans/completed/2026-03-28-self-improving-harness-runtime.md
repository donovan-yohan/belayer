# Self-Improving Harness Runtime (M1) Implementation Plan

> **Status**: Completed | **Created**: 2026-03-28 | **Completed**: 2026-03-28
> **Design Doc**: `docs/design-docs/2026-03-28-self-improving-harness-runtime-design.md`
> **Consulted Learnings**: L-20260325-mandate-skill-invocation, L-20260325-subagent-type-required, L-20260325-merge-friendly-formats, L-20260325-prune-as-migration
> **For Claude:** Use /harness:orchestrate to execute this plan.

**Goal:** Add a self-improving runtime (`.harness/`) to the harness plugin — agent definitions evolve based on metrics, learnings get scope classification, and a stateless command protocol enables context-isolated execution.

**Architecture:** All changes live in `plugins/harness/` (markdown commands, shell scripts, agent definitions). No Go code changes. Seven deterministic shell scripts handle file I/O; command definitions handle LLM judgment. `.harness/` is repo-local with `~/.harness/repo-slug/` global fallback.

**Tech Stack:** Bash (persistence scripts), Markdown (command/agent definitions), JSON (metrics/state), YAML (manifest/config), Python3 (JSON manipulation in scripts — available on macOS/Linux)

---

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-28 | Design | Approach B: Metrics-Driven Self-Improvement | Full runtime with data-driven evolution, not incremental or spec-first |
| 2026-03-28 | Design | Plugin-first, no Go code changes | Self-improvement lives in the harness plugin, belayer integration is future |
| 2026-03-28 | Design | Two storage tiers: `.harness/` + `~/.harness/slug/` | Lower adoption barrier, gradual opt-in |
| 2026-03-28 | Design | Stateless command protocol with run-state.json | Context isolation per Anthropic article's #1 finding |
| 2026-03-28 | Design | M1 scope: self-improving core only | M2 (test/document) ships after M1 is validated |
| 2026-03-28 | Design | Deprecate /harness:loop | Stateless command composition replaces single-session loop |
| 2026-03-28 | Planning | python3 for JSON manipulation in scripts | Available on macOS/Linux, avoids jq dependency |
| 2026-03-28 | Implementation | Batch execution: 7 parallel worktree agents | All 10 tasks have non-overlapping file targets; Tasks 7+8+10 grouped to share review.md/reflect.md/complete.md |

## Progress

- [x] Task 1: Reference Docs & Schemas _(completed 2026-03-28)_
- [x] Task 2: Persistence Scripts _(completed 2026-03-28)_
- [x] Task 3: Script Tests _(completed 2026-03-28)_
- [x] Task 4: Init Extension — .harness/ scaffold _(completed 2026-03-28)_
- [x] Task 5: Evolver Agent Definition _(completed 2026-03-28)_
- [x] Task 6: /harness:evolve Command _(completed 2026-03-28)_
- [x] Task 7: /harness:review Extension — Evaluator + Structured Output _(completed 2026-03-28)_
- [x] Task 8: /harness:reflect & /harness:complete Extensions _(completed 2026-03-28)_
- [x] Task 9: /harness:bug Extension — Retroactive Harness Trace _(completed 2026-03-28)_
- [x] Task 10: Run-State Protocol, Loop Deprecation & Plugin Registration _(completed 2026-03-28)_

## Surprises & Discoveries

| Date | Discovery | Impact | Action |
|------|-----------|--------|--------|
| 2026-03-28 | `((PASS++))` under bash `set -e` exits when counter is 0 | Test script crashes on first assertion | Changed to `PASS=$((PASS+1))` in test-harness-scripts.sh |
| 2026-03-28 | Plugin version bump to 5.0.0 requires Go test + codex skills sync | Go tests fail without `agentassets_test.go` update and `gencodexskills` re-run | Fixed version expectation and regenerated skills snapshot |

## Plan Drift

| Task | Plan Said | Actually Did | Why |
|------|-----------|--------------|-----|
| Task 3 | Use `((PASS++))` / `((FAIL++))` in assert helpers | Used `PASS=$((PASS+1))` / `FAIL=$((FAIL+1))` | Original pattern fails under `set -e` when counter is 0 (arithmetic expression evaluates to false) |

---

## File Structure

### New Files

| File | Responsibility |
|------|----------------|
| `plugins/harness/references/runtime-spec.md` | manifest.yaml, config.yaml, and all metrics JSON schemas |
| `plugins/harness/scripts/harness-resolve-dir.sh` | Resolve `.harness/` vs `~/.harness/slug/` storage tier |
| `plugins/harness/scripts/harness-init-runtime.sh` | Scaffold `.harness/` directory with defaults |
| `plugins/harness/scripts/harness-read-state.sh` | Read run-state.json to stdout |
| `plugins/harness/scripts/harness-update-state.sh` | Update run-state.json phase/completion |
| `plugins/harness/scripts/harness-write-metrics.sh` | Increment metric counters from run data |
| `plugins/harness/scripts/harness-write-proposal.sh` | Create proposal markdown file |
| `plugins/harness/scripts/harness-write-run.sh` | Write timestamped run record to runs/ |
| `plugins/harness/scripts/test-harness-scripts.sh` | Test suite for all persistence scripts |
| `plugins/harness/agents/harness-evolver.md` | Meta-agent for proposing agent modifications |
| `plugins/harness/commands/evolve.md` | Self-modification command definition |

### Modified Files

| File | Change Summary |
|------|----------------|
| `plugins/harness/references/learnings-format.md` | Add `scope: repo \| universal` field to entry format |
| `plugins/harness/commands/init.md` | Add Phase 2.5: `.harness/` scaffold |
| `plugins/harness/commands/review.md` | Add Phase 2.5: evaluator pass; add review-results.json output contract |
| `plugins/harness/commands/reflect.md` | Add Phase 5.8: evolve trigger |
| `plugins/harness/commands/complete.md` | Add Phase 5.7: evolution summary in PR |
| `plugins/harness/commands/bug.md` | Add Phase 4.7: retroactive harness trace |
| `plugins/harness/commands/loop.md` | Add deprecation notice |
| `plugins/harness/commands/brainstorm.md` | Add run-state.json write |
| `plugins/harness/commands/plan.md` | Add run-state.json write |
| `plugins/harness/commands/orchestrate.md` | Add run-state.json read/write |
| `plugins/harness/.claude-plugin/plugin.json` | Register evolve command + evolver agent |
| `plugins/harness/agents/harness-pruner.md` | Add `.harness/` audit checks |

---

## Phase A: Foundation (Tasks 1-3)

### Task 1: Reference Docs & Schemas

**Files:**
- Modify: `plugins/harness/references/learnings-format.md`
- Create: `plugins/harness/references/runtime-spec.md`

- [ ] **Step 1: Add `scope` field to learnings format**

In `plugins/harness/references/learnings-format.md`, add `scope` to the entry format template. After the `category` line, add:

```markdown
### L-YYYYMMDD-slug: {one-line summary}
- status: active
- category: {category}
- scope: {repo|universal}                    # NEW: classification for evolution
- source: {command} {date}
- branch: {branch}
```

Also add a new section after "Category Vocabulary":

```markdown
### Scope Vocabulary

| Value | Use for |
|-------|---------|
| `repo` | References specific file paths, module names, project-specific concepts, or domain-specific patterns |
| `universal` | Describes general patterns without project-specific references; actionable without project context |

Default to `repo` (conservative). Only promote to `universal` when ALL of:
- Contains zero project-specific references (file paths, module names, domain concepts)
- Matches a known general pattern category (error handling, testing strategy, review methodology, agent coordination, security, performance)
- The recommendation is actionable without project-specific context

When the `scope` field is absent (legacy entries), treat as `repo`.
```

- [ ] **Step 2: Create runtime-spec.md**

Create `plugins/harness/references/runtime-spec.md` containing the manifest.yaml spec, config.yaml spec, and all metrics JSON schemas. This is the single reference for the `.harness/` file formats.

```markdown
# Harness Runtime Specification

Reference spec for `.harness/` directory formats. Used by `commands/init.md`, `commands/evolve.md`, and persistence scripts.

---

## Directory Structure

```
.harness/                              # per-repo adaptive runtime (or ~/.harness/repo-slug/)
|-- manifest.yaml                      # repo identity, harness version, feature flags
|-- config.yaml                        # repo-specific tuning
|-- agents/                            # evolved agent definitions
|-- metrics/                           # quantitative self-assessment
|   |-- review-effectiveness.json
|   |-- plan-accuracy.json
|   |-- learning-efficacy.json
|   `-- phase-costs.json
|-- memory/                            # persistent memory across sessions
|   |-- IMPROVEMENTS.md                # audit trail of self-modifications
|   `-- session-history.json           # timestamped session summaries
|-- proposals/                         # pending agent evolution proposals
|-- handoffs/                          # context reset artifacts
|-- runs/                              # timestamped run records
|-- run-state.json                     # current lifecycle state (gitignored)
|-- review-results.json                # last review output (gitignored, ephemeral)
`-- .gitignore
```

### Storage Tier Resolution

1. Check `.harness/` in repo root (committed, team-shared)
2. If not found, check `~/.harness/{repo-slug}/` (personal, global)
3. If neither exists, use `plugins/harness/` defaults (static, no evolution)

---

## manifest.yaml

```yaml
protocol_version: "1.0.0"       # .harness/ protocol version
created: "YYYY-MM-DD"
repo: "owner/repo"
storage_tier: "repo"             # "repo" or "global"
features:
  evolve: true
  test: false                    # M2
  document: false                # M2
  contribute: false              # Future
```

---

## config.yaml

```yaml
agents:
  code-reviewer: true
  silent-failure-hunter: true
  pr-test-analyzer: true
  type-design-analyzer: true
  comment-analyzer: true
  learnings-reviewer: true

evolve:
  auto_apply: true
  min_runs_for_auto: 5
  contribute: false
  contribute_repo: "owner/repo"

review:
  max_cycles: 3
  adversarial: true
  evaluator: true
```

---

## Metrics Schemas

### review-effectiveness.json

```json
{
  "schema_version": 1,
  "agents": {
    "<agent-name>": {
      "runs": 0,
      "findings": 0,
      "false_positives": 0,
      "unique_catches": 0,
      "last_run": null,
      "disabled": false,
      "disable_reason": null
    }
  },
  "last_updated": null
}
```

### plan-accuracy.json

```json
{
  "schema_version": 1,
  "plans": {
    "<plan-slug>": {
      "tasks_planned": 0,
      "tasks_completed": 0,
      "drift_entries": 0,
      "surprise_entries": 0,
      "completion_date": null
    }
  },
  "aggregate": {
    "avg_drift_rate": 0,
    "avg_surprise_rate": 0,
    "avg_completion_rate": 0
  },
  "last_updated": null
}
```

### learning-efficacy.json

```json
{
  "schema_version": 1,
  "learnings": {
    "<learning-id>": {
      "created": "YYYY-MM-DD",
      "category": "review-escape",
      "scope": "universal",
      "recurrence_count": 0,
      "last_recurrence": null,
      "prevented_count": 0,
      "last_prevented": null
    }
  },
  "last_updated": null
}
```

### phase-costs.json

```json
{
  "schema_version": 1,
  "phases": {
    "plan": { "runs": 0, "avg_duration_s": 0, "avg_tokens": 0, "last_run": null },
    "orchestrate": { "runs": 0, "avg_duration_s": 0, "avg_tokens": 0, "last_run": null },
    "review": { "runs": 0, "avg_duration_s": 0, "avg_tokens": 0, "last_run": null },
    "reflect": { "runs": 0, "avg_duration_s": 0, "avg_tokens": 0, "last_run": null },
    "evolve": { "runs": 0, "avg_duration_s": 0, "avg_tokens": 0, "last_run": null },
    "complete": { "runs": 0, "avg_duration_s": 0, "avg_tokens": 0, "last_run": null }
  },
  "last_updated": null
}
```

### review-results.json (ephemeral, gitignored)

Written by `/harness:review`, consumed by `/harness:evolve`.

```json
{
  "schema_version": 1,
  "session_date": "YYYY-MM-DD",
  "branch": "branch-name",
  "agents": {
    "<agent-name>": {
      "ran": true,
      "findings": [
        {
          "text": "description",
          "severity": "medium",
          "file": "path/to/file.go",
          "line": 47,
          "accepted": true,
          "unique": true
        }
      ],
      "verdict": "PASS"
    }
  },
  "overall_verdict": "PASS",
  "cycles_run": 1
}
```

### run-state.json (ephemeral, gitignored)

```json
{
  "schema_version": 1,
  "plan": "docs/exec-plans/active/YYYY-MM-DD-slug.md",
  "design_doc": "docs/design-docs/YYYY-MM-DD-slug-design.md",
  "branch": "branch-name",
  "phase": "current-phase",
  "completed_phases": [
    { "name": "brainstorm", "completed_at": "ISO-8601" }
  ],
  "started_at": "ISO-8601",
  "last_updated": "ISO-8601"
}
```

---

## IMPROVEMENTS.md Format

Append-only audit trail in `.harness/memory/IMPROVEMENTS.md`:

```markdown
### YYYY-MM-DD: {one-line description}
- **Agent:** {agent modified}
- **Signal:** {escape ID / metric anomaly / learning ID}
- **Change:** {what was added/modified}
- **Scope:** {repo|universal}
- **Auto-applied:** {yes|no}
- **Rollback:** {none|rolled-back-YYYY-MM-DD}

{Reasoning for why this change should improve outcomes.}

---
```

---

## Proposal Format

Files in `.harness/proposals/{YYYY-MM-DD}-{slug}.md`:

```markdown
# Proposal: {slug}

- **Date:** YYYY-MM-DD
- **Signal:** {escape ID / metric / learning ID}
- **Agent:** {agent being modified}
- **Scope:** {repo|universal}
- **Status:** {pending|applied|rejected|rolled-back}

## Current

{Relevant section of current agent definition}

## Proposed

{Proposed change in diff format or full replacement}

## Reasoning

{Why this change should improve outcomes}
```
```

- [ ] **Step 3: Verify reference docs**

Check that learnings-format.md has the new `scope` section and runtime-spec.md exists:

```bash
grep -c "scope" plugins/harness/references/learnings-format.md
# Expected: multiple hits
ls plugins/harness/references/runtime-spec.md
# Expected: file exists
```

- [ ] **Step 4: Commit**

```bash
git add plugins/harness/references/learnings-format.md plugins/harness/references/runtime-spec.md
git commit -m "feat(harness): add scope field to learnings format and runtime spec reference"
```

---

### Task 2: Persistence Scripts

**Files:**
- Create: `plugins/harness/scripts/harness-resolve-dir.sh`
- Create: `plugins/harness/scripts/harness-init-runtime.sh`
- Create: `plugins/harness/scripts/harness-read-state.sh`
- Create: `plugins/harness/scripts/harness-update-state.sh`
- Create: `plugins/harness/scripts/harness-write-metrics.sh`
- Create: `plugins/harness/scripts/harness-write-proposal.sh`
- Create: `plugins/harness/scripts/harness-write-run.sh`

- [ ] **Step 1: Create harness-resolve-dir.sh**

```bash
#!/usr/bin/env bash
# Resolve .harness/ or ~/.harness/<repo-slug>/ storage tier.
# Outputs the resolved path to stdout, or empty string if neither exists.
# Usage: harness-resolve-dir.sh [--repo-root <path>]
set -euo pipefail

REPO_ROOT="."
while [[ $# -gt 0 ]]; do
  case $1 in
    --repo-root) REPO_ROOT="$2"; shift 2 ;;
    *) echo "Usage: $0 [--repo-root <path>]" >&2; exit 1 ;;
  esac
done

REPO_SLUG=$(basename "$(cd "$REPO_ROOT" && git rev-parse --show-toplevel 2>/dev/null || echo "$REPO_ROOT")")

# Tier 1: repo-local
if [ -d "$REPO_ROOT/.harness" ]; then
  echo "$REPO_ROOT/.harness"
  exit 0
fi

# Tier 2: global
GLOBAL_DIR="$HOME/.harness/$REPO_SLUG"
if [ -d "$GLOBAL_DIR" ]; then
  echo "$GLOBAL_DIR"
  exit 0
fi

# Neither exists — output empty
echo ""
```

Make executable: `chmod +x plugins/harness/scripts/harness-resolve-dir.sh`

- [ ] **Step 2: Create harness-init-runtime.sh**

```bash
#!/usr/bin/env bash
# Scaffold .harness/ directory with empty defaults.
# Usage: harness-init-runtime.sh --harness-dir <path> [--repo-name <owner/repo>]
set -euo pipefail

HARNESS_DIR=""
REPO_NAME=""

while [[ $# -gt 0 ]]; do
  case $1 in
    --harness-dir) HARNESS_DIR="$2"; shift 2 ;;
    --repo-name) REPO_NAME="$2"; shift 2 ;;
    *) echo "Usage: $0 --harness-dir <path> [--repo-name <owner/repo>]" >&2; exit 1 ;;
  esac
done

[ -z "$HARNESS_DIR" ] && { echo "Error: --harness-dir required" >&2; exit 1; }

mkdir -p "$HARNESS_DIR"/{agents,metrics,memory,proposals,handoffs,runs}

# Determine storage tier from path
STORAGE_TIER="repo"
case "$HARNESS_DIR" in
  "$HOME"/.harness/*) STORAGE_TIER="global" ;;
esac

# manifest.yaml
cat > "$HARNESS_DIR/manifest.yaml" <<EOF
protocol_version: "1.0.0"
created: "$(date +%Y-%m-%d)"
repo: "$REPO_NAME"
storage_tier: "$STORAGE_TIER"
features:
  evolve: true
  test: false
  document: false
  contribute: false
EOF

# config.yaml
cat > "$HARNESS_DIR/config.yaml" <<EOF
agents:
  code-reviewer: true
  silent-failure-hunter: true
  pr-test-analyzer: true
  type-design-analyzer: true
  comment-analyzer: true
  learnings-reviewer: true

evolve:
  auto_apply: true
  min_runs_for_auto: 5
  contribute: false
  contribute_repo: "$REPO_NAME"

review:
  max_cycles: 3
  adversarial: true
  evaluator: true
EOF

# Empty metrics files
for metric in review-effectiveness plan-accuracy learning-efficacy phase-costs; do
  cat > "$HARNESS_DIR/metrics/$metric.json" <<METRICEOF
{
  "schema_version": 1,
  "last_updated": null
}
METRICEOF
done

# IMPROVEMENTS.md
cat > "$HARNESS_DIR/memory/IMPROVEMENTS.md" <<'EOF'
# Harness Improvements

Audit trail of self-modifications to agent definitions. Append-only.

---
EOF

# .gitignore
cat > "$HARNESS_DIR/.gitignore" <<'EOF'
handoffs/
memory/session-history.json
review-results.json
phase-timing.tmp
run-state.json
EOF

echo "Scaffolded $HARNESS_DIR"
```

Make executable: `chmod +x plugins/harness/scripts/harness-init-runtime.sh`

- [ ] **Step 3: Create harness-read-state.sh**

```bash
#!/usr/bin/env bash
# Read run-state.json to stdout. Empty output if file doesn't exist.
# Usage: harness-read-state.sh --harness-dir <path>
set -euo pipefail

HARNESS_DIR=""
while [[ $# -gt 0 ]]; do
  case $1 in
    --harness-dir) HARNESS_DIR="$2"; shift 2 ;;
    *) echo "Usage: $0 --harness-dir <path>" >&2; exit 1 ;;
  esac
done

[ -z "$HARNESS_DIR" ] && { echo "Error: --harness-dir required" >&2; exit 1; }

STATE_FILE="$HARNESS_DIR/run-state.json"

if [ -f "$STATE_FILE" ]; then
  cat "$STATE_FILE"
fi
```

Make executable: `chmod +x plugins/harness/scripts/harness-read-state.sh`

- [ ] **Step 4: Create harness-update-state.sh**

```bash
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
```

Make executable: `chmod +x plugins/harness/scripts/harness-update-state.sh`

- [ ] **Step 5: Create harness-write-metrics.sh**

```bash
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
```

Make executable: `chmod +x plugins/harness/scripts/harness-write-metrics.sh`

- [ ] **Step 6: Create harness-write-proposal.sh**

```bash
#!/usr/bin/env bash
# Create a proposal markdown file in .harness/proposals/
# Usage: harness-write-proposal.sh --harness-dir <path> --slug <slug> --scope <repo|universal> --signal <signal> --agent <agent> --current-file <path> --proposed-file <path> --reasoning-file <path>
# Note: --current-file, --proposed-file, --reasoning-file are paths to temp files containing the content (avoids shell escaping issues with long markdown)
set -euo pipefail

HARNESS_DIR=""
SLUG=""
SCOPE="repo"
SIGNAL=""
AGENT=""
CURRENT_FILE=""
PROPOSED_FILE=""
REASONING_FILE=""

while [[ $# -gt 0 ]]; do
  case $1 in
    --harness-dir) HARNESS_DIR="$2"; shift 2 ;;
    --slug) SLUG="$2"; shift 2 ;;
    --scope) SCOPE="$2"; shift 2 ;;
    --signal) SIGNAL="$2"; shift 2 ;;
    --agent) AGENT="$2"; shift 2 ;;
    --current-file) CURRENT_FILE="$2"; shift 2 ;;
    --proposed-file) PROPOSED_FILE="$2"; shift 2 ;;
    --reasoning-file) REASONING_FILE="$2"; shift 2 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

[ -z "$HARNESS_DIR" ] || [ -z "$SLUG" ] && { echo "Error: --harness-dir and --slug required" >&2; exit 1; }

DATE=$(date +%Y-%m-%d)
PROPOSAL_FILE="$HARNESS_DIR/proposals/${DATE}-${SLUG}.md"

{
  echo "# Proposal: $SLUG"
  echo ""
  echo "- **Date:** $DATE"
  echo "- **Signal:** $SIGNAL"
  echo "- **Agent:** $AGENT"
  echo "- **Scope:** $SCOPE"
  echo "- **Status:** pending"
  echo ""
  echo "## Current"
  echo ""
  [ -f "$CURRENT_FILE" ] && cat "$CURRENT_FILE" || echo "(no current text provided)"
  echo ""
  echo "## Proposed"
  echo ""
  [ -f "$PROPOSED_FILE" ] && cat "$PROPOSED_FILE" || echo "(no proposed text provided)"
  echo ""
  echo "## Reasoning"
  echo ""
  [ -f "$REASONING_FILE" ] && cat "$REASONING_FILE" || echo "(no reasoning provided)"
} > "$PROPOSAL_FILE"

echo "$PROPOSAL_FILE"
```

Make executable: `chmod +x plugins/harness/scripts/harness-write-proposal.sh`

- [ ] **Step 7: Create harness-write-run.sh**

```bash
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
```

Make executable: `chmod +x plugins/harness/scripts/harness-write-run.sh`

- [ ] **Step 8: Make all scripts executable and verify**

```bash
chmod +x plugins/harness/scripts/harness-*.sh
ls -la plugins/harness/scripts/harness-*.sh
# Expected: 7 scripts, all with execute permission
```

- [ ] **Step 9: Commit**

```bash
git add plugins/harness/scripts/harness-*.sh
git commit -m "feat(harness): add deterministic persistence scripts for .harness/ runtime"
```

---

### Task 3: Script Tests

**Files:**
- Create: `plugins/harness/scripts/test-harness-scripts.sh`

- [ ] **Step 1: Create test script**

```bash
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
    ((PASS++))
  else
    echo "  FAIL: $desc"
    echo "    expected: $expected"
    echo "    actual:   $actual"
    ((FAIL++))
  fi
}

assert_file_exists() {
  local desc="$1" path="$2"
  if [ -f "$path" ]; then
    echo "  PASS: $desc"
    ((PASS++))
  else
    echo "  FAIL: $desc — file not found: $path"
    ((FAIL++))
  fi
}

assert_dir_exists() {
  local desc="$1" path="$2"
  if [ -d "$path" ]; then
    echo "  PASS: $desc"
    ((PASS++))
  else
    echo "  FAIL: $desc — dir not found: $path"
    ((FAIL++))
  fi
}

assert_contains() {
  local desc="$1" file="$2" pattern="$3"
  if grep -q "$pattern" "$file" 2>/dev/null; then
    echo "  PASS: $desc"
    ((PASS++))
  else
    echo "  FAIL: $desc — pattern '$pattern' not found in $file"
    ((FAIL++))
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

# ─── Test: harness-init-runtime.sh ───
echo "Testing harness-init-runtime.sh"

INIT_DIR="$TEST_DIR/init-test/.harness"
"$SCRIPT_DIR/harness-init-runtime.sh" --harness-dir "$INIT_DIR" --repo-name "test/repo" >/dev/null

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

# ─── Test: harness-read-state.sh / harness-update-state.sh ───
echo "Testing harness-read-state.sh and harness-update-state.sh"

STATE_DIR="$TEST_DIR/state-test"
mkdir -p "$STATE_DIR"

# Read nonexistent → empty
RESULT=$("$SCRIPT_DIR/harness-read-state.sh" --harness-dir "$STATE_DIR")
assert_eq "read nonexistent state returns empty" "" "$RESULT"

# Create state
"$SCRIPT_DIR/harness-update-state.sh" --harness-dir "$STATE_DIR" --phase "brainstorm" --plan "test-plan.md" --branch "test-branch" >/dev/null
assert_file_exists "creates run-state.json" "$STATE_DIR/run-state.json"
assert_contains "state has phase" "$STATE_DIR/run-state.json" '"phase": "brainstorm"'
assert_contains "state has plan" "$STATE_DIR/run-state.json" "test-plan.md"

# Update state
"$SCRIPT_DIR/harness-update-state.sh" --harness-dir "$STATE_DIR" --phase "plan" >/dev/null
assert_contains "updated phase" "$STATE_DIR/run-state.json" '"phase": "plan"'

# Verify both phases in completed_phases
COMPLETED_COUNT=$(python3 -c "import json; d=json.load(open('$STATE_DIR/run-state.json')); print(len(d['completed_phases']))")
assert_eq "two completed phases" "2" "$COMPLETED_COUNT"

# ─── Test: harness-write-metrics.sh ───
echo "Testing harness-write-metrics.sh"

# Use the init-test .harness dir which has metrics files
"$SCRIPT_DIR/harness-write-metrics.sh" --harness-dir "$INIT_DIR" --metric "review-effectiveness" --agent "code-reviewer" --findings 3 --false-pos 1 --unique 2 >/dev/null
assert_contains "metrics updated" "$INIT_DIR/metrics/review-effectiveness.json" '"code-reviewer"'

RUNS=$(python3 -c "import json; d=json.load(open('$INIT_DIR/metrics/review-effectiveness.json')); print(d['agents']['code-reviewer']['runs'])")
assert_eq "agent runs incremented" "1" "$RUNS"

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

# ─── Test: harness-write-run.sh ───
echo "Testing harness-write-run.sh"

RUN_FILE=$("$SCRIPT_DIR/harness-write-run.sh" --harness-dir "$INIT_DIR" --phase "review" --branch "test-branch")
assert_file_exists "run file created" "$RUN_FILE"
assert_contains "run has phase" "$RUN_FILE" '"phase": "review"'

# ─── Summary ───
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
```

Make executable: `chmod +x plugins/harness/scripts/test-harness-scripts.sh`

- [ ] **Step 2: Run tests**

```bash
bash plugins/harness/scripts/test-harness-scripts.sh
# Expected: all tests pass
```

- [ ] **Step 3: Commit**

```bash
git add plugins/harness/scripts/test-harness-scripts.sh
git commit -m "test(harness): add test suite for persistence scripts"
```

---

## Phase B: Core Runtime (Tasks 4-6)

### Task 4: Init Extension — .harness/ Scaffold

**Files:**
- Modify: `plugins/harness/commands/init.md`

- [ ] **Step 1: Add Phase 2.3 to init.md**

After Phase 2 step 8.7 (Generate REVIEW_GUIDANCE.md) and before Phase 3 (Discover Extra Categories), add a new phase. Insert after the REVIEW_GUIDANCE.md generation block:

```markdown
### Phase 2.3: Harness Runtime Scaffold (optional)

8.8. **Offer `.harness/` runtime setup:**
    ```
    ## Harness Runtime

    The harness can track metrics, evolve agent definitions, and self-improve
    across sessions via a `.harness/` directory.

    Options:
    1. `.harness/` in repo root (committed, team-shared) — recommended
    2. `~/.harness/{repo-slug}/` global (personal, not committed)
    3. Skip — use static plugin defaults (can add later with /harness:init)

    Which option? (1/2/3)
    ```

8.9. **If user chose option 1 or 2:**

    Determine the harness directory path:
    - Option 1: `HARNESS_DIR=".harness"`
    - Option 2: `HARNESS_DIR="$HOME/.harness/{repo-slug}"` where repo-slug is derived from `basename $(git rev-parse --show-toplevel)`

    Determine the repo name for manifest:
    ```bash
    git remote get-url origin 2>/dev/null | sed 's|.*github.com[:/]||;s|\.git$||' || echo "local/$(basename $(pwd))"
    ```

    Run the scaffold script:
    ```bash
    bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-init-runtime.sh \
      --harness-dir "$HARNESS_DIR" \
      --repo-name "$REPO_NAME"
    ```

    Copy default agent definitions from plugin to `.harness/agents/`:
    - Read each agent file in `${CLAUDE_PLUGIN_ROOT}/agents/` (harness-pruner.md, learnings-reviewer.md, harness-evolver.md)
    - Copy them to `$HARNESS_DIR/agents/` as starting points for evolution
    - These copies will diverge from the plugin defaults as they evolve

    If option 1: add `.harness/` to the Documentation Map in CLAUDE.md:
    ```markdown
    | Harness Runtime | `.harness/` | Agent evolution, metrics, proposals, self-improvement |
    ```

    If option 1: ensure `.harness/handoffs/`, `.harness/memory/session-history.json`, `.harness/review-results.json`, `.harness/phase-timing.tmp`, and `.harness/run-state.json` are in `.gitignore` (the scaffold script creates `.harness/.gitignore` for this, but also check the repo root `.gitignore`).

8.10. **If user chose option 3:** Skip silently. The harness works without `.harness/` — all commands fall back to plugin defaults.
```

- [ ] **Step 2: Update Phase 7 report in init.md**

In the Report section (Phase 7), add `.harness/` to the Files Created list:

```markdown
    - .harness/ (if opted in: manifest.yaml, config.yaml, agents/, metrics/, memory/)
```

- [ ] **Step 3: Verify init.md has new phase**

```bash
grep -c "Phase 2.3" plugins/harness/commands/init.md
# Expected: 1
grep -c "harness-init-runtime.sh" plugins/harness/commands/init.md
# Expected: 1
```

- [ ] **Step 4: Commit**

```bash
git add plugins/harness/commands/init.md
git commit -m "feat(harness): extend /harness:init with .harness/ runtime scaffold"
```

---

### Task 5: Evolver Agent Definition

**Files:**
- Create: `plugins/harness/agents/harness-evolver.md`

- [ ] **Step 1: Create harness-evolver.md**

```markdown
---
name: harness-evolver
description: Use when proposing modifications to agent definitions based on review escapes, metric anomalies, or universal learnings — invoked by /harness:evolve Phase 3
color: magenta
---

# Harness Evolver

You propose concrete edits to agent definitions in `.harness/agents/` based on evidence from review escapes, metric anomalies, and universal learnings. You are the meta-agent — you improve the agents that improve the code.

## Context

The `.harness/` runtime tracks how each review agent performs:
- `metrics/review-effectiveness.json` — runs, findings, false positives, unique catches per agent
- `proposals/` — pending and applied evolution proposals
- `memory/IMPROVEMENTS.md` — audit trail of applied changes

Agent definitions in `.harness/agents/` are markdown files with system prompts. They start as copies of `plugins/harness/agents/` defaults and diverge as they evolve to match the repo's specific needs.

## Process

For each signal provided (escape, metric anomaly, universal learning):

1. **Read the relevant agent definition** from `.harness/agents/`. If the file doesn't exist, the agent is using plugin defaults — propose creating a `.harness/agents/` copy with the modification.

2. **Semantic dedup check**: Before proposing an addition, scan the agent definition for existing checks that cover the same concern. If a similar check exists, propose refining it rather than adding a duplicate. Two checks for the same bug class is worse than one precise check.

3. **Propose a concrete edit**:
   - For escapes: add a specific check or question that would catch this bug class
   - For metric anomalies: adjust thresholds, disable underperforming checks, add context
   - For universal learnings: add a general pattern check
   - Changes must be **additive** unless the signal is a false positive rate over 50% (then removal is appropriate)

4. **Line budget enforcement**: Agent definitions must stay under 200 lines. If the agent is already near the limit, propose consolidating existing checks before adding new ones. A focused agent with 15 precise checks beats a bloated agent with 40 vague ones.

5. **Write the proposal** using the harness-write-proposal.sh script, providing:
   - The current text (relevant section of the agent definition)
   - The proposed replacement text
   - Reasoning that connects the signal to the proposed change

## Signal Types

| Signal | Source | Typical Response |
|--------|--------|-----------------|
| Review escape | `docs/REVIEW_GUIDANCE.md` escape log | Add "what breaks?" question to the agent's checklist |
| False positive rate > 50% | `metrics/review-effectiveness.json` | Remove or narrow the check causing false positives |
| Zero unique catches after 10+ runs | `metrics/review-effectiveness.json` | Consider disabling the agent for this repo |
| Universal learning | `docs/LEARNINGS.md` with `scope: universal` | Add general pattern check |
| Metric regression after auto-apply | `memory/IMPROVEMENTS.md` | Propose rollback of the change |

## Quality Criteria

Every proposal must:
- **Be specific**: "Add check for connection pool exhaustion in database handler code" not "improve error handling checks"
- **Have evidence**: Link to the escape ID, metric data point, or learning ID
- **Be testable**: The next review run on similar code should exercise the new check
- **Preserve existing value**: Don't remove checks that catch real issues to make room for new ones

## Output Format

For each signal, output:

```
### Proposal: {slug}

**Signal:** {escape ID / metric / learning ID}
**Agent:** {agent name}
**Scope:** {repo|universal}

**Current section:**
{relevant lines from current agent definition}

**Proposed change:**
{the new/modified lines}

**Reasoning:**
{why this change should improve outcomes}

**Auto-apply eligible:** {yes|no} — {reason}
```

## Behavioral Rules

**You MUST:**
- Check for semantic duplicates before proposing additions
- Respect the 200-line budget per agent
- Include the signal source (evidence) in every proposal
- Classify scope as `repo` or `universal`

**You MUST NOT:**
- Remove checks without evidence of false positives
- Propose changes to agents that have fewer than 3 runs (insufficient data)
- Modify the harness-evolver agent definition (yourself) — this requires manual human review
- Propose changes that make an agent definition model-specific (must stay model-agnostic)
```

- [ ] **Step 2: Verify agent file**

```bash
head -5 plugins/harness/agents/harness-evolver.md
# Expected: frontmatter with name: harness-evolver
wc -l plugins/harness/agents/harness-evolver.md
# Expected: reasonable length (under 120 lines)
```

- [ ] **Step 3: Commit**

```bash
git add plugins/harness/agents/harness-evolver.md
git commit -m "feat(harness): add harness-evolver agent for self-modification proposals"
```

---

### Task 6: /harness:evolve Command

**Files:**
- Create: `plugins/harness/commands/evolve.md`

- [ ] **Step 1: Create evolve.md**

```markdown
---
description: Use after reflect to classify learnings, update metrics, and propose agent evolution, or when user says "evolve", "self-improve", "classify learnings"
---

# Evolve

Self-modification phase. Classifies learning scope, updates metrics from review results, proposes agent evolution based on evidence, and auto-applies safe changes. Run after `/harness:reflect`, before `/harness:complete`.

## Usage

```
/harness:evolve                    # Run full evolution cycle
```

## Prerequisites

Requires a `.harness/` runtime directory. Resolve it:
```bash
HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
```

If `HARNESS_DIR` is empty, STOP and print:
```
No .harness/ runtime found. Run /harness:init and choose option 1 or 2 to enable self-improvement.
```

## Invocation

**IMMEDIATELY execute this workflow:**

### Phase 1: Scope Classification

1. Read `docs/LEARNINGS.md`. Filter to entries written in this session (match current date and current branch in `source:` and `branch:` fields).

2. For each learning from this session that does NOT already have a `scope:` field:

   Classify as `scope: repo` or `scope: universal` using these rules:

   **Default to `repo`** (conservative). Only promote to `universal` when ALL of:
   - Contains zero project-specific references (file paths, module names, domain concepts)
   - Matches a known general pattern category (error handling, testing strategy, review methodology, agent coordination, security, performance)
   - The recommendation is actionable without project-specific context

   Examples:
   - "When unit-testing Temporal activities that call `activity.RecordHeartbeat`, wrap in `recover()`" → `repo` (references Temporal, a specific dependency)
   - "Review agents should always check stderr capture in shell commands" → `universal` (general pattern)
   - "The pipeline engine has race conditions in concurrent node execution" → `repo` (references specific module)
   - "When embedding YAML in Go raw strings, backticks cannot be nested" → `universal` (general language pattern)

3. Write the `scope` tag into each learning entry as a new metadata line after `category`:
   ```
   - scope: {repo|universal}
   ```

4. Report classification results:
   ```
   Classified {N} learnings: {R} repo, {U} universal
   ```

### Phase 2: Metrics Update

5. Read `$HARNESS_DIR/review-results.json` (written by `/harness:review`). If the file doesn't exist, skip to Phase 3 — no review data to process.

6. Read `$HARNESS_DIR/metrics/review-effectiveness.json`. For each agent in review-results.json:

   Count findings that were accepted (led to code changes) vs dismissed:
   - `findings` = count of entries where `accepted: true`
   - `false_positives` = count of entries where `accepted: false`
   - `unique_catches` = count of entries where `unique: true`

   Update metrics using the persistence script:
   ```bash
   bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-metrics.sh \
     --harness-dir "$HARNESS_DIR" \
     --metric "review-effectiveness" \
     --agent "{agent-name}" \
     --findings {N} \
     --false-pos {N} \
     --unique {N}
   ```

7. If an active plan exists in `docs/exec-plans/active/`, update plan accuracy metrics:
   - Count `- [x]` and `- [ ]` in the Progress section
   - Count entries in Surprises & Discoveries and Plan Drift tables

   ```bash
   bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-metrics.sh \
     --harness-dir "$HARNESS_DIR" \
     --metric "plan-accuracy" \
     --plan-slug "{plan-slug}" \
     --tasks-planned {N} \
     --tasks-completed {N} \
     --drift {N} \
     --surprises {N}
   ```

8. Update `$HARNESS_DIR/metrics/learning-efficacy.json`:
   - For each new learning from this session, check if any bugs in this session match existing learning categories (recurrence detection)
   - If a learning's recommendation was supposed to prevent this class of bug but the bug occurred anyway, increment `recurrence_count`
   - If a learning's recommendation was consulted and the relevant bug class did NOT occur, increment `prevented_count`
   - Use python3 for JSON manipulation:
     ```bash
     python3 -c "
     import json
     with open('$HARNESS_DIR/metrics/learning-efficacy.json') as f:
         data = json.load(f)
     learnings = data.setdefault('learnings', {})
     # ... update per learning ID ...
     data['last_updated'] = '$(date -u +%Y-%m-%dT%H:%M:%SZ)'
     with open('$HARNESS_DIR/metrics/learning-efficacy.json', 'w') as f:
         json.dump(data, f, indent=2)
     "
     ```

### Phase 3: Agent Evolution Proposals

9. Read `$HARNESS_DIR/metrics/review-effectiveness.json`. Identify:
   - **Escapes**: check `docs/REVIEW_GUIDANCE.md` escape log for entries from this session
   - **Metric anomalies**: agents with false positive rate > 50%, or zero unique catches after 10+ runs
   - **Universal learnings**: entries from Phase 1 classified as `scope: universal`

10. If no signals found, skip to Phase 4 report. Otherwise, for each signal:

    <MANDATORY>
    You MUST use the Agent tool with `subagent_type: "harness:harness-evolver"` to generate proposals. The evolver agent has the semantic dedup check and line budget enforcement logic. Do NOT generate proposals inline — the evolver agent's methodology prevents bloated agents.

    Example invocation:
    ```
    Agent(
      subagent_type="harness:harness-evolver",
      prompt="Generate evolution proposals for these signals: {signal list}. Read agent definitions from $HARNESS_DIR/agents/. Read metrics from $HARNESS_DIR/metrics/. Write proposals using ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-proposal.sh."
    )
    ```
    </MANDATORY>

11. For each proposal the evolver produces, write it to disk:
    ```bash
    bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-proposal.sh \
      --harness-dir "$HARNESS_DIR" \
      --slug "{slug}" \
      --scope "{repo|universal}" \
      --signal "{signal-source}" \
      --agent "{agent-name}" \
      --current-file "{temp-file}" \
      --proposed-file "{temp-file}" \
      --reasoning-file "{temp-file}"
    ```

### Phase 4: Auto-Apply Safe Proposals

12. Read `$HARNESS_DIR/config.yaml`. Check `evolve.auto_apply` and `evolve.min_runs_for_auto`.

13. For each proposal from Phase 3, determine auto-apply eligibility. A proposal is auto-applied when ALL criteria are met:
    - `evolve.auto_apply` is `true` in config.yaml
    - The signal is a review escape (concrete evidence of a miss)
    - The change is additive (adds a check, doesn't remove one)
    - The agent has run N+ times where N = `evolve.min_runs_for_auto` (default: 5)

14. For eligible proposals:
    - Read the agent definition from `$HARNESS_DIR/agents/{agent}.md`
    - Apply the proposed change
    - Update the proposal status from `pending` to `applied`
    - Log the change to `$HARNESS_DIR/memory/IMPROVEMENTS.md`:
      ```markdown

      ### {YYYY-MM-DD}: {one-line description of change}
      - **Agent:** {agent name}
      - **Signal:** {signal source}
      - **Change:** {what was added/modified}
      - **Scope:** {repo|universal}
      - **Auto-applied:** yes
      - **Rollback:** none

      {Reasoning from the proposal}

      ---
      ```

15. For non-eligible proposals: leave status as `pending`. These will be surfaced in the PR description by `/harness:complete`.

### Phase 5: Auto-Rollback Check

16. Read `$HARNESS_DIR/memory/IMPROVEMENTS.md`. For each auto-applied change from previous sessions (not the current session):
    - Check if the agent's metrics worsened after the change was applied:
      - False positive rate increased by more than 20%
      - Unique catches decreased
    - If metrics worsened, auto-revert:
      - Undo the change in the agent definition
      - Update the IMPROVEMENTS.md entry: `- **Rollback:** rolled-back-{YYYY-MM-DD}`
      - Update the proposal status to `rolled-back`
      - Log a new IMPROVEMENTS.md entry explaining the rollback

17. This phase only runs when there are at least 2 post-change review runs to compare against. Skip if insufficient data.

### Phase 6: Write Run Record

18. Write a run record for this evolve session:
    ```bash
    bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-run.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "evolve" \
      --branch "$(git branch --show-current)"
    ```

### Report

19. Output:
    ```
    ## Evolve Complete

    **Runtime:** {$HARNESS_DIR}

    ### Scope Classification
    - Learnings classified: {N} ({R} repo, {U} universal)

    ### Metrics Updated
    - Review effectiveness: {N} agents updated
    - Plan accuracy: {updated | no active plan}
    - Learning efficacy: {N} learnings tracked

    ### Evolution Proposals
    - Proposals generated: {N}
    - Auto-applied: {N} (signals: {list})
    - Pending review: {N}

    ### Auto-Rollback
    - Rollbacks: {N | none | skipped (insufficient data)}

    ## Next Step

    Run `/harness:complete` to archive the plan and create the PR (proposals will be listed in the PR description).
    ```
```

- [ ] **Step 2: Verify evolve.md structure**

```bash
grep -c "Phase" plugins/harness/commands/evolve.md
# Expected: 6 (Phases 1-6)
grep "harness:harness-evolver" plugins/harness/commands/evolve.md
# Expected: appears in MANDATORY block
grep "harness-write-proposal.sh" plugins/harness/commands/evolve.md
# Expected: appears in Phase 3
```

- [ ] **Step 3: Commit**

```bash
git add plugins/harness/commands/evolve.md
git commit -m "feat(harness): add /harness:evolve command for self-modification"
```

---

## Phase C: Integration (Tasks 7-10)

### Task 7: /harness:review Extension — Evaluator + Structured Output

**Files:**
- Modify: `plugins/harness/commands/review.md`

- [ ] **Step 1: Add Phase 2.5 — Evaluator Pass**

In `plugins/harness/commands/review.md`, after Phase 2 (Verification Gate, step 3) and before Phase 3 (Adversarial Production Review, step 4), insert:

```markdown
### Phase 2.5: Evaluator Pass (optional)

3.1. Resolve the harness runtime directory:
   ```bash
   HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
   ```

3.2. If `HARNESS_DIR` is not empty AND `$HARNESS_DIR/agents/evaluator.md` exists:
   - Read `$HARNESS_DIR/config.yaml`. Check `review.evaluator` is `true`.
   - If enabled, read `$HARNESS_DIR/agents/evaluator.md` — this is the repo-specific evaluator agent definition (independent from the code-review agents, following the GAN pattern from the Anthropic article).
   - Run the evaluator as a separate Agent with the diff from Phase 1:
     ```
     Agent(
       subagent_type="general-purpose",
       prompt="{evaluator.md content}\n\nReview this diff:\n{diff}\n\nReturn findings in structured format: severity, title, location, scenario, impact, fix."
     )
     ```
   - Evaluator findings feed into Phase 4 review loop alongside adversarial findings.

3.3. If `HARNESS_DIR` is empty or `evaluator.md` doesn't exist: skip silently. The evaluator is opt-in.
```

- [ ] **Step 2: Add review-results.json output contract**

At the end of the Report section (after step 23), add:

```markdown
### Phase 8: Structured Output (if .harness/ exists)

24. Resolve the harness runtime directory:
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    ```

25. If `HARNESS_DIR` is not empty, write structured review results for `/harness:evolve` consumption:

    Build a JSON object from the review cycle results:
    - For each agent that ran: record `ran`, `findings` (with text, severity, file, line, accepted, unique), and `verdict`
    - Record `overall_verdict` and `cycles_run`
    - Write to `$HARNESS_DIR/review-results.json`

    Use python3 to write the JSON:
    ```bash
    python3 -c "
    import json
    results = {
        'schema_version': 1,
        'session_date': '$(date +%Y-%m-%d)',
        'branch': '$(git branch --show-current)',
        'agents': {
            # Populated from review cycle results
        },
        'overall_verdict': '{PASS|FAIL}',
        'cycles_run': {N}
    }
    with open('$HARNESS_DIR/review-results.json', 'w') as f:
        json.dump(results, f, indent=2)
    "
    ```

    To determine `accepted` and `unique` for each finding:
    - `accepted`: the finding led to a code change (check if the fix commit touched the flagged file/line)
    - `unique`: no other agent reported the same file+line with similar severity

26. If `HARNESS_DIR` is empty: skip. Review works without `.harness/` — structured output is additive.
```

- [ ] **Step 3: Verify review.md changes**

```bash
grep -c "Phase 2.5" plugins/harness/commands/review.md
# Expected: 1
grep -c "review-results.json" plugins/harness/commands/review.md
# Expected: at least 2
grep -c "harness-resolve-dir.sh" plugins/harness/commands/review.md
# Expected: 2
```

- [ ] **Step 4: Commit**

```bash
git add plugins/harness/commands/review.md
git commit -m "feat(harness): extend /harness:review with evaluator pass and structured output"
```

---

### Task 8: /harness:reflect & /harness:complete Extensions

**Files:**
- Modify: `plugins/harness/commands/reflect.md`
- Modify: `plugins/harness/commands/complete.md`

- [ ] **Step 1: Add Phase 5.8 to reflect.md**

In `plugins/harness/commands/reflect.md`, after Phase 5.5 (Review Escape Mining, step 14.11) and before Phase 6 (Outcomes & Retrospective, step 15), insert:

```markdown
### Phase 5.8: Evolve Trigger

14.12. Resolve the harness runtime directory:
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    ```

14.13. If `HARNESS_DIR` is not empty:
    - Invoke `/harness:evolve` using the Skill tool: `Skill("harness:evolve")`. Follow the loaded skill's full process.

    <MANDATORY>
    You MUST use the Skill tool to invoke `/harness:evolve`. Do NOT classify learnings or generate proposals inline — the evolve command has the persistence script integration, evolver agent dispatch, and auto-apply safety checks that prevent both under-evolution and over-evolution.
    </MANDATORY>

14.14. If `HARNESS_DIR` is empty: skip silently. Repos without `.harness/` work exactly as before — backward compatible.

14.15. Include evolve results in the Report output (append after the Review Escape Mining section):
    ```
    ### Evolution
    - {evolve report summary, or "Skipped — no .harness/ runtime"}
    ```
```

- [ ] **Step 2: Add Phase 5.7 to complete.md**

In `plugins/harness/commands/complete.md`, after Phase 5 (Tier 2 Summary Updates, step 11.5) and before Phase 6 (Prune Health Check, step 12), insert:

```markdown
### Phase 5.7: Evolution Summary

11.6. Resolve the harness runtime directory:
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    ```

11.7. If `HARNESS_DIR` is not empty, check for proposals from this session:
    - List files in `$HARNESS_DIR/proposals/` matching today's date prefix
    - For each proposal, read its status, scope, signal, and agent

11.8. If proposals exist, include them in the PR description (Phase 7). Add a "Harness Evolution" section to the PR body:
    ```markdown
    ## Harness Evolution

    | Proposal | Agent | Signal | Scope | Status |
    |----------|-------|--------|-------|--------|
    | {slug} | {agent} | {signal} | {scope} | {applied/pending} |

    **Auto-applied ({N}):** These proposals met all safety criteria (escape-sourced, additive, 5+ agent runs) and were applied automatically.

    **Pending review ({N}):** These proposals need human review before applying.
    ```

11.9. If no proposals or no `.harness/`: skip. The PR is created normally.
```

- [ ] **Step 3: Verify changes**

```bash
grep -c "Phase 5.8" plugins/harness/commands/reflect.md
# Expected: 1
grep "Skill(\"harness:evolve\")" plugins/harness/commands/reflect.md
# Expected: 1
grep -c "Phase 5.7" plugins/harness/commands/complete.md
# Expected: 1
grep "Harness Evolution" plugins/harness/commands/complete.md
# Expected: 1
```

- [ ] **Step 4: Commit**

```bash
git add plugins/harness/commands/reflect.md plugins/harness/commands/complete.md
git commit -m "feat(harness): wire evolve into reflect (Phase 5.8) and evolution summary into complete (Phase 5.7)"
```

---

### Task 9: /harness:bug Extension — Retroactive Harness Trace

**Files:**
- Modify: `plugins/harness/commands/bug.md`

- [ ] **Step 1: Add Phase 4.7 to bug.md**

In `plugins/harness/commands/bug.md`, after Phase 4.6 (Write learnings, step 4.6) and before step 5 (Update index), insert:

```markdown
4.7. **Retroactive harness trace** (if `.harness/` runtime exists):

    This is the fitness function for the self-improving harness. When a bug is investigated, trace it back to the harness run that should have caught it.

    a. Resolve the harness runtime directory:
       ```bash
       HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
       ```
       If empty, skip this phase silently.

    b. **Identify the originating run**: Search `.harness/runs/` for the most recent run records on the branch where the bug was introduced. Cross-reference with git blame on the buggy code to find the commit, then match that commit's date/branch against run records.

    c. **Trace the review that missed it**: Read the run record to find if a review phase ran. If yes:
       - Read the review-results.json from that period (if still available in runs/)
       - Determine which review agents ran and what they reported
       - Identify: did any agent flag the area but the finding was dismissed? Or did no agent flag it at all?

    d. **Write retroactive review escape**: If the bug should have been caught by review:
       - Add an entry to `docs/REVIEW_GUIDANCE.md` Escape Log:
         ```markdown
         | {date} | {bug description} | retroactive-trace | {category} | {question} |
         ```
       - Formulate the "what breaks?" question that would catch this bug class
       - Add the question to the appropriate Adversarial Question Bank category

    e. **Update metrics retroactively**: If the originating run and agent are identified:
       ```bash
       bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-metrics.sh \
         --harness-dir "$HARNESS_DIR" \
         --metric "review-effectiveness" \
         --agent "{agent-that-missed}" \
         --false-pos 0 \
         --findings 0 \
         --unique 0
       ```
       Note: This increments runs without incrementing findings, worsening the agent's effectiveness ratio. This is intentional — the agent ran but missed the bug.

    f. **Write retroactive learning** with category `review-escape`:
       ```markdown
       ### L-{YYYYMMDD}-{slug}: {what the review missed}
       - status: active
       - category: review-escape
       - scope: {repo|universal}
       - source: /harness:bug {YYYY-MM-DD} (retroactive trace)
       - branch: {current branch}

       {What the review should have checked. Actionable recommendation.}

       ---
       ```

    g. Append to the bug analysis document under a new section:
       ```markdown
       ## Harness Trace

       - **Originating run:** {run record path or "not found"}
       - **Review ran:** {yes/no}
       - **Agents that ran:** {list}
       - **Escape category:** {category}
       - **Retroactive question added:** {yes/no — question text}
       - **Metric impact:** {agent effectiveness ratio before → after}
       ```

    If the originating run cannot be identified (no `.harness/runs/` data for the relevant period), note this in the Harness Trace section as "Insufficient run history — harness trace unavailable" and skip substeps c-f.
```

- [ ] **Step 2: Verify bug.md changes**

```bash
grep -c "4.7" plugins/harness/commands/bug.md
# Expected: at least 1
grep "Retroactive harness trace" plugins/harness/commands/bug.md
# Expected: 1
grep "harness-resolve-dir.sh" plugins/harness/commands/bug.md
# Expected: 1
```

- [ ] **Step 3: Commit**

```bash
git add plugins/harness/commands/bug.md
git commit -m "feat(harness): add retroactive harness trace to /harness:bug — the fitness function"
```

---

### Task 10: Run-State Protocol, Loop Deprecation & Plugin Registration

**Files:**
- Modify: `plugins/harness/commands/brainstorm.md`
- Modify: `plugins/harness/commands/plan.md`
- Modify: `plugins/harness/commands/orchestrate.md`
- Modify: `plugins/harness/commands/review.md` (already modified in Task 7)
- Modify: `plugins/harness/commands/reflect.md` (already modified in Task 8)
- Modify: `plugins/harness/commands/complete.md` (already modified in Task 8)
- Modify: `plugins/harness/commands/loop.md`
- Modify: `plugins/harness/.claude-plugin/plugin.json`
- Modify: `plugins/harness/agents/harness-pruner.md`

- [ ] **Step 1: Add run-state protocol to brainstorm.md**

At the end of brainstorm.md's final step (before the closing Report), add:

```markdown
{N}. **Update run-state** (if `.harness/` runtime exists):
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-update-state.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "brainstorm" \
      --design-doc "docs/design-docs/{filename}" \
      --branch "$(git branch --show-current)"
    ```
```

- [ ] **Step 2: Add run-state protocol to plan.md**

The plan.md already has step numbering from the harness:plan skill. At the end of step 5 (saving the plan), add:

```markdown
5.5. **Update run-state** (if `.harness/` runtime exists):
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-update-state.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "plan" \
      --plan "docs/exec-plans/active/{filename}" \
      --design-doc "{design-doc-path}"
    ```
```

- [ ] **Step 3: Add run-state protocol to orchestrate.md**

At the start of orchestrate.md's invocation (Phase 1), add:

```markdown
0.1. **Read run-state** (if `.harness/` runtime exists):
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-read-state.sh --harness-dir "$HARNESS_DIR"
    ```
    Use the run-state to auto-detect the active plan if no argument was provided.
```

At the end of orchestrate.md's final step, add:

```markdown
{N}. **Update run-state** (if `.harness/` runtime exists):
    ```bash
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-update-state.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "orchestrate"
    ```
```

- [ ] **Step 4: Add run-state to review.md, reflect.md, complete.md**

For each of review.md, reflect.md, and complete.md (already modified in Tasks 7-8), add the same pattern at the end of their Report sections:

```markdown
{N}. **Update run-state** (if `.harness/` runtime exists):
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-update-state.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "{review|reflect|complete}"
    ```
```

- [ ] **Step 5: Add deprecation notice to loop.md**

At the top of `plugins/harness/commands/loop.md`, immediately after the frontmatter `---`, add:

```markdown
> **DEPRECATED:** `/harness:loop` is deprecated in favor of stateless command composition. Each command (brainstorm, plan, orchestrate, test, review, reflect, complete) is a stateless function that reads inputs from disk and writes outputs to disk. Run `/clear` between commands for context isolation. The `run-state.json` protocol connects them. Loop will be removed in a future version.
>
> **Migration:** Instead of `/harness:loop`, run each command individually:
> ```
> /harness:brainstorm → /clear → /harness:plan → /clear → /harness:orchestrate → /clear → /harness:review → /clear → /harness:reflect → /clear → /harness:complete
> ```
> For autonomous execution, use a belayer pipeline YAML with one node per command.
```

- [ ] **Step 6: Update plugin.json**

In `plugins/harness/.claude-plugin/plugin.json`:
- Add `"./commands/evolve.md"` to the `commands` array
- Add `"./agents/harness-evolver.md"` to the `agents` array
- Add relevant keywords: `"evolve"`, `"self-improving"`, `"metrics"`, `"runtime"`
- Bump version to `5.0.0` (major: new command, new agent, new runtime protocol, deprecation)

Updated plugin.json:

```json
{
  "name": "harness",
  "version": "5.0.0",
  "description": "Self-improving documentation system with agent evolution, metrics-driven self-modification, and stateless command protocol. Brainstorm, plan, orchestrate, evolve, review, reflect, complete.",
  "author": {
    "name": "donovanyohan"
  },
  "keywords": ["documentation", "claude-md", "planning", "brainstorm", "bug", "refactor", "strangler-fig", "orchestrate", "batch", "maintenance", "reflect", "loop", "autonomous", "learnings", "frontmatter", "lifecycle", "health-score", "evolve", "self-improving", "metrics", "runtime"],
  "agents": [
    "./agents/harness-pruner.md",
    "./agents/learnings-reviewer.md",
    "./agents/harness-evolver.md"
  ],
  "commands": [
    "./commands/init.md",
    "./commands/brainstorm.md",
    "./commands/bug.md",
    "./commands/refactor.md",
    "./commands/refactor-status.md",
    "./commands/plan.md",
    "./commands/orchestrate.md",
    "./commands/batch.md",
    "./commands/review.md",
    "./commands/evolve.md",
    "./commands/complete.md",
    "./commands/reflect.md",
    "./commands/prune.md",
    "./commands/loop.md"
  ],
  "skills": ["./skills/strangler-fig"]
}
```

- [ ] **Step 7: Add .harness/ audit checks to harness-pruner.md**

In `plugins/harness/agents/harness-pruner.md`, add to the Audit Checks table:

```markdown
| **Harness Runtime Missing** | `.harness/` or `~/.harness/slug/` not found when CLAUDE.md Documentation Map lists it | warn |
| **Stale Proposals** | Proposals in `.harness/proposals/` with status `pending` older than 14 days | warn |
| **Metrics Cold Start** | `.harness/metrics/review-effectiveness.json` has zero agent runs after harness has been active 30+ days | info |
| **Agent Budget Exceeded** | Agent definition in `.harness/agents/` exceeds 200 lines | warn |
| **IMPROVEMENTS.md Missing** | `.harness/memory/IMPROVEMENTS.md` doesn't exist but `.harness/` is initialized | error |
```

Add corresponding audit steps (Steps 25-29) to the Audit Process section:

```markdown
### Step 25: Harness Runtime Consistency

If CLAUDE.md Documentation Map includes a `.harness/` entry:
- Check if `.harness/` exists on the filesystem
- If missing, flag as **warn** — the map references a directory that doesn't exist

### Step 26: Stale Proposals

If `.harness/proposals/` exists and has files:
- For each proposal with `Status: pending`, check the date in the filename
- If older than 14 days, flag as **warn** — proposals should be reviewed promptly

### Step 27: Metrics Cold Start

If `.harness/metrics/review-effectiveness.json` exists:
- Check if any agent has `runs > 0`
- If all agents have zero runs and `.harness/manifest.yaml` was created more than 30 days ago, flag as **info** — the evolution system has no data

### Step 28: Agent Line Budget

For each file in `.harness/agents/`:
- Count lines using `wc -l`
- If over 200 lines, flag as **warn** — agent definitions should stay focused

### Step 29: IMPROVEMENTS.md Presence

If `.harness/` exists but `.harness/memory/IMPROVEMENTS.md` does not:
- Flag as **error** — the audit trail is missing
```

Add to the Applying Fixes section:

```markdown
23. **Stale proposals** — List pending proposals for user review; offer to mark as `rejected`
24. **Agent budget exceeded** — Suggest running `/harness:evolve` to consolidate checks
25. **IMPROVEMENTS.md missing** — Recreate from scaffold
```

- [ ] **Step 8: Verify all changes**

```bash
# Check deprecation notice in loop.md
grep "DEPRECATED" plugins/harness/commands/loop.md
# Expected: 1 match

# Check evolve.md registered in plugin.json
grep "evolve.md" plugins/harness/.claude-plugin/plugin.json
# Expected: 2 matches (command + no false hits)

# Check version bump
grep '"5.0.0"' plugins/harness/.claude-plugin/plugin.json
# Expected: 1 match

# Check harness-evolver registered
grep "harness-evolver.md" plugins/harness/.claude-plugin/plugin.json
# Expected: 1 match

# Check run-state protocol in brainstorm, plan, orchestrate
grep "harness-update-state.sh" plugins/harness/commands/brainstorm.md
grep "harness-update-state.sh" plugins/harness/commands/plan.md
grep "harness-update-state.sh" plugins/harness/commands/orchestrate.md
# Expected: each returns at least 1 match

# Check pruner has .harness/ checks
grep "Harness Runtime" plugins/harness/agents/harness-pruner.md
# Expected: at least 1 match
```

- [ ] **Step 9: Run script tests to verify nothing broke**

```bash
bash plugins/harness/scripts/test-harness-scripts.sh
# Expected: all tests pass
```

- [ ] **Step 10: Commit**

```bash
git add plugins/harness/commands/brainstorm.md \
  plugins/harness/commands/plan.md \
  plugins/harness/commands/orchestrate.md \
  plugins/harness/commands/review.md \
  plugins/harness/commands/reflect.md \
  plugins/harness/commands/complete.md \
  plugins/harness/commands/loop.md \
  plugins/harness/.claude-plugin/plugin.json \
  plugins/harness/agents/harness-pruner.md
git commit -m "feat(harness): run-state protocol, loop deprecation, plugin registration (v5.0.0)"
```

---

## Deliverable Traceability

| Design Doc Deliverable (M1) | Plan Task |
|------------------------------|-----------|
| `scope: repo \| universal` in learnings format | Task 1 |
| Deterministic persistence scripts (7 scripts) | Task 2 |
| Script test suite | Task 3 |
| `.harness/` scaffold in `/harness:init` | Task 4 |
| `harness-evolver` agent with semantic dedup + line budget | Task 5 |
| `/harness:evolve` command (classify, propose, auto-apply, rollback) | Task 6 |
| Metrics collection (review-effectiveness, plan-accuracy, learning-efficacy, phase-costs) | Task 1 (schemas), Task 2 (init script), Task 6 (evolve writes), Task 7 (review writes) |
| Structured review output contract (review-results.json) | Task 7 |
| `IMPROVEMENTS.md` audit trail | Task 2 (init script creates), Task 6 (evolve writes) |
| `~/.harness/repo-slug/` global fallback | Task 2 (resolve-dir script), Task 4 (init offers choice) |
| `run-state.json` lifecycle | Task 2 (state scripts), Task 10 (cross-cutting protocol) |
| Retroactive harness trace in `/harness:bug` | Task 9 |
| Extend `/harness:reflect` to trigger evolve | Task 8 |
| Extend `/harness:complete` with evolution summary | Task 8 |
| Deprecate `/harness:loop` | Task 10 |
| Hard line budget on agents (200 lines) | Task 5 (evolver enforces), Task 10 (pruner checks) |
| Auto-rollback on metric regression | Task 6 (Phase 5) |
| Manifest.yaml + config.yaml specs | Task 1 (runtime-spec.md), Task 2 (init script generates) |
| Plugin registration (evolve command + evolver agent) | Task 10 |

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
-

**What didn't:**
-

**Learnings to codify:**
-
