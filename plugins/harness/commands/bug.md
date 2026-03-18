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

3. **Invoke `superpowers:systematic-debugging`** with the user's bug description. Follow the full debug cycle: reproduce, bisect, hypothesize, verify root cause.

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
    ```

5. Update `docs/bug-analyses/index.md` — add a row for the new analysis:
    ```markdown
    | [{name}-bug-analysis.md]({date}-{name}-bug-analysis.md) | {one-line summary} | {date} |
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

**IMPORTANT:** Do NOT attempt to fix the bug during investigation. The bug command produces a diagnosis; `/harness:plan` turns it into an executable fix plan.
