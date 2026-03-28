# Code Restructuring: V1 Removal + Package Flattening

> **Status**: Active | **Created**: 2026-03-27 | **Last Updated**: 2026-03-27
> **Design Doc**: `docs/design-docs/2026-03-27-code-restructuring-v1-removal-design.md`
> **Consulted Learnings**: L-20260323-raw-string-no-backticks, L-20260321-resolve-model-conflicts
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-27 | Design | Full v1 removal + flatten | v3 has zero v1 deps, v1 is dead code |
| 2026-03-27 | Plan | Execute directly (not subagent-per-task) | Mechanical refactor: rm, mv, find-replace |

## Progress

- [ ] Task 1: Delete all v1 legacy packages
- [ ] Task 2: Delete legacy CLI command files
- [ ] Task 3: Move v3 packages to internal/ (flatten)
- [ ] Task 4: Update all imports (v3/ removal)
- [ ] Task 5: Update CLI root.go entry point
- [ ] Task 6: Retire DefaultPipelineYAML + rename pipeline nodes
- [ ] Task 7: Remove FanOut/Per/FanIn from model.go
- [ ] Task 8: Fix deferred review items (5 items)
- [ ] Task 9: Update documentation
- [ ] Task 10: Verify build + tests

## Surprises & Discoveries

_None yet._

## Plan Drift

_None yet._

---

### Task 1: Delete all v1 legacy packages
Delete 27 packages under internal/ (excluding v3/, plugins/, cli/root.go).

### Task 2: Delete legacy CLI command files
Delete all .go files in internal/cli/ except root.go.

### Task 3: Move v3 packages to internal/
Move internal/v3/X/ → internal/X/ for all 9 packages.

### Task 4: Update all imports
Find-replace `internal/v3/` → `internal/` across all Go files.

### Task 5: Update CLI root.go
Remove v3cli alias, register commands directly.

### Task 6: Retire DefaultPipelineYAML + rename nodes
Delete defaults.go. Update ClimbWorkflow to error on missing pipeline. Rename nodes in templates.

### Task 7: Remove FanOut/Per/FanIn from model.go
Remove 3 fields and their validation.

### Task 8: Fix deferred review items
- pollForCompletion exit channel tests
- writeNodeContext gate round-trip test
- Local-path Install test
- Consolidate run-node.sh + run-gate.sh
- Clean up old .belayer/output/ paths in tests

### Task 9: Update documentation
CLAUDE.md, ARCHITECTURE.md, DESIGN.md, TODOS.md, PLANS.md

### Task 10: Verify build + tests
go build, go test, go vet

## Deliverable Traceability

| Design Doc Deliverable | Plan Task |
|----------------------|-----------|
| Delete 27 v1 packages | Task 1 |
| Delete legacy CLI files | Task 2 |
| Flatten v3 to internal/ | Task 3 |
| Update all imports | Task 4 |
| Update root.go entry point | Task 5 |
| Retire DefaultPipelineYAML | Task 6 |
| Rename pipeline nodes | Task 6 |
| Remove FanOut/Per/FanIn | Task 7 |
| pollForCompletion exit channel test | Task 8 |
| writeNodeContext gate test | Task 8 |
| Local-path Install test | Task 8 |
| Consolidate shell scripts | Task 8 |
| Clean old test paths | Task 8 |
| Update docs | Task 9 |
| Verify build + tests | Task 10 |

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._
