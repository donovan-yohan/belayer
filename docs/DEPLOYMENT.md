# Deployment

Belayer is the control plane for ONE run. It does not impose an isolation boundary. Isolation is the responsibility of the outer deployment topology.

---

## Topologies

| Topology | Sandbox provider | Trust model | Network exposure |
|----------|-----------------|-------------|-----------------|
| **Host-native** (dev laptop) | None — agents run as the current OS user | Working directory is the only boundary; full filesystem access | Unix socket only (`~/.belayer/daemon.sock`); no network by default |
| **Nightshift worker** (production) | Outer container (provided by Nightshift); one container per run | Container boundary enforced by Nightshift; Belayer trusts everything inside | Unix socket inside container + optional TCP (`--bind`) for external observability |
| **Clamshell** (legacy) | Docker per-session sandbox with MITM egress proxy | Agents in Docker; daemon on host VM; proxy enforces egress allowlist | Daemon on host VM reachable via bind-mounted workspace socket or Docker host gateway |

The outer topology — not Belayer — is responsible for isolating agent processes from each other and from the host. Belayer reads and writes the working directory freely; it has no concept of "this directory is off-limits."

---

## Trust Model

- **Working directory is trusted.** Belayer reads project files, writes artifacts, and records events without access controls. The agent can read and write the working directory through its tools.
- **Trace tier captures file content.** At `log_level: "trace"`, `trace:fs_snapshot` events record file digests and `trace:subprocess_exec` events capture environment variables (redacted). Treat trace archives as potentially containing secrets. Store them accordingly.
- **Outer sandbox enforces isolation.** If you need agents to be unable to read `~/.ssh` or write to paths outside the project, configure the outer sandbox (container bind-mounts, namespace restrictions). Belayer will not enforce this.

---

## Credentials

Provider credentials (e.g. `OPENCODE_GO_API_KEY`) are loaded from two files, in this order:

1. `~/.belayer.env` — user-level credential store (global default).
2. `.belayer/.belayer.env` — workspace-level credential store (project-local override; workspace wins).

Both files are loaded via `godotenv.Load()` at daemon startup into the daemon's environment. Values already set in the process environment are not overwritten. When the daemon spawns a bridge subprocess, it serialises the relevant environment variables into a temporary file passed to the bridge process.

**Redaction is best-effort.** The daemon applies a redaction pass to trace events and bridge environment dumps, stripping known secret patterns (bearer tokens, API keys, GitHub PATs, PEM blocks). This redaction is a convenience, not a compliance boundary. Do not rely on it to satisfy regulatory requirements. Secrets present in the working directory, tool outputs, or injected via environment variables may appear in trace fragments.

---

## Ports and Sockets

| Surface | Default | Purpose |
|---------|---------|---------|
| `~/.belayer/daemon.sock` | Always present | Local Unix socket; no authentication required; trusted local access only |
| `.belayer/runs/<session>/daemon.sock` | When `WorkspaceSockPath` is set | Workspace-relative socket for in-container bridge access via bind-mount |
| TCP `--bind <addr>` | Opt-in | Remote observability and dashboard access; requires `--auth-token` |

**TCP opt-in:** pass `--bind 127.0.0.1:<port>` (or `--tcp-addr`) to enable a TCP listener. When `--bind` is set without `--auth-token`, the daemon auto-generates a 32-byte random token and logs it to stderr. Pass `--cors-origin <url>` to allow browser-based dashboards from a specific origin.

**Bridge stdout logs:** raw bridge subprocess stdout/stderr is captured per-agent at `.belayer/runs/<session>/bridge.<agent>.log`. These files rotate (3 generations) and are not automatically deleted. At verbose+ tier, they are included in the session archive under `bridges/<agent>.stdout.log`.

---

## Landlock Write Confinement

Belayer optionally enforces **kernel-level write confinement** for agent bridge
subprocesses using Linux Landlock v2. When enabled, agents can only write to an
explicitly enumerated set of paths; any write outside that set fails with
`EACCES` at the kernel, regardless of prompt discipline.

### Enabling

Set in `.belayer/config.yaml`:

```yaml
confine_agent_writes: true
```

Or pass `--confine-agent-writes` to `belayer daemon`.

### Kernel requirement

**Linux 5.19+** is required for Landlock v2 (which adds file-truncation
control). To check your kernel's Landlock ABI level:

```sh
cat /sys/kernel/security/landlock/abi
```

A value of `2` or higher confirms Landlock v2 support. If the file is absent,
Landlock is not available on this kernel. Belayer uses `BestEffort()` — on
kernels without Landlock the restriction degrades to passthrough; there is no
hard failure.

### `belayer-landlock-exec` helper binary

Write confinement is implemented by `belayer-landlock-exec`, a small Go binary
that applies the Landlock ruleset and then `exec`-replaces itself with the
actual bridge command. It must be on `PATH` when `confine_agent_writes: true` is
active.

**For deployment images (Nightshift worker, clamshell):** bake
`belayer-landlock-exec` into the image alongside `belayer`. The binary is built
from `cmd/belayer-landlock-exec/` and cross-compiles cleanly with:

```sh
GOOS=linux GOARCH=arm64 go build -o belayer-landlock-exec ./cmd/belayer-landlock-exec
```

**TODO (clamshell image):** The clamshell Dockerfile (`docker/` or the
external clamshell image build) needs `belayer-landlock-exec` added to `/usr/local/bin`.
This is tracked as a follow-up; Landlock confinement is ineffective in the
clamshell topology until the binary is present in the container image.

### What is and is not protected

| Threat | Protected? | Notes |
|--------|------------|-------|
| Specialist deletes `.belayer/` runtime | Yes — EACCES | Core gap 10 enforcement |
| Specialist writes outside its worktree | Yes — EACCES | Gap 15 enforcement |
| Agent reads secrets outside workspace | No | Landlock only limits writes here |
| Agent exfiltrates data via `/tmp` | No | `/tmp` is always writable (needed for compilers, pip, npm) |
| fd-inherited file writes | No | Descriptors opened before ruleset are not restricted |
