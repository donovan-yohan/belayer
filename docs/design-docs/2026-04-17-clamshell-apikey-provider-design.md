---
status: superseded
created: 2026-04-17
branch: master
supersedes:
superseded-by: 2026-04-17-belayer-in-clamshell-design.md
implemented-by:
consulted-learnings: []
---

> **Superseded 2026-04-17** by [`2026-04-17-belayer-in-clamshell-design.md`](2026-04-17-belayer-in-clamshell-design.md).
>
> The upstream `apikey` provider design in this doc is what landed in `extend-clamshell@feat/apikey-provider` (commit `393fe61`) and is still current — keep as the upstream spec reference. What changed is the **belayer-side integration**: we are no longer adding a `sandbox.providers` config block plus per-session `clamshell sandbox create --provider` calls from the belayer daemon. Instead, belayer runs inside one clamshell container per run, and the `apikey` provider is consumed at container-start time by the host-side launcher. See the new design doc.

# Clamshell `apikey` provider type + belayer integration

## Goal

Add a new `apikey` provider type to `extend-clamshell` that brokers long-lived HTTPS-header API-key secrets via the same opaque-handle + proxy-rewrite pattern that already backs `github`. Belayer's clamshell mode consumes it to project `OPENCODE_GO_API_KEY` (and, trivially, `OPENAI_API_KEY` / `ANTHROPIC_API_KEY`) into the sandbox as an opaque `clak_<random>` handle. The real secret never enters the container's environment, process table, or env-file on disk.

This closes `docs/SANDBOXING.md` §2 (agent sees raw credentials) in full. It closes §1 (bridge exec bypasses the clamshell CLI) in spirit — the reason §1 mattered was that the raw-env-file exec was the *exfiltration surface*; remove the raw creds and the exec path is no longer sensitive. A dedicated `clamshell exec` wrapper is no longer needed for this class of credential.

## Motivation

Today the daemon spawns every bridge subprocess with `docker exec --env-file <tmpfile>`, where `<tmpfile>` contains `OPENCODE_GO_API_KEY=<real>`. The file lives on the host for the duration of the exec, and the container process inherits the real secret. A compromised agent reads `/proc/self/environ` and walks away with a working opencode credential.

Upstream `extend-clamshell` already solved this problem for GitHub PATs and AWS STS credentials. The primitives exist:

- Host-side `ProviderStore` keeps the real secret.
- Per-sandbox `SandboxAttachmentStore` mints an opaque handle and records which env keys it projects into.
- `docker run -e KEY=<handle>` ships the handle (not the secret) into the container; `docker exec` into the same container inherits the same env.
- The MITM proxy, reading attachment records from a read-only `/controlplane` mount, rewrites outbound headers to substitute `handle → real secret` for declared endpoints.

What's missing is a provider type for the generic case: "my credential is a value in an HTTPS header, on a declared host, for the lifetime of the secret." That's what `apikey` adds.

`github` continues to exist because GitHub credential handling is weirder than header rewriting (URL-embedded PATs for git protocol, dual env projection `GITHUB_TOKEN`/`GH_TOKEN`, dual-host dispatch for `github.com` vs `api.github.com`, dual-scheme acceptance `Bearer`/`Token`). `aws` continues to exist because STS mints ephemeral credentials via `credential_process` rather than rewriting headers. `apikey` is the clean header-rewrite case.

## Architecture

### Provider type

```bash
clamshell provider create \
  --type apikey \
  --name opencode \
  --secret OPENCODE_GO_API_KEY=<real key> \
  --project OPENCODE_GO_API_KEY \
  --endpoints opencode.ai \
  --auth-header Authorization \
  --auth-scheme Bearer
```

- `--secret KEY=VALUE` — the real credential. Value is sensitive; CLI supports `--from-existing` to pull from an env var without it appearing in argv.
- `--project` — comma-separated env keys to project into the sandbox. Each receives the same `clak_<handle>`. One handle, N env keys, so operators can cover legacy aliases (`OPENAI_API_KEY` + anything that reads `OPENAI_KEY`, say) from one provider.
- `--endpoints` — comma-separated hosts the proxy is allowed to rewrite against. Required; there is no default because `apikey` does not know which service this secret is for.
- `--auth-header` — HTTP header name that carries the credential. Default `Authorization`.
- `--auth-scheme` — scheme prefix before the credential value. Default `Bearer`. Empty string means "no prefix, raw value" — this is the Anthropic `x-api-key: <key>` case.

