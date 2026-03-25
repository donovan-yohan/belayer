# Three-Phase Architecture Documentation

> **Status**: Active | **Created**: 2026-03-25 | **Last Updated**: 2026-03-25
> **Design Doc**: `docs/design-docs/2026-03-25-three-phase-architecture-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-25 | Eng Review | Three phases: Explore/Climb/Summit | Imperative verbs, maps to CLI |
| 2026-03-25 | Eng Review | Multi-repo is additive | Per-repo pipeline unchanged |
| 2026-03-25 | Eng Review | PR inside Climb | Natural retry loops |
| 2026-03-25 | CEO Review | Config model accepted | Foundational for all future work |
| 2026-03-25 | CEO Review | "Why Belayer" accepted | Market positioning |
| 2026-03-25 | CEO Review | Pipeline examples accepted | Makes architecture tangible |

## Progress

- [x] Task 1: Update ARCHITECTURE.md with three-phase model
- [x] Task 2: Update DESIGN.md with contracts and principles
- [x] Task 3: Update CLAUDE.md to reflect new naming
- [x] Task 4: Update design docs index with new design doc
- [x] Task 5: Create TODOS.md with deferred items
- [x] Task 6: Create Go parse test for pipeline YAML examples

---

### Task 1: Update ARCHITECTURE.md with three-phase model

**Why:** ARCHITECTURE.md is the canonical reference for belayer's module boundaries and data flow. It currently describes v1/v3 sequential pipeline with no three-phase model.

**Files:** `docs/ARCHITECTURE.md`

**Steps:**
1. Replace the "Orchestration Layers" section with the three-phase model (Explore/Climb/Summit)
2. Update the ASCII diagram to show three phases with their contracts
3. Add Named Roles table (Setter, Spotter, Lead, Boulderer)
4. Update "Pipeline Engine" section to note relationship between three-phase model and v3 code
5. Add Config Hierarchy section with the ~/.belayer + ./.belayer structure
6. Keep all existing module-level documentation (Code Map, Data Flow details, etc.)

**Done when:** ARCHITECTURE.md reflects three-phase model, named roles, config hierarchy. `go test ./...` still passes (docs-only change).

---

### Task 2: Update DESIGN.md with contracts and principles

**Why:** DESIGN.md describes patterns and conventions. Needs setter/spotter contracts, additive principle, PR manifest interface, and "Why Belayer" positioning.

**Files:** `docs/DESIGN.md`

**Steps:**
1. Add "Strategic Principles" section with the 5 principles from the design doc
2. Add "Setter/Spotter Contracts" section describing the multi-repo interfaces
3. Add "PR Manifest" section with the JSON schema
4. Add "Why Belayer" section with the competitive comparison table
5. Update "Naming Convention" table to include Boulderer and clarify Setter/Spotter are multi-repo only
6. Add note about the additive workflow principle in the Validation Pipeline section

**Done when:** DESIGN.md includes contracts, principles, positioning. No code changes.

---

### Task 3: Update CLAUDE.md to reflect new naming

**Why:** CLAUDE.md is loaded by every Claude session working on belayer. Must reflect the three-phase model vocabulary.

**Files:** `CLAUDE.md`

**Steps:**
1. Update the project description to mention three phases (Explore/Climb/Summit)
2. Add a brief note about the three-layer model: 3 phases (Explore/Climb/Summit) with additive multi-repo via setter/spotter
3. Keep under 120 lines (currently 48)
4. Do NOT add extensive documentation — CLAUDE.md is a map, not a manual. Point to docs/ARCHITECTURE.md for details

**Done when:** CLAUDE.md mentions three-phase model. Still under 120 lines.

---

### Task 4: Update design docs index

**Why:** The new design doc needs to be listed in the index as current.

**Files:** `docs/design-docs/index.md`

**Steps:**
1. Add the new design doc to the "Current Designs" section:
   ```
   | [2026-03-25-three-phase-architecture-design](2026-03-25-three-phase-architecture-design.md) | Three-phase architecture: Explore/Climb/Summit, multi-repo setter/spotter, config hierarchy | Current | 2026-03-25 |
   ```
2. Remove the "No current designs" placeholder text

**Done when:** Index lists the new design doc as current.

---

### Task 5: Create TODOS.md with deferred items

**Why:** The CEO review deferred the `using-belayer` skill and other items. These need a persistent home.

**Files:** `docs/TODOS.md` (new)

**Steps:**
1. Create TODOS.md with standard format
2. Add `using-belayer` skill TODO with full context:
   - What: Claude Code / Codex skill that agents load to bootstrap belayer configs via natural language
   - Why: Distribution strategy — any agent can say "set up belayer for this repo" and get a working pipeline
   - Blocked by: CLI finalization and config model implementation
   - Priority: P2
3. Add other deferred items from the design doc's Deferred Items table

**Done when:** TODOS.md exists with structured entries for all deferred items.

---

### Task 6: Create Go parse test for pipeline YAML examples

**Why:** The design doc contains 3 pipeline YAML examples (single-repo, multi-repo, custom TDD). A parse test prevents drift between documentation and code.

**Files:** `internal/v3/pipeline/doc_examples_test.go` (new)

**Steps:**
1. Create test file that reads the design doc
2. Extract YAML blocks between triple-backtick fences that start with `yaml`
3. For each YAML block that looks like a pipeline (has `nodes:` key), run `ParsePipelineNoValidate`
4. Assert each parses without error
5. Note: Use `ParsePipelineNoValidate` because some fields (top-level `setter:`, `spotter:`) don't exist in the current schema yet. The test validates YAML structure, not full semantic validation.
6. If `ParsePipelineNoValidate` doesn't exist, use a simple `yaml.Unmarshal` into a `map[string]interface{}` to verify YAML syntax

**Done when:** Test passes with `go test ./internal/v3/pipeline/ -run TestDocExamples -v`. Pipeline YAML examples in the design doc are syntactically valid.

---

## Outcomes & Retrospective

**What worked:**
- Parallel agent dispatch for independent doc tasks (ARCHITECTURE.md, DESIGN.md, CLAUDE.md+index+TODOS)
- Parse test caught all 4 pipeline YAML examples from design doc — verified they're syntactically valid
- Review agent found 5 significant inconsistencies (old spotter/anchor descriptions) that would have confused future developers

**What didn't:**
- Background agents for Tasks 3-5 were slow; ended up completing those inline
- The old spotter/anchor naming in v1 code sections of ARCHITECTURE.md and DESIGN.md creates ongoing confusion — need the P1 code restructuring to fully resolve

**Learnings:**
- When reframing terminology (spotter from per-repo to multi-repo), EVERY mention in EVERY doc file needs updating — not just the section you're adding. The review caught 5 places we missed.
- Pipeline YAML examples in design docs should have parse tests from day one — catches drift early
