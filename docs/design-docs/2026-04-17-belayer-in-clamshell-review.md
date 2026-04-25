---
status: historical
---

# Belayer-in-Clamshell — Run Review

> Status: Historical — the exec plan this review accompanied has been
> archived. Kept as a snapshot of decisions made during the clamshell
> integration so future Belayer/Clamshell work has the assumption context.
> Purpose: give reviewers a single doc summarizing assumptions made, what changed, where Clamshell ends and Belayer begins, and how Belayer could be decoupled from Clamshell.
>
> **Note on file paths (2026-04-17):** Deployment artifacts referenced in the
> assumption table (Dockerfile, entrypoint, build script, host launcher,
> policy yaml) have been relocated to `extend-clamshell/integrations/belayer/`.
> Paths below describe where the files lived during the reviewed run.

## 1. Assumptions made during this mode

These are choices where I picked something because the spec was silent or the environment forced a call. Each is a candidate for pushback in review.

| # | Assumption | Where it shows up | Fallback if wrong |
|---|---|---|---|
| A1 | One belayer container per run (not per session) — trust unit = run | Whole exec plan; Dockerfile; entrypoint | Back to per-session sibling containers via old clamshell.go path (still in git history) |
| A2 | `belayer daemon` can run inside the clamshell sandbox image without the `clamshell` build tag | `scripts/build-belayer-clamshell-image.sh` passes `-tags ''` | Compile with `clamshell` tag and accept dead code for sandbox driver |
| A3 | The daemon's UDS socket lives at `/tmp/belayer/daemon.sock` as uid 1000 (sandbox) | `dockerfiles/belayer-clamshell-entrypoint.sh` | Use the workspace-mounted `.belayer/` dir instead (requires workspace layout convention) |
| A4 | Node 24 + pnpm 10.15.0 baked into the belayer-clamshell image (not the clamshell sandbox-rootfs) | `dockerfiles/belayer-clamshell.Dockerfile` | Fall back to modifying `images/sandbox-rootfs/Dockerfile` upstream |
| A5 | Port forwarding goes through `clamshell forward start`, not docker `-p` | `scripts/belayer-host` | Would require upstream `--publish` on `sandbox create` — explicitly not on roadmap |
| A6 | `provider create` for arielcharts uses `--from-existing OPENCODE_GO_API_KEY` to stay out of the repo | `scripts/belayer-host` `cmd_setup` | Accept `--secret` at the cost of shell history leakage |
| A7 | v1 egress allowlist is `opencode.ai:443` only; everything else is turned off or bind-mounted | `policies/belayer-in-clamshell-arielcharts.yaml` | Uncomment `providers.npm / providers.github` if the agent truly needs network installs or pushes |
| A8 | Identity gating is allowed to degrade to "unknown binary" when we use `--image belayer-clamshell:dev` (override path) | Upstream `internal/runtime/docker.py:_assert_image_exists` + my commit message | If per-agent attribution matters, mirror `/opt/agent-bin` + `runtime-identity-catalog.json` into the belayer-clamshell image |
| A9 | It is acceptable for `pnpm run dev` to listen on 3000/4000 and go through the gateway proxy (TLS terminated, headers rewritten) | Task 6 E2E will verify | Loosen policy per-run |

## 2. Changes introduced for this mode

Summary of artifacts added in `feat/belayer-in-clamshell`:

- `dockerfiles/belayer-clamshell.Dockerfile` — new image: debian-slim + node 24 + pnpm + python3 + tini + the belayer binary.
- `dockerfiles/belayer-clamshell-entrypoint.sh` — bootstraps `/tmp/belayer` and defaults `belayer daemon --socket` when invoked bare.
- `scripts/build-belayer-clamshell-image.sh` — cross-compiles `belayer-linux-<arch>`, builds the Dockerfile.
- `scripts/belayer-host` — host-side launcher: `setup` (idempotent provider registration) and `run` (gateway start, sandbox create, forward start, exec `belayer run start`).
- `policies/belayer-in-clamshell-arielcharts.yaml` — v5 clamshell policy scoping egress to opencode.ai:443.
- `tests/scripts/test-*.sh` — smoke tests: image build, entrypoint boot, launcher dry-run. (E2E script arrives in Task 6.)

Upstream (separate branch `feat/apikey-provider` in extend-clamshell):

- `clamshell sandbox create --image <ref>` — skips the clamshell-managed rootfs build/validation when you supply a pre-built image. Proxy image is still built per-launch; identity gating degrades to unknown-binary (host/port enforcement preserved via policy). Commit `cf08a2f`.

## 3. Clamshell vs. Belayer — what each actually owns

This is the boundary as it stands after the exec plan. Read left-to-right as "who enforces / owns this concern."

| Concern | Owner | Notes |
|---|---|---|
| Egress network policy (host:port allowlist, MITM, TLS terminate) | Clamshell | Policy file + gateway + iptables REDIRECT |
| Credential projection (`clak_` handles, real key in proxy memory) | Clamshell | `apikey` provider; belayer never sees the real key |
| Container lifecycle (create, stop, attach, forward ports) | Clamshell | `sandbox create`, `forward start`, `gateway start` |
| Filesystem sandboxing (read-only rootfs, tmpfs, masked paths) | Clamshell | `SandboxPolicy` in the YAML |
| Process sandboxing (seccomp, cap-drop, no-new-privs, memory/pids) | Clamshell | `ProcessPolicy` in the YAML |
| Identity attestation (which binary made which request) | Clamshell (degraded under `--image`) | `observe_process` + identity catalog; unknown-binary when override is used |
| Agent session lifecycle (supervisor spawn, heartbeats, exit detection) | **Belayer** | `internal/daemon`, bridge subprocess |
| Agent roster + messaging (agent A → agent B, broadcasts, events) | **Belayer** | SQLite-backed session bus |
| Agent identity (system prompt, tool loadout, model) | **Belayer** | `agents/<name>/` + Hermes `ephemeral_system_prompt` |
| Artifact registry (durable outputs) | **Belayer** | `artifacts` table in the session DB |
| PM completion gate (adversarial spec-vs-reality) | **Belayer** | `belayer finish` → auto-spawn PM |

