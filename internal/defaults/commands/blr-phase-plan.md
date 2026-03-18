---
description: Turn research and current context into a phases.md plan with repo and draft candidates
argument-hint: "[focus area or milestone]"
allowed-tools: ["Bash", "Read", "Write", "Glob", "Grep"]
---

Use this command when the research is strong enough to break the initiative into phases and candidate belayer problems.

## Resolve the planning context

Prefer the planning root explicitly named in the session context:

- In setter sessions, `CLAUDE.md` defines the crag docs directory and current draft root.
- In explorer sessions, `CLAUDE.md` defines the explorer workspace to use.

If the session does not explicitly name the planning root, detect it in this order:

1. Reuse the directory that already contains `phases.md`, `research.md`, or `research-notes.md`.
2. If `BELAYER_CRAG` is set, use `~/.belayer/crags/$BELAYER_CRAG/docs`.
3. Otherwise use the current working directory.

Also resolve the draft namespace for downstream draft creation:

1. If `BELAYER_CRAG` is set, use that as the namespace.
2. If the current workspace lives under `~/.belayer/explorer/<name>/`, use that workspace directory name.
3. Otherwise use the strongest stable project or crag name already present in the session.
4. If no stable name can be inferred, keep writing `phases.md` but call out that draft creation should wait until the project or crag name is settled.

Use these files inside the resolved planning root:

- `research-notes.md`
- `research.md`
- `phases.md`

Downstream draft directories should live at:

- `~/.belayer/drafts/<draft-namespace>/problems/<nnn>/`

## Workflow

1. Read `research.md` first if it exists, then fill gaps from `research-notes.md`, the PRD, or the user's latest clarification.
2. If none of those artifacts exist yet, do not invent certainty. Say which source you are planning from and keep assumptions explicit.
3. Write or rewrite `phases.md` in the planning root.
4. Structure `phases.md` so it captures:
   - overall objective and constraints
   - ordered phases or milestones
   - repos or systems touched in each phase
   - candidate belayer problems or drafts in each phase
   - `order` and `depends_on` hints for draft sequencing
   - open questions or risk gates
5. Treat `depends_on` as a planning hint only. It should express preferred ordering, not a publish blocker.
6. Make each candidate problem clear enough that `/blr-draft-create` can turn it into a `spec.md` and `climbs.json` pair.

## Suggested `phases.md` shape

```md
# Phase Plan

## Objective

## Phase 1 - Foundation
- Goal:
- Repos:
- Candidate drafts:
  - Title:
  - Order:
  - Depends on:
  - Source:
  - Notes:

## Phase 2 - ...
```

## Output

Summarize the phase breakdown in the conversation and state exactly where `phases.md` was written.

## Next Steps (say this exactly)

> Next Steps:
>
> 1. `/blr-draft-create` — turn the next phase/problem candidate into `spec.md` + `climbs.json`
> 2. `/blr-draft-list` — inspect the current draft queue and dependency hints
> 3. `/blr-research-summarize` — tighten `research.md` if the phase boundaries are still fuzzy
