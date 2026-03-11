# Complete Instance-to-Crag Rename

> **Status**: Proposed | **Created**: 2026-03-11
> **Prior art**: `docs/exec-plans/completed/2026-03-10-instance-to-crag-rename.md` (partial rename)

## Context

The 2026-03-10 rename plan completed user-facing renames (CLI flags, env vars, DB columns, disk paths) but explicitly deferred:

1. **Package name**: `internal/instance/` kept as-is ("high churn, low value")
2. **Config file on disk**: `instance.json` kept (retrospective noted "now confusing")
3. **Internal variable names**: `instanceDir`, `instConfig`, `saveInstanceConfig`, `loadInstanceConfig` throughout
4. **Historical docs**: Design docs and exec plans left with "instance" terminology

The retrospective admitted this created confusion. The setter's `/problem-create` command writing to `~/.belayer/instances/belayer-guide/` (a path that no longer exists) is a symptom of this incomplete rename.

## Design

### 1. Package Rename: `internal/instance/` → `internal/crag/`

- Rename directory
- Rename file: `instance.go` → `crag.go`, `instance_test.go` → `crag_test.go`
- Change package declaration to `package crag`
- Update all 10 import sites across `internal/cli/`, `internal/belayer/`

### 2. Config File Rename: `instance.json` → `crag.json`

- Rename constant: `instanceConfigFile = "instance.json"` → `cragConfigFile = "crag.json"`
- Add backward compat in `loadCragConfig()`: try `crag.json` first, fall back to `instance.json`
- On save, always write `crag.json` (natural migration on next write)

### 3. Internal Variable Renames

In `internal/crag/crag.go`:
- `instanceDir` → `cragDir` (all function params and locals)
- `instConfig` → `cfg` or `cragCfg`
- `saveInstanceConfig()` → `saveConfig()`
- `loadInstanceConfig()` → `loadConfig()`
- Comment on `CragConfig` struct: remove "persisted as instance.json"

In `internal/cli/setter_cmd.go`:
- `instConfig` → `cragCfg` (line 29)

In `internal/mail/filestore.go`:
- Comment: "instanceDir" → "cragDir" (line 24)

### 4. Items to Leave Alone

- **SQL migrations** (`001_initial.sql`, `002_rename_crag.sql`, `003_rename_instance_to_crag.sql`): These are historical records of the schema evolution. Never edit applied migrations.
- **`internal/tmux/tmux.go:48`**: "returns a new RealTmux instance" — generic English, not belayer terminology.
- **`internal/logmgr/logmgr.go:146`**: "maxInstanceSize" — generic term for a single log file, not belayer instances.
- **`internal/config/config.go` backward compat struct**: The `DefaultInstance`/`Instances` fields in the raw unmarshal struct are intentionally there for JSON migration.
- **`internal/cli/helpers.go:21`**: `BELAYER_INSTANCE` env var fallback — keep for backward compat.
- **`internal/cli/setter_cmd.go:64-67`**: `BELAYER_INSTANCE` dedup filter — keep for backward compat.

### 5. Documentation Prune

**Live docs** (update "instance" → "crag" where still present):
- `docs/ARCHITECTURE.md` (3 occurrences)
- `docs/QUALITY.md` (1 occurrence)
- `docs/PLANS.md` (2 occurrences)

**Historical docs** (update for consistency since they're still indexed and loaded as context):
- `docs/design-docs/index.md` — rename the design doc entry from "Instance & repository management" to "Crag & repository management"
- Design docs with "instance" in their content — leave body text as-is (historical), but the index entry can be clarified

**Completed exec plans**: Leave as-is. They're historical records with retrospectives.

## Scope Summary

| Category | Files | Estimated Changes |
|----------|-------|-------------------|
| Package rename | 12 Go files | Directory + imports + call sites |
| Config file rename | 1 file + backward compat | Constant + load function |
| Internal var renames | 3 files | ~60 identifier replacements |
| Live doc prune | 3 MD files | Minor text updates |
| Tests | 1 test file | Package name + var names |

## Risks

- **Import path change**: Every file importing `internal/instance` must update. `go build` will catch any misses immediately.
- **Backward compat for `instance.json`**: Existing crags have `instance.json` on disk. The fallback load ensures they still work; first config save migrates them.
