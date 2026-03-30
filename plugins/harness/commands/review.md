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

3. **Apply `superpowers:verification-before-completion`** using the Skill tool: `Skill("superpowers:verification-before-completion")`. Follow the loaded skill to run the project's verification commands (tests, build, lint, typecheck) BEFORE starting review. If verification fails, STOP and fix first. Do not review broken code.

### Phase 2.5: Evaluator Pass (optional)

3.1. Resolve the harness runtime directory:
   ```bash
   HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
   ```

3.2. If `HARNESS_DIR` is not empty AND `$HARNESS_DIR/agents/evaluator.md` exists:
   - Read `$HARNESS_DIR/config.yaml`. Check `review.evaluator` is `true`.
   - If enabled, read `$HARNESS_DIR/agents/evaluator.md` — this is the repo-specific evaluator agent definition (independent from the code-review agents, following the GAN pattern from the Anthropic article).
   - Run the evaluator as a separate Agent with the diff from Phase 1:
     ```
     Agent(
       subagent_type="general-purpose",
       prompt="{evaluator.md content}\n\nReview this diff:\n{diff}\n\nReturn findings in structured format: severity, title, location, scenario, impact, fix."
     )
     ```
   - Evaluator findings feed into Phase 4 review loop alongside adversarial findings.

3.3. If `HARNESS_DIR` is empty or `evaluator.md` doesn't exist: skip silently. The evaluator is opt-in.

### Phase 3: Adversarial Production Review

Context-isolated adversarial review using the bundled `scripts/adversarial-review.sh` script. This invokes `claude -p` as a **completely separate OS process** — no conversation context, no plugins, no hooks, no shared state. The reviewer sees only the diff and a targeted adversarial prompt. Output is structured JSON validated against a schema.

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
   - Write the constructed prompt to a temp file via the Write tool:
     ```bash
     ADVERSARIAL_PROMPT=$(mktemp /tmp/harness-adversarial-prompt-XXXXXX.md)
     ```

8. **Generate the diff:**
   ```bash
   ADVERSARIAL_DIFF=$(mktemp /tmp/harness-adversarial-XXXXXX.patch)
   git diff HEAD~{N} > "$ADVERSARIAL_DIFF"
   ```

9. **Run the adversarial review script.** The script handles process isolation (`env -u CLAUDECODE`), `--json-schema` validation, temp file management, timeouts, and error handling. All of that complexity lives in the script, not here.
   ```bash
   ${CLAUDE_PLUGIN_ROOT}/scripts/adversarial-review.sh \
     --prompt-file "$ADVERSARIAL_PROMPT" \
     --diff-file "$ADVERSARIAL_DIFF" \
     --model sonnet \
     --effort max \
     --timeout 300
   ```
   Use `timeout: 360000` (6 minutes, allowing buffer beyond the script's internal 5-minute timeout) on the Bash call.

   **Exit codes:**
   | Code | Meaning | Action |
   |------|---------|--------|
   | 0 | PASS | Proceed to Phase 4 |
   | 1 | FAIL — CRITICAL findings | Must fix before Phase 4 |
   | 2 | FAIL — HIGH/MEDIUM only | Add to fix queue, proceed to Phase 4 |
   | 3 | Inconclusive | Log warning, proceed to Phase 4 |
   | 4 | Error/timeout/skip | Log warning, proceed to Phase 4 |

   The script outputs structured JSON to stdout:
   ```json
   {
     "verdict": "FAIL",
     "findings": [
       {
         "severity": "CRITICAL",
         "title": "Connection pool exhaustion under load",
         "location": "handleRequest()",
         "scenario": "1000 concurrent requests",
         "impact": "Service becomes unresponsive",
         "fix": "Add connection pool limit with timeout"
       }
     ],
     "summary": "Found 1 critical production failure mode."
   }
   ```

10. **Parse the JSON output** and capture the exit code:
    - Read stdout as JSON — no free-text parsing needed
    - If the script exited non-zero with no stdout, check stderr for the error JSON

11. **Integrate findings into the review cycle:**
    - **Exit 1 (CRITICAL):** Fix all CRITICAL findings inline, commit, and re-run verification before proceeding to Phase 4.
    - **Exit 2 (HIGH/MEDIUM):** Add findings to the fix queue. These will be addressed alongside Phase 4 agent findings.
    - **Exit 0 (PASS):** Proceed to Phase 4 normally.
    - **Exit 3/4 (inconclusive/error):** Print a warning with the reason from the JSON output. Proceed to Phase 4 — adversarial review is additive, not blocking.

12. Clean up:
    ```bash
    rm -f "$ADVERSARIAL_DIFF" "$ADVERSARIAL_PROMPT" 2>/dev/null
    ```

### Phase 4: Review Loop

13. Set `cycle = 1`, `max_cycles = 3`, `failing_agents = all 4 review agents`.

14. **Review cycle loop** — repeat until all pass or `cycle > max_cycles`:

   a. Spawn each agent in `failing_agents` **in parallel** via the Agent tool, passing each the git diff from Phase 1. Use these pr-review-toolkit agent types:

   | Agent | `subagent_type` | Focus |
   |-------|----------------|-------|
   | Code Reviewer | `pr-review-toolkit:code-reviewer` | Code quality, bugs, logic errors, CLAUDE.md adherence |
   | Silent Failure Hunter | `pr-review-toolkit:silent-failure-hunter` | Silent failures, error handling, inappropriate fallbacks |
   | Type Design Analyzer | `pr-review-toolkit:type-design-analyzer` | Type design, encapsulation, invariant expression |
   | Learnings Reviewer | `harness:learnings-reviewer` | Checks diff against active learnings for violations |

   Each agent prompt should include:
   - The full diff (from Phase 1 scope)
   - The changed file list
   - Any HIGH/MEDIUM findings from Phase 3 adversarial review that haven't been fixed yet (so agents don't duplicate effort but can validate fixes)
   - Instruction: "Review these local changes (not a PR). Return your findings in your standard format. End with a verdict: PASS (no critical/important issues) or FAIL (critical/important issues found)."

   b. Wait for all agents to complete. Collect results.

   c. Determine pass/fail for each agent:
      - **PASS**: No critical or important issues reported
      - **FAIL**: Critical or important issues found (code-reviewer confidence ≥80, type-design-analyzer ratings ≤4, silent-failure-hunter CRITICAL/HIGH, learnings-reviewer FAIL verdict)

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

