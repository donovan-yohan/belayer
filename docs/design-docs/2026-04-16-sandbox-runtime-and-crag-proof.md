---
status: current
created: 2026-04-16
branch: master
supersedes: docs/design-docs/2026-04-16-vm-sandbox-and-template-bootstrap.md
implemented-by:
consulted-learnings:
  - docs/design-docs/2026-04-15-nightshift-v1-deployment-topology.md
  - docs/design-docs/2026-04-15-git-backed-agent-identity.md
  - docs/design-docs/2026-04-15-hermes-bridge-sidecar.md
  - docs/design-docs/2026-04-16-vm-sandbox-and-template-bootstrap.md
---

# Sandbox, Runtime, and Crag Proof-of-Concept

End-to-end proof: lightweight crag on macOS dispatches a request to a Lima VM worker, which provisions a runtime, creates a sandboxed execution boundary, runs a belayer session against arielcharts, and completes.

## Philosophy: Runtime vs Sandbox

Belayer's original "sandbox" concept was overloaded — it meant both "where agents execute" and "what agents work against." These are two concerns with different lifecycles and different owners.

**Runtime** = the dev stack agents work against. Databases, app servers, mock services. Provisioned before agents spawn. Owned by the project (each repo defines its own `pnpm dev` or `xt up` or `docker compose up`). Lives for the session.

**Sandbox** = the execution boundary agents run inside. Network egress control, credential mediation, filesystem isolation. Owned by the operator (security policy is a deployment concern, not a project concern). Also lives for the session.

They compose: runtime services become allowed TCP endpoints in the sandbox policy. The runtime says "Postgres is on port 5432." The sandbox says "agents may reach port 5432 and nothing else."

```
┌─────────────────────── Worker ───────────────────────┐
│                                                       │
│  Runtime (project-owned)         Sandbox (operator-owned)
│  ┌─────────────────────┐        ┌────────────────────┐│
│  │ pnpm dev             │        │ clamshell sandbox  ││
│  │  → server :4000      │◄──────►│  → agent processes ││
│  │  → web    :3000      │ shared │  → deny-by-default ││
│  └─────────────────────┘  mount  │  → policy YAML     ││
│         host filesystem ─────────│──► /workspace       ││
│                                  └────────────────────┘│
└───────────────────────────────────────────────────────┘
```

The shared filesystem is the connection between runtime and sandbox. When an agent inside the sandbox writes to `/workspace/src/app.tsx`, that write lands on the host filesystem via Docker bind mount. The runtime's hot-reload picks it up. No sync protocol needed.

## Identity: `.belayer/` in the Repo

Agent identities live in `.belayer/agents/` in the repo itself, committed to git. The shipped defaults live alongside the binary at `<BelayerRoot>/agents/`; the loader reads project-local first, falls back to shipped (see `internal/daemon/agents.go`).

On first run (`belayer init` or auto-scaffolded by `belayer run`), belayer creates:

```
.belayer/
  config.yaml
  agents/
    supervisor/                # orchestrator; default Opus
      system-prompt.md
      agent.yaml               # belayer_tools: [spawn, request_completion]
    pm/                        # adversarial spec verifier; final completion gate
      system-prompt.md
      agent.yaml               # belayer_tools: [approve, reject]
    web-dev/                   # frontend/web app implementer; worktree-isolated
      system-prompt.md
      agent.yaml
    backend-dev/               # backend/API implementer; worktree-isolated
      system-prompt.md
      agent.yaml
    qa/                        # outside-in tester; drives the running app
      system-prompt.md
      agent.yaml
    reviewer/                  # adversarial code/plan reviewer
      system-prompt.md
      agent.yaml
  policies/
    standard.yaml              # placeholder clamshell policy; opt in via config.yaml
```

Six default agents — **supervisor, pm, web-dev, backend-dev, qa, reviewer**. Users customize from there: edit souls, add or remove agents, adjust policies. The defaults are embedded into the binary via `//go:embed` (see `embed.go` at the repo root); `belayer init` writes them to the user's `.belayer/agents/`. The on-disk copy is the source of truth at runtime.

