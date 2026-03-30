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
      "last_run": null
    }
  },
  "last_updated": null
}
```

<!-- NOTE: Agent disabling (disabled/disable_reason fields) is planned but not yet implemented.
     The write-metrics script does not write these fields. Add them to both the spec and script
     when the disable-agent feature is built. -->

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
  "last_updated": null
}
```

<!-- TODO: Add an "aggregate" block (avg_drift_rate, avg_surprise_rate,
     avg_completion_rate) when the evolver needs cross-plan trend signals
     to inform evolution proposals. Requires recomputation after each
     plan-accuracy update in harness-write-metrics.sh. Not needed until
     there are 3+ completed plans to compare against. -->

### learning-efficacy.json

```json
{
  "schema_version": 1,
  "learnings": {
    "<learning-id>": {
      "category": "review-escape",
      "scope": "universal",
      "recurrence_count": 0,
      "prevented_count": 0
    }
  },
  "last_updated": null
}
```

<!-- NOTE: schema_version is reserved for future migration use (not validated at runtime).
     created/last_recurrence/last_prevented are not currently set by any script —
     add them back when the learning lifecycle tracking feature is implemented. -->

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
          "severity": "critical | high | medium | low | info",
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
