# Harness Plugin Audit & Workflow Fix

> **Status**: Completed | **Created**: 2026-03-19 | **Completed**: 2026-03-19
> **Design Doc**: `docs/design-docs/2026-03-19-harness-audit-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-19 | Design | Approach B (workflow fix) over C (knowledge layer) | Fixes immediate pain, lower risk, keeps door open |
| 2026-03-19 | Design | Markdown+YAML learning format over JSONL | Human-readable in PR review, append-only for merge safety |
| 2026-03-19 | Design | Minimal frontmatter (status/created/branch/supersedes) | Low maintenance overhead, sufficient for filtering |
| 2026-03-19 | Design | Health score as session-start advisory, not blocker | Graceful degradation — never block harness workflow |

## Progress

- [x] Task 1: Create LEARNINGS.md scaffold and format spec _(completed 2026-03-19)_
- [x] Task 2: Add structured frontmatter to brainstorm.md output _(completed 2026-03-19)_
- [x] Task 3: Add learning read preamble to brainstorm.md _(completed 2026-03-19)_
- [x] Task 4: Update bug.md to write learnings on root cause _(completed 2026-03-19)_
- [x] Task 5: Update reflect.md with learning write + frontmatter status updates _(completed 2026-03-19)_
- [x] Task 6: Update complete.md with auto-archive and index update _(completed 2026-03-19)_
- [x] Task 7: Update prune.md with health score, status badges, and frontmatter validation _(completed 2026-03-19)_
- [x] Task 8: Update init.md to generate LEARNINGS.md and frontmatter template _(completed 2026-03-19)_
- [x] Task 9: Update plan.md to filter by frontmatter status _(completed 2026-03-19)_
- [x] Task 10: Update loop.md with health check at start _(completed 2026-03-19)_
- [x] Task 11: Update harness-pruner agent with frontmatter awareness _(completed 2026-03-19)_
- [x] Task 12: Bump plugin version to 3.0.0 _(completed 2026-03-19)_

## Surprises & Discoveries

_None yet — updated during execution by /harness:orchestrate._

## Plan Drift

_None yet — updated when tasks deviate from plan during execution._

---

### Task 1: Create LEARNINGS.md scaffold and format spec

**Goal:** Create the learning persistence file that all other commands will read from and write to.

**Files:** `plugins/harness/commands/_learnings-format.md` (new — shared reference doc)

**Steps:**
1. Create `plugins/harness/commands/_learnings-format.md` as a shared format reference that other commands include by reference. This is NOT a slash command — it's a shared spec. Content:
   - LEARNINGS.md format specification (H3 entries with YAML-style metadata)
   - Status vocabulary: `active | superseded`
   - Category vocabulary: `architecture | testing | patterns | workflow | debugging | performance`
   - ID format: `L-NNN` (sequential, auto-incremented by scanning existing entries)
   - Reading instructions: grep for `status: active`, parse H3 headers
   - Writing instructions: append new entry with `---` separator, auto-increment ID
   - Frontmatter spec for design docs: `status | created | branch | supersedes | implemented-by | consulted-learnings`

**Acceptance criteria:**
- File exists and is well-structured
- Format is documented clearly enough that other command files can reference it

---

### Task 2: Add structured frontmatter to brainstorm.md output

**Goal:** When `/harness:brainstorm` creates a new design doc, it includes YAML frontmatter.

**File:** `plugins/harness/commands/brainstorm.md`

**Steps:**
1. Read the current brainstorm.md
2. In step 3 (HARNESS_OVERRIDES section), add an override for the design doc output format:
   - After the spec is written to `docs/design-docs/`, prepend YAML frontmatter block
   - Frontmatter fields: `status: current`, `created: {YYYY-MM-DD}`, `branch: {current git branch}`, `supersedes:`, `implemented-by:`, `consulted-learnings: []`
3. The branch should be detected via `git branch --show-current`

**Acceptance criteria:**
- brainstorm.md instructs the agent to add frontmatter to new design docs
- Frontmatter uses the exact format from the design doc specification

---

### Task 3: Add learning read preamble to brainstorm.md

**Goal:** When `/harness:brainstorm` starts, it reads LEARNINGS.md and surfaces relevant past learnings.

**File:** `plugins/harness/commands/brainstorm.md`

**Steps:**
1. Add a new step between current step 2 (read DESIGN.md and index) and step 3 (invoke brainstorming):
   - Check if `docs/LEARNINGS.md` exists
   - If it exists, read it and extract all `status: active` entries
   - Match learnings against the brainstorm topic (by category and keyword overlap)
   - Surface top 3 most relevant learnings before the brainstorm dialogue:
     ```
     ## Relevant Past Learnings

     Based on past work in this project:
     - **L-001**: {description} — {recommendation}
     - **L-003**: {description} — {recommendation}

     These learnings will be recorded in the design doc's `consulted-learnings` field.
     ```
   - Record consulted learning IDs in the design doc frontmatter `consulted-learnings` field
2. If LEARNINGS.md doesn't exist, skip silently

**Acceptance criteria:**
- brainstorm.md reads and surfaces learnings when available
- Consulted learning IDs are recorded in the design doc frontmatter

---

### Task 4: Update bug.md to write learnings on root cause

**Goal:** When `/harness:bug` confirms a root cause, it writes a learning entry.

**File:** `plugins/harness/commands/bug.md`

**Steps:**
1. Read current bug.md
2. After step 4 (bug analysis saved), add a new step:
   - Extract the key learning from the root cause and recommended fix direction
   - Read `docs/LEARNINGS.md` to determine next ID (or create file if missing)
   - Append a new learning entry:
     ```markdown
     ---

     ### L-{NNN}: {one-line root cause summary}
     - status: active
     - category: debugging
     - source: /harness:bug {date}
     - branch: {current branch}

     {Root cause description and what to watch for in the future.}
     ```
3. The learning should be actionable — not just "X was broken" but "when doing Y, check Z"

**Acceptance criteria:**
- bug.md writes a learning entry after confirming root cause
- Learning is append-only (no modification of existing entries)

---

### Task 5: Update reflect.md with learning write + frontmatter status updates

**Goal:** `/harness:reflect` mines the conversation for learnings AND updates frontmatter status on touched design docs.

**File:** `plugins/harness/commands/reflect.md`

**Steps:**
1. Read current reflect.md
2. In Phase 5 (Conversation Mining), add after step 14:
   - For each **doc-update** finding that represents a reusable insight (not just a one-off fix), also write it as a learning entry to `docs/LEARNINGS.md`
   - Category should match the domain (architecture, testing, patterns, etc.)
3. Add a new Phase between current Phase 7 and Phase 8 — "Frontmatter Status Update":
   - For each design doc in `docs/design-docs/` that was the source for the current plan:
     - If the plan is being marked complete, update the design doc's frontmatter `status` from `current` to `implemented` and add `implemented-by: {plan path}`
   - For design docs that have been superseded by newer docs on the same topic:
     - Update frontmatter `status` to `superseded` and add `supersedes` reference
4. In Phase 3 (Staleness Check), add frontmatter awareness:
   - When scanning design docs, check frontmatter `status` field
   - Docs with `status: implemented` or `status: superseded` should not be flagged as stale — they're correctly archived
   - Docs with `status: current` that reference deleted code ARE stale
5. Add to the Reflect Report output a "Learnings Written" section

**Acceptance criteria:**
- reflect.md writes conversation-mined learnings to LEARNINGS.md
- reflect.md updates frontmatter status on design docs
- Reflect report includes learnings written count

---

### Task 6: Update complete.md with auto-archive and index update

**Goal:** `/harness:complete` archives the design doc (marks as implemented) and updates the index.

**File:** `plugins/harness/commands/complete.md`

**Steps:**
1. Read current complete.md
2. In Phase 3 (Plan Archival), after moving the plan file, add:
   - Read the plan's `Design Doc:` header to find the source design doc path
   - Update the design doc's frontmatter: `status: implemented`, `implemented-by: docs/exec-plans/completed/{file}`
3. In Phase 5 (Tier 2 Summary Updates), add:
   - Read `docs/design-docs/index.md`
   - If the index doesn't have Current/Archived sections, restructure it:
     - Add `## Current Designs` and `## Archived` headers
     - Move the implemented design doc to the Archived section
     - Add a Status column to the index table
   - If the index already has sections, move the entry to Archived with status "Implemented"

