---
description: Compile research-notes.md into a durable research.md summary
allowed-tools: ["Bash", "Read", "Write", "Glob", "Grep"]
---

Use this command when the working notes are mature enough to condense into a durable research summary.

## Resolve the research root

Prefer the research root explicitly named in the session context:

- In setter sessions, `CLAUDE.md` defines the crag docs directory to use.
- In explorer sessions, `CLAUDE.md` defines the explorer workspace to use.

If the session does not explicitly name the research root, detect it in this order:

1. Reuse the directory that already contains `research-notes.md` if one exists.
2. If `BELAYER_CRAG` is set and `~/.belayer/crags/$BELAYER_CRAG/docs` exists, use that docs directory.
3. Otherwise use the current working directory.

Use these files inside the resolved research root:

- `research-notes.md` — source material to summarize
- `research.md` — compiled output written by this command

## Workflow

1. Read the full `research-notes.md`.
2. If `research-notes.md` does not exist yet, do not invent content. Tell the user there is nothing to summarize yet and point them to `/blr-research` or `/blr-research-url`.
3. Distill the notes into a clear `research.md` that captures:
   - the research question or goal
   - major findings and patterns
   - decisions or recommendations the research supports
   - unresolved questions
   - notable sources or references
4. Rewrite `research.md` as the current compiled summary. Do not append to old summaries unless the user explicitly asks for versioned summaries.
5. Leave `research-notes.md` intact as the incremental working log.

## Suggested Summary Structure

```md
# Research Summary

## Goal

## Key Findings

## Recommendations

## Open Questions

## Sources
```

## Output

Summarize the compiled research in the conversation and state exactly where `research.md` was written.

## Next Steps (say this exactly)

> Next Steps:
>
> 1. `/blr-phase-plan` — capture the phase structure, repo split, and candidate problems
> 2. `/blr-draft-create` — turn the next candidate into `spec.md` + `climbs.json`
> 3. `/blr-draft-review <draft-id>` — iterate on a draft and publish it when it is ready
