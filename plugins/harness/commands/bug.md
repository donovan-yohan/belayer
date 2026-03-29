---
description: Use when investigating a bug, diagnosing an error, or when user says "debug", "fix bug", "investigate issue", or "root cause"
---

# Bug

Investigate a bug through systematic debugging, saved as a versioned bug analysis document.

## Usage

```
/harness:bug                              # Start bug investigation
/harness:bug login fails after timeout    # With initial description
```

## Invocation

**IMMEDIATELY execute this workflow:**

1. Verify the project has been initialized (check for "Documentation Map" with "When to look here" column in CLAUDE.md). If not, suggest running `/harness:init` first.

2. Read `docs/bug-analyses/index.md` to understand prior investigations. If the directory doesn't exist, create it with an empty index.

2.5. **Check prior learnings** (if available):
   - Follow the consultation pattern defined in `references/learnings-format.md` § "Consulting Learnings"
   - Match learnings against the bug's affected area and symptoms
   - If relevant learnings are found, surface them before starting systematic debugging — they may accelerate diagnosis
   - Also check for recurrence per the "Recurrence Detection" section in `references/learnings-format.md`: if a prior learning's recommendation directly addresses this bug class, note it explicitly
   - If LEARNINGS.md doesn't exist or has no matches, skip silently

3. **Invoke `superpowers:systematic-debugging`** using the Skill tool: `Skill("superpowers:systematic-debugging")`. Then follow the loaded skill's full debug cycle: reproduce, bisect, hypothesize, verify root cause. You MUST use the Skill tool — do not replicate the debugging methodology from memory.

4. When debugging reaches confirmed root cause, save findings to `docs/bug-analyses/{YYYY-MM-DD}-{kebab-name}-bug-analysis.md`:

    ```markdown
    # Bug Analysis: {Title}

    > **Status**: Confirmed | **Date**: {date}
    > **Severity**: {Critical/High/Medium/Low}
    > **Affected Area**: {module/component}

    ## Symptoms
    - {what the user observed}

    ## Reproduction Steps
    1. {steps to reproduce}

    ## Root Cause
    {confirmed root cause from systematic debugging}

    ## Evidence
    - {code references, logs, test output that confirm the diagnosis}

    ## Impact Assessment
    - {what's affected, blast radius}

    ## Recommended Fix Direction
    {high-level approach — detailed plan comes from /harness:plan}

    ## Architecture Review

    _Populated after root cause confirmation._
    ```

4.5. **Architecture review** — With root cause confirmed, step back and answer: *"Why was it possible for this bug to be written, and how do we prevent it in the future?"*
   - Read `references/architecture-review-prompt.md`. If the file cannot be found, STOP and print: "ERROR: Missing reference file: references/architecture-review-prompt.md. The harness plugin may be installed incorrectly. Architecture review cannot proceed."
   - Conduct the review across all four dimensions: systemic spread, design gap, testing gaps, harness context gaps
   - Replace the placeholder `## Architecture Review` section in the bug analysis document with the completed findings
   - Each dimension produces actionable findings or an explicit "nothing systemic" signal — no forced output, but the section is always written (use "None" templates when clean)
   - These findings directly expand the scope of the fix plan created by `/harness:plan`

4.6. **Write learnings from root cause + architecture review:**
   - Produce one learning per dimension that has actionable findings, using the categories below. Follow the `references/learnings-format.md` spec for format, IDs, and scaffold.
   - Check if `docs/LEARNINGS.md` exists. If not, create it with the scaffold from `references/learnings-format.md` § "LEARNINGS.md Scaffold".
   - Determine the next learning ID: use today's date (`YYYYMMDD`) and a short kebab-case slug from the learning topic. Format: `L-YYYYMMDD-slug`. See `references/learnings-format.md` § "ID Format" for details.

   | Finding dimension | Learning category |
   |-------------------|-------------------|
   | Systemic spread | `patterns` |
   | Design gap | `architecture` |
   | Testing gaps | `testing` |
   | Root cause itself | `debugging` |
   | Harness context gaps | No learning (flagged for the plan) |

   - Only write a learning if the finding is actionable. "None — isolated to this call site" produces no learning for that dimension.
   - `/harness:bug` intentionally does not produce `review-escape` category learnings — review escapes are detected by `/harness:reflect`'s Review Escape Mining phase.
   - Each learning must be forward-looking: "When doing X, always check Y because Z." Not just "X was broken because of Y."
   - Source field: `/harness:bug {YYYY-MM-DD}`

