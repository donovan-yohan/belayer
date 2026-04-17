# Clamshell Sandboxing

Clamshell is Belayer's Docker-based sandbox mode. Each session runs its bridge subprocess inside an isolated container with a MITM CONNECT proxy enforcing an egress allowlist and process attestation.

## Architecture

```mermaid
graph TD
    subgraph host["macOS Host"]
        CLI["belayer CLI\n(run start / logs / roster)"]
        ENV["~/.belayer.env\n(OPENCODE_GO_API_KEY, GITHUB_TOKEN, …)"]
    end

    subgraph vm["Colima VM"]
        subgraph daemon["belayer daemon (Go)"]
            API["HTTP API\n(daemon.sock +\n/workspace/.belayer/daemon.sock)"]
            DB[("SQLite\nsessions · agents · events")]
            ENVLOAD["godotenv.Load()\n~/.belayer.env → os.Setenv()"]
        end

        subgraph sandbox["Docker: clamshell-{session}"]
            subgraph bridge["bridge subprocess\npython3 -m hermes_bridge\n(env-file injected at docker exec)"]
                MAIN["__main__.py\nresolve_runtime_provider()\n→ provider=opencode-go"]
                PATCH["monkey-patch\n_create_openai_client()\n→ always fresh httpx.Client\n   proxy=proxy.internal:3128\n   + TCP keepalive"]
                AGENT["Hermes AIAgent\nOpenAI SDK\nhttp_client=↑"]
                TOOLS["belayer tools\nterminal · spawn_agent\nrequest_completion …"]
            end
            WS["/workspace bind-mount\n(project dir)\nincludes hermes_bridge/\n+ PYTHONPATH entry"]
        end

        subgraph proxy["Docker: clamshell-proxy-{session}"]
            EGRESS["egress broker\nCONNECT proxy\nproxy.internal:3128"]
            ATTEST["process attestation\nbinary=python3 ✓"]
            ALLOW["allowlist\nopencode.ai:443 ✓\n192.168.5.2:7523 ✓\n192.168.5.2:3000-4000 ✓"]
        end
    end

    subgraph ext["External"]
        LLM["opencode.ai\n/zen/go/v1\nkimi-k2.5"]
    end

    ENV -->|"loaded at daemon startup"| ENVLOAD
    ENVLOAD --> API
    CLI -->|"daemon.sock"| API
    API -->|"docker exec --env-file\nOPENCODE_GO_API_KEY\nHTTPS_PROXY\nBELAYER_SESSION_ID\nBELAYER_SOCKET\nBELAYER_TOOLS\nPYTHONPATH"| MAIN
    MAIN --> PATCH
    PATCH --> AGENT
    AGENT -->|"CONNECT tunnel\nvia proxy.internal:3128"| EGRESS
    EGRESS --> ATTEST
    ATTEST --> ALLOW
    ALLOW -->|"TLS to opencode.ai:443"| LLM
    LLM -->|streaming response| ALLOW
    AGENT <-->|"/workspace/.belayer/daemon.sock\n(Unix socket in bind-mount)"| API
    API <--> DB
    TOOLS -.->|"execute inside container"| AGENT
    WS -.->|"imports hermes_bridge"| MAIN
```

## Credential chain

Provider credentials (e.g. `OPENCODE_GO_API_KEY`) live in `~/.belayer.env` on the host. At daemon startup, `godotenv.Load()` reads the workspace `.belayer/.belayer.env` first (workspace wins), then `~/.belayer.env`. Both are loaded into the daemon's `os.Environ()` without overwriting already-set variables.

When the daemon spawns a bridge subprocess via `docker exec`, `bridge.BuildEnv()` serialises the daemon's env into a temp env-file. The container process inherits `OPENCODE_GO_API_KEY` (and any other provider-specific vars) from there. `resolve_runtime_provider()` in `__main__.py` detects the key and selects the `opencode-go` Hermes provider.

`BELAYER_PROVIDER` / `BELAYER_BASE_URL` act as fallbacks only — they are ignored when the Hermes config already resolves a provider.

## Proxy client lifecycle fix

Hermes rebuilds its OpenAI client on every tool-call cycle (`_close_openai_client` → `_create_openai_client`). The close tears down the underlying `httpx.Client`, breaking the proxy connection for subsequent LLM calls.

The fix monkey-patches `agent._create_openai_client` at spawn time so every invocation creates a **fresh** `httpx.Client` with:
- `proxy=httpx.Proxy("http://proxy.internal:3128")`
- TCP keepalive socket options (SO_KEEPALIVE, TCP_KEEPIDLE=30, TCP_KEEPINTVL=10, TCP_KEEPCNT=3)

The patched method strips any `http_client` key from the kwargs snapshot before forwarding to the original method, so Hermes's internal recovery path (`_try_recover_primary_transport`) also gets a fresh client automatically.

## hermes_bridge distribution gap

`hermes_bridge/` is plain Python source — it is **not** embedded in the Go binary. The container imports it from `/workspace/hermes_bridge/` (the project's bind-mounted directory). This means:

1. Every project that uses Clamshell must have a copy of `hermes_bridge/` checked in or synced.
2. Changes to `hermes_bridge/` in the belayer repo must be manually propagated to each project.

Long-term options: embed a wheel in the binary and extract via `belayer init`, or publish `hermes_bridge` as a PyPI package.
