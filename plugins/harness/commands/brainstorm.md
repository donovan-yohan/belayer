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

3. **Invoke `superpowers:brainstorming`** with the user's arguments. Follow the brainstorming skill's full process (explore context, clarify questions, propose approaches, present design).

   <HARNESS_OVERRIDES>
   The following overrides REPLACE conflicting instructions from superpowers:brainstorming.
   These take ABSOLUTE PRECEDENCE over any path, save location, or handoff instruction in that skill:

   - **Save location:** Save specs to `docs/design-docs/{YYYY-MM-DD}-{kebab-name}-design.md` — NOT `docs/superpowers/specs/`. This is non-negotiable.
   - **Handoff:** Do NOT invoke `writing-plans` or any other skill at the end. Do NOT treat "invoke writing-plans" as a terminal state. Instead, after writing the design doc, proceed to step 4 below.
   - **Spec Review Loop:** When the brainstorming skill dispatches its spec-document-reviewer subagent, the reviewer's `[SPEC_FILE_PATH]` must point to `docs/design-docs/`, not `docs/superpowers/specs/`.
   - **Visual Companion:** Skip the visual companion offer — harness does not ship the brainstorm server.
   </HARNESS_OVERRIDES>

4. After the design doc is written, update `docs/design-docs/index.md` — add a row:
   ```markdown
   | [{name}-design.md]({date}-{name}-design.md) | {one-line purpose} | {date} |
   ```

5. If the design introduces new principles, patterns, or significant decisions, update `docs/DESIGN.md`:
   - Add to the "Current State" bullets if a new pattern was established
   - Add to the "Key Decisions" table if a non-trivial decision was made

6. Guide user to next step:
   ```
   Design saved to: docs/design-docs/{filename}.md

   ## Next Steps

   1. `/harness:plan` — Create the implementation plan from this design
   2. `/harness:orchestrate` — Execute the plan with agent teams
   3. `/harness:complete` — Reflect, review, and create PR

   Run `/harness:plan` to continue.
   ```