### Handle shape

`clak_<url-safe-random>`, mirroring `clgh_` (github) and `claws_` (aws). Regex `^clak_[A-Za-z0-9_-]+$` for proxy-side detection. Handles are per-attachment, not per-provider; rotating the sandbox mints a new handle, stopping the sandbox revokes it.

### Proxy rewrite

New module `internal/proxy/apikey_credentials.py`, structured identically to `github_credentials.py`:

- `SandboxAPIKeyCredentialResolver.resolve_handle(sandbox_name, handle) → (real_secret, auth_header, auth_scheme)` — reads the attachment record, validates the handle is registered for this sandbox, returns the tuple.
- `rewrite_apikey_headers(host, method, path, headers, sandbox_name, resolver)` — matches the host against the attachment's declared endpoints, then for the declared `auth_header`:
  - If `auth_scheme` is non-empty: expects header value to start with `<auth_scheme> clak_...`, rewrites to `<auth_scheme> <real_secret>`.
  - If `auth_scheme` is empty: expects header value to equal `clak_...`, rewrites to `<real_secret>`.

The dispatcher in `internal/proxy/rewriter.py` (or wherever github/aws are wired) gains an `apikey` branch before the default pass-through. Non-declared hosts bypass the rewriter entirely and still see the handle — which will fail authentication upstream, by design.

### Attachment projection

`ATTACHABLE_PROVIDER_TYPES` in `internal/gateway/attachments.py` grows to include `apikey`. `SandboxAttachmentStore.prepare_attachment_bundle()` for apikey attachments mints the handle and records `{projected_env_keys, auth_header, auth_scheme, endpoints}` in the attachment. `internal/runtime/docker.py` gains an `apikey_attachment` loader mirroring `github_attachment`, projecting the env dict at `docker run -e` time. The proxy's `/controlplane` read-only mount already exposes attachment records; no new plumbing there.

### Belayer config

Belayer gains a `sandbox.providers` block in `.belayer/config.yaml`:

```yaml
sandbox:
  providers:
    - name: opencode
      type: apikey
      secret_env: OPENCODE_GO_API_KEY   # daemon reads real value from here
      project: [OPENCODE_GO_API_KEY]    # sandbox env keys to project the handle into
      endpoints: [opencode.ai]
      auth_header: Authorization        # optional, defaults shown
      auth_scheme: Bearer
```

