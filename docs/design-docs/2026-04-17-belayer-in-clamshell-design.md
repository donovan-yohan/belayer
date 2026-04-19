---
status: current
created: 2026-04-17
branch: feat/belayer-in-clamshell
supersedes: 2026-04-17-clamshell-apikey-provider-design.md
implemented-by:
consulted-learnings: []
---

# Belayer-in-clamshell: one-container-per-run sandbox model

> **Note on file paths (2026-04-17):** Deployment artifacts referenced below
> (Dockerfile, entrypoint, build scripts, policy yaml, seccomp profile, E2E
> script, host-side launcher) have been relocated from belayer to
> `extend-clamshell/integrations/belayer/` so clamshell-specific deployment
> config doesn't live in the belayer runtime repo. Paths in this document
> describe the design as originally implemented; the files themselves moved
> without changing shape.

## Goal

Run the entire belayer daemon — plus every bridge subprocess it spawns — inside a single `extend-clamshell` container per run. The container is the trust boundary. Egress goes through the clamshell MITM proxy with an allowlist. Provider secrets never enter the container: they stay in the host-side clamshell `ProviderStore`, projected into the container as `clak_<handle>` via the already-committed `apikey` provider type (`extend-clamshell@feat/apikey-provider`, commit `393fe61`).

Acceptance proof: a macOS → Colima → clamshell container → belayer daemon topology where agents check out `arielcharts`, edit its source, and run `pnpm run dev` — with web + server dev servers reachable from the macOS host via forwarded ports.

## Why

The status quo runs belayer on the host and spawns **one clamshell container per session** via `docker exec --env-file`, which forces the daemon to bear the credential-brokering burden (`SANDBOXING.md` §1 and §2). The prior design doc [`2026-04-17-clamshell-apikey-provider-design.md`](2026-04-17-clamshell-apikey-provider-design.md) closed those gaps by teaching belayer to drive `clamshell sandbox create --provider apikey=...` itself. That works, but the belayer Go surface that makes it work — `internal/sandbox/clamshell.go`, `internal/bridge/bridge.go` env-file plumbing, policy injection, per-session orchestration — is the actual complexity. The apikey design solves the leak, not the plumbing.

Under the decision recorded below — "the whole run is one trust unit" — per-agent isolation inside a run is not a goal. That means the plumbing can collapse to one container and the apikey machinery can be reused with no modifications. Net: same security posture on credentials, ~900 fewer lines of Go, no per-session Docker orchestration.

## Decision recorded

1. **Trust unit = run, not session, not agent.** A compromised agent implies a compromised run. The outer VM + clamshell container + per-run credential scoping is the only defense; we do not add a per-agent boundary.
2. **Keep the apikey provider.** It is the cheapest way to keep real secrets off the container disk and out of `/proc/*/environ`. Committed upstream at `feat/apikey-provider` (393fe61); untracked tests also ready. We consume it unchanged.
3. **Drop the per-session clamshell driver.** `internal/sandbox/clamshell.go` (build-tag `clamshell`), its stub, and the env-file temp-file machinery go away.
4. **Bridges are plain subprocesses of the daemon.** No `docker exec`. The daemon calls `exec.Cmd(python3, -m, hermes_bridge, ...)` directly.
5. **arielcharts + `pnpm run dev` is the E2E acceptance test.** If that loop works (agent edits code, dev server starts, ports exposed, HMR functional), the topology is proven.

## Architecture

### Topology

```text
macOS host
└── Colima VM (Linux, Docker, iptables REDIRECT)
    │
    ├── clamshell (Python CLI + control-plane state at ~/.clamshell/control-plane/)
    │   ├── ProviderStore: providers/opencode.json (holds real OPENCODE_GO_API_KEY)
    │   └── SandboxAttachmentStore: sandbox-attachments/belayer-run-<id>.json
    │                              (holds clak_<handle>, endpoints, auth header/scheme)
    │
    ├── clamshell-proxy-<id> (sidecar container, started by `clamshell gateway start`)
    │   └── MITM CONNECT proxy on proxy.internal:3128
    │       - reads /controlplane (ro mount) for attachment records
    │       - rewrites Authorization: Bearer clak_xxx → Authorization: Bearer <real>
    │       - enforces allowlist: opencode.ai:443, registry.npmjs.org:443 (opt-in), …
    │
    └── clamshell-belayer-<id> (ONE container per run)
        ├── entrypoint: belayer daemon (PID 1-ish, via tini or equivalent)
        ├── env:
        │   OPENCODE_GO_API_KEY=clak_xxx         (handle, not real)
        │   HTTPS_PROXY=http://proxy.internal:3128
        │   BELAYER_SOCKET=/run/belayer/daemon.sock
        ├── bridge subprocesses (spawned by daemon, NO docker exec):
        │   ├── supervisor: python3 -m hermes_bridge
        │   ├── specialist-1: python3 -m hermes_bridge
        │   └── pm (on finish)
        ├── workspace bind-mount: /workspace = ~/Documents/Programs/personal/arielcharts
        ├── dev-server port forwards: `clamshell forward start 3000|4000 <sandbox> --remote-port …` (gateway-proxied; web + server)
        └── node + pnpm available in image (for `pnpm run dev` and friends)
```