**Acceptance criteria:**
- complete.md updates design doc frontmatter to `status: implemented`
- complete.md restructures index.md with Current/Archived sections
- complete.md moves archived design doc entry to Archived section

---

### Task 7: Update prune.md and harness-pruner agent with health score, status badges, and frontmatter validation

**Goal:** Prune gains health score output, frontmatter validation, and status badge management.

**Files:** `plugins/harness/commands/prune.md`, `plugins/harness/agents/harness-pruner.md`

**Steps:**
1. Read current prune.md and harness-pruner.md
2. In the harness-pruner agent, add three new audit checks (bringing total to 18):
   - **Check 16: Missing Frontmatter** — Design docs without YAML frontmatter. Severity: warn. Fix: add `status: unknown` frontmatter.
   - **Check 17: Frontmatter Status Consistency** — Docs with `status: current` that have been superseded by newer docs. Severity: warn. Fix: update to `status: superseded`.
   - **Check 18: Index Status Badges** — Verify index.md Status column matches frontmatter status on each doc. Severity: warn. Fix: sync badges.
3. In the harness-pruner agent output format, add a **Health Score** line:
   - Score calculation: start at 10, subtract 1 per error, 0.5 per warning, cap at 0
   - Format: `**Health Score:** {N}/10`
4. Update prune.md invocation prompt to pass the new check instructions
5. In prune.md, add a "Quick Health" mode:
   - When invoked with no args from a health check context, run only checks 1, 4, 9, 13, 16, 17 (fast subset)
   - Output only the health score line, not the full report
   - This is what the loop.md health check will call

