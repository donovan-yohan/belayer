# Learnings Format Reference

Shared format spec for LEARNINGS.md entries and design doc frontmatter. Referenced by `commands/init.md`, `commands/brainstorm.md`, `commands/bug.md`, `commands/plan.md`, `commands/reflect.md`, `commands/complete.md`, and any command that reads or writes learnings.

---

## LEARNINGS.md Entry Format

Each learning is an H3 header followed by YAML-style metadata lines, then prose:

```markdown
### L-YYYYMMDD-slug: {one-line summary}
- status: active
- category: {category}
- scope: {repo|universal}                    # NEW: classification for evolution
- source: {command} {date}
- branch: {branch}

{Description and recommendation. Actionable, not just "X was broken."}

---
```

### Status Vocabulary

| Value | Meaning |
|-------|---------|
| `active` | Learning is current and applies |
| `superseded` | Replaced by a newer entry |

### Category Vocabulary

| Value | Use for |
|-------|---------|
| `architecture` | Module boundaries, data flow, structural decisions |
| `testing` | Test patterns, isolation, coverage strategies |
| `patterns` | Recurring code or design patterns |
| `workflow` | Process, tooling, agent coordination |
| `debugging` | Diagnostic techniques, failure modes |
| `performance` | Latency, throughput, resource usage |
| `review-escape` | Issues that escaped code review, missed by review agents |

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

### ID Format

IDs use date-slug format: `L-YYYYMMDD-slug` where:
- `YYYYMMDD` is the date the learning was captured
- `slug` is a short kebab-case descriptor derived from the learning's topic (2-4 words)

Example: `L-20260320-heartbeat-panic`, `L-20260321-score-then-route`

To assign a new ID: use today's date and a descriptive slug. If two learnings share a date and slug would collide, differentiate the slugs (e.g., `L-20260321-workflow-no-file-io` vs `L-20260321-workflow-id-dedup`). No sequential numbering — this prevents merge conflicts when multiple branches add learnings concurrently.

---

## Reading Learnings

To find active learnings: grep for H3 headers (`^### L-`) and check that the following `status:` line reads `active`.

To find learnings by date: grep for `^### L-YYYYMMDD` (e.g., `^### L-20260321` for all learnings from 2026-03-21).

To match against a topic: compare the `category` field and keyword overlap with the learning's title and body text against the topic being researched.

---

## Writing Learnings

- Always append to the end of the file.
- Add a `---` separator between entries.
- Never modify existing entries inline.
- To supersede an entry: append a new entry, then change the old entry's `status:` line from `active` to `superseded`. Do not alter any other field in the old entry.

---

## Design Doc Frontmatter Spec

All design docs under `docs/design-docs/` should open with this frontmatter block:

```yaml
---
status: current        # current | implemented | superseded | stale
created: YYYY-MM-DD
branch: {branch name}
supersedes:            # optional: relative path to older doc
implemented-by:        # optional: path to exec plan
consulted-learnings:   # optional: [L-20260320-heartbeat-panic, L-20260321-score-then-route]
---
```

### Status Vocabulary

| Value | Meaning |
|-------|---------|
| `current` | Active design being worked against |
| `implemented` | Design was built; see `implemented-by` for the plan |
| `superseded` | Replaced by a newer design doc; see `supersedes` |
| `stale` | No longer accurate; not formally superseded |

---

## LEARNINGS.md Scaffold

When `init.md` generates a new LEARNINGS.md, use exactly this scaffold:

```markdown
# Learnings

Persistent learnings captured across sessions. Append-only, merge-friendly.

Status: `active` | `superseded`
Categories: `architecture` | `testing` | `patterns` | `workflow` | `debugging` | `performance` | `review-escape`

---
```

---

## Consulting Learnings

Shared pattern for reading and surfacing relevant learnings. Referenced by `brainstorm.md` (step 2.5), `bug.md` (step 2.5), and `plan.md` (step 3.5).

### Matching Algorithm

1. Read `docs/LEARNINGS.md`. Filter to entries with `status: active`.
2. Match each learning against the current context using:
   - **Category match:** Compare learning `category` against the affected domain (e.g., a bug in the pipeline executor matches `architecture` and `patterns` learnings)
   - **Keyword overlap:** Check for keyword overlap between the learning title/body and the current topic description (bug description, design doc title, planned modules)
   - **File path match:** If the learning body names specific file paths, check for overlap with the files/modules relevant to the current task
3. Rank by relevance (prefer learnings that match on multiple criteria).
4. Surface the **top 3** most relevant learnings.
5. If LEARNINGS.md doesn't exist or has no active learnings matching the context, skip silently.

### Output Format

When surfacing learnings, use this format:

```
## Relevant Past Learnings

Based on past work in this project:
- **{L-YYYYMMDD-slug}**: {one-line summary} — {recommendation}
- **{L-YYYYMMDD-slug}**: {one-line summary} — {recommendation}

These learnings will inform the current task.
```

### Recurrence Detection

When consulting learnings during `/harness:bug`, also check for recurrence: if a learning's recommendation directly addresses the class of bug being investigated, note this explicitly:
- "L-20260320-heartbeat-panic recommended always checking X, but this bug is exactly that class — the learning failed to prevent recurrence."
This signals that the learning may need strengthening or that additional guardrails are needed beyond documentation.