4.7. **Retroactive harness trace** (if `.harness/` runtime exists):

    This is the fitness function for the self-improving harness. When a bug is investigated, trace it back to the harness run that should have caught it.

    a. Resolve the harness runtime directory:
       ```bash
       HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
       ```
       If empty, skip this phase silently.

    b. **Identify the originating run**: Search `.harness/runs/` for the most recent run records on the branch where the bug was introduced. Cross-reference with git blame on the buggy code to find the commit, then match that commit's date/branch against run records.

    c. **Trace the review that missed it**: Read the run record to find if a review phase ran. If yes:
       - Read the review-results.json from that period (if still available in runs/)
       - Determine which review agents ran and what they reported
       - Identify: did any agent flag the area but the finding was dismissed? Or did no agent flag it at all?

    d. **Write retroactive review escape**: If the bug should have been caught by review:
       - Add an entry to `docs/REVIEW_GUIDANCE.md` Escape Log:
         ```markdown
         | {date} | {bug description} | retroactive-trace | {category} | {question} |
         ```
       - Formulate the "what breaks?" question that would catch this bug class
       - Add the question to the appropriate Adversarial Question Bank category

    e. **Update metrics retroactively**: If the originating run and agent are identified:
       ```bash
       bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-write-metrics.sh \
         --harness-dir "$HARNESS_DIR" \
         --metric "review-effectiveness" \
         --agent "{agent-that-missed}" \
         --false-pos 0 \
         --findings 0 \
         --unique 0
       ```
       Note: This increments runs without incrementing findings, worsening the agent's effectiveness ratio. This is intentional — the agent ran but missed the bug.

    f. **Write retroactive learning** with category `review-escape`:
       ```markdown
       ### L-{YYYYMMDD}-{slug}: {what the review missed}
       - status: active
       - category: review-escape
       - scope: {repo|universal}
       - source: /harness:bug {YYYY-MM-DD} (retroactive trace)
       - branch: {current branch}

       {What the review should have checked. Actionable recommendation.}

       ---
       ```

    g. Append to the bug analysis document under a new section:
       ```markdown
       ## Harness Trace

       - **Originating run:** {run record path or "not found"}
       - **Review ran:** {yes/no}
       - **Agents that ran:** {list}
       - **Escape category:** {category}
       - **Retroactive question added:** {yes/no — question text}
       - **Metric impact:** {agent effectiveness ratio before → after}
       ```

    If the originating run cannot be identified (no `.harness/runs/` data for the relevant period), note this in the Harness Trace section as "Insufficient run history — harness trace unavailable" and skip substeps c-f.

5. Update `docs/bug-analyses/index.md` — append a line:
    ```markdown
    - [{date}-{name}-bug-analysis]({date}-{name}-bug-analysis.md) — {one-line summary} ({date})
    ```

6. Guide user to next step:
    ```
    Bug analysis saved to: docs/bug-analyses/{filename}.md

    ## Next Steps

    1. `/harness:plan docs/bug-analyses/{filename}.md` — Create the fix implementation plan
    2. `/harness:orchestrate` — Execute the plan with agent teams
    3. `/harness:complete` — Reflect, review, and create PR

    Run `/harness:plan docs/bug-analyses/{filename}.md` to continue.
    ```

**IMPORTANT:** Do NOT attempt to fix the bug during investigation. The bug command produces a diagnosis + architecture review; `/harness:plan` turns it into an executable fix plan that addresses the instance, systemic spread, and missing guardrails.
