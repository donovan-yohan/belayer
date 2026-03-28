---
status: implemented
created: 2026-03-27
branch: dy/feat/exec-spawner-framework
supersedes:
implemented-by:
consulted-learnings: [L-20260323-raw-string-no-backticks, L-20260321-resolve-model-conflicts]
---

# Code Restructuring: V1 Removal + Package Flattening

Remove all legacy v1 code, flatten `internal/v3/` to `internal/`, rename pipeline nodes to three-phase names, retire `DefaultPipelineYAML`, and fix 5 deferred review items.

## Problem

The codebase has two parallel architectures:
- `internal/v3/` — the current Temporal-based pipeline engine (ExecSpawner, gates, frameworks)
- `internal/` — legacy v1 daemon code (belayer daemon, lead runner, spotter, anchor, mail, etc.)

V3 has zero imports of v1. V1 code is dead — no commands register it, no tests exercise active paths. The `v3` namespace is confusing for new contributors and makes imports unnecessarily long.

## Approach

### Phase 1: Delete legacy v1 packages

Delete these packages entirely (27 packages, ~15K LOC):

| Package | Reason for deletion |
|---------|-------------------|
| `internal/agentic/` | Replaced by ExecSpawner |
| `internal/anchor/` | Merged into gate nodes |
| `internal/belayer/` | Replaced by Temporal ClimbWorkflow |
| `internal/belayerconfig/` | V1-only config |
| `internal/climbctx/` | V1-only context types |
| `internal/config/` | V1-only global config |
| `internal/crag/` | V1-only workspace management |
| `internal/db/` | V1-only SQLite (v3 uses Temporal for state) |
| `internal/defaults/` | V1-only embedded defaults |
| `internal/env/` | V1-only environment provider |
| `internal/envprovider/` | V1-only environment client |
| `internal/lead/` | Replaced by ExecSpawner |
| `internal/logmgr/` | V1-only log management |
| `internal/mail/` | V1-only inter-agent mail |
| `internal/manage/` | V1-only session workspace |
| `internal/model/` | V1-only domain types (v3 has its own) |
| `internal/pidfile/` | V1-only daemon PID |
| `internal/repo/` | V1-only git operations |
| `internal/review/` | V1-only reaction engine |
| `internal/scm/` | V1-only PR management |
| `internal/spotter/` | Replaced by gate nodes |
| `internal/store/` | V1-only SQLite CRUD |
| `internal/testutil/` | V1-only test helpers |
| `internal/tmux/` | Replaced by ExecSpawner (tmux now in framework) |
| `internal/tracker/` | V1-only issue tracker |
| `internal/intake/` | V1-only intake (v3 has its own) |

Also delete legacy CLI files in `internal/cli/` (all except `root.go`):
- `belayer_cmd.go`, `config_cmd.go`, `crag_cmd.go`, `env.go`, `explorer.go`, `init.go`, `learnings.go`, `logs.go`, `mail_cmd.go`, `message_cmd.go`, `pr_cmd.go`, `problem.go`, `review.go`, `setter.go`, `status_cmd.go`, `tracker_cmd.go`, and all their tests

### Phase 2: Flatten v3 to internal/

Move all `internal/v3/` packages up one level:

| From | To |
|------|-----|
| `internal/v3/cli/` | `internal/cli/` (merge with cleaned root.go) |
| `internal/v3/temporal/` | `internal/temporal/` |
| `internal/v3/session/` | `internal/session/` |
| `internal/v3/pipeline/` | `internal/pipeline/` |
| `internal/v3/gate/` | `internal/gate/` |
| `internal/v3/events/` | `internal/events/` |
| `internal/v3/intake/` | `internal/intake/` |
| `internal/v3/model/` | `internal/model/` |
| `internal/v3/outcome/` | `internal/outcome/` |

Update all imports from `github.com/donovan-yohan/belayer/internal/v3/X` to `github.com/donovan-yohan/belayer/internal/X`.

Update `internal/cli/root.go`: remove `v3cli` import alias, register commands directly.

### Phase 3: Rename pipeline nodes

In `frameworks/claude-tmux/pipeline.yaml`:
- `implement` (already named correctly)
- `review` (already named correctly)

In `internal/pipeline/defaults.go` — retire `DefaultPipelineYAML`:
- Remove the entire constant
- Update `ClimbWorkflow` to error if no pipeline YAML is provided (instead of falling back to default)
- Update `intake.ResolvePipelineYAML` to error if no pipeline file found (instead of falling back)

In `internal/pipeline/templates/` — update template files:
- `solo.yaml`: `lead` → `implement`, `spotter` → `review`
- `team.yaml`: `setter` → `plan`, `lead` → `implement`, `spotter` → `review`, `pr-creator` → `pr-author`

Remove `FanOut`, `Per`, `FanIn` fields from `NodeConfig` in `model.go`.

### Phase 4: Fix deferred review items

1. **pollForCompletion exit channel test**: Add tests that use a real exit channel (not nil) through pollForCompletion — test fast-fail on process death and race condition (file exists + exit fires).

2. **writeNodeContext gate round-trip test**: Add test with Dimensions and Thresholds populated, verify JSON serialization includes them.

3. **Local-path Install test**: Create temp dir with pipeline.yaml + scripts, call Install with path, verify files copied and .sh permissions are 0755.

4. **Consolidate shell scripts**: Merge `run-node.sh` and `run-gate.sh` into a single `run.sh` that reads `node_type` from `node-context.json` and conditionally creates the output directory for gates.

5. **Clean up old paths**: With DefaultPipelineYAML gone, all remaining `.belayer/output/` references are in the `processGateResult` test fixtures. Update them to use `.belayer/.internal/output/` paths.

### Phase 5: Update docs and imports

- `internal/plugins/` — standalone, no v1 deps, keep as-is
- `CLAUDE.md` — update Quick Reference, remove v3-specific references
- `ARCHITECTURE.md` — remove v1 code map, update module table
- `DESIGN.md` — remove v1-specific patterns (mail, daemon, lead execution)
- `TODOS.md` — remove P1 (done), clean up references

## Testing

- All existing v3 tests move with their packages (just import path changes)
- New tests for deferred items (3 test additions)
- `go test ./...` must pass with only the new packages
- `go build ./cmd/belayer` must succeed

## NOT in scope

- Multi-language SDKs (P2 TODO)
- Multi-repo runtime (P2 TODO)
- Summit operations (P2 TODO)
- Boulderer (P3 TODO)
