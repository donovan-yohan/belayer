---
description: Run a guided research workflow and append findings to research-notes.md
argument-hint: "[topic or question]"
allowed-tools: ["Bash", "Read", "Write", "Glob", "Grep", "Skill"]
---

Use this command when the user is still exploring a space, validating an idea, or gathering technical or domain context before drafting a belayer problem.

## Resolve the research root

Prefer the research root explicitly named in the session context:

- In setter sessions, `CLAUDE.md` defines the crag docs directory to use.
- In explorer sessions, `CLAUDE.md` defines the explorer workspace to use.

If the session does not explicitly name the research root, detect it in this order:

1. Reuse the directory that already contains `research-notes.md` if one exists.
2. If `BELAYER_CRAG` is set and `~/.belayer/crags/$BELAYER_CRAG/docs` exists, use that docs directory.
3. Otherwise use the current working directory.

Use these files inside the resolved research root:

- `research-notes.md` — append-only working notes
- `research.md` — compiled summary created later by `/blr-research-summarize`

## Workflow

1. Clarify the research question, constraints, and what decision this research is meant to support.
2. Use the strongest brainstorm or discovery skill available in the session as the conversational backbone. Prefer the session's superpowers brainstorm skill when available; otherwise use the local brainstorm workflow.
3. Keep the interaction focused on research output, not a final implementation plan.
4. After each meaningful batch, append to `research-notes.md` instead of replacing it.
5. Each appended section should capture:
   - a timestamp or session marker
   - the question or topic explored
   - key findings
   - assumptions, risks, or caveats
   - open questions or follow-up angles
6. If the user points at external sources that need direct analysis, route to `/blr-research-url`.
7. Do not overwrite `research.md` here. Compilation belongs to `/blr-research-summarize`.

If `research-notes.md` does not exist yet, create it before the first append.

## Append Pattern

Append new sections in this shape:

```md
## 2026-03-17 14:05 - Topic

Context:
- ...

Findings:
- ...

Risks / Unknowns:
- ...

Follow-up:
- ...
```

## Output

Summarize the latest findings in the conversation and state exactly where `research-notes.md` was updated.

## Next Steps (say this exactly)

> Next Steps:
>
> 1. `/blr-research-url <url>` — pull a concrete source into `research-notes.md`
> 2. `/blr-research-summarize` — compile the working notes into `research.md`
> 3. `/blr-phase-plan` — turn the research into phased repo/problem candidates before drafting