**Acceptance criteria:**
- harness-pruner has 18 checks including frontmatter validation
- Prune outputs a health score
- Quick health mode exists for fast session-start checks

---

### Task 8: Update init.md to generate LEARNINGS.md and frontmatter template

**Goal:** `/harness:init` creates the LEARNINGS.md scaffold and documents the frontmatter format.

**File:** `plugins/harness/commands/init.md`

**Steps:**
1. Read current init.md
2. In Phase 2 (Scaffold Core Structure), step 8, add:
   - Create `docs/LEARNINGS.md` with scaffold:
     ```markdown
     # Learnings

     Persistent learnings captured across sessions. Append-only, merge-friendly.

     Status: `active` | `superseded`
     Categories: `architecture` | `testing` | `patterns` | `workflow` | `debugging` | `performance`

     ---
     ```
3. In Phase 5 (Rewrite CLAUDE.md), add LEARNINGS.md to the Documentation Map:
   ```
   | Learnings | `docs/LEARNINGS.md` | Past learnings, corrections, patterns discovered across sessions |
   ```
4. In Phase 2, step 12 (design-docs index), update the generated index template to include a Status column:
   ```markdown
   ## Current Designs

   | Document | Purpose | Status | Created |
   |----------|---------|--------|---------|

   ## Archived

   | Document | Purpose | Status | Created |
   |----------|---------|--------|---------|
   ```

**Acceptance criteria:**
- init.md generates LEARNINGS.md scaffold
- init.md adds Learnings row to Documentation Map
- init.md generates index with Status column and Current/Archived sections

---

### Task 9: Update plan.md to filter by frontmatter status

**Goal:** `/harness:plan` skips design docs with `status: implemented` or `status: superseded` when auto-detecting the most recent design doc.

