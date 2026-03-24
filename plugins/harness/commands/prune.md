---
description: Use when auditing docs for staleness, broken links, or bloat. Also use when user says "docs feel stale", "prune docs", or when CLAUDE.md exceeds 120 lines.
---

# Prune

Audit documentation for staleness, broken links, orphaned guides, and bloat. Produces a health report and can apply fixes.

## Usage

```
/harness:prune               # Full documentation audit with fix suggestions
/harness:prune --fix         # Audit and auto-apply safe fixes
```

## Checks

| Check | Severity |
|-------|----------|
| CLAUDE.md exceeds 120 lines | warn |
| Documentation Map missing "When to look here" column (v1 format) | warn |
| Broken Documentation Map links | error |
| Orphaned Tier 2 files (not in Documentation Map) | warn |
| Orphaned Tier 3 files (not in any index or Deep Docs table) | warn |
| Stale Tier 2/3 docs (90+ days unchanged) | info |
| Stale active plans (30+ days old) | warn |
| Missing design-docs/index.md entries | warn |
| PLANS.md Active Plans table doesn't match exec-plans/active/ | warn |
| Tier 2 Deep Docs tables reference missing files | error |
| Broken cross-references between docs | error |
| Code Map paths that don't exist on filesystem | error |
| Design-docs index lacks Current/Archived separation | warn |
| Superseded design docs without "superseded by" marker | warn |
| PLANS.md references features for deleted modules | warn |
| REVIEW_GUIDANCE.md missing (harness initialized but no review guidance) | warn |
| REVIEW_GUIDANCE.md Deployment Context is empty/placeholder ("unknown") | warn |
| REVIEW_GUIDANCE.md Escape Log entries without matching question in bank | warn |
| REVIEW_GUIDANCE.md question categories with zero escapes after 5+ reviews | info |

## Invocation

**IMMEDIATELY invoke the Task tool:**

```
subagent_type: "harness:harness-pruner"
prompt: |
  Audit the harness documentation system for this project.

  Arguments: [user's arguments]

  ## Instructions

  1. Read CLAUDE.md and verify it has a "Documentation Map" section
  2. Run ALL audit checks (see your agent instructions and the Checks table)
  3. Produce the full prune report with severity, location, and suggested fix for every issue
  4. Calculate health classification (HEALTHY / NEEDS ATTENTION / UNHEALTHY)
  5. Present the report to the user

  If --fix flag is present:
  - After presenting the report, automatically apply safe fixes:
    - Add missing files to docs/design-docs/index.md
    - Remove broken links from Documentation Map
    - Create docs/REVIEW_GUIDANCE.md from default scaffold if missing (read references/adversarial-review-prompt.md for default question bank)
    - Add escape log questions to matching bank categories if orphaned
  - For destructive fixes (deleting files, modifying CLAUDE.md), still ask for confirmation

  If no --fix flag:
  - Present the report and ask: "Would you like me to fix the errors and warnings automatically?"
  - Apply fixes only if user approves
```

## Quick Health Mode

When invoked from a health-check context (e.g., from `/harness:loop` at session start), prune can run in quick mode:

```
/harness:prune --quick
```

In quick mode, the pruner runs only these fast checks:
- Check 1: CLAUDE.md Size
- Check 4: Broken Map Links
- Check 9: Missing Index Entries
- Check 13: Code Map Ghost Paths
- Check 16: Missing Frontmatter
- Check 17: Frontmatter Status Consistency

Output is a single health score line only:
```
Harness health: {N}/10 ({error count} errors, {warning count} warnings)
```

No full report is generated in quick mode. This keeps session-start latency low.
