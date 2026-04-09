# CLAUDE.md

Belayer is on the `feature/v6` clean-break baseline.

## Branch Intent

This branch intentionally removed the v5 orchestration stack:

- Temporal worker/runtime code
- YAML pipeline engine
- gates / routes / outcome detection
- intake bridge and plugin registry
- framework installers

## What Still Exists

- `cmd/belayer/main.go`
- `cmd/gencodexskills/main.go` as a disabled placeholder
- `internal/cli/` command scaffolding
- `internal/model/types.go`
- `internal/events/`
- v6 baseline docs

## Guidance For Future Work

- Build toward a session-runtime architecture.
- Prefer local state and SQLite-backed metadata.
- Do not reintroduce the v5 pipeline model as a stopgap.
- Keep docs aligned with the current branch reality so future agents do not mix v5 and v6 concepts.
