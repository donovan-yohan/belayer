---
description: Use when implementation is done and code needs quality review, when user says "review the code", "check the code", or after /harness:orchestrate completes all tasks
---

# Review

Multi-agent review loop on local changes using pr-review-toolkit agents. Runs all review agents in parallel, fixes issues, and re-runs failing agents until all pass or max cycles reached. Run after `/harness:orchestrate`, before `/harness:complete`.

**Compatibility note:** This command no longer uses `review-personas.toml` or the previous multi-persona loop configuration. Any remaining references to it in other docs are legacy and will be cleaned up.

## Usage

```
/harness:review                    # Review changes since active plan creation
/harness:review HEAD~5             # Review last 5 commits
```

## Prerequisites

This command requires the **pr-review-toolkit** plugin. Verify it is installed by checking if its agents are available (e.g., `pr-review-toolkit:code-reviewer` appears in the agent list). If not installed, STOP and print:
```
ERROR: Missing required plugin: pr-review-toolkit

Install it:
  /plugins add pr-review-toolkit
```

Optional: The **adr** plugin enables architecture compliance checking in Phase 6. If not installed, Phase 6 is skipped.

## Invocation

**IMMEDIATELY execute this workflow:**

### Phase 1: Determine Scope

1. Determine the diff scope:
   - If a commit reference was provided, use it
   - Otherwise, check for an active plan in `docs/exec-plans/active/`. If one exists, diff from its creation date.
   - Fallback: diff the last 5 commits

2. Gather the full diff and changed file list:
   ```bash
   git diff HEAD~{N} --stat
   git diff HEAD~{N} --name-only
   ```

### Phase 2: Verification Gate

3. **Apply `superpowers:verification-before-completion`** — run the project's verification commands (tests, build, lint, typecheck) BEFORE starting review. If verification fails, STOP and fix first. Do not review broken code.

### Phase 3: Adversarial Production Review

Context-isolated adversarial review using `claude -p`. This phase catches production failure patterns (thundering herds, resource exhaustion, distributed coordination bugs) that open-ended subagent review misses. The key: the reviewer gets **only the diff and targeted questions** — no conversation history, no knowledge of intent.

4. Check if `docs/REVIEW_GUIDANCE.md` exists. If it does not exist, generate it now using the default scaffold (see `/harness:init` Phase 2 step 8.7 for the scaffold template, or read `references/adversarial-review-prompt.md` for the default question bank). Commit it:
   ```bash
   git add docs/REVIEW_GUIDANCE.md
   git commit -m "docs: initialize review guidance for adversarial review"
   ```

5. Read `docs/REVIEW_GUIDANCE.md`. Extract:
   - **Deployment Context** section (instances, database, scale, infrastructure)
   - **Adversarial Question Bank** sections (all categories with questions)
   - **Known Non-Issues** section if it exists (to annotate the prompt)

6. **Filter questions by relevance** to the diff:
   - Read the changed file list from Phase 1
   - Match file paths and diff content against question categories:
     - Database/SQL code → include Data Integrity, Concurrency & Scale
     - HTTP handlers/API code → include Concurrency & Scale, Failure Modes
     - Auth/security code → include Security
     - Cron/scheduler code → include Distributed Systems
     - All diffs → include Resource Exhaustion
   - Minimum: always include at least 3 questions even after filtering
   - If filtering removes all questions, use the full bank

7. **Construct the adversarial prompt** using the template from `references/adversarial-review-prompt.md`:
   - Insert deployment context
   - Insert filtered questions
   - Select perspective variants based on deployment context (SRE, Scale, Security, Distributed — these stack)
   - If Known Non-Issues exist, append: "Note: the following have been reviewed and confirmed as non-issues for this project: {list}. Do not report these unless circumstances have materially changed."

8. **Generate the diff for the isolated reviewer** using a unique temp file to avoid collisions with concurrent reviews:
   ```bash
   ADVERSARIAL_DIFF=$(mktemp /tmp/harness-adversarial-XXXXXX.patch)
   git diff HEAD~{N} > "$ADVERSARIAL_DIFF"
   ```
   If the diff is empty, skip the adversarial review with status "skipped — empty diff" and proceed to Phase 4.

9. **Shell out to `claude -p`** with the constructed prompt and diff. Write the prompt to a temp file first to avoid shell escaping issues with deployment context or question content:
   ```bash
   ADVERSARIAL_PROMPT=$(mktemp /tmp/harness-adversarial-prompt-XXXXXX.md)
   # Write the constructed prompt to $ADVERSARIAL_PROMPT
   cat "$ADVERSARIAL_DIFF" | claude -p "$(cat "$ADVERSARIAL_PROMPT")" --output-format text
   ```
   Use `timeout: 300000` (5 minutes) on the Bash call. This is the critical isolation step — no conversation context crosses this boundary.

10. **Parse the adversarial review output:**
    - Look for `VERDICT: FAIL` or `VERDICT: PASS`
    - Extract individual findings with their severity (CRITICAL, HIGH, MEDIUM, LOW)
    - If the output contains no parseable `VERDICT:` line, treat as inconclusive — set status to "inconclusive — could not parse verdict" and report the raw output
    - If the command fails or times out, print a warning: "Adversarial review skipped: {reason}. Proceeding with agent review." Set the adversarial review status to "skipped — {reason}" for the final report