**File:** `plugins/harness/commands/plan.md`

**Steps:**
1. Read current plan.md
2. In step 2 (locate design document), modify the auto-detection logic:
   - When searching for the most recently modified design doc, check frontmatter status
   - Skip docs with `status: implemented`, `status: superseded`, or `status: stale`
   - Only consider docs with `status: current` or no frontmatter (unknown/legacy)
3. Add a note: "If no `status: current` design docs are found, suggest running `/harness:brainstorm`"

**Acceptance criteria:**
- plan.md filters design docs by frontmatter status
- plan.md only considers `current` or unknown-status docs

---

### Task 10: Update loop.md with health check at start

**Goal:** `/harness:loop` runs a lightweight health check before starting the pipeline.

**File:** `plugins/harness/commands/loop.md`

**Steps:**
1. Read current loop.md
2. In Phase 1 (Locate Design & Initialize Decision Log), add a step after step 2 (read design doc):
   - Run a quick health check:
     - Count design docs with `status: current` vs total
     - Check if `docs/LEARNINGS.md` exists
     - Check CLAUDE.md line count
     - Scan `docs/design-docs/index.md` for any entries missing Status column
   - Output one-line health summary: `Harness health: {N}/10 ({details})`
   - If score < 5, suggest running `/harness:prune` first but do NOT block
   - Append health check result to decision log
3. In Phase 6 (Reflect), after the existing prune check, also:
   - Write any conversation-mined learnings to LEARNINGS.md (same as reflect.md enhancement)

**Acceptance criteria:**
- loop.md outputs health score at start
- Health check is advisory, never blocking
- Loop reflects learnings at the end

---

### Task 11: Update harness-pruner agent with frontmatter awareness

**Goal:** The pruner agent understands and can fix frontmatter issues.

**File:** `plugins/harness/agents/harness-pruner.md`

NOTE: This task is partially covered by Task 7. This task focuses on the "Applying Fixes" section updates.

**Steps:**
1. In the "Applying Fixes" section, add fix procedures for the three new checks:
   - **Missing frontmatter fix:** Read the design doc, infer status from context (if in index Archived section → `implemented`, if newer doc on same topic exists → `superseded`, otherwise → `current`), add frontmatter block
   - **Status consistency fix:** Update frontmatter `status` and add `supersedes` reference
   - **Index badge fix:** Read each design doc's frontmatter, update the index Status column to match
2. Update the Fifteen Audit Checks table header to "Eighteen Audit Checks"
3. Ensure the pruner can handle design docs with no frontmatter gracefully (treat as `status: unknown`)

**Acceptance criteria:**
- Pruner can fix missing frontmatter
- Pruner can sync index badges with frontmatter
- Pruner handles legacy docs without frontmatter

---

### Task 12: Bump plugin version to 3.0.0

**Goal:** Update plugin.json version and description to reflect new capabilities.

**File:** `plugins/harness/.claude-plugin/plugin.json`

**Steps:**
1. Update version from `2.2.0` to `3.0.0` (major: new frontmatter format, LEARNINGS.md, health score)
2. Update description to mention learning persistence and doc lifecycle
3. Add `learnings`, `frontmatter`, `lifecycle` to keywords

**Acceptance criteria:**
- plugin.json version is 3.0.0
- Description reflects new capabilities

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
- Parallel worker dispatch was very effective — 12 tasks across 3 waves completed quickly
- All tasks were truly independent (different files), enabling maximum parallelism
- The design doc from CEO review provided clear, unambiguous specs for each task

**What didn't:**
- Workers couldn't commit individually (git permission issues in subagent mode)
- TestPluginVersion test caught the version bump mismatch — good that it exists, but should have been in the plan

**Learnings to codify:**
- When bumping vendored plugin versions, always check for Go test assertions on the version string
- The harness plugin's weakness was opt-in lifecycle management — making it automatic (health check, auto-archive) is the fix pattern for any agent tooling system
