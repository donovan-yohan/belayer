# Unified Agent Plugin Source Implementation

> **Status**: Complete | **Created**: 2026-03-17 | **Last Updated**: 2026-03-17
> **Design Doc**: `docs/design-docs/2026-03-17-unified-agent-plugin-source-design.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.
> **For Codex:** Execute the same plan using the generated `harness-*` and `pr-*` skills.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-17 | Design | Add a generated Codex asset pack now, even if Claude remains the authored source for this first pass | Delivers the user-visible install goal immediately while preserving a clean seam for later full source-neutral migration |
| 2026-03-17 | Design | Make Belayer prompts provider-aware in the same batch | Installing Codex skills without updating prompts would still leave Belayer instructing Codex to run Claude-only slash commands |
| 2026-03-17 | Design | Treat README + `.codex/INSTALL.md` as part of the feature, not follow-up docs | Install behavior without instructions will create support debt immediately |
| 2026-03-17 | Implementation | Put the shared asset code at the module root (`agentassets.go`) | `go:embed` can package the vendored plugin source directly from the repo root without copying it into another tree first |

## Progress

- [x] Task 1: Add generated cross-agent asset metadata and rendering helpers _(completed 2026-03-17)_
- [x] Task 2: Install embedded Codex skill assets during `belayer init` when Codex is present _(completed 2026-03-17)_
- [x] Task 3: Make Belayer’s provider-specific prompts and initial task prompts Codex-aware _(completed 2026-03-17)_
- [x] Task 4: Document the new installation and source-of-truth workflow _(completed 2026-03-17)_
- [x] Task 5: Verify with targeted tests and complete the loop artifacts _(completed 2026-03-17)_

## Surprises & Discoveries

| Date | What | Impact | Resolution |
|------|------|--------|------------|
| 2026-03-17 | Harness command source still references Superpowers namespace and Claude slash-command syntax | Generated Codex skills needed runtime-specific rewriting to stay usable in Codex | Codex rendering now rewrites `/harness:*`, `/pr:*`, and `superpowers:*` references and the docs call out the Superpowers dependency explicitly |
| 2026-03-17 | `go:embed` cannot reach sibling paths from an internal package | The intended `internal/agentassets` package would have required copying source files or a codegen hop before the feature could ship | Implemented shared asset parsing in a module-root package so the vendored plugin source can be embedded directly |

## Plan Drift

| Task | Plan Said | Actually Did | Why |
|------|-----------|-------------|-----|
| Task 1 | Introduce a new internal shared asset package and runtime-neutral manifest | Added a module-root embedded asset package that parses the existing vendored plugin markdown and plugin metadata | This shipped a real single-source install path immediately without first migrating the whole workflow corpus into a new authored tree |
| Task 4 | Add docs after the install path exists | Expanded docs to call out the current Superpowers composition requirement for some harness flows | The generated Codex wrappers would have been misleading without documenting that dependency |

---

## Task 1: Add Generated Cross-Agent Asset Metadata and Rendering Helpers

**Outcome:** Added [agentassets.go](/Users/donovanyohan/Documents/Programs/personal/belayer/agentassets.go) and [agentassets_test.go](/Users/donovanyohan/Documents/Programs/personal/belayer/agentassets_test.go) to embed the vendored plugin source, expose plugin versions, render Codex `SKILL.md` files from the shared command markdown, and rewrite Claude/Superpowers references for Codex.

## Task 2: Install Embedded Codex Skill Assets During `belayer init`

**Outcome:** Extended [codex.go](/Users/donovanyohan/Documents/Programs/personal/belayer/internal/plugins/codex.go), [registry.go](/Users/donovanyohan/Documents/Programs/personal/belayer/internal/plugins/registry.go), [init.go](/Users/donovanyohan/Documents/Programs/personal/belayer/internal/cli/init.go), and their tests so `belayer init` now writes a versioned Codex skill tree under `~/.belayer/agent-assets/codex/` and mounts it into `~/.agents/skills/belayer` when `codex` is present.

## Task 3: Make Belayer Prompts and Initial Prompts Codex-Aware

**Outcome:** Updated [taskrunner.go](/Users/donovanyohan/Documents/Programs/personal/belayer/internal/belayer/taskrunner.go) and [lead.md](/Users/donovanyohan/Documents/Programs/personal/belayer/internal/defaults/claudemd/lead.md) so Codex sessions are instructed to use `harness-*` skills instead of Claude slash commands.

## Task 4: Document the New Installation and Source-of-Truth Workflow

**Outcome:** Updated [README.md](/Users/donovanyohan/Documents/Programs/personal/belayer/README.md), added [.codex/INSTALL.md](/Users/donovanyohan/Documents/Programs/personal/belayer/.codex/INSTALL.md), and reconciled [ARCHITECTURE.md](/Users/donovanyohan/Documents/Programs/personal/belayer/docs/ARCHITECTURE.md) and [DESIGN.md](/Users/donovanyohan/Documents/Programs/personal/belayer/docs/DESIGN.md) with the shipped shared-source approach.

## Task 5: Verify and Complete the Loop

**Verification:** `go test ./...`

---

## Outcomes & Retrospective

**What worked:**
- Embedding the existing vendored plugin source let Belayer share versions and workflow content between Claude registry writes and Codex skill generation without duplicating files
- Provider-aware lead prompts closed the most immediate runtime gap for Codex-driven Belayer sessions
- Tests around `belayer init`, shared asset rendering, and Codex skill install gave good coverage for the new behavior

**What didn't:**
- This first pass does not yet introduce the full runtime-neutral `agentpacks/` source tree described in the design doc
- Some harness workflows still depend on Superpowers-style skills, so Belayer's Codex pack is better described as a generated wrapper layer than a fully standalone workflow stack

**Learnings to codify:**
- If Belayer wants to reuse repo-local authored content across runtimes from inside the binary, `go:embed` package placement matters early and should be treated as an architecture choice, not an implementation detail
- Cross-runtime workflow packaging needs explicit reference rewriting; otherwise provider-specific invocation strings leak into the wrong runtime
