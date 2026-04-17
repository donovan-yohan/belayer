---
status: current
created: 2026-04-17
branch: feat/clamshell-e2e
supersedes:
implemented-by:
consulted-learnings: []
---

# Embed hermes_bridge in binary + document deployment topologies

## Goal

Ship `hermes_bridge/` as embedded assets inside the belayer binary and extract them into `.belayer/hermes_bridge/` during `belayer init`, eliminating the manual sync step required for clamshell sessions. Rewrite `docs/SANDBOXING.md` to document two deployment topologies (ideal Nightshift Linux VM vs current macOS/Colima dev) and explicitly call out the known security gaps in the current clamshell exec path.

## Motivation

### Bridge distribution

Today `hermes_bridge/` is plain Python source that must exist at `/workspace/hermes_bridge/` inside the clamshell container. The container imports it from the bind-mounted project workspace, which means every project that uses clamshell needs a copy of the bridge files, and every change to bridge code must be manually propagated to every project. This silently breaks whenever bridge source drifts from the binary version.

Embedding the bridge and extracting via `belayer init` makes the bridge binary-versioned: upgrading the binary automatically upgrades the bridge on next `belayer init` or auto-init. Projects gitignore the extracted copy, so there's no confusion about whether it's source or generated.

### Deployment documentation

`docs/SANDBOXING.md` currently documents the Colima setup but conflates "what you need on a developer macOS machine" with "how the system is meant to run in production." Nightshift workers are always Linux VMs, so the macOS/Colima layer is purely a local dev tax. The docs should make that distinction explicit so future operators aren't confused about which steps are essential vs. which are macOS-only workarounds.

### Known security gaps

While mapping the current architecture for the docs rewrite, two gaps surfaced that need to be captured — not fixed in this PR, but documented so they're not forgotten:

1. **Exec path bypasses clamshell CLI.** `internal/sandbox/clamshell.go:213-273` calls `docker exec --env-file <tmpfile> <container>` directly for every bridge spawn. The clamshell CLI is only used for `sandbox create`, `sandbox connect`, `sandbox stop`, and `gateway start`. Any policy or credential-handling a future `clamshell exec` would enforce is absent on today's hot path.

2. **Agent sees raw credentials.** The env-file today contains `OPENCODE_GO_API_KEY=<real key>` (plus `HTTPS_PROXY`, `BELAYER_*`, `PYTHONPATH`). That file — and the process environment that inherits from it — is readable inside the container. A compromised agent can `cat /proc/self/environ`, exfiltrate the key, and the attacker has the real opencode credential. The correct shape is agent gets an opaque session token and the proxy does the credential swap at egress, but that's a separate design.

This design doc ships the bridge-embedding change today and documents both gaps in SANDBOXING.md. Gap #2 specifically is the next brainstorm.

## Architecture

### Bridge embedding

Parallel to the existing `DefaultAgents` pattern in `embed.go`:

```go
// DefaultBridge is the hermes_bridge Python package copied into a project's
// .belayer/hermes_bridge/ directory by `belayer init`. Unlike DefaultAgents,
// this copy is machine-generated and gitignored — it is always overwritten
// on init so the extracted bridge matches the binary version.
//
//go:embed all:hermes_bridge
var DefaultBridge embed.FS
```

`internal/cli/init.go` grows a `copyDefaultBridge(dst string) ([]string, error)` function that walks the embedded tree and writes files to `dst`, always overwriting (no `force` parameter). It skips `__pycache__/` directories and `*.pyc` files so stale bytecode from the host build never leaks into projects.

`scaffold()` calls `copyDefaultBridge()` after `copyDefaultAgents()` and includes the written paths in the result summary. The `--force` flag's scope is unchanged — it only affects `copyDefaultAgents()`, since bridge extraction already overwrites unconditionally.

`autoInitIfMissing()` is already wired into `belayer run`; the new bridge extraction runs through the same path, so binary upgrades propagate without requiring users to manually re-run `belayer init`.

### Gitignore update

The `gitignoreBlock` constant gets one new line:

```
/.belayer/hermes_bridge/
```

Added alongside the existing `/.belayer/runs/` and `/.belayer/worktrees/` entries. The marker comment and append logic are unchanged — projects that have already been initialized will get the new entry appended on the next `belayer init` run (or can add it manually).

### PYTHONPATH routing in clamshell mode

