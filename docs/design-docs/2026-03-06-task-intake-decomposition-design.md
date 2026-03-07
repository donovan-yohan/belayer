# Design: Task Intake & Decomposition (Goal 5)

## Overview

This goal implements the full task intake pipeline: text/Jira input, context sufficiency checking with interactive brainstorm, instance-aware decomposition, and multi-ticket grouping.

## Architecture

### Intake Pipeline (pre-coordinator, CLI-level)

```
User Input (text or --jira)
       |
       v
  Parse & Group (multi-ticket → single task)
       |
       v
  Sufficiency Check (agentic node)
       |
   sufficient? ─── yes ──> Create Task → Start Coordinator
       |
      no
       |
       v
  Interactive Brainstorm (CLI Q&A loop)
       |
       v
  Enriched Description → Create Task → Start Coordinator
```

The sufficiency check and brainstorm must happen at the **CLI level** (not in the coordinator) because brainstorm requires interactive stdin/stdout. The coordinator runs autonomously.

### New Package: `internal/intake/`

| File | Purpose |
|------|---------|
| `intake.go` | `Pipeline` struct: orchestrates text/jira parsing, sufficiency check, brainstorm |
| `intake_test.go` | Unit tests with mock agentic execution |

### Modified Files

| File | Changes |
|------|---------|
| `internal/cli/task.go` | Use `intake.Pipeline` before creating task; pass `--jira` as comma-separated list |
| `internal/coordinator/coordinator.go` | Make decomposition instance-aware (include available repo names in prompt); add `sufficiency_done` flag to skip redundant check |
| `internal/model/types.go` | Add `EnrichedDescription` field to Task; add `TaskStatusSufficiencyChecked` |

## Design Decisions

### 1. Sufficiency check at CLI vs coordinator level

**Decision**: Run at CLI level.

The brainstorm loop requires interactive I/O (stdin/stdout Q&A). The coordinator runs as an autonomous polling loop with no user interaction. Therefore, sufficiency + brainstorm must happen before the task is created and the coordinator is started.

The coordinator's existing sufficiency check remains but is skipped when the task's `source` indicates it was already checked (via a `sufficiency_checked` field on the task).

### 2. Jira ticket handling

**Decision**: Parse comma-separated ticket IDs, format them into a structured description. No Jira API integration in this goal — external ticket content must be provided by the user or enriched via brainstorm.

Format: `belayer task create --jira PROJ-123,PROJ-456`

The description becomes:
```
Jira tickets: PROJ-123, PROJ-456
[User can provide additional context via brainstorm if sufficiency check identifies gaps]
```

Future goals can add Jira REST API integration to auto-fetch ticket details.

### 3. Interactive brainstorm

**Decision**: Simple CLI Q&A loop. The sufficiency agentic node returns gaps (list of questions). Each gap is presented to the user, who provides an answer. Answers are appended to the task description to create an enriched spec.

The brainstorm can be skipped with `--no-brainstorm` flag for non-interactive/CI usage.

### 4. Instance-aware decomposition

**Decision**: The decomposition prompt includes the list of available repos from the instance config. This constrains the agentic node to only decompose into repos that actually exist in the instance.

### 5. Multi-ticket grouping

**Decision**: Multiple `--jira` ticket IDs create a single task with all tickets referenced. The description includes all ticket IDs. The source_ref field stores the comma-separated list.

### 6. Task enrichment storage

**Decision**: Rather than adding a new field, the enriched description (after brainstorm) replaces the original `description` field on the task. The original input is preserved in the `source_ref` for text inputs or stays as Jira IDs for Jira inputs. A new boolean column `sufficiency_checked` in the tasks table tracks whether pre-coordinator sufficiency was done.

## Agentic Node Prompts

### Sufficiency Check (enhanced)
```
You are a task sufficiency checker for a multi-repo coding orchestrator.

Evaluate whether this task description has enough context to be decomposed
into per-repo implementation specs.

Available repos: {repo_names}

Task description:
{description}

Respond with JSON:
{
  "sufficient": true/false,
  "gaps": ["specific question about missing context"],
  "confidence": 0.0-1.0
}
```

### Decomposition (enhanced, instance-aware)
```
You are a task decomposer for a multi-repo coding orchestrator.

Break this task into per-repo implementation specs. You MUST only use
repos from the available list. Not all repos need to be included —
only those relevant to the task.

Available repos: {repo_names}

Task description:
{description}

Respond with JSON:
{
  "repos": [
    {"name": "repo-name", "spec": "detailed implementation spec"}
  ]
}
```

## Schema Change

New migration `003_task_intake.sql`:
```sql
ALTER TABLE tasks ADD COLUMN sufficiency_checked INTEGER DEFAULT 0;
```

## Test Strategy

- **Unit tests**: `internal/intake/` with mock agentic execution (no real claude calls)
- **Integration**: Enhanced mock claude script to handle brainstorm-related prompts
- **CLI test**: Verify `task create` with various flag combinations