On `sandbox create`, `internal/sandbox/clamshell.go` walks this list, idempotently upserts each provider (`clamshell provider create ... --from-existing` or equivalent, read from the daemon's env), and passes `--provider opencode=<attachment-name>` to `clamshell sandbox create`. Bridge env construction in `internal/bridge/bridge.go` redacts any env key listed in a provider's `project:` array from the env-file so the real value, still present in the daemon's `os.Environ()`, never lands on disk.

## Components / files touched

**extend-clamshell (upstream):**
- `internal/gateway/providers.py` — add `APIKEY_PROVIDER_TYPE`, extend `_resolve_projected_env_keys` (apikey accepts caller-supplied list with non-empty validation), extend `_resolve_provider_endpoint` (apikey requires operator declaration), persist `auth_header` + `auth_scheme` in provider record.
- `internal/gateway/attachments.py` — add apikey to `ATTACHABLE_PROVIDER_TYPES`, mint `clak_` handle, write attachment with projection + header config.
- `internal/runtime/docker.py` — mirror `_load_github_attachment_projection` / `_validate_github_attachment_projection` for apikey; merge into `docker run -e` env.
- `internal/proxy/apikey_credentials.py` **(new)** — resolver + rewriter, structured as github_credentials.py.
- `internal/proxy/rewriter.py` (or wherever dispatch lives) — register apikey branch.
- `clamshell_cli/commands/provider.py` — `--type apikey` with `--secret`, `--project`, `--endpoints`, `--auth-header`, `--auth-scheme`, `--from-existing`.

**belayer:**
- `internal/cli/init.go` — scaffold commented-out `sandbox.providers` example in `.belayer/config.yaml`.
- Config schema (wherever clamshell config is parsed) — add `sandbox.providers` list with the fields above.
- `internal/sandbox/clamshell.go` `Create` — iterate providers, idempotent upsert, pass `--provider` flags to `sandbox create`.
- `internal/bridge/bridge.go` `BuildEnv` — redact projected keys from the env-file.
- `docs/SANDBOXING.md` — update §2 to "closed (see design)", update §1 to "closed in practice: raw creds removed from env-file; no longer the exfiltration surface." Update the Mermaid diagram's warning annotation.

## Error handling

- Provider creation fails (bad secret, daemon env var unset, upstream CLI error) → sandbox create aborts with the upstream error message surfaced as-is. No partial state: attachment record is only written after provider create succeeds.
- Handle not resolvable at proxy time (stale mount, deleted attachment, wrong sandbox) → proxy returns 502 to the agent. Surfaces as an LLM call failure with a clear log line; not silently passed through with the handle.
- Agent enumerates `/proc/self/environ` → sees `OPENCODE_GO_API_KEY=clak_abc123...`. Handle is useless off-host; expected outcome.
- Daemon crash mid-`sandbox create` → provider record persists (idempotent on retry), partial attachment cleaned up on next `sandbox stop`.
- Operator declares `auth_scheme: ""` but the SDK sends `Authorization: Bearer clak_...` (wrong scheme) → rewriter's scheme check fails, request passes through unmodified, upstream rejects. Diagnosable from proxy logs.

## Testing

- **Upstream unit:** `tests/proxy/test_apikey_credentials.py` mirrors `test_github_credentials.py`: resolver hit/miss, rewriter with Bearer+scheme, rewriter with empty scheme (x-api-key shape), rewriter on non-declared host (no rewrite).
- **Upstream CLI:** `provider create/list/get/delete/rotate` over the apikey type, including `--from-existing`.
- **Upstream E2E:** local echo server + proxy; curl `Authorization: Bearer clak_xxx` lands with real Bearer at the echo server for a declared host, and lands unchanged for an undeclared host.
- **Belayer unit:** `BuildEnv` with a provider configured for `OPENCODE_GO_API_KEY` produces an env-file that does not contain that key.
- **Belayer E2E:** clamshell session with the opencode apikey provider configured — `docker exec <container> env | grep OPENCODE_GO_API_KEY` returns a `clak_` handle, not the real key. Session reaches `bridge:idle` with `api_calls >= 2`. This is the same E2E rig used for the embed-hermes-bridge proof.

## Out of scope

- **OAuth flows.** Anthropic OAuth, Codex OAuth, and GitHub OAuth have token-refresh lifecycles incompatible with the static-secret assumption here. They get their own provider type when needed.
- **Credential rotation on the fly.** Upstream `provider rotate` may work with apikey, but v1 does not exercise it and the belayer daemon does not re-project on rotate.
- **Anthropic `x-api-key` E2E in v1.** The design supports it (empty `auth_scheme`), but the v1 proof only wires OPENCODE_GO_API_KEY. OPENAI_API_KEY works as a pure-config swap (same Bearer shape, different endpoint); Anthropic needs a config swap plus the empty-scheme path, left for a follow-up when we actually wire Anthropic.
- **Removing env-file entirely.** Other env (`BELAYER_SESSION_ID`, `BELAYER_SOCKET`, `BELAYER_TOOLS`, `PYTHONPATH`) still needs to reach the bridge. The env-file stays — it just no longer contains provider secrets.
- **Dedicated `clamshell exec` wrapper.** The rationale for §1 was the raw-cred leak. With secrets removed from the env-file, the exec path is no longer the exfiltration surface. If a future need emerges (policy gating, audit hooks), it gets its own design.
