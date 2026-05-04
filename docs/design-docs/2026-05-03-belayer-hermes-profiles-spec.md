# Belayer Hermes Profiles Spec

**Date:** 2026-05-03
**Status:** Forward-looking, not implemented
**Epic:** #131
**Pattern harvest:** #132

## Goal

Move Belayer's runtime backing for talents from a single shared `default` Hermes profile to per-talent-instance profiles forked from a shared `belayer` base. Lean on Hermes's existing profile primitive (`hermes_cli/profiles.py`) for filesystem layout, auth, skills, memories, and sessions. Keep Belayer's identity contract (talents, crags, gates, evaluations) declarative and git-tracked; let runtime state live where Hermes already manages it.

## Why

Current state: every Belayer agent runs on the `default` Hermes profile. All talents share auth, memories, skills, and `state.db`. This blocks:

- Multi-vendor talent teams (different providers per talent)
- Per-crag isolation (software-personal vs software-work vs parley NPCs sharing one memory bank)
- Talent-specific learned context (each talent's accumulated MEMORY.md drowns in the shared default)
- Operator separation of concerns between projects

Hermes already solves the per-instance isolation problem with profiles. Belayer should adopt the primitive instead of working around it.

## Architecture

Two stacks, parallel coordination, distinct ownership:

```
.belayer/                    Belayer-owned, declarative, git-tracked.
                             Identity templates, crag config, climb artifacts.

~/.belayer/                  Belayer-owned, cross-project knowledge.
                             Talent catalog, crag SOPs, evaluations, promotions.

~/.hermes/profiles/          Hermes-owned, runtime instance state.
                             Per-talent-instance profile dirs.
```

Belayer reads/writes Hermes profiles via the existing `hermes_cli.profiles` API (or equivalent Go-native logic).

## Profile naming

```
~/.hermes/profiles/
├── blyr/                                    ← base, single source of auth + plugin reg
├── blyr-<cragSlug>-<instanceID>/            ← per-talent-instance, forked from base
```

`instanceID` resolution:
- Singleton talent: `<talentName>` (e.g. `supervisor`)
- Parallel mains: `<talentName>-<short8hex>` (e.g. `backend-dev-a3f9c2d1`)
- Generated talent: `<generatedTalentID>` (e.g. `generated-reviewer-1`)

Char budget: `blyr-` (5) + crag (≤20) + `-` (1) + instance (≤38) ≤ 64 chars (Hermes regex `^[a-z0-9][a-z0-9_-]{0,63}$`). Validate at `belayer crag init`.

Note: Phase 6.A (#144) renamed the base profile from `belayer` to `blyr` and the fork prefix from `belayer-` to `blyr-`, saving 3 characters per profile dir name (8-char `belayer-` prefix → 5-char `blyr-` prefix) to give more room within the 64-char Hermes profile name limit.

## Forward materialization (.belayer → profile)

Spawn sequence:

1. Resolve identity through 4-layer precedence (already shipped, `internal/daemon/agents.go`).
2. Resolve crag context from `.belayer/config.yaml#crag.name` or explicit flag.
3. Compute profile name: `blyr-<crag>-<instanceID>`.
4. If profile dir missing:
   - Fork from `blyr` base with config + skills + memory seed copied.
   - Auth: see "Auth fork strategy" below.
5. Render `system-prompt.md` template → write `<profile>/SOUL.md`.
6. Materialize `talent.yaml`-derived config:
   - `memory.scope` → profile teardown rule (climb / crag / talent).
   - `retention.scope` → archive rule.
   - `authority.tools` → `BELAYER_TOOLS` env (existing wiring).
   - `model` → `config.yaml` model field or `BELAYER_MODEL` env.
7. Set `HERMES_HOME=~/.hermes/profiles/blyr-<crag>-<instance>` and spawn bridge subprocess via existing path.

## Backward sync (profile → .belayer)

Evidence-driven only. No silent copy of profile state back to `.belayer/`. Per `docs/CRAG_FILESYSTEM.md` privacy contract.

| Trigger | Action |
|---|---|
| Climb end (memory.scope == "climb") | Daemon reads profile MEMORY.md → writes `talent-evaluation` artifact → tears down profile dir. |
| Climb end (memory.scope == "crag") | Same evaluation artifact written; profile preserved across climbs. |
| Operator retire (`belayer prune`) | Snapshot final SOUL.md, MEMORY.md, USER.md → archive in `~/.belayer/crags/<crag>/evaluations/<talent>/<date>.json`; delete profile. |
| Operator promote (`belayer promote <eval>`) | Apply diff to catalog `talent.yaml` (maturity bump) or crag `sops/`, `gates/`. Profile untouched. |

## Lifecycle binding

`talent.yaml#runtime.lifecycle` (per #119) maps directly to profile retention:

| Lifecycle | Profile retention | Sessions | Memory persists |
|---|---|---|---|
| `resident` | until climb end | active throughout | yes during climb |
| `resumable` | until crag retire or explicit prune | dormant after run; mail wake (#118) reuses | yes |
| `ephemeral` | tear down after `final_response` | discarded | no (unless evaluation extracts) |

No new contract fields required.

## Auth fork strategy

**Resolved by Phase 0 spike (#133).** Symlink `auth.json` from each fork to base `blyr/auth.json`. Hermes's `utils.atomic_replace` (utils.py:61-82) preserves symlinks via `os.path.realpath` per #16743 upstream — token rotation in base propagates to all forks transparently.

Same pattern extends to `plugins/` and `skills/` subdirs (plugin discovery walks symlinks). Per-fork dir holds only mutable state: `memories/`, `sessions/`, `state.db`, `SOUL.md`, `config.yaml`.

## CLI surface

```
belayer auth                            # creates ~/.hermes/profiles/blyr/, runs hermes auth login
belayer auth status                     # base profile auth state, refresh time

belayer doctor                          # profile health: orphans, mismatched agent_runs/profiles, auth staleness
belayer doctor --crag <slug>            # scoped to one crag

belayer prune                           # interactive: list orphan profiles, remove
belayer prune --crag <slug>             # scoped

belayer uninstall --crag <slug>         # remove all blyr-<slug>-* + .belayer/crags/<slug>/
belayer uninstall                       # global: remove all blyr-* + base + ~/.belayer/
```

## Why not Hermes Kanban

Hermes ships a durable multi-agent task board (`~/.hermes/kanban.db`) with overlapping primitives: dispatcher-driven worker spawn, structured handoff, crash reclaim, retry rows, dashboard. We choose not to migrate Belayer's coordination plane to Kanban for two reasons:

1. **Maintenance burden**: Kanban evolves on Hermes's gateway-universal axis; Belayer needs per-instance unique scoping. Riding both stacks creates compounding drift.
2. **Architectural fit**: Kanban is board-global, dispatcher-driven, designed for many profiles sharing one queue. Belayer is climb-scoped, supervisor-LLM-driven, with mainline peer-to-peer mailbox semantics. Real-time mainline coordination doesn't fit kanban's task-thread model.

Patterns worth porting independently (crash reclaim, idempotency keys, max-runtime, per-attempt rows, structured handoff schema) are tracked separately — see issue [TBD: kanban-pattern-harvest].

The meeting point between Hermes and Belayer is the profile primitive, not the coordination plane.

## Phases

Each phase is a separate GitHub issue with blocking dependencies on prior phases.

### Phase 0 — spike (#133) — COMPLETE 2026-05-03

Findings (all four investigations resolved without runtime experiments — answered by hermes-agent source inspection):

**1. Auth fork mechanism: symlink confirmed viable.**

`hermes-agent/utils.py:61-82` defines `atomic_replace(tmp, target)` that explicitly preserves symlinks:

> When *target* is a symlink, the symlink itself is replaced with a regular file — silently detaching managed deployments that symlink config.yaml / SOUL.md / auth.json etc. from ~/.hermes/ to a git-tracked profile package or dotfiles repo (GitHub #16743).
>
> This helper resolves the symlink first so os.replace writes to the real file in-place while the symlink survives.

```python
real_path = os.path.realpath(target_str) if os.path.islink(target_str) else target_str
os.replace(str(tmp_path), real_path)
```

`_save_auth_store` (auth.py:853-886) uses `atomic_replace`. Token rotation in base `blyr/auth.json` propagates to every symlinked fork. Decision: **Option 1 (symlink) selected**. Drop Options 2 (watch+sync) and 3 (lazy copy).

**2. Plugin/skill discovery from symlinked profile dirs: walks symlinks correctly.**

`PluginManager._scan_directory_level` (plugins.py:781-835) uses `Path.iterdir()`, `Path.is_dir()`, and `Path.exists()` — all follow symlinks per Python stdlib. Symlinking `<profile>/plugins/belayer → <base>/plugins/belayer` works without special handling. Skills directory uses identical iteration shape.

Belayer plugin currently lives at `~/.hermes/plugins/belayer/` (`kind: standalone`, version 0.2.0). Per-profile install via symlink from base profile, with `plugins.enabled` listing belayer in profile config.yaml.

**3. SQLite WAL contention: not a concern.**

`DEFAULT_DB_PATH = get_hermes_home() / "state.db"` (hermes_state.py:34). Each profile owns its own state.db. WAL is per-file, so isolation is automatic. Hermes's documented contention concerns (hermes_state.py:167-180) are about multiple processes sharing one DB — not multiple profiles.

**Bug surfaced for Phase 2:** `hermes_bridge/__main__.py:410` hardcodes `~/.hermes/state.db` ignoring HERMES_HOME. Bridge must read `<HERMES_HOME>/state.db` when constructing `SessionDB`. Add to Phase 2 task list.

**4. Upstream Hermes engagement: filed.**

NousResearch/hermes-agent#19436 — proposing `hermes profile create --link-auth <base>` CLI flag plus docs for the symlink fork pattern. Optional companion flags `--link-skills` / `--link-plugins`. Belayer can implement the pattern locally in the meantime; upstream adoption only affects DX.

**Storage growth:** With shared symlinked `auth.json` + `plugins/` + `skills/` from base, per-fork dir holds only `memories/`, `sessions/`, `state.db`, `SOUL.md`, `config.yaml` — likely under 5MB per dormant fork. Storage growth concern from spec (~2.5GB for 25 talents) is mitigated.

### Phase 0 — output

- All four spike questions resolved.
- Auth fork mechanism: symlink (Option 1).
- Plugin/skill discovery: symlinks supported.
- WAL contention: non-issue with per-profile DBs.
- New blocker for Phase 2: bridge state.db path bug.
- Upstream issue: NousResearch/hermes-agent#19436.

Estimate vs actual: 1 day budget; resolved in single session via source reading. No runtime experiments needed because Hermes already documents the symlink-preserving pattern in source.

### Phase 1 — base blyr profile + `belayer auth` (#134, blocked by #133)

- `belayer auth` command scaffolds `~/.hermes/profiles/blyr/`.
- Plugin install path: `~/.hermes/profiles/blyr/plugins/belayer/` (symlink or copy).
- Daemon defaults to `blyr` profile (was `default`).
- Backwards-compat: `--profile default` still works.

### Phase 2 — talent → profile materialization (#135, blocked by #134)

- New module: `internal/daemon/profiles.go` — fork base, render SOUL, lifecycle binding.
- Hook into existing spawn path in `internal/daemon/agents.go`.
- Profile name resolution + crag context wiring.
- Tests: parallel mains get distinct profiles; generated talents get scoped names; crag context resolved.

Depends on: Phase 1.

### Phase 3 — lifecycle wiring (#136, blocked by #135)

- Tear-down on climb end honors `memory.scope`.
- Resumable mains get profile preserved across runs.
- Mail wake (#118) reuses profile for resume.
- `talent-evaluation` artifact extraction at tear-down.

Depends on: Phase 2.

### Phase 4 — devex (doctor / prune / uninstall) (#137, blocked by #136)

- `belayer doctor [--crag <slug>]` — orphan detection, auth staleness check.
- `belayer prune [--crag <slug>]` — interactive removal.
- `belayer uninstall [--crag <slug>]` — scoped or global.
- `agent_runs` ↔ profile dir reconciliation logic.

Depends on: Phase 3.

### Phase 5 — Parley library shape

- Talent management Go SDK or stable CLI subcommands for downstream embed (Parley).
- Defer until Phase 4 ships and Parley language picked.

Depends on: Phase 4.

## Open questions

1. ~~Auth fork mechanism~~ — **resolved**: symlink (Phase 0).
2. Profile dir creation: shell to `hermes profile create` vs Go-native write? Single-source-of-truth vs Python dependency tradeoff. **Recommendation**: shell out for now (avoids drift); revisit if startup latency hurts.
3. `agent_runs.hermes_session_id` retention when profile torn down — does session ID become invalid if `state.db` gone? **Likely yes** per Hermes session model. Phase 3 must reconcile by either (a) preserving state.db for resumable lifecycle, or (b) clearing stale `hermes_session_id` on teardown.
4. Crag context resolution at spawn — `.belayer/config.yaml#crag.name` already shipped (#113); needs plumbing to spawn path. **Phase 2 task.**
5. ~~Storage growth~~ — **resolved**: symlink-shared `skills/` + `plugins/` + `auth.json` from base. Per-fork ~5MB.
6. **NEW from Phase 0**: `hermes_bridge/__main__.py:410` hardcodes `~/.hermes/state.db` ignoring HERMES_HOME. **Phase 2 task** — read `<HERMES_HOME>/state.db` instead.

## Validation

Required for each phase PR:

```bash
go build ./cmd/belayer
go test ./...
go test ./internal/daemon
go test ./internal/agentlint
git diff --check
```
