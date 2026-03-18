---
description: Use when initializing structured documentation for a repository, when CLAUDE.md exceeds 120 lines, or when user says "set up docs" or "initialize harness"
---

# Init

Transform a monolithic CLAUDE.md into a 3-tier progressive disclosure documentation system.

- **Tier 1: CLAUDE.md** — 60-120 line map with trigger-based navigation
- **Tier 2: docs/ARCHITECTURE.md, DESIGN.md, PLANS.md** — domain summary files
- **Tier 3: docs/design-docs/, exec-plans/, references/** — deep knowledge directories

> "Give the agent a map, not a 1,000-page manual."

## Usage

```
/harness:init              # Full guided extraction
```

## Invocation

**IMMEDIATELY execute this workflow:**

### Phase 1: Analyze

1. Read the project's `CLAUDE.md` and count lines.
2. If under 120 lines and already has a "Documentation Map" section with a "When to look here" column, report "Already initialized (3-tier)" and stop.
3. If it has a "Documentation Map" but NO "When to look here" column, this is a v1 harness — proceed with migration (Phase 4 will handle `docs/guides/` migration).
4. Scan for extractable content by reading CLAUDE.md headers and body:

   **Extract to Tier 2 summary files:**

   | Content Pattern | Tier 2 Target | Tier 3 Directory |
   |----------------|---------------|------------------|
   | Architecture, modules, packages, dependencies | `docs/ARCHITECTURE.md` | None (self-contained) |
   | Design principles, patterns, conventions, core beliefs | `docs/DESIGN.md` | `docs/design-docs/` |
   | Plans, active work, tech debt | `docs/PLANS.md` | `docs/exec-plans/` |

   **Discover extra Tier 2 files based on repo content:**

   | Trigger | Tier 2 Target |
   |---------|---------------|
   | Auth code, encryption, security patterns found in repo | `docs/SECURITY.md` |
   | Frontend framework code (React, Vue, etc.) found | `docs/FRONTEND.md` |
   | Testing patterns, quality standards, coverage targets | `docs/QUALITY.md` |
   | Other significant domains with substantial CLAUDE.md content | `docs/{DOMAIN}.md` |

5. **Extract Quick Reference commands** — scan CLAUDE.md for build/test/lint/format/run commands.

6. **Extract Key Patterns** — identify critical conventions (max 10) that agents MUST know.

7. Present the extraction plan to the user:
   ```
   ## Extraction Plan

   CLAUDE.md is {N} lines. Proposing 3-tier structure:

   ### Tier 2 Domain Summaries
   | File | Content Source | ~Lines |
   |------|---------------|--------|
   | docs/ARCHITECTURE.md | {sections} | ~{N} |
   | docs/DESIGN.md | {sections} | ~{N} |
   | docs/PLANS.md | (generated from exec-plans/) | ~20 |
   | docs/{DISCOVERED}.md | {sections} | ~{N} |

   ### Tier 3 Deep Knowledge
   - docs/design-docs/ (index.md + core-beliefs.md)
   - docs/exec-plans/active/ and completed/
   - docs/references/

   ### CLAUDE.md Map
   - Quick Reference: {N} commands
   - Key Patterns: {N} items
   - Documentation Map: {N} categories

   Remaining CLAUDE.md: ~{N} lines

   Proceed? (y/n)
   ```

### Phase 2: Scaffold Core Structure

8. Create directories:
   ```bash
   mkdir -p docs/design-docs docs/bug-analyses docs/refactor-scopes docs/exec-plans/active docs/exec-plans/completed docs/references
   ```

9. Generate `docs/ARCHITECTURE.md` using the matklad format. Analyze the project's directory structure, key modules, and build system to produce:

   ```markdown
   # Architecture

   This document describes the high-level architecture of {project}.
   If you want to familiarize yourself with the codebase, you are in the right place.

   ## Bird's Eye View

   {2-3 paragraphs derived from CLAUDE.md project description and repo analysis:
    what problem does this solve, what goes in, what comes out}

   ## Code Map

   This section talks briefly about various important directories and data structures.

   ### `{module/directory}`

   {What it does, why it exists. 2-4 sentences derived from CLAUDE.md and directory analysis.}

   **Architecture Invariant:** {What this module deliberately does NOT do or depend on — infer from existing patterns, dependency structure, and any stated conventions}

   {Repeat for each significant module/directory}

   ## Cross-Cutting Concerns

   {Extract from CLAUDE.md: error handling patterns, testing philosophy, logging, build system, etc.}
   ```

   **Rules for generating ARCHITECTURE.md:**
   - Name files, modules, types — but don't hyperlink (links rot). Say "see `AgentLoader`" and let the reader use symbol search.
   - Architecture Invariants — call out what something deliberately does NOT do (absences are invisible in code)
   - API Boundaries — mark where layers meet and what contracts exist
   - Codemap only, not atlas — what each module is for, not how it works internally
   - Keep short — every contributor/agent re-reads this. Brevity matters.
   - Stable content only — things unlikely to change frequently

10. Generate `docs/DESIGN.md` following the Tier 2 domain summary convention:

    ```markdown
    # Design

    {2-3 sentence overview: design philosophy of this project}

    ## Current State

    - {3-5 bullet points extracted from CLAUDE.md about current design patterns/conventions}

    ## Key Decisions

    | Decision | Rationale | Source |
    |----------|-----------|--------|
    | {extracted from CLAUDE.md or ADRs if they exist} | {why} | {reference} |

    ## Deep Docs

    | Document | Purpose |
    |----------|---------|
    | `design-docs/core-beliefs.md` | Agent-first operating principles |

    ## See Also

    - [Architecture](ARCHITECTURE.md) — module boundaries and invariants
    - [Plans](PLANS.md) — active and completed execution plans
    ```

11. Generate `docs/PLANS.md`:

    ```markdown
    # Plans

    Execution plans for active and completed work.

    ## Current State

    - {List any existing active plans, or "No active plans"}

    ## Active Plans

    | Plan | Created | Status |
    |------|---------|--------|
    | {scan exec-plans/active/ for files} | {date} | Active |

    ## Completed Plans

    | Plan | Created | Completed |
    |------|---------|-----------|
    | {scan exec-plans/completed/ for files} | {date} | {date} |

    ## Tech Debt

    {If a tech-debt-tracker.md exists, summarize. Otherwise: "No tech debt tracked yet."}
    ```

12. Generate `docs/design-docs/index.md`:

    ```markdown
    # Design Documents

    | Document | Purpose | Last Updated |
    |----------|---------|--------------|
    | [core-beliefs.md](core-beliefs.md) | Agent-first operating principles | {date} |
    ```

12a. Generate `docs/bug-analyses/index.md`:

    ```markdown
    # Bug Analyses

    | Document | Summary | Date |
    |----------|---------|------|
    ```

12b. Generate `docs/refactor-scopes/index.md`:

    ```markdown
    # Refactor Scopes

    | Document | Summary | Date |
    |----------|---------|------|
    ```

13. Generate `docs/design-docs/core-beliefs.md`:

    ```markdown
    # Core Beliefs

    Operating principles for this project's documentation and agent workflow.

    ## Repository Is the System of Record

    Anything an agent can't access in-context while running effectively doesn't exist.
    Knowledge that lives in chat threads, documents, or people's heads is not accessible.
    Repository-local, versioned artifacts (code, markdown, schemas, executable plans) are all the agent can see.

    ## CLAUDE.md Is a Map, Not a Manual

    CLAUDE.md stays under 120 lines. It tells agents WHERE to look, not HOW to do things.
    Detailed guidance lives in Tier 2 summaries and Tier 3 deep docs.

    ## Plans Are Living Documents

    Execution plans accumulate progress, surprises, decisions, and drift during implementation.
    They are updated at every stopping point, not reconstructed after the fact.

    ## Reflect Early and Often

    Lightweight reflection runs after each task to catch stale docs immediately.
    Full reflection runs at completion to mine for deeper learnings.

    ## Structure Prevents Decay

    Semantic categories (architecture, design, plans, references) tell agents and humans
    what belongs where. A flat "guides/" directory decays into a junk drawer.
    ```

### Phase 3: Discover Extra Categories

14. Scan the repository for extra domain signals:
    - Check for auth/security directories or code → propose `docs/SECURITY.md`
    - Check for frontend framework (React, Vue, etc.) → propose `docs/FRONTEND.md`
    - Check for testing patterns beyond basic test commands → propose `docs/QUALITY.md`
    - Check CLAUDE.md for other substantial domain sections

15. Present discovered categories to user for approval:
    ```
    ## Discovered Categories

    Based on repository analysis, these additional Tier 2 summaries are recommended:

    | File | Trigger | Approve? |
    |------|---------|----------|
    | docs/SECURITY.md | Found src/auth/, security middleware | y/n |
    | docs/FRONTEND.md | Found React components, Redux store | y/n |
    ```

16. Create approved Tier 2 files following the domain summary convention (same format as DESIGN.md).

### Phase 4: Migrate Existing Content

17. If `docs/guides/` exists (v1 harness migration):
    - Read each guide file
    - Determine which Tier 2/3 category it belongs to
    - Move content to the appropriate location:
      - Architecture-related → merge into `docs/ARCHITECTURE.md`
      - Design/patterns → merge into `docs/DESIGN.md` or create `docs/design-docs/{topic}.md`
      - Testing → merge into `docs/QUALITY.md` or keep as `docs/design-docs/testing.md`
      - Other → create appropriate `docs/design-docs/{topic}.md`
    - Update `docs/design-docs/index.md` with migrated files
    - Remove `docs/guides/` directory after migration (ask user for confirmation)

18. If `docs/plans/` exists (legacy plan location):
    - Identify design docs vs implementation plans by filename pattern (`*-design.md` vs `*-plan.md`)
    - Move design docs to `docs/design-docs/`
    - Move implementation plans to `docs/exec-plans/active/` or `docs/exec-plans/completed/`
    - Ask user which plans are still active vs completed
    - Remove `docs/plans/` after migration (ask user for confirmation)

### Phase 5: Rewrite CLAUDE.md

19. Rewrite CLAUDE.md as the Tier 1 map:

    ```markdown
    # {Project Name}

    {2-3 sentences: what this project is, primary language/framework, role in larger system}

    ## Quick Reference

    | Action | Command |
    |--------|---------|
    | Build | `{extracted build command}` |
    | Test | `{extracted test command}` |
    | Lint | `{extracted lint command}` |
    | Format | `{extracted format command}` |
    | Run | `{extracted run command}` |

    ## Documentation Map

    | Category | Path | When to look here |
    |----------|------|-------------------|
    | Architecture | `docs/ARCHITECTURE.md` | Understanding module boundaries, package layering, where code lives |
    | Design | `docs/DESIGN.md` | Design principles, core beliefs, pattern decisions |
    | Plans | `docs/PLANS.md` | Active work, completed plans, tech debt tracking |
    | Bug Analyses | `docs/bug-analyses/` | When investigating bugs, understanding past root causes |
    | Refactor Scopes | `docs/refactor-scopes/` | When planning refactoring, reviewing past extraction patterns |
    | References | `docs/references/` | External library docs, API specs, llms.txt files |
    | {Discovered} | `docs/{DISCOVERED}.md` | {trigger condition} |
    | ADRs | `docs/adrs/` | Architecture decision records |

    ## Key Patterns

    - {critical convention 1 — rules that, if violated, break things}
    - {critical convention 2}
    - {max 10 bullets}

    ## Workflow

    > brainstorm → plan → orchestrate → review → reflect → complete

    | Step | Command | Purpose |
    |------|---------|---------|
    | 1a | `/harness:brainstorm` | Design through collaborative dialogue |
    | 1b | `/harness:bug` | Investigate and diagnose a bug |
    | 1c | `/harness:refactor` | Scope incremental refactoring |
    | 2 | `/harness:plan` | Create living implementation plan |
    | 3 | `/harness:orchestrate` | Execute with agent teams + micro-reflects |
    | 4 | `/harness:review` | Code simplification + multi-perspective review |
    | 5 | `/harness:reflect` | Full reflection, conversation mining, retrospective |
    | 6 | `/harness:complete` | Archive plan, prune check, and create PR |
    ```

    Target: 60-120 lines. Every line earns its place in the context window.

    **Rules:**
    - Only include rows in Documentation Map for files/dirs that actually exist
    - Omit Risk Contract and ADRs rows if those aren't set up yet
    - Do NOT include hyperlinks in paths (just backtick code format) — links rot

### Phase 6: Integrate

20. Check if `docs/adrs/` exists. If not, inform user and suggest `/adr:init`.

21. Read `~/.claude/CLAUDE.md`. If it does not contain a "Harness Documentation System" section, append:

    ```markdown

    # ═══════════════════════════════════════════════════
    # Harness Documentation System
    # ═══════════════════════════════════════════════════

    **IMPORTANT**: For projects with a Documentation Map in their CLAUDE.md:
    - New features/creative work → use `/harness:brainstorm` (design through dialogue)
    - Create implementation plan → use `/harness:plan` (living execution plan)
    - Execute the plan → use `/harness:orchestrate` (agent teams + micro-reflects)
    - Work complete → use `/harness:complete` (reflect + review + PR)
    - Quick doc check → use `/harness:reflect` (lightweight, any time)
    - Docs feel stale/bloated → use `/harness:prune` (audit and fix)
    - Adding project knowledge → update the appropriate `docs/*.md` file, NOT CLAUDE.md
    - CLAUDE.md is a **map**, not a manual — keep under 120 lines
    ```

    If the section exists but uses the old routing (references `docs/guides/` or misses brainstorm/orchestrate), update it in place.

### Phase 7: Report

22. Output summary:
    ```
    ## Harness Initialized (3-Tier)

    ### Structure Created
    - Tier 1: CLAUDE.md ({N} lines)
    - Tier 2: {list of docs/*.md summary files}
    - Tier 3: docs/design-docs/, docs/exec-plans/, docs/references/

    ### Content Migrated
    - {N} guide files migrated from docs/guides/ (if applicable)
    - {N} plans migrated from docs/plans/ (if applicable)

    ### Integration
    - Global routing: {installed | updated | already present}
    - ADRs: {already exist | suggest running /adr:init}

    ### Files Created
    - docs/ARCHITECTURE.md
    - docs/DESIGN.md
    - docs/PLANS.md
    - docs/design-docs/index.md
    - docs/design-docs/core-beliefs.md
    - {any discovered Tier 2 files}

    ### Next Steps
    - `/harness:brainstorm` — start a new feature design
    - `/harness:prune` — verify docs health
    ```
