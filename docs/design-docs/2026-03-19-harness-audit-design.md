---
status: implemented
created: 2026-03-19
branch: master
supersedes:
implemented-by: docs/exec-plans/completed/2026-03-19-harness-audit.md
---

# Design: Harness Plugin Audit & Workflow Fix

> CEO review output from /plan-ceo-review on 2026-03-19
> Mode: SELECTIVE EXPANSION | Approach: B (Workflow Fix)

## Problem

Projects struggle with iteration when using the harness to build an MVP then pivot in later branches. Stale harness docs linger despite running /harness:loop and /harness:reflect. References to stale docs mislead agents. Learnings from /harness:bug or other corrections are not persisted and used in a way that makes the agent smarter over time.

## Root Cause Analysis

1. **No doc lifecycle management**: Design docs accumulate with no archival discipline. 31 design docs in 13 days, none marked superseded.
2. **Prune/reflect are opt-in**: They exist but don't run automatically. Agents only invoke them when explicitly asked.
3. **No learning persistence**: Corrections from /harness:bug are conversation-scoped. They don't carry forward to the next session.
4. **No branch awareness**: Docs reference code that may only exist on another branch.
5. **Two parallel knowledge systems**: Belayer's SQLite learnings table works but is isolated from the harness doc system.

## Approach: Workflow Fix (Approach B)

Enhance the harness plugin with:

### Core Scope
1. **Auto-prune on session start** — lightweight staleness detection when harness commands are invoked
2. **Branch-aware context loading** — reflect/prune account for branch divergence
3. **Learning persistence file** — `docs/LEARNINGS.md` (append-only, merge-friendly markdown with YAML-style metadata entries)
4. **Design doc archival discipline** — reflect/complete mark old design docs as superseded

### Accepted Expansions
5. **Learning summary in brainstorm preamble** — surface 2-3 relevant past learnings before design dialogue
6. **Auto-archive completed design docs** — /harness:complete marks design doc as implemented, updates index
7. **Harness health score at session start** — one-line health indicator, suggests /harness:prune if low
8. **Cross-branch learning sync** — learnings in docs/ folder, append-only format minimizes merge conflicts
9. **Stale doc badges in design-docs index** — Status column (Current/Implemented/Superseded/Stale)
10. **Structured YAML frontmatter for all docs** — machine-parseable status, branch, supersedes fields
11. **Consulted-learnings trail** — design doc frontmatter records which learnings influenced the design

## Key Decisions

### LEARNINGS.md Format
Markdown with YAML-style metadata entries. Append-only for merge-friendliness.

```markdown
## Learnings

### L-001: Embedded asset paths must match test expectations
- status: active
- category: testing
- source: /harness:bug 2026-03-17
- branch: master

When adding new embedded assets via embed.FS, the test fixtures must use
the exact same path structure. Discovered during CLI command rename.

---
```

### Design Doc Frontmatter
Minimal YAML frontmatter — just enough for filtering.

```yaml
---
status: current        # current | implemented | superseded | stale
created: 2026-03-19
branch: master
supersedes:            # optional: path to older doc
implemented-by:        # optional: plan path
consulted-learnings:   # optional: [L-001, L-003]
---
```

### Migration Path
- Existing docs with no frontmatter treated as `status: unknown`
- First prune run adds frontmatter retroactively
- No big-bang migration required

## Deferred (TODOS.md)
- **Approach C: Generalized Knowledge Layer** (P2) — extract belayer's SQLite learning system into standalone plugin
- **harness:context read skill** (P3) — smart doc retrieval based on frontmatter queries

## Files to Modify

| File | Change |
|------|--------|
| `plugins/harness/commands/brainstorm.md` | Add learning read preamble |
| `plugins/harness/commands/reflect.md` | Write learnings, update frontmatter status |
| `plugins/harness/commands/complete.md` | Auto-archive design doc, update index |
| `plugins/harness/commands/prune.md` | Update status badges, frontmatter validation |
| `plugins/harness/commands/loop.md` | Integrate health check, learning persistence |
| `plugins/harness/commands/plan.md` | Read frontmatter to skip stale docs |
| `plugins/harness/commands/bug.md` | Write learnings on root cause confirmation |
| `plugins/harness/commands/init.md` | Generate LEARNINGS.md scaffold, frontmatter template |
| New: shared preamble pattern | Health check + learning retrieval logic (DRY) |