~~**chalk-bag's role**: canonical source for developing and versioning shared agent archetypes. Belayer's compiled-in defaults are built from chalk-bag. Users can also copy templates from chalk-bag manually when bootstrapping a new project. But chalk-bag is not a runtime dependency.~~ *(superseded by Phase 2.5: defaults now ship embedded in the belayer binary; chalk-bag is no longer the bootstrap source.)*

~~The defaults included only `supervisor` and `pm`; users were expected to add `backend`, `frontend`, `qa`, `reviewer` themselves.~~ *(superseded by Phase 2.5: the six-agent default team now scaffolds on init. `frontend` → `web-dev`, `backend` → `backend-dev`, and the redundant `sprite` identity was removed in favor of hermes's built-in `delegate_task`.)*

### Multi-repo workspaces

For multi-repo setups, `.belayer/config.yaml` can't live in any single repo because the workspace is assembled at session start. Instead, the workspace definition lives in **crag** — the outer daemon that submits requests to workers. Crag is infrastructure (a server with a database), not source code. It knows "extend-fullstack means these repos with this config."

For local dev without crag, pass a workspace config explicitly:

```bash
belayer run start --workspace ~/.belayer/workspaces/extend-fullstack.yaml --task "..."
```

Resolution:
- **Single-repo**: `.belayer/config.yaml` in the repo, auto-discovered
- **Multi-repo**: workspace definition from crag or `--workspace` flag
- **No config**: `belayer run` auto-runs `belayer init` and scaffolds the six-agent default team

## Phase 2.5: `belayer init` workflow

Added on top of Phase 2's sandbox driver registry. The init command and the six-agent default team turn "what does a fresh repo need" from a copy-paste exercise into a single command.

### `belayer init`

```
belayer init                  # scaffold .belayer/ in cwd, idempotent
belayer init --target <dir>   # scaffold elsewhere
belayer init --force          # refresh shipped agents from defaults; never touches config.yaml
```

Behavior:
1. If `.belayer/` exists and `--force` is not set: print "belayer already initialized at <path>" and exit 0.
2. Otherwise: create `.belayer/{config.yaml,policies/standard.yaml,agents/<six defaults>}`.
3. Append a managed block to `<project>/.gitignore` (creating it if absent) so per-climb scratch directories don't get committed: `/.belayer/climbs/` and `/.belayer/worktrees/`. The committed surface — `agents/`, `config.yaml`, `policies/` — stays tracked. The block is fenced by a `# belayer — per-climb state` marker so re-runs are a no-op and user-added rules above it are preserved verbatim.
4. Print one line per file created so the user knows what landed in their repo.
5. `--force` overwrites only `.belayer/agents/`. The user's `config.yaml`, `policies/standard.yaml`, and `.gitignore` are never overwritten — re-running with `--force` is the supported path for upgrading shipped agents after a `belayer` upgrade.

### Auto-init from `belayer run`

`belayer run start` calls `autoInitIfMissing(workdir)` before creating the session. When `.belayer/` does not exist, it runs init and prints:

```
Auto-initialized <workdir>/.belayer with default agents — review and tweak as needed before next run.
```

The notice is one line. Existing `.belayer/` directories are silently skipped — no warning, no log entry. Rationale: the supervisor's first identity lookup needs `.belayer/agents/supervisor/system-prompt.md` to exist, and forcing a separate command before every fresh-repo run is gratuitous friction.

### Decision rule: `belayer_spawn_agent` vs hermes `delegate_task`

The supervisor has two ways to push work onto another agent. The system prompt at `agents/supervisor/system-prompt.md` is the source of truth; the rule in short:

- **Spawn a belayer peer** (`belayer_spawn_agent`) for ongoing work that needs its own workspace, bidirectional dialogue, or a presence in the session log. Implementers on a feature branch, reviewers iterating with the supervisor on findings, QA reporting back over multiple rounds.
- **Delegate a focused subtask** (hermes's built-in `delegate_task`) for a one-shot result with no follow-up. Research, summaries, isolated lint fixes. Cheaper than spawning a peer; runs in an isolated context; returns only the summary.

If you want a back-and-forth with a delegated task, you should have spawned a peer. If you spawn a peer just to ask one question, delegate next time. Schemas in `hermes_bridge/tools.py` carry the same WHEN / WHEN NOT framing so the model picks consistently.

## Worktrees: Coding Tool, Not Integration Surface

Git worktrees are a tool for parallel coding — multiple agents editing different parts of the codebase without file conflicts. They are **not** the integration surface.

One session builds toward one goal. Agents may use worktrees to parallelize work, but real integration, testing, and QA happen against the shared workspace (the main feature branch) after worktree work is merged back. The runtime runs against the shared workspace, not against individual worktrees.

Concretely:
- Runtime starts from the repo root (the shared workspace)
- Agents may create worktrees for isolated branch work
- When an agent's work is ready, it merges to the shared branch
- Integration tests and QA run against the shared workspace where the runtime is hot-reloading
- The PM gate verifies the integrated result, not individual worktrees

This means worktree-based agents cannot live-test against the runtime mid-work. They code, merge, then the supervisor or QA agent validates the integrated result. This is intentional — it matches how human teams work with feature branches.

## SandboxDriver Interface

The sandbox driver manages the agent execution boundary. Belayer holds one driver per session.

```go
// internal/sandbox/driver.go
package sandbox

type Driver interface {
    // Create prepares an execution environment for the session.
    // Called once per session, before any agents spawn.
    Create(ctx context.Context, cfg Config) (Handle, error)

    // Exec runs a command inside the sandbox. Used for each agent spawn.
    // The caller manages stdin/stdout/stderr wiring.
    Exec(ctx context.Context, h Handle, cmd []string, opts ExecOpts) (*os.Process, error)

    // Stop tears down the sandbox. Called when the session ends.
    Stop(ctx context.Context, h Handle) error
}

type Config struct {
    Name       string        // sandbox identifier (typically session ID)
    Workspace  string        // host path to mount at /workspace
    Policy     string        // path to policy YAML (driver-specific)
    Mounts     []Mount       // additional mounts
    Endpoints  []TCPEndpoint // runtime services to allow through the sandbox
}

type ExecOpts struct {
    Env    []string        // environment variables
    Dir    string          // working directory inside sandbox
    Stdin  io.Reader
    Stdout io.Writer
    Stderr io.Writer
}

type Mount struct {
    HostPath    string
    SandboxPath string
    ReadOnly    bool
}

type TCPEndpoint struct {
    Name string
    Host string
    Port int
}

type Handle struct {
    ID   string // opaque identifier
    Meta map[string]string // driver-specific metadata
}
```

### What ships in belayer

- `internal/sandbox/driver.go` — the `Driver` interface
- `internal/sandbox/registry.go` — the driver registry (name → `Driver`)
- `internal/sandbox/noop.go` — the noop driver (direct exec, zero overhead, always built)
- `internal/sandbox/clamshell.go` — the clamshell driver, behind `//go:build clamshell`
- `internal/sandbox/clamshell_stub.go` — the "not in this build" stub, behind `//go:build !clamshell`
- `internal/runtime/provider.go` — the `Provider` interface
- `internal/runtime/noop.go` — the noop provider
- `internal/runtime/command.go` — the command provider (shells out to up/health/down)
- Default policy YAMLs scaffolded on `belayer init`
- `daemon.Config.SandboxDrivers *sandbox.Registry` — the daemon resolves drivers by name from `.belayer/config.yaml`'s `sandbox.mode`

### What is NOT shipped by default

- The `clamshell` CLI itself — runtime dependency, not a code dependency. The driver shells out to it; if the CLI isn't installed on the worker, the driver returns a runtime error.
- The crag binary — separate project
- Specific agent identities — per-repo `.belayer/` or chalk-bag
- Workspace definitions — crag config

### Driver selection: build tag, not separate repo

The default `go build ./cmd/belayer` produces a binary with only the noop driver wired into the registry. A `clamshell` entry exists in the registry but resolves to a stub that fails session start with a clear "not built with clamshell support" error.

To compile the real clamshell driver in, build with `-tags clamshell`:

```bash
go build ./cmd/belayer                                # noop only (default)
go build -tags clamshell ./cmd/belayer                # noop + clamshell
go install -tags clamshell github.com/.../belayer/cmd/belayer@latest
```

The mechanism is the standard Go build constraint, not a registry override. Two files share the same `clamshell` registration — only one compiles per build:

```go
// internal/sandbox/clamshell.go
//go:build clamshell

package sandbox

func init() {
    Register("clamshell", &Clamshell{})  // real driver
}

type Clamshell struct{ /* ... */ }
// Create/Exec/Stop call into the clamshell CLI
```

```go
// internal/sandbox/clamshell_stub.go
//go:build !clamshell

package sandbox

func init() {
    Register("clamshell", newUnavailable("clamshell",
        "this binary was built without -tags clamshell"))
}
```

The registry itself is trivial — a `map[string]Driver` populated by `init()` functions, with a `Get(name) (Driver, error)`. No `Override` parameter, no separate `cmd/` main per build.

When an OSS-belayer session arrives with `sandbox.mode: clamshell`, `Get("clamshell")` succeeds (the name is registered) but the stub's `Create` returns an error. The daemon keeps serving other sessions; only the one requesting clamshell fails, with a useful message instead of `driver not registered` or a nil-pointer panic. A `-tags clamshell` build replaces the stub with the real driver; the same session config runs.

The daemon cannot pre-validate at startup — `.belayer/config.yaml` is per-project, so the daemon doesn't know which mode any future session will request. Per-session resolution is the right hook.

Why build tags over alternatives:

- vs. **separate `belayer-extend` binary that imports a private clamshell driver package**: rejected. Adds a second repo, version skew between belayer and the extension binary, and registry/override plumbing — all to avoid one file's worth of `os/exec` calls in the public repo. The constraint is "default `go install` produces an unsandboxed binary," not "no clamshell-related code anywhere in the public repo." A build tag satisfies the actual constraint.
- vs. **subprocess driver protocol** (driver = separate binary, JSON-RPC over stdio): rejected as overkill. Adds a versioned wire-protocol surface and stdio plumbing for agent processes, in exchange for crash isolation and hot-swap — neither of which we need at PoC.
- vs. **Go plugins** (`plugin.Open`): rejected. GOOS/GOARCH lockstep, fragile in practice.
- vs. **shipping clamshell in-tree without a build tag**: rejected. The default `go install` would compile clamshell-CLI references into every binary, including ones run by people who never installed clamshell.

The cost we accept: anyone can build the clamshell-enabled binary by passing `-tags clamshell` themselves. That's fine — the closed thing is the `clamshell` CLI on the worker, not the wrapper code that calls it. Anyone who has the CLI can use the wrapper; anyone who doesn't gets a runtime error from the CLI itself.

### noop driver (ships with belayer)

Zero overhead, current behavior. No isolation.

```go
func (n *Noop) Create(ctx context.Context, cfg Config) (Handle, error) {
    return Handle{ID: cfg.Name}, nil
}

func (n *Noop) Exec(ctx context.Context, h Handle, cmd []string, opts ExecOpts) (*os.Process, error) {
    c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
    c.Env = opts.Env
    c.Dir = opts.Dir
    c.Stdin = opts.Stdin
    c.Stdout = opts.Stdout
    c.Stderr = opts.Stderr
    if err := c.Start(); err != nil {
        return nil, err
    }
    return c.Process, nil
}

func (n *Noop) Stop(ctx context.Context, h Handle) error {
    return nil
}
```

### clamshell driver (built with `-tags clamshell`)

Wraps the clamshell CLI. Creates a sandbox at session start, execs agents via `docker exec`. Lives in `internal/sandbox/clamshell.go` behind `//go:build clamshell`. Code shape:

```go
func (c *Clamshell) Create(ctx context.Context, cfg Config) (Handle, error) {
    // 1. Ensure gateway is running
    //    clamshell gateway start
    //
    // 2. Inject runtime endpoints into policy tcp_endpoints
    //    (read policy YAML, append endpoints, write to temp file)
    //
    // 3. Create sandbox
    //    clamshell sandbox create --name <cfg.Name> --policy <policy> --workspace <cfg.Workspace>
    //
    // 4. Discover container name
    //    clamshell --json sandbox connect <cfg.Name> → extract container ID
    //
    // 5. If endpoints use deferred materialization, refresh
    //    clamshell sandbox refresh <cfg.Name>
}

func (c *Clamshell) Exec(ctx context.Context, h Handle, cmd []string, opts ExecOpts) (*os.Process, error) {
    // docker exec -u sandbox -i <container> env KEY=VAL ... sh -lc "<cmd>"
    // Wire stdin/stdout/stderr through
}

func (c *Clamshell) Stop(ctx context.Context, h Handle) error {
    // clamshell sandbox stop <name>
}
```

## RuntimeProvider Interface

The runtime provider provisions the dev stack. Belayer calls it before creating the sandbox.

```go
// internal/runtime/provider.go
package runtime

type Provider interface {
    // Up starts the dev stack and returns discovered endpoints.
    Up(ctx context.Context) ([]Endpoint, error)

    // Health checks whether the dev stack is ready.
    Health(ctx context.Context) error

    // Down stops the dev stack.
    Down(ctx context.Context) error
}

type Endpoint struct {
    Name string
    Host string
    Port int
}
```

### noop provider

For projects with no dev stack (pure library, CLI tool, etc.).

### command provider

Reads from `.belayer/config.yaml`:

```yaml
runtime:
  up: "pnpm dev &"
  health: "curl -sf http://localhost:4000/health"
  down: "pkill -f 'pnpm dev'"
  endpoints:
    - {name: server, host: localhost, port: 4000}
    - {name: web, host: localhost, port: 3000}
```

`Up` runs the up command, polls health until ready (with timeout), returns the declared endpoints. `Down` runs the down command.

## Integration: Session Start Flow

```go
func (d *Daemon) startSession(req SessionRequest) error {
    // 1. Provision runtime
    endpoints, err := d.runtime.Up(ctx)
    if err != nil { return err }
    defer func() {
        if err != nil { d.runtime.Down(ctx) }
    }()

    // 2. Wait for runtime health
    if err := d.runtime.Health(ctx); err != nil { return err }

    // 3. Create sandbox (inject runtime endpoints)
    handle, err := d.sandbox.Create(ctx, sandbox.Config{
        Name:      sessionID,
        Workspace: workspaceDir,
        Policy:    policyPath,
        Endpoints: endpoints,
    })
    if err != nil { return err }

    // 4. Store handle — all subsequent agent spawns use it
    d.session.sandboxHandle = handle

    // 5. Spawn supervisor (existing flow, but through sandbox.Exec)
    return d.spawnBridgeAgent(supervisorReq)
}
```

The change to `bridgeLaunchAgent` is minimal — instead of calling `bridge.Spawn(cfg)` which runs `exec.Command` directly, it calls `d.sandbox.Exec(handle, cmd, opts)`. The bridge `Config` assembly (env vars, PYTHONPATH, system prompt loading) is unchanged.

## Bridge Refactor

The current `bridge.Spawn()` both assembles the environment AND runs the process. We split these:

```go
// bridge.BuildEnv(cfg) → []string        (pure, no side effects)
// bridge.BuildCmd(cfg) → []string         (pure, no side effects)
// sandbox.Exec(handle, cmd, env, ...)     (runs the process)
```

The bridge package becomes a pure environment/command builder. Process execution moves to the sandbox driver.

The existing `bridge.Process` type (stdin pipe, wait, interrupt) still wraps the underlying process. The sandbox driver's `Exec` returns an `*os.Process`, and the bridge `Process` wrapper is constructed from it. This preserves the interrupt/wait/done interface that the daemon depends on — the only change is that the underlying `os.Process` may be a `docker exec` process rather than a direct Python process.

## Hermes Network Policy

Hermes agents make HTTP requests to LLM providers, auth servers, search APIs, and skills hubs. The sandbox policy must allow these.

### Endpoint inventory (from hermes-agent source)

**Always needed:**

| Domain | Purpose |
|--------|---------|
| `api.anthropic.com` | Claude API |
| `api.openai.com` | OpenAI API |
| `auth.openai.com` | Codex OAuth |
| `chatgpt.com` | Codex inference |
| `portal.nousresearch.com` | Nous OAuth + agent keys |
| `inference-api.nousresearch.com` | Nous inference |

**Common providers:**

| Domain | Purpose |
|--------|---------|
| `openrouter.ai` | Multi-model gateway |
| `api.deepseek.com` | DeepSeek |
| `api.kimi.com` | Kimi coding |
| `api.moonshot.cn` | Moonshot |

**Agent tools:**

| Domain | Purpose |
|--------|---------|
| `api.github.com`, `github.com` | Git operations, skill fetching |
| `api.tavily.com` | Web search |
| `registry.npmjs.org` | npm packages |
| `pypi.org`, `files.pythonhosted.org` | Python packages |

### Default policies shipped with belayer

**`belayer-minimal.yaml`** — Anthropic + GitHub only. For testing.

**`belayer-standard.yaml`** — Multi-provider + packages + search + skills hub. For typical runs.

**`belayer-workbench.yaml`** — Standard + `tcp_endpoints` section for sidecar services (Postgres, Redis, LocalStack). For runs with a dev stack.

These are scaffolded into `.belayer/policies/` on init. Users can edit them.

## Lightweight Crag

Minimal Go binary that runs on macOS and dispatches to the Lima VM worker.

### What crag does

- Holds named workspace definitions (arielcharts, extend-fullstack)
- Maintains a request queue (SQLite)
- Manages one worker: the `devbox` Lima VM
- Dispatches requests: SSHs into the VM, runs `belayer run start`
- Polls belayer status until complete
- Collects results

### CLI surface

```
crag start                    # start the crag daemon
crag submit --task "..."      # submit a request (auto-detects workspace from cwd)
crag submit --workspace extend-fullstack --task "..."
crag status                   # show active/queued requests
crag list                     # show request history
crag workspace add arielcharts ~/arielcharts
crag workspace list
```

### Workspace definition

```yaml
# ~/.crag/workspaces/arielcharts.yaml
name: arielcharts
repos:
  - path: ~/Documents/Programs/personal/arielcharts
worker:
  type: lima
  vm: devbox
```

```yaml
# ~/.crag/workspaces/extend-fullstack.yaml
name: extend-fullstack
repos:
  - name: api
    path: ~/Documents/Programs/work/extend-api
  - name: app
    path: ~/Documents/Programs/work/extend-app
worker:
  type: lima
  vm: devbox
```

### Dispatch flow

```
1. crag submit --workspace arielcharts --task "Add dark mode"
2. crag checks worker status: limactl list → devbox Running
3. crag SSHs into VM:
   limactl shell devbox -- bash -c '
     cd ~/Documents/Programs/personal/arielcharts
     belayer daemon &
     belayer run start --task "Add dark mode"
   '
4. crag polls: limactl shell devbox -- belayer status
5. On completion, crag records result
```

Crag doesn't know about sandbox or runtime — those are belayer's concern, configured in `.belayer/config.yaml` in the repo.

## Proof-of-Concept: arielcharts

### Target

arielcharts: real-time collaborative Mermaid diagram editor. pnpm monorepo, Next.js + Node.js server, LevelDB (embedded). No external services needed.

### What we prove

1. **Crag dispatch**: macOS → Lima VM via `limactl shell`
2. **Runtime provision**: `pnpm dev` starts server + web, health check passes
3. **Sandbox creation**: clamshell sandbox with standard policy, runtime endpoints allowed
4. **Agent execution**: supervisor spawns inside sandbox, hermes agent runs, reaches Anthropic API
5. **Network egress**: deny events are empty (all legitimate traffic allowed, nothing else leaks)
6. **Code changes**: agent edits files inside sandbox, runtime hot-reloads, changes visible
7. **Session completion**: PM gate fires, session completes, crag records result

### arielcharts `.belayer/config.yaml`

```yaml
runtime:
  up: "pnpm install --frozen-lockfile && pnpm dev &"
  health: "curl -sf http://localhost:4000/health"
  down: "pkill -f 'next dev'; pkill -f 'tsx watch'"
  endpoints:
    - {name: server, host: localhost, port: 4000}
    - {name: web, host: localhost, port: 3000}

sandbox:
  mode: clamshell
  policy: .belayer/policies/belayer-standard.yaml
```

### Phases

**Phase 1: SandboxDriver + RuntimeProvider interfaces**
- Create `internal/sandbox/` and `internal/runtime/` packages
- Implement noop drivers
- Refactor bridge.Spawn to separate env building from process execution
- Wire into daemon session start
- Test: existing behavior unchanged with noop drivers

**Phase 2: Sandbox registry + clamshell driver (build-tagged)**

Default-build work (no build tag, this is what `go install` produces):
- Add `internal/sandbox/registry.go` with a package-level `map[string]Driver`, `Register(name, driver)`, and `Get(name) (Driver, error)`
- Add `internal/sandbox/clamshell_stub.go` (`//go:build !clamshell`) that registers `clamshell` to a stub returning "this binary was built without -tags clamshell" from every method
- Replace `daemon.Config.Sandbox sandbox.Driver` with `daemon.Config.SandboxDrivers *sandbox.Registry`; resolve the per-session driver by name from `.belayer/config.yaml`'s `sandbox.mode` (default `noop`)
- `cmd/belayer/main.go` constructs the registry once at startup; `init()` functions in each driver file populate it
- Ship default policies in `.belayer/policies/`
- Tests: registry returns noop by default; default build returns the stub for clamshell with the documented error; daemon resolves `sandbox.mode` correctly and surfaces the unavailable error at session start

Tagged-build work (`-tags clamshell`):
- Add `internal/sandbox/clamshell.go` (`//go:build clamshell`) implementing `Driver` against the `clamshell` CLI (Create/Exec/Stop per the code sketch above)
- The same `init()`-based registration replaces the stub at compile time when the tag is set
- Tests guarded by the same tag: build with `-tags clamshell` in CI and exercise the driver against a Lima VM with the CLI installed
- Manual verification in Lima VM: create sandbox, exec agent, verify egress policy; iterate on policy by watching deny events

**Phase 3: Command runtime provider**
- Implement `runtime.Config` loader that reads the `runtime:` section of `.belayer/config.yaml`
- Implement the command runtime provider (`internal/runtime/command.go`) satisfying `runtime.Provider`
- Unit tests covering Up/Health/Down with shell-command fixtures (no real dev stack required)
- **Out of scope for this phase** (deferred to Phase 5):
  - Wiring `runtime.Up`/`Down` into the daemon session lifecycle
  - Selecting noop vs command provider based on config at daemon startup
  - Live testing against arielcharts (`pnpm dev`, health check, endpoint discovery)
  - Verifying shared filesystem round-trip from agent edits to runtime hot-reload

**Phase 4: Lightweight crag**
- Minimal Go binary with workspace definitions and request queue
- Lima VM as single worker
- Dispatch via `limactl shell`
- Poll belayer status

**Phase 5: End-to-end**
- Wire `runtime.Up`/`Down` into daemon `startSession` (per "Integration: Session Start Flow" above)
- Select the configured runtime provider (noop vs command) at daemon startup based on `.belayer/config.yaml`
- `crag submit --workspace arielcharts --task "Add dark mode toggle"`
- Full flow: dispatch → runtime → sandbox → agents → PM gate → complete
- Verify: network locked down, code changes hot-reload, session completes
- Live test with arielcharts: confirm endpoint discovery and filesystem round-trip

## Open Questions

### 1. Sandbox stdin/stdout wiring

The bridge uses stdin for daemon→agent interrupts and stdout for logging. When exec'ing via `docker exec`, stdin/stdout piping works but needs care — `docker exec -i` keeps stdin open. Need to verify interrupt delivery works through the Docker exec layer.

### 2. Daemon socket access from sandbox

The bridge communicates with the daemon via Unix socket HTTP. Inside a clamshell sandbox, the socket must be reachable. Options:
- Mount the socket into the sandbox as an additional mount
- Use a TCP localhost forward instead of Unix socket
- The daemon listens on both Unix socket and TCP when sandbox mode is clamshell

### 3. Hermes auth inside sandbox

Hermes needs API keys or OAuth tokens. With clamshell, credentials should flow through the proxy's `inference.local` routing (opaque handles). For the PoC, we can inject `ANTHROPIC_API_KEY` as an env var at exec time and migrate to opaque handles later.

### 4. Crag persistence

SQLite for the PoC. Workspace definitions as YAML files in `~/.crag/workspaces/`. No web UI yet — CLI only.

### 5. `runtime.Endpoint` shape (deferred)

`Endpoint` currently carries only `Name`/`Host`/`Port`. A protocol/scheme field plus a `URL()` helper was raised in code review (2026-04-16, CodeRabbit on PR #71) — it would help once endpoints drive sandbox egress policy or get injected as env vars for agents. Deferred until a real provider lands so we design the shape around concrete needs (e.g., does the clamshell policy need a protocol discriminator, or is host/port enough?). Revisit when implementing the command provider's endpoint wiring in Phase 5.

### 6. Multi-repo: revisit `--repos` and per-agent starting cwd

Today `belayer climb start --repos name=path,...` synthesizes a workspace at `.belayer/climbs/<sessionID>/workspace/` by symlinking each repo into it. The agent's `workdir` is that synthetic dir, and individual implementers see all repos as siblings. The cleaner pattern in practice is a meta-directory with sibling repos (`~/projects/extend-fullstack/{extend-app,extend-api,.belayer}`) — no `--repos` flag needed, the meta-dir IS the workspace.

Two concrete things to revisit before multi-repo gets serious use:

- **Should `--repos` stay?** It exists for the messy disk-layout case (repos in unrelated parent dirs). If teams converge on the meta-dir pattern, `--repos` is dead weight. If they don't, the symlink synthesis needs more care (worktrees, gitignore handling inside the synthetic dir, etc.).
- **Per-agent starting cwd in `agent.yaml`.** The `workspace:` field today takes `inherit` (run inside the supervisor's workdir) or `none` (no workspace). For multi-repo, web-dev probably wants to start in `extend-app/` and backend-dev in `extend-api/`. A `workspace: ./extend-app` (relative to the run workdir) would let the agent identity declare its starting repo without the supervisor having to pass it in every spawn message. Open question: do we put repo selection in the identity (web-dev always starts in the web app) or in the spawn call (supervisor decides per-task)? Identity feels right for the default, spawn-override for special cases.

Both belong in a follow-up once we have a real multi-repo run to design against. Don't over-spec ahead of usage.