### Credential chain (what changes)

**Before (today):**
1. Daemon on host reads `OPENCODE_GO_API_KEY=<real>` from `~/.belayer.env` via godotenv.
2. Daemon writes temp env-file with real secret.
3. `docker exec --env-file <tmpfile> <bridge-container>` hands secret to the bridge process.
4. Bridge sees real secret in `/proc/self/environ`.

**After:**
1. On host: operator (or `belayer host-init`) runs `clamshell provider create --type apikey --name opencode --from-existing OPENCODE_GO_API_KEY --project OPENCODE_GO_API_KEY --endpoints opencode.ai`. Real secret lands in clamshell's `ProviderStore` (once).
2. Belayer launcher script on host runs `clamshell sandbox create belayer-run-<id> --provider apikey=opencode --image belayer:<ver> --workspace ~/Documents/Programs/personal/arielcharts`. Clamshell mints `clak_xxx`, writes attachment record, starts container with `OPENCODE_GO_API_KEY=clak_xxx` in env.
3. Belayer daemon inside container sees `OPENCODE_GO_API_KEY=clak_xxx`. Spawns bridges as subprocesses; they inherit env and also see the handle.
4. Bridge makes `POST https://opencode.ai/zen/go/v1/...` with `Authorization: Bearer clak_xxx` via `HTTPS_PROXY`.
5. Proxy reads its `/controlplane` ro mount, finds the attachment for this sandbox, rewrites the `Authorization` header to `Bearer <real>`, forwards to opencode.ai.
6. Response streams back unchanged. Real key never touched container filesystem, process env, or memory.

### Allowlist (per-run, operator-declared)

Allowlist lives in a policy file the launcher passes to `clamshell sandbox create --policy`. Union of everything any agent in the run might need. For the arielcharts E2E proof:

```yaml
# policies/belayer-in-clamshell-arielcharts.yaml
tcp_endpoints:
  - { name: opencode, host: opencode.ai, port: 443 }
  - { name: npmjs,    host: registry.npmjs.org, port: 443 }   # pnpm install, if not pre-installed
  - { name: github,   host: github.com, port: 443 }           # clone / git push if agents push
  - { name: localhost-web,    host: 127.0.0.1, port: 3000 }   # loopback, implicit but explicit
  - { name: localhost-server, host: 127.0.0.1, port: 4000 }
```

Loopback is inherently allowed by clamshell (MITM is only on outbound); listing it is documentation, not enforcement.

### Workspace + dev-server access

- `arielcharts` is bind-mounted at `/workspace` inside the container (`clamshell sandbox create --workspace <host-path>`).
- node + pnpm installed in the image during build. Pin to arielcharts' declared `engines.node >=24.0.0` and `packageManager: pnpm@10.15.0`.
- `pnpm install` runs either pre-container (cached in node_modules on host, bind-mounted through) or post-start inside the container (requires registry.npmjs.org in allowlist).
- Dev servers: `pnpm run dev` starts `@arielcharts/server dev` + `@arielcharts/web dev` in parallel. Ports 3000 (web) and 4000 (server, TBD — confirm in E2E) exposed via `docker run -p`. macOS host reaches them at `http://localhost:3000` thanks to Colima's port forwarding.
- Agents call `pnpm run dev` via the `terminal` belayer tool. Dev servers run until the agent calls `belayer finish` or the run is torn down.

### Container image

A new `belayer-clamshell` image, built from the upstream clamshell base image:

```dockerfile
# (sketch, not final)
FROM ghcr.io/paywithextend/clamshell-sandbox-base:latest

# node + pnpm for JS/TS workspaces like arielcharts
RUN curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
 && apt-get install -y nodejs \
 && corepack enable \
 && corepack prepare pnpm@10.15.0 --activate

# belayer binary (cross-compiled on host, copied in)
COPY belayer-linux-arm64 /usr/local/bin/belayer

# entrypoint: start daemon, keep PID 1 alive for run lifetime
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
```

The entrypoint script is small: `exec belayer daemon --socket /run/belayer/daemon.sock` (plus any `belayer run start` invocation the operator passes as CMD args). The daemon stays PID 1; when it exits, the container exits.

