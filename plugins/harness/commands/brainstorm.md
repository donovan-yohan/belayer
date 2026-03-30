---
description: Use when starting new feature work, creative design, or when user says "brainstorm", "design a feature", or "let's think about"
---

# Brainstorm

Design through collaborative dialogue, saved as a versioned design document in the 3-tier documentation system.

## Usage

```
/harness:brainstorm                    # Start brainstorming
/harness:brainstorm add user auth      # Brainstorm with initial topic
```

## Invocation

**IMMEDIATELY execute this workflow:**

1. Verify the project has been initialized (check for "Documentation Map" with "When to look here" column in CLAUDE.md). If not, suggest running `/harness:init` first.

2. Read `docs/DESIGN.md` and `docs/design-docs/index.md` to understand existing design context. This grounds the brainstorming in what already exists.

2.5. **Surface past learnings** (if available):
   - Follow the consultation pattern defined in `references/learnings-format.md` § "Consulting Learnings"
   - Match learnings against the brainstorm topic
   - Surface the top 3 most relevant learnings before starting the brainstorm dialogue (using the output format from `references/learnings-format.md`)
   - Record the IDs of consulted learnings for inclusion in the design doc frontmatter (step 3's HARNESS_OVERRIDES `consulted-learnings` field)
   - If LEARNINGS.md doesn't exist or has no active learnings, skip silently

3. **Invoke `superpowers:brainstorming`** using the Skill tool: `Skill("superpowers:brainstorming")`. Then follow the loaded skill's full process (explore context, clarify questions, propose approaches, present design). You MUST use the Skill tool — do not replicate the brainstorming methodology from memory.

   <HARNESS_OVERRIDES>
   The following overrides REPLACE conflicting instructions from superpowers:brainstorming.
   These take ABSOLUTE PRECEDENCE over any path, save location, or handoff instruction in that skill:

   - **Save location:** Save specs to `docs/design-docs/{YYYY-MM-DD}-{kebab-name}-design.md` — NOT `docs/superpowers/specs/`. This is non-negotiable.
   - **Handoff:** Do NOT invoke `writing-plans` or any other skill at the end. Do NOT treat "invoke writing-plans" as a terminal state. Instead, after writing the design doc, proceed to step 4 below.
   - **Spec Review Loop:** When the brainstorming skill dispatches its spec-document-reviewer subagent, the reviewer's `[SPEC_FILE_PATH]` must point to `docs/design-docs/`, not `docs/superpowers/specs/`.
   - **Visual Companion:** Skip the visual companion offer — harness does not ship the brainstorm server.
   - **Frontmatter:** Every design doc written to `docs/design-docs/` MUST start with YAML frontmatter:
     ```yaml
     ---
     status: current
     created: {YYYY-MM-DD}
     branch: {result of `git branch --show-current`}
     supersedes:
     implemented-by:
     consulted-learnings: [{learning IDs from step 2.5, or empty}]
     ---
     ```
     Insert this frontmatter block at the very top of the file, before the H1 title. The `branch` field must be the actual current git branch, not a placeholder.
   </HARNESS_OVERRIDES>

4. **You MUST update `docs/design-docs/index.md`** immediately after writing the design doc — this is not optional. Append a line under **Current Designs**:
   ```markdown
   - [{date}-{name}-design]({date}-{name}-design.md) — {one-line purpose} ({date})
   ```
   After writing the entry, **read back `docs/design-docs/index.md`** and verify the new entry appears. If the entry is missing after read-back, write it again and re-verify. If the index file doesn't exist, create it with a `## Current Designs` header and the entry. Skipping this step is the #1 cause of orphaned design docs.

5. If the design introduces new principles, patterns, or significant decisions, update `docs/DESIGN.md`:
   - Add to the "Current State" bullets if a new pattern was established
   - Add to the "Key Decisions" table if a non-trivial decision was made

5.5. **Update run-state** (if `.harness/` runtime exists):
    ```bash
    HARNESS_DIR=$(bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-resolve-dir.sh --repo-root .)
    [ -n "$HARNESS_DIR" ] && bash ${CLAUDE_PLUGIN_ROOT}/scripts/harness-update-state.sh \
      --harness-dir "$HARNESS_DIR" \
      --phase "brainstorm" \
      --design-doc "docs/design-docs/{filename}.md" \
      --branch "$(git branch --show-current)"
    ```

6. Guide user to next step:
   ```
   Design saved to: docs/design-docs/{filename}.md

   ## Next Steps

   1. `/harness:plan` — Create the implementation plan from this design
   2. `/harness:orchestrate` — Execute the plan with agent teams
   3. `/harness:complete` — Reflect, review, and create PR

   Run `/harness:plan` to continue.
   ```
