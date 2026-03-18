---
description: Create the next draft problem directory with spec.md and climbs.json
argument-hint: "[draft title | phase reference]"
allowed-tools: ["Bash", "Read", "Write", "Glob", "Grep"]
---

Use this command to turn a phase-plan entry into a draft belayer problem that can be reviewed before publication.

## Resolve the planning context and draft root

Prefer the planning root and draft root explicitly named in the session context:

- In setter sessions, `CLAUDE.md` defines the crag docs directory and current draft root.
- In explorer sessions, `CLAUDE.md` defines the explorer workspace to use.

If the session does not explicitly name the planning root, detect it in this order:

1. Reuse the directory that already contains `phases.md`, `research.md`, or `research-notes.md`.
2. If `BELAYER_CRAG` is set, use `~/.belayer/crags/$BELAYER_CRAG/docs`.
3. Otherwise use the current working directory.

Resolve the draft namespace in this order:

1. If `BELAYER_CRAG` is set, use that as the namespace.
2. If the current workspace lives under `~/.belayer/explorer/<name>/`, use that workspace directory name.
3. Otherwise use the strongest stable project or crag name already present in the session.
4. If the inferred namespace is empty or starts with `_unnamed-`, stop and ask the user to choose the real project or crag name before writing drafts.

Use these files and directories:

- planning inputs: `phases.md`, `research.md`, `research-notes.md`
- drafts root: `~/.belayer/drafts/<draft-namespace>/problems`
- draft files: `~/.belayer/drafts/<draft-namespace>/problems/<nnn>/spec.md` and `climbs.json`

## Workflow

1. Read `phases.md` first when it exists, then use `research.md` or `research-notes.md` to fill context gaps.
2. If the user named a specific phase, title, or problem candidate, use that. Otherwise choose the next uncreated draft candidate by the lowest phase and `order` values you can infer, and say which candidate you selected.
3. Find existing draft directories under the drafts root named as three-digit IDs. Use the next sequential value, starting with `001`.
4. Set `draft_id` equal to that directory ID. This is the stable identifier used by `depends_on`.
5. Create the draft directory and write `spec.md` with YAML frontmatter containing:
   - `draft_id`
   - `phase`
   - `order`
   - `depends_on`
   - `source`
   - `created`
6. Use an empty array for `depends_on` when there are no ordering hints.
7. Write the `spec.md` body with a concrete problem statement, requirements, acceptance criteria, constraints, and any repo-specific notes needed for decomposition.
8. Write a matching `climbs.json` with the intended repo split. In setter sessions, keep repo names limited to the current crag's repos. In explorer sessions, use the planned repo names from `phases.md` and call out any repo assumptions.
9. Keep `depends_on` as an ordering hint only. Publication order is guided by it, but not blocked by it.
10. Do not publish here. Review and publication belong to `/blr-draft-review`.

## Suggested `spec.md` frontmatter

```md
---
draft_id: "001"
phase: "Phase 1 - Foundation"
order: 1
depends_on: []
source: "phases.md"
created: "2026-03-17T15:04:05Z"
---
```

## Output

Summarize the created draft, including the chosen `draft_id`, and state exactly where `spec.md` and `climbs.json` were written.

## Next Steps (say this exactly, substituting the real draft ID)

> Next Steps:
>
> 1. `/blr-draft-review <draft-id>` — iterate on this draft and publish when it is ready
> 2. `/blr-draft-list` — inspect the queue and dependency hints across all drafts
> 3. `/blr-draft-create` — add the next draft in sequence
