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