The short version: Clamshell is a **trust boundary around one Linux process tree**; Belayer is the **multi-agent workflow engine** running inside that boundary. They are orthogonal — Clamshell doesn't know agents exist, Belayer doesn't know iptables exists.

## 4. Decoupling — running Belayer without Clamshell

The design target "belayer as the agent control plane" should hold regardless of sandbox. Here are three deploy shapes ranked by distance from today.

### 4a. Bare host (no sandbox at all)

**When:** dev loop; low-trust tasks where you already trust the agent.
**What changes:** nothing — `belayer daemon` runs on the host, `belayer run start` spawns bridges as normal child processes. No network interception. Credentials flow through the host env.
**Gap:** no egress control, no header rewriting — api keys are visible to the agent.
**Ship cost:** zero new code; this is already the dev path.

### 4b. Generic Docker (belayer-clamshell image + docker CLI, no clamshell policy)

**When:** CI with basic container isolation but no need for MITM policy.
**What changes:** replace `scripts/belayer-host` with plain `docker run --rm -v $PWD:/workspace -p 3000:3000 belayer-clamshell:dev`. Supply env vars directly; no header-rewriting proxy.
**Gap:** api keys are visible inside the container (same as 4a, just containerized).
**Ship cost:** a `scripts/belayer-container` sibling that skips the clamshell-specific bits (gateway/provider/forward). ~30 lines of bash; the image itself is already Clamshell-agnostic.

### 4c. Third-party sandbox (Firecracker, Kata, gVisor, …)

**When:** you want a different trust boundary than Docker+iptables.
**What changes:** the belayer-clamshell image is a stock OCI image — it boots on any runtime that speaks OCI. The things that move are:
1. **Egress policy**: replace clamshell's MITM proxy with whatever the host provides (e.g., Envoy sidecar, Firecracker's tap-device ACLs, a cloud egress gateway).
2. **Credential projection**: replace the `apikey` provider with whatever your environment gives you for short-lived handles (Vault-issued tokens, AWS IAM role, GCP Workload Identity). The header-rewrite contract is: something intercepts egress, swaps `clak_…` (or equivalent) for the real secret.
3. **Container lifecycle**: your orchestrator (k8s, nomad, firecracker-containerd, …) replaces `sandbox create` / `forward start`.
**Ship cost:** the belayer side needs a sandbox-adapter interface — today all of Belayer's knowledge of "where am I running" lives in env/CWD conventions baked into the image. If we wanted a clean split, the six runtime interfaces listed in `docs/PHILOSOPHY.md` are the natural seams (egress, credential, process, filesystem, observability, lifecycle).

### The minimal "Belayer Anywhere" interface

If we wanted to lift Belayer out of Clamshell entirely, the contracts we'd have to pin down are:

1. **Egress credential handle** — "give agent a token that the outbound network layer can resolve to a real secret, without the agent ever seeing the secret." Clamshell's `apikey` provider is one impl; a Vault sidecar is another.
2. **Sandbox lifecycle** — "create me a process tree with this rootfs and these resource caps; give me back something I can `exec` into." `sandbox create` today; `kubectl exec` or `firectl` tomorrow.
3. **Port exposure** — "forward this container port to <somewhere> that an outside human/service can reach, with audit." `clamshell forward start` today; a k8s Service + Ingress tomorrow.
4. **Policy/allowlist loader** — "here's the set of (host, port, protocol) tuples this run may reach." Clamshell v5 policy today; OpenPolicyAgent Rego tomorrow.
5. **Identity attestation** (optional) — "prove which binary inside the sandbox made this request." Today: identity catalog + wrapped binaries; also achievable via SPIFFE SVIDs or OCI runtime hooks.

A reasonable near-term goal is to publish these as five typed Go interfaces in `internal/sandbox/`, with today's Clamshell path as the first implementation. That gives us a seam to plug a `BareHostSandbox`, `GenericDockerSandbox`, or `FirecrackerSandbox` without touching the daemon or bridge code.

## 5. Known risks / open questions (to discuss)

- **A8 (identity degradation)**: is "unknown binary" acceptable for v1? We lose per-agent attribution on egress logs; host/port is still enforced.
- **A3 (socket path)**: `/tmp/belayer/daemon.sock` is tmpfs-local. If we ever want agents in sibling containers to reach the daemon, we need to bind the socket to `/run/agent` or similar.
- **A7 (allowlist scope)**: is opencode.ai alone really sufficient? If the agent tries to `git push`, we need `github.com:443`. The decision for v1 is "no, it can't" — confirm with user.
- **CI story**: none of the smoke tests run in CI because they require a Docker daemon. If we want green CI signal on this work, we need an integration lane with buildx or a Colima-in-GHA.

---

## 6. Change log (append-only, one line per commit)

- `e7ad401` feat(clamshell): add belayer-clamshell image + host build script
- `7651e6d` feat(clamshell): real entrypoint with socket bootstrap + smoke test
- `407b4c2` feat(clamshell): host-side launcher scripts/belayer-host
- `29141f3` feat(clamshell): reference allowlist policy for arielcharts E2E
- (upstream) `cf08a2f` feat(sandbox): add --image override to skip rootfs build