### Phase 5: ADR Compliance

16. Check if `docs/ARCHITECTURE.md` exists AND the `adr` plugin is available (i.e., `/adr:review` is a recognized command).

17. **If both exist:**
    - Run `/adr:review` against the diff from Phase 1
    - Report any CRITICAL or WARNING violations
    - If the diff introduces new architectural patterns that aren't covered by existing ADRs, note them for `/harness:reflect` — do NOT create new ADRs during review. The bar for flagging is high: only note patterns that represent genuinely new architectural decisions, not routine implementation choices.

18. **If either is missing:** Skip silently — not every project uses ADRs or has the adr plugin installed.

### Phase 6: Resolution

19. If unresolved issues remain after max cycles:
    - Present options:
      ```
      ## Review: {N} unresolved issues after {max_cycles} cycles

      Options:
      1. Fix now — address remaining findings inline
      2. `/harness:orchestrate` — create tasks for significant fixes
      3. Defer — proceed with findings noted

      Which approach?
      ```

20. If user chooses to fix now, apply fixes and re-run verification.

### Phase 7: Structured Output (if .harness/ exists)

21. Resolve the harness runtime directory:
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    ```

22. If `HARNESS_DIR` is not empty, write structured review results for `/harness:evolve` consumption:

    Build a JSON object from the review cycle results:
    - For each agent that ran: record `ran`, `findings` (with text, severity, file, line, accepted, unique), and `verdict`
    - Record `overall_verdict` and `cycles_run`
    - Write to `$HARNESS_DIR/review-results.json`

    Use python3 to write the JSON. The template below shows the structure — populate all placeholder values (`{PASS|FAIL}`, `{N}`, agent entries) with actual values from the review cycle before running:
    ```bash
    python3 -c "
    import json
    results = {
        'schema_version': 1,
        'session_date': '$(date +%Y-%m-%d)',
        'branch': '$(git branch --show-current)',
        'agents': {
            # Populated from review cycle results
        },
        'overall_verdict': '{PASS|FAIL}',
        'cycles_run': {N}
    }
    with open('$HARNESS_DIR/review-results.json', 'w') as f:
        json.dump(results, f, indent=2)
    "
    ```

    To determine `accepted` and `unique` for each finding:
    - `accepted`: the finding led to a code change (check if the fix commit touched the flagged file/line)
    - `unique`: no other agent reported the same file+line with similar severity

23. If `HARNESS_DIR` is empty: skip. Review works without `.harness/` — structured output is additive.

24. **Update run-state** (if `.harness/` runtime exists, reuse `$HARNESS_DIR` from step 21):
    ```bash
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-update-state.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "review"
    ```

### Report

25. Output:
    ```
    ## Review Complete

    **Scope:** {diff description}
    **Verification:** {passing — with evidence}
    **Adversarial review:** {PASS | FAIL — N findings (breakdown by severity) | skipped}
    **Review cycles:** {N} of {max_cycles}
    **Agents:** {passed}/4 passed
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
    | type-design-analyzer | pass | 0 | 0 |
    | learnings-reviewer | pass | 0 | 0 |

    ### Unresolved Issues
    - {any remaining issues, or "None"}

    ## Next Step

    Run `/harness:complete` to archive the plan and commit all changes.
    ```
