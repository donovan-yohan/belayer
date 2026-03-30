---
description: Use when creating an implementation plan from a design doc, bug analysis, or refactor scope, or when user says "create a plan", "plan this", or "write the plan"
---

# Plan

Create a living execution plan from a design document, saved as a versioned artifact with built-in progress tracking.

## Usage

```
/harness:plan                                    # Plan from most recent design doc
/harness:plan docs/design-docs/{file}.md         # Plan from specific design doc
```

## Invocation

**IMMEDIATELY execute this workflow:**

1. Verify the project has been initialized (check for "Documentation Map" with "When to look here" in CLAUDE.md). If not, suggest `/harness:init` first.

2. Locate the design document:
   - If a path argument was provided, use it
   - Otherwise, search for the most recently modified context document across:
     - `docs/design-docs/*-design.md`
     - `docs/bug-analyses/*-bug-analysis.md`
     - `docs/refactor-scopes/*-refactor-scope.md`
   - **Frontmatter filtering:** For each candidate, check if it has YAML frontmatter with a `status` field. Skip documents with `status: implemented`, `status: superseded`, or `status: stale`. Only consider documents with `status: current` or no frontmatter (legacy/unknown).
   - If no eligible context document found, suggest running `/harness:brainstorm`, `/harness:bug`, or `/harness:refactor` first

3. Read the design document fully. Extract:
   - The design title and goal
   - Key decisions made during brainstorming
   - Architecture approach and tech stack
   - Components/tasks implied by the design
   - **If the context document is a refactor scope:** Skip steps already marked `completed` in the scope doc. Plan only the next batch of actionable steps (up to the next async gate). Use `Refactor Scope:` instead of `Design Doc:` in the plan header.

3.5. **Consult learnings** (if available):
   - Follow the consultation pattern defined in `references/learnings-format.md` § "Consulting Learnings"
   - Match learnings against the modules and areas being planned (extracted from the design document in step 3)
   - Surface relevant learnings so the plan can incorporate their recommendations
   - Record consulted learning IDs for inclusion in the plan header (step 5)
   - If LEARNINGS.md doesn't exist or has no matches, skip silently

4. **Invoke `superpowers:writing-plans`** using the Skill tool: `Skill("superpowers:writing-plans")`. Then follow the loaded skill's full process to produce bite-sized TDD tasks with exact file paths, code, and commands. You MUST use the Skill tool — do not write the plan from memory or replicate the planning methodology without loading it.

   <HARNESS_OVERRIDES>
   The following overrides REPLACE conflicting instructions from superpowers:writing-plans.
   These take ABSOLUTE PRECEDENCE over any path, save location, header, or handoff instruction in that skill:

   - **Save location:** Do NOT save to `docs/superpowers/plans/`. The plan will be saved in step 5 below to `docs/exec-plans/active/`.
   - **Plan header:** Do NOT include "use superpowers:subagent-driven-development" or "use superpowers:executing-plans" in the header. The plan header is defined in step 5 below and references `/harness:orchestrate`.
   - **Execution handoff:** Do NOT invoke `subagent-driven-development`, `executing-plans`, or any other execution skill. After producing the plan content, STOP and let step 5 wrap it with the living document format.
   - **Plan Review Loop:** When dispatching the plan-document-reviewer subagent, the reviewer's `[PLAN_FILE_PATH]` must point to `docs/exec-plans/active/`, not `docs/superpowers/plans/`. The `[SPEC_FILE_PATH]` must point to the design doc from step 2 (in `docs/design-docs/`, `docs/bug-analyses/`, or `docs/refactor-scopes/`).
   </HARNESS_OVERRIDES>

5. After superpowers:writing-plans produces its output, wrap it with the living document sections and save to `docs/exec-plans/active/{YYYY-MM-DD}-{kebab-name}.md`:

   ```markdown
   # {Plan Title}

   > **Status**: Active | **Created**: {date} | **Last Updated**: {date}
   > **Design Doc**: `docs/design-docs/{design-doc-filename}` (or **Refactor Scope**: / **Bug Analysis**: with matching path)
   > **Consulted Learnings**: {L-YYYYMMDD-slug, L-YYYYMMDD-slug from step 3.5, or "None"}
   > **For Claude:** Use /harness:orchestrate to execute this plan.

   ## Decision Log

   | Date | Phase | Decision | Rationale |
   |------|-------|----------|-----------|
   | {date} | Design | {decisions from brainstorming design doc} | {rationale} |

   ## Progress

   - [ ] Task 1: {name}
   - [ ] Task 2: {name}
   - [ ] ...

   ## Surprises & Discoveries

   _None yet — updated during execution by /harness:orchestrate._

   ## Plan Drift

   _None yet — updated when tasks deviate from plan during execution._

   ---

   {superpowers:writing-plans output — full tasks with bite-sized steps}

   ---

   ## Outcomes & Retrospective

   _Filled by /harness:complete when work is done._

   **What worked:**
   -

   **What didn't:**
   -

   **Learnings to codify:**
   -
   ```

5.5. **Update run-state** (if `.harness/` runtime exists):
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-update-state.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "plan" \
      --plan "docs/exec-plans/active/{filename}.md" \
      --design-doc "{design-doc-path}"
    ```

6. **Verify the exec plan file exists before updating PLANS.md.** Read or glob for `docs/exec-plans/active/{filename}.md` to confirm the file from step 5 was successfully written. Only after confirming the file exists on disk, update `docs/PLANS.md` — add the new plan to the Active Plans table. If the file doesn't exist, report the write failure to the user, retry writing the file, and re-verify before proceeding. Do NOT add an entry to PLANS.md until the file is confirmed on disk (this prevents phantom entries that reference nonexistent plans).

7. Report:
   ```
   Plan saved to: docs/exec-plans/active/{filename}.md

   ## Next Steps

   1. `/harness:orchestrate` — Execute this plan with agent teams + micro-reflects
   2. `/harness:complete` — When done: reflect, review, and create PR

   Run `/harness:orchestrate` to begin execution.
   ```