### Launcher UX (host-side)

The macOS-side launcher is a shell script or a small Go CLI — not the belayer binary itself, because the belayer binary lives inside the container now. Proposed surface:

```bash
# One-time (idempotent):
belayer-host setup   # wraps `clamshell provider create --type apikey ...`

# Per run:
belayer-host run \
  --workspace ~/Documents/Programs/personal/arielcharts \
  --task "wire up a playwright test for the homepage hero" \
  --publish 3000:3000 --publish 4000:4000
# ↓ expands to:
# clamshell gateway start
# clamshell sandbox create belayer-run-$(uuid) \
#   --provider apikey=opencode \
#   --image belayer-clamshell:<ver> \
#   --workspace ~/Documents/Programs/personal/arielcharts \
#   --policy .../policies/belayer-in-clamshell-arielcharts.yaml
# clamshell forward start 3000 belayer-run-<id> --remote-port 3000 --background
# clamshell forward start 4000 belayer-run-<id> --remote-port 4000 --background
# docker exec -it clamshell-belayer-<id> belayer run start --task "..."
```

This is a thin shim. It does not replace the per-session clamshell driver we're deleting — it is user-facing wrapping, not runtime orchestration, and it sits outside the trust boundary.

## What we get rid of

**Delete outright:**
- `internal/sandbox/clamshell.go` (472 lines) — per-session Docker orchestration.
- `internal/sandbox/clamshell_stub.go` + `clamshell_stub_test.go` — stub for default builds; no longer needed since `sandbox.mode: clamshell` is no longer a daemon-level concept.
- `internal/sandbox/clamshell_test.go` — tests for the above.
- `-tags clamshell` build tag. Daemon compiles one way.
- `docker exec --env-file` code path in `bridge.BuildEnv` (the HTTPS_PROXY/HOME/HTTPProxy plumbing keyed on clamshell-only assumptions — retain generic HTTPS_PROXY behavior, remove the rest).
- `internal/sandbox/` sandbox driver registry, if clamshell was the only non-stub impl. Worth auditing.

**Shrink:**
- `bridge.BuildEnv` — drops HTTPProxy-as-clamshell-signal, drops HOME rewriting (daemon and bridge share an env inside the container), drops `BELAYER_API_KEY`-as-clamshell-override (bridges just read `OPENCODE_GO_API_KEY` from env, which is the handle).
- `docs/SANDBOXING.md` — rewrite around the one-container-per-run topology. §1 and §2 are **neither gaps nor accepted tradeoffs** under the new model — they are architecturally impossible in the way they were posed, because there is no host-to-container creds handoff at all.

**Retain, unchanged:**
- Everything above the sandbox boundary: session bus, roster, messages, events, artifacts, PM gate, agent identity loader, hermes_bridge embedding.
- The upstream clamshell apikey work (`feat/apikey-provider` in extend-clamshell) — consumed as-is.

**Add:**
- `dockerfiles/belayer-clamshell.Dockerfile` — image definition.
- `scripts/belayer-host` (or similar) — host-side launcher shim.
- `policies/belayer-in-clamshell-arielcharts.yaml` — reference allowlist for the E2E proof.
- CI: build the image on tags/main, push to a registry the Colima VM can pull from (or `docker save | docker load` for local dev).

## E2E proof plan (arielcharts + pnpm run dev)

Six-step acceptance loop; each step has an unambiguous pass/fail.

1. **Image builds.** `docker build -t belayer-clamshell:e2e -f dockerfiles/belayer-clamshell.Dockerfile .` on the Colima VM succeeds. Binary, node, pnpm, corepack present.
2. **Provider registration persists.** `clamshell provider create --type apikey --name opencode --from-existing OPENCODE_GO_API_KEY --project OPENCODE_GO_API_KEY --endpoints opencode.ai` on the Colima VM writes a provider record. `clamshell provider list` shows `opencode (apikey)`.
3. **Sandbox boot, handle projected.** `clamshell sandbox create belayer-e2e --provider apikey=opencode --image belayer-clamshell:e2e --workspace <arielcharts> --policy <policy>` brings up the container + proxy; ports come up next via `clamshell forward start 3000 belayer-e2e --remote-port 3000 --background` (and the same for 4000) — clamshell has no `--publish` on create. `docker exec clamshell-belayer-e2e env | grep OPENCODE_GO_API_KEY` returns `clak_*`, not the real key.
4. **Daemon reaches bridge idle.** Inside the container, `belayer run start --task "say hi"` produces a supervisor that completes ≥2 LLM round-trips end-to-end (proxy logs show `Authorization: Bearer clak_...` rewritten to `Bearer <real>`, opencode.ai returns 200, session reaches `bridge:idle`).
5. **Agent edits arielcharts.** A supervisor task "add a comment to apps/web/src/main.tsx explaining what it does" results in a file diff visible from the macOS host (because `/workspace` is bind-mounted to `~/Documents/Programs/personal/arielcharts`).
6. **Dev server reachable.** A supervisor task "run pnpm install if needed, then run pnpm run dev in the background and confirm the web server responds on localhost:3000" produces a process that the macOS host can `curl -sSf http://localhost:3000 | head -5` successfully. Bonus: `curl` the server port too.

