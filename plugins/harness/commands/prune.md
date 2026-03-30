---
description: Use when auditing docs for staleness, broken links, or bloat. Also use when user says "docs feel stale", "prune docs", or when CLAUDE.md exceeds 120 lines.
---

# Prune

Audit documentation for staleness, broken links, orphaned guides, and bloat. Produces a health report and can apply fixes.

## Usage

```
/harness:prune                      # Full documentation audit with fix suggestions
/harness:prune --fix                # Audit and auto-apply safe fixes
/harness:prune --archive-completed  # Bulk-archive all completed plans (100% tasks ✓ or Status: Complete)
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
| Completed plans not archived (100% tasks ✓ in active/) | warn |
| Completed plan header in active/ directory (Status: Complete) | error |
| Plan header/checkbox state mismatch | warn |
| Abandoned plans (0% done, 14+ days old) | warn |
| Stale active plans (30+ days, partial progress) | warn |
| Missing design-docs/index.md entries | warn |
| PLANS.md phantom entry (entry exists, file doesn't) | error |
| PLANS.md undocumented plan (file exists, entry doesn't) | warn |
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
| LEARNINGS.md contains legacy `L-NNN` sequential IDs (should be `L-YYYYMMDD-slug`) | warn |
| Index files use legacy markdown table format (should be bullet lists) | warn |

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
    - Migrate legacy `L-NNN` learning IDs to `L-YYYYMMDD-slug` format (using date from source metadata)
    - Convert legacy table-format index files to bullet list format
  - For destructive fixes (deleting files, modifying CLAUDE.md), still ask for confirmation

  If no --fix flag:
  - Present the report and ask: "Would you like me to fix the errors and warnings automatically?"
  - Apply fixes only if user approves

  If --archive-completed flag:
  - Scan docs/exec-plans/active/ for plans that are complete:
    - 100% of Progress checkboxes are [x], OR
    - Status header value is exactly "Complete" or "Completed" (case-insensitive; do not treat "Incomplete" or other variants as complete)
  - For each completed plan found:
    1. Update Status header to "Completed" with today's date
    2. Move file from docs/exec-plans/active/ to docs/exec-plans/completed/
    3. Update docs/PLANS.md: move entry from Active to Completed table
    4. Update the source design doc's frontmatter: set `status: implemented` and `implemented-by: docs/exec-plans/completed/{file}` (if source design doc exists)
    5. Update docs/design-docs/index.md: move entry from Current to Archived. If the index lacks Current/Archived sections, add them following the pattern in /harness:complete
  - Report all plans archived with a summary
  - If no completed plans found, report "No completed plans to archive"
  - This is a batch operation — does not run verification gates or retrospectives (use /harness:complete for individual plans that need those)
```

## Quick Health Mode

When invoked from a health-check context (e.g., at session start), prune can run in quick mode:

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
