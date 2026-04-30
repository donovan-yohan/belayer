# Artifact Schemas

Belayer artifacts are durable files registered with the daemon by `kind`, `path`,
`producer`, and `summary`. The daemon does not parse the file content today.
These schemas define the expected content shape for organization-mode artifacts
so agents, prompts, tests, and future UI consumers can agree on names before the
runtime grows typed storage.

Schema files live under `docs/artifact-schemas/`.

## Current Runtime Envelope

The runtime stores this metadata for every artifact:

```json
{
  "kind": "org-plan",
  "path": "artifacts/org-plan.json",
  "producer": "supervisor",
  "summary": "Task plan and gate assignments"
}
```

The artifact file at `path` should then follow the schema for its `kind`.

## Organization Artifact Kinds

| Kind | Schema | Producer | Purpose |
|------|--------|----------|---------|
| `org-plan` | [`org-plan.schema.json`](artifact-schemas/org-plan.schema.json) | lead talent | Task graph, assignments, dependencies, and gate plan |
| `gate-result` | [`gate-result.schema.json`](artifact-schemas/gate-result.schema.json) | gate talent | Generic verdict from QA, reviewer, PM, continuity editor, or any custom gate |
| `org-retro` | [`org-retro.schema.json`](artifact-schemas/org-retro.schema.json) | lead talent | Lessons, bottlenecks, and catalog/prompt follow-ups after a run |
| `talent-evaluation` | [`talent-evaluation.schema.json`](artifact-schemas/talent-evaluation.schema.json) | lead or gate talent | Per-run performance evidence and growth recommendations for one talent |
| `world-state` | [`world-state.schema.json`](artifact-schemas/world-state.schema.json) | storyteller or lorekeeper | Durable state snapshot for story-world runs |
| `continuity-report` | [`continuity-report.schema.json`](artifact-schemas/continuity-report.schema.json) | continuity gate | Story consistency verdict and required fixes |

Existing software artifacts such as `spec`, `design-doc`, `qa-report`,
`review-report`, and `verification-report` remain valid. They can be wrapped or
summarized by `gate-result` when a run needs a uniform gate contract.

## Conventions

- Use hyphenated kind names.
- Use JSON for machine-readable org artifacts unless a human-readable markdown
  report is explicitly more useful.
- Keep `schema_version` at the top of every JSON artifact.
- Put evidence paths in arrays, not prose, so later consumers can link them.
- Use `verdict: "pass" | "pass-with-notes" | "fail" | "blocked"` for gates.
- Preserve role neutrality: do not require `qa`, `reviewer`, or `pm` fields in
  generic schemas.
- Treat growth fields as recommendations unless a reviewed promotion applies
  them to `~/.belayer/crags/` or `~/.belayer/talent-catalog/`.

## Gate Result Example

```json
{
  "schema_version": "belayer-gate-result/v1",
  "gate_id": "runtime-qa",
  "stage": "task",
  "authority": "blocking",
  "producer": "qa",
  "subject": {
    "task_id": "task-2",
    "artifact_paths": ["artifacts/runtime-notes.md"]
  },
  "verdict": "pass-with-notes",
  "findings": [
    {
      "severity": "medium",
      "summary": "Manual mobile viewport check still pending",
      "evidence": ["artifacts/screenshots/desktop.png"]
    }
  ],
  "required_fixes": [],
  "checked_at": "2026-04-30T00:00:00Z"
}
```

## Promotion Rule

Do not add database tables for these artifacts until proof climbs show a repeated
query the file registry cannot answer well. Promote fields only when operators
or dashboards need indexed access across sessions.

Do not let normal climbs silently mutate global crag knowledge. Climbs produce
`talent-evaluation` and `org-retro` artifacts. A later reviewed promotion step
may apply selected lessons to `~/.belayer/crags/<crag-name>/` or
`~/.belayer/talent-catalog/<category>/`.