11. **Integrate findings into the review cycle:**
    - **CRITICAL findings:** These MUST be fixed before proceeding to Phase 4. Fix inline, commit, and re-run verification.
    - **HIGH/MEDIUM findings:** Add to the fix queue. These will be addressed alongside Phase 4 agent findings.
    - **LOW findings:** Note in the report but do not block.
    - **PASS verdict:** Proceed to Phase 4 normally.
    - **Inconclusive/skipped:** Proceed to Phase 4. Note in the report.

12. Clean up:
    ```bash
    rm -f "$ADVERSARIAL_DIFF" "$ADVERSARIAL_PROMPT"
    ```

### Phase 4: Review Loop

13. Set `cycle = 1`, `max_cycles = 3`, `failing_agents = all 5 review agents`.

14. **Review cycle loop** — repeat until all pass or `cycle > max_cycles`:

   a. Spawn each agent in `failing_agents` **in parallel** via the Agent tool, passing each the git diff from Phase 1. Use these pr-review-toolkit agent types:

   | Agent | `subagent_type` | Focus |
   |-------|----------------|-------|
   | Code Reviewer | `pr-review-toolkit:code-reviewer` | Code quality, bugs, logic errors, CLAUDE.md adherence |
   | Silent Failure Hunter | `pr-review-toolkit:silent-failure-hunter` | Silent failures, error handling, inappropriate fallbacks |
   | Test Analyzer | `pr-review-toolkit:pr-test-analyzer` | Test coverage completeness, missing edge cases |
   | Type Design Analyzer | `pr-review-toolkit:type-design-analyzer` | Type design, encapsulation, invariant expression |
   | Comment Analyzer | `pr-review-toolkit:comment-analyzer` | Comment accuracy, staleness, maintainability |

   Each agent prompt should include:
   - The full diff (from Phase 1 scope)
   - The changed file list
   - Any HIGH/MEDIUM findings from Phase 3 adversarial review that haven't been fixed yet (so agents don't duplicate effort but can validate fixes)
   - Instruction: "Review these local changes (not a PR). Return your findings in your standard format. End with a verdict: PASS (no critical/important issues) or FAIL (critical/important issues found)."

   b. Wait for all agents to complete. Collect results.

   c. Determine pass/fail for each agent:
      - **PASS**: No critical or important issues reported
      - **FAIL**: Critical or important issues found (code-reviewer confidence ≥80, test-analyzer criticality ≥7, type-design-analyzer ratings ≤4, silent-failure-hunter CRITICAL/HIGH, comment-analyzer Critical Issues)

   d. If all pass → exit loop.

   e. If any fail:
      - Present findings grouped by agent
      - Fix all reported issues inline (edit files directly)
      - Re-run verification (tests/build must still pass after fixes)
      - Commit fixes: `git commit -am "fix: address review findings (cycle {cycle})"`
      - Set `failing_agents = only agents that failed`
      - Increment `cycle`

15. After loop exits:
   - If all passed: review is green
   - If max cycles reached with failures remaining: note unresolved issues

### Phase 5: Code Simplification

16. Spawn the `pr-review-toolkit:code-simplifier` agent scoped to the changed files. This agent modifies code directly — let it make improvements.

17. If the simplifier made changes, commit and re-run verification:
   ```bash
   git add {changed files}
   git commit -m "refactor: simplify code from review"
   ```

### Phase 6: ADR Compliance

18. Check if `docs/ARCHITECTURE.md` exists AND the `adr` plugin is available (i.e., `/adr:review` is a recognized command).

19. **If both exist:**
    - Run `/adr:review` against the diff from Phase 1
    - Report any CRITICAL or WARNING violations
    - If the diff introduces new architectural patterns that aren't covered by existing ADRs, note them for `/harness:reflect` — do NOT create new ADRs during review. The bar for flagging is high: only note patterns that represent genuinely new architectural decisions, not routine implementation choices.

20. **If either is missing:** Skip silently — not every project uses ADRs or has the adr plugin installed.

### Phase 7: Resolution

21. If unresolved issues remain after max cycles:
    - Present options:
      ```
      ## Review: {N} unresolved issues after {max_cycles} cycles

      Options:
      1. Fix now — address remaining findings inline
      2. `/harness:orchestrate` — create tasks for significant fixes
      3. Defer — proceed with findings noted

      Which approach?
      ```

22. If user chooses to fix now, apply fixes and re-run verification.

### Report

23. Output:
    ```
    ## Review Complete

    **Scope:** {diff description}
    **Verification:** {passing — with evidence}
    **Adversarial review:** {PASS | FAIL — N findings (breakdown by severity) | skipped}
    **Review cycles:** {N} of {max_cycles}
    **Agents:** {passed}/5 passed
    **Simplification:** {changes made | no changes needed}
    **ADR compliance:** {N violations | compliant | skipped}

    ### Adversarial Review Findings
    | Severity | Finding | Status |
    |----------|---------|--------|
    | CRITICAL | {title} | fixed |
    | HIGH | {title} | fixed |
    | MEDIUM | {title} | deferred |
    {or "No production failure patterns found."}

    ### Per-Agent Results
    | Agent | Status | Issues Found | Issues Resolved |
    |-------|--------|-------------|-----------------|
    | code-reviewer | pass | 2 | 2 |
    | silent-failure-hunter | pass | 1 | 1 |
    | pr-test-analyzer | pass | 0 | 0 |
    | type-design-analyzer | pass | 0 | 0 |
    | comment-analyzer | fail | 3 | 1 |

    ### Unresolved Issues
    - {any remaining issues, or "None"}

    ## Next Step

    Run `/harness:complete` to archive the plan and commit all changes.
    ```