Step 1 and 2 can be manual the first time. 3-6 should be a single shell script under `tests/e2e/belayer-in-clamshell.sh` that a human or CI can re-run.

## Open questions (flag before implementation)

1. **pnpm-install provenance.** If the agent runs `pnpm install` inside the container, the allowlist must include `registry.npmjs.org` (and `fastly.jsdelivr.net` etc. for some packages). Is that an acceptable broadening? Alternative: require `pnpm install` pre-run on host, node_modules bind-mounted. Decide in step 1 of E2E.
2. **Port forwarding discovery.** Today's belayer doesn't know which ports agents will expose. We hardcode 3000+4000 in the launcher for the arielcharts proof; generalize later. Not a blocker.
3. **Image distribution on Colima.** Colima can pull from a registry, or load a local `docker save` tarball. For first proof: `docker save belayer-clamshell:e2e | colima ssh -- docker load`. For ongoing dev: push to ghcr or similar.
4. **Does the daemon need PID 1 niceties?** Zombie-reaping, signal forwarding. If bridges spawn short-lived shell subprocesses a lot, running daemon as PID 1 without `tini` is risky. Either use `--init` on `docker run` (clamshell may not expose this) or bake `tini` into the image and `ENTRYPOINT ["tini", "--", "belayer", "daemon"]`.
5. **What happens on `belayer finish`?** The run container can't really "shut itself down cleanly" — that's a job for the host-side launcher (e.g., `docker stop` after `belayer finish` completes). Minor lifecycle wiring, not architectural.
6. **Does the apikey provider cover `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` for free?** Anthropic uses `x-api-key: <key>` (no scheme prefix). Apikey supports this via `auth_scheme: ""`. OpenAI uses Bearer. Both land as pure-config when/if we wire them. Not in scope for v1 proof — opencode-only.

## Out of scope

- **Per-agent sandboxing inside a run.** If a future agent type needs a stricter policy than the run, it spawns its own clamshell. That's a separate design.
- **Multi-session daemons.** Today's "one daemon per workspace, many sessions" model collapses to "one container per run" for Nightshift. Local dev with many concurrent sessions-per-workspace is a regression we accept.
- **OAuth / refreshing creds.** Apikey is static-secret-only. OAuth gets its own provider type if/when.
- **Removing `BELAYER_*` env entirely.** Daemon and bridges still pass `BELAYER_SOCKET`, `BELAYER_TOOLS`, etc. — those are internal coordination, not credentials, and they stay.
- **Registry publication pipeline.** v1 uses local `docker save`/`load`. A real CI/CD path to ghcr is follow-up.

## Testing

- **Go unit:** delete `internal/sandbox/clamshell_*_test.go`. Verify `bridge.BuildEnv` no longer touches HOME/clamshell-specific branches.
- **Integration:** `go test ./...` on Colima VM inside the container — proves the daemon runs correctly under the in-container execution model.
- **E2E:** `tests/e2e/belayer-in-clamshell.sh` runs steps 3-6 above, emits pass/fail per step.
- **Security regression:** explicit test that `docker exec clamshell-belayer-<id> cat /proc/1/environ` does not contain the real `OPENCODE_GO_API_KEY` value. Only `clak_` handles.

## Rollout

1. Land this design.
2. Produce the exec plan (`/harness:plan`).
3. Execute incrementally: image first, then delete sandbox driver, then E2E script, then arielcharts proof.
4. Document in SANDBOXING.md.
5. Supersede `2026-04-17-clamshell-apikey-provider-design.md` (its belayer-integration section is replaced; its extend-clamshell-integration section is what's already in `feat/apikey-provider`).

## References

- Prior superseded design: `docs/design-docs/2026-04-17-clamshell-apikey-provider-design.md`
- Upstream apikey WIP: `extend-clamshell@feat/apikey-provider`, commit `393fe61`
- Current SANDBOXING topology: `docs/SANDBOXING.md`
- Upstream apikey code to consume: `internal/gateway/{providers,attachments}.py`, `internal/proxy/{apikey_credentials,server}.py`, `internal/runtime/docker.py`