Current state: `cfg.BelayerRoot` holds the host path to the belayer repo root (where `hermes_bridge/` lives on the developer's machine). In clamshell mode this path is unreachable inside the container, but bridge imports work anyway because the bind-mounted workspace at `/workspace/` happens to contain a copy of `hermes_bridge/` and `/workspace` is added to PYTHONPATH.

New state: when spawning a clamshell bridge, `internal/daemon/agents.go` overrides `cfg.BelayerRoot = "/workspace/.belayer"` (the container's view of the extracted bridge's parent directory). `internal/bridge/bridge.go`'s existing PYTHONPATH construction appends `BelayerRoot` to PYTHONPATH, so `python3 -m hermes_bridge` resolves to `/workspace/.belayer/hermes_bridge/`.

Host (non-clamshell) mode is unchanged — `BelayerRoot` continues to point at the belayer repo root.

### SANDBOXING.md rewrite

Replace the current single-topology documentation with four sections:

1. **Architecture diagram** — updated Mermaid diagram reflecting the new `/workspace/.belayer/hermes_bridge/` import path. The existing "distribution gap" subgraph is removed. A dashed-line annotation on the `docker exec --env-file` edge calls out that this path is raw-credential today; details live in section 4.

2. **Ideal deployment: Nightshift Linux VM** — the target production topology. Daemon runs directly on a Linux VM, `belayer init` extracts the bridge, `~/.belayer.env` holds credentials (or they come from a secret store mount), `belayer run start` — three moving pieces: daemon, clamshell container, proxy container. No cross-compile, no VM abstraction layer, no manual bridge sync.

3. **Local dev: macOS via Colima** — the current developer workflow. Adds Colima VM hosting the daemon + Docker. Requires cross-compile (`GOOS=linux GOARCH=arm64 go build -tags clamshell`), `scp` binary to VM, install to `/usr/local/bin/belayer`, `~/.belayer.env` placed on the VM filesystem. Everything else identical to the prod topology. Documented as "a local dev tax for proving Linux patterns on macOS" so readers know it's not the intended deployment.

4. **Known security gaps** — explicit, prominent section covering:
    - **Exec bypasses clamshell CLI.** Bridge spawns go via `docker exec --env-file` directly rather than a `clamshell exec` wrapper. Fix requires a new clamshell CLI command and plumbing through `internal/sandbox/clamshell.go` `Exec`. Tracked as next design.
    - **Agent sees raw credentials.** Env-file contains the real `OPENCODE_GO_API_KEY`. A compromised agent can exfiltrate it trivially. Target design: opaque session token in env, real-key injection at the egress proxy (or a clamshell-owned credential broker). Explicitly out of scope here; documented so it isn't forgotten.

    Each gap gets a one-paragraph explanation of what's wrong and a pointer to the upcoming design. No half-fixes in this PR.

Both topology sections (2 and 3) share the same credential chain, proxy client lifecycle, and egress allowlist subsections — those details are common to any deployment.

## Components / files touched

- `embed.go` — add `DefaultBridge embed.FS`
- `internal/cli/init.go` — add `copyDefaultBridge()`; call from `scaffold()`; update `gitignoreBlock`
- `internal/daemon/agents.go` — override `cfg.BelayerRoot` to `/workspace/.belayer` when spawning clamshell bridges
- `docs/SANDBOXING.md` — rewrite with the two-topology + known-gaps structure
- `.gitignore` (the belayer repo's own) — add `/.belayer/hermes_bridge/` so the extracted copy in the belayer repo itself doesn't get committed if someone runs `belayer init` at the repo root

## Error handling

- `copyDefaultBridge()` returns an error if the embed walk fails or a file write fails. `scaffold()` propagates the error — init aborts rather than leaving a partial extraction on disk.
- `__pycache__/` and `*.pyc` filtering uses string prefix + suffix checks on the relative path. Defensive only — the embed is built from source in CI; pyc files shouldn't appear. If one does, it's skipped.
- If `.belayer/hermes_bridge/` already exists as a regular file (not a directory), the writer returns an error and init aborts with a clear message. Recovery is manual: the user deletes the stale file and re-runs init.

## Testing

- Unit: `copyDefaultBridge()` given a known embedded tree produces the expected files on disk, overwrites existing files, and skips `__pycache__` entries.
- Unit: `scaffold()` with a fresh directory writes the bridge alongside agents; running again overwrites the bridge but leaves agents untouched (without `--force`).
- Integration: `belayer init` in a fresh project produces `.belayer/hermes_bridge/__main__.py` matching the source file byte-for-byte.
- E2E: clamshell session on the Colima VM reaches `bridge:idle` with `api_calls >= 2`, importing the bridge from `/workspace/.belayer/hermes_bridge/` (verified via session logs showing the new PYTHONPATH entry). This validates the BelayerRoot override lands correctly.

## Out of scope

- **Credential swap / opaque session tokens.** The current raw-env-file exec path stays unchanged in this PR. Its replacement (clamshell-CLI-owned credential broker or proxy-side injection) gets its own brainstorm and design doc.
- **Clamshell exec wrapper.** Bridge spawns continue to call `docker exec` directly. The policy-aware `clamshell exec` wrapper is part of the credential-swap design, not this one.
- **PyPI publishing of `hermes_bridge`.** Embedding is the lightweight fix; PyPI is a separate future option if we ever need to support bridge use outside a belayer binary install.
- **Changes to agent template distribution.** `DefaultAgents` keeps its current "write once, `--force` to overwrite" semantics — user-editable, unlike the bridge.
