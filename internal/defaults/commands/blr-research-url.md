---
description: Fetch a URL, extract relevant findings, and append them to research-notes.md
argument-hint: "<url> [what to look for]"
allowed-tools: ["Bash", "Read", "Write", "Glob", "Grep"]
---

Use this command to pull specific source material into the active research notes.

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

1. Confirm the URL and the question the user wants answered from it.
2. Fetch the page with the best tool available in the session. Prefer first-party tooling when present; otherwise fall back to a direct CLI fetch such as `curl -L`.
3. Read the content closely enough to extract the claims, evidence, caveats, and any data points relevant to the user's research goal.
4. Create `research-notes.md` if it does not exist yet.
5. Append a new source-focused section to `research-notes.md` instead of rewriting prior notes.
6. Each appended section should capture:
   - a timestamp or session marker
   - the source URL
   - the question being answered
   - key takeaways
   - caveats or trust concerns
   - follow-up questions or cross-checks still needed
7. Leave `research.md` untouched. Summary compilation belongs to `/blr-research-summarize`.

## Append Pattern

Append new sections in this shape:

```md
## 2026-03-17 14:20 - Source review

Source:
- https://example.com/article

Question:
- ...

Key takeaways:
- ...

Caveats:
- ...

Follow-up:
- ...
```

## Output

Summarize the source findings in the conversation and state exactly where `research-notes.md` was updated.

## Next Steps (say this exactly)

> Next Steps:
>
> 1. `/blr-research-url <url>` — add another source to the same `research-notes.md`
> 2. `/blr-research` — continue the broader guided research conversation
> 3. `/blr-research-summarize` — compile the accumulated notes into `research.md`
