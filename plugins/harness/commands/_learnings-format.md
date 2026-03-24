# Learnings Format Reference

Shared format spec for LEARNINGS.md entries and design doc frontmatter. Referenced by `init.md`, `brainstorm.md`, `bug.md`, `plan.md`, `reflect.md`, `complete.md`, and any command that reads or writes learnings.

---

## LEARNINGS.md Entry Format

Each learning is an H3 header followed by YAML-style metadata lines, then prose:

```markdown
### L-NNN: {one-line summary}
- status: active
- category: {category}
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

### ID Format

IDs are `L-NNN` — sequential, zero-padded to 3 digits.

To assign the next ID: scan existing H3 headers matching `### L-\d+:`, extract the highest number, and increment by 1. If no entries exist, start at `L-001`.

---

## Reading Learnings

To find active learnings: grep for H3 headers (`^### L-`) and check that the following `status:` line reads `active`.

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
consulted-learnings:   # optional: [L-001, L-003]
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
- **{L-NNN}**: {one-line summary} — {recommendation}
- **{L-NNN}**: {one-line summary} — {recommendation}

These learnings will inform the current task.
```

### Recurrence Detection

When consulting learnings during `/harness:bug`, also check for recurrence: if a learning's recommendation directly addresses the class of bug being investigated, note this explicitly:
- "L-012 recommended always checking X, but this bug is exactly that class — the learning failed to prevent recurrence."
This signals that the learning may need strengthening or that additional guardrails are needed beyond documentation.
