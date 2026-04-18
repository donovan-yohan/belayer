# Clamshell CLI reconnoiter (feat/apikey-provider @ 3fe6f7e)

Ran against `clamshell_cli` from `/path/to/extend-clamshell` on branch `feat/apikey-provider`, commit `3fe6f7e` ("test(apikey): add CLI surface + 42 unit tests").

## Confirmed flags

### `clamshell sandbox create`

```text
--name NAME                       [required]
--policy POLICY                   [required]
--workspace WORKSPACE             [required]
--provider TYPE=NAME              [repeatable]
--session-backend {builtin-tmux,external}
--pty-api-listen ADDR
--bootstrap-repo URL
--bootstrap-ref REF
--bootstrap-dir PATH
--bootstrap-setup CMD             [repeatable]
--bootstrap-command CMD
--wait-timeout N
--wait-interval N
--no-wait
```

### `clamshell provider create`

```text
--name NAME                       [required]
--type PROVIDER_TYPE              [required]
--endpoint URL
--secret VALUE
--from-existing [ENV_VAR]
# github:
--credential KEY[=VALUE]
# aws:
--access-key-id / --secret-access-key / --role-arn / --region /
--session-duration / --external-id / --session-policy-file
# apikey:
--project CSV                     [required for apikey]
--endpoints CSV                   [required for apikey]
--auth-header NAME                [default Authorization]
--auth-scheme PREFIX              [default Bearer; "" for x-api-key shape]
```

### `clamshell forward start`

```text
local_port                        [positional, 0 = ephemeral]
sandbox                           [positional]
--remote-port N                   [defaults to local_port]
--bind-address ADDR               [loopback]
--background
```

### `clamshell gateway`

`start | status | stop | config` — standard daemon lifecycle, no surprises.

## Gaps vs. our plan assumptions

| Assumption in plan | Reality | Impact |
|---|---|---|
| `provider create --type apikey` with `--project/--endpoints/--auth-header/--auth-scheme` | **Present.** All four flags ship on `feat/apikey-provider`. | No action. Plan Tasks 4-5 can use these verbatim. |
| `sandbox create --provider TYPE=NAME` | **Present, repeatable.** | No action. |
| `sandbox create --image <image>` | **Missing.** Image is hardcoded at `internal/runtime/docker.py:55` as `IMAGE_REPO = 'extend-clamshell-sandbox'`; built from `images/sandbox-rootfs/Dockerfile` on every `sandbox create`. No config-file override either. | **Gap-fill: modify `images/sandbox-rootfs/Dockerfile` upstream** to add what belayer needs (node 24, pnpm via corepack, belayer binary, python ≥3.11 for hermes_bridge). Keeps one image, no CLI change needed. See "Gap-fill plan" below. |
| `sandbox create --publish <host>:<container>` | **Missing.** Clamshell does not expose Docker port bindings; it uses a gateway-side port forwarder (`clamshell forward start <local_port> <sandbox>`) that proxies through the gateway. | **Better than -p for us.** Forwards go through the gateway (logged, policed). Launcher will call `clamshell forward start 3000 <sandbox>` + `4000 <sandbox>` after create. No CLI change needed. |

## Base image inspection

Existing `images/sandbox-rootfs/Dockerfile` ships:
- Node 22.22.2 (arielcharts needs ≥24 — **gap**)
- No pnpm (arielcharts uses pnpm 10.15.0 — **gap**)
- python 3.13, git, curl, iptables, procps, tmux, awscli, gh
- `/workspace` as WORKDIR, `sandbox` (uid 1000) / `sandbox-alt` (uid 1001) users
- Identity-catalog wrappers (`codex`, `claude-code`) injected at build time via `generate_wrappers.py`

Everything we need other than node 24 + pnpm + the belayer binary is already there.

## Gap-fill plan

**Chosen path: modify `extend-clamshell/images/sandbox-rootfs/Dockerfile` on `feat/apikey-provider`.**

Two minimal changes:

1. **Bump node base** in `node_runtime` stage from `node:22.22.2-bookworm-slim` to `node:24.x-bookworm-slim`. Enable corepack so `pnpm` is available on PATH:
   ```dockerfile
   RUN corepack enable && corepack prepare pnpm@10.15.0 --activate
   ```
   Rationale: node 22 is only there to host `codex` + `claude-code` npm packages; both work on node 24 LTS. No separate node 22 lane needed.

2. **Ship the belayer binary** at `/opt/belayer/belayer`. Since the binary is large and we rebuild it often, bind-mount it at `sandbox create` time instead of baking it in. Bind-mount path: `--workspace` already supports arbitrary host paths, but the workspace is the *agent's* working dir — we do NOT want belayer in there. Options:
   - (a) Add `--extra-mount HOST:CONTAINER` flag upstream. Minor CLI change.
   - (b) Put the belayer binary inside the `--workspace` directory and have the launcher arrange the workspace layout (e.g., `<workspace>/.belayer/bin/belayer` + `<workspace>/arielcharts/...`). No upstream change; ugly, and puts belayer in the agent's `workspace`.
   - (c) Add a `COPY belayer /opt/belayer/belayer` step to the sandbox Dockerfile and rebuild the image on belayer changes. Slow iteration; bake image against tagged belayer releases.
   - (d) **Host the belayer binary at a local HTTP url and download during `--bootstrap-setup`.** Requires policy to permit that host. Ugly.

   **Pick (a) for v1.** Adding `--mount HOST:CONTAINER:ro` (or a smaller `--belayer-binary PATH` flag) upstream is ~20 lines in `clamshell_cli/commands/sandbox.py` + plumbing to `docker run -v`. This keeps image builds cheap and belayer iteration fast.

   If (a) turns out to be a bigger upstream change than expected, fall back to (c) — bake belayer into the image, accept slow iteration.

## Plumbing decisions captured

- **Policy format**: We use the existing policy file format (no CLI extension needed). See `policies/` in extend-clamshell for examples.
- **Workspace**: Host-side `--workspace /path/to/run-root` bind-mounts at `/workspace` in-container. Our launcher puts `arielcharts/` + `.belayer/` under the run-root before `sandbox create`.
- **Ports**: `clamshell forward start 3000 <sandbox>` after create. The launcher waits for sandbox-ready then starts forwards.
- **Provider wiring**: `clamshell provider create --type apikey --name opencode --from-existing OPENCODE_GO_API_KEY --project OPENCODE_GO_API_KEY --endpoints opencode.ai`, then `clamshell sandbox create --provider apikey=opencode ...`.

## Decisions to log in plan

Two Decision Log entries to add:

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-04-17 | Exec/Task 1 | Modify upstream `extend-clamshell/images/sandbox-rootfs/Dockerfile` (bump node → 24, enable corepack+pnpm) rather than adding `sandbox create --image` CLI flag | Single-image design keeps clamshell simple; node 24 doesn't break codex/claude-code; bump is a one-line change to the base image. |
| 2026-04-17 | Exec/Task 1 | Ship belayer binary via a new upstream `--mount HOST:CONTAINER:ro` (preferred) or baked into the image (fallback), not via workspace bind-mount | Agents should not see the belayer binary in their workspace. A mount flag is ~20 lines upstream; the fallback bake-in path is available if the flag change stalls. |

## Open question for the user

**Do we add `--mount` upstream or bake belayer into the image?**

`--mount` is cleaner but requires another upstream PR. Baking in is uglier but keeps this work self-contained on the existing branch. Defaulting to `--mount` and planning the fallback as Option (c) — will pursue the PR path first and fall back on friction.
