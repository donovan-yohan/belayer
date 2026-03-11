# Complete Instance-to-Crag Rename

> **Status**: Completed | **Created**: 2026-03-11 | **Completed**: 2026-03-11
> **Design Doc**: `docs/design-docs/2026-03-11-instance-to-crag-complete-rename-design.md`

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-11 | Execute | Backward compat fallback for `instance.json` in `loadConfig()` | Existing crags have `instance.json` on disk; first config save migrates to `crag.json` |
| 2026-03-11 | Execute | Left `BELAYER_INSTANCE` env var fallback in `helpers.go` and setter dedup | Backward compat for existing scripts/sessions |
| 2026-03-11 | Execute | Left migration filenames and historical exec plans untouched | These are immutable historical records |

## Progress

- [x] Task 1: Rename `internal/instance/` package to `internal/crag/`
- [x] Task 2: Rename internal variables and functions in the new `internal/crag/` package
- [x] Task 3: Update all import paths and call sites across codebase
- [x] Task 4: Update live documentation (ARCHITECTURE.md, QUALITY.md, PLANS.md)
- [x] Task 5: Build + test verification

## Outcomes & Retrospective

**What worked:**
- Mechanical rename was clean — `go build` caught all import issues immediately
- All 27 tests pass on first run
- No surprising dependencies or edge cases

**Files changed:** 19 files (12 Go source, 2 Go test, 3 docs, 1 design doc index, 1 new design doc)
