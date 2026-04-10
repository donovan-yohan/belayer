# Belayer v6 Architecture

Status: `implemented` — v6 session runtime (2026-04-09)

> Many robots, bring your own pilots.

Belayer v6 is a daemon-based session runtime for orchestrating multiple AI coding agents through structured session templates. Agent sandboxes are powered by clamshell for deny-by-default isolation. This document provides both high-level diagrams and implementation details for technical audiences.

---

## System Architecture Diagram

```mermaid
flowchart TB
    subgraph Client["CLI Client"]
        CLI["belayer CLI"]
    end

    subgraph Daemon["Belayer Daemon"]
        HTTP["HTTP Router"]
        Store[("SQLite Store")]
        Broker["Message Broker"]
        Memory[("Three-Tier Memory")]
    end

    subgraph Templates["Session Templates"]
        Climb["Climb"]
        ClimbFS["Climb Fullstack"]
        Epic["Epic"]
    end

    subgraph RuntimeLayer["Runtime Interface"]
        Local["LocalRuntime (tmux)"]
        ClamshellRT["ClamshellRuntime (sandbox)"]
        DockerRT["DockerRuntime (compose)"]
    end

    subgraph Isolation["Clamshell Sandbox"]
        Policy["Deny-by-default egress"]
        Creds["Host-owned credentials"]
        Proxy["inference.local routing"]
        Audit["Per-binary audit"]
    end

    subgraph Vendors["Vendor Adapters"]
        Claude["Claude"]
        Codex["Codex"]
        Generic["Generic"]
    end

    CLI --> HTTP
    HTTP --> Store
    HTTP --> Broker
    HTTP --> Memory
    
    Store --> Templates
    Broker --> RuntimeLayer
    Memory --> RuntimeLayer
    
    ClamshellRT --> Isolation
    Isolation --> Vendors
    Local --> Vendors
    DockerRT --> Vendors
```

---

## Session Lifecycle (Climb)

```mermaid
sequenceDiagram
    participant U as User
    participant C as belayer CLI
    participant D as Daemon
    participant S as SQLite Store
    participant P as Pilot Agent
    participant I as Implementer
    participant R as Reviewer

    U->>C: belayer session start --template climb --input "task"
    C->>D: POST /sessions {name, template}
    D->>S: INSERT session
    D-->>C: {session_id, status: pending}
    
    Note over D: Load template → select runtime (local/clamshell/docker)
    
    D->>P: Launch via runtime
    D->>I: Launch via runtime
    D->>R: Launch via runtime
    
    loop Agent Coordination
        P->>D: POST /messages (instruction)
        D->>I: Route via Message Broker
        I->>D: POST /events (code written)
        D->>P: Notify completion
        
        P->>D: POST /messages (review request)
        D->>R: Route to reviewer
        R->>D: POST /events (review comments)
        D->>P: Return review
    end
    
    P->>D: PATCH /sessions/{id} {status: completed}
    D->>S: UPDATE status
    
    U->>C: belayer logs <session-id>
    C->>D: GET /sessions/{id}/events
    D->>S: SELECT events
    S-->>D: Event stream
    D-->>C: Return events
    C-->>U: Display logs
```

---

## Component Overview

Belayer v6 is built around a session runtime rather than a pipeline engine.

## Runtime Layers

1. **CLI shell** (`internal/cli/`)
   - Starts and inspects runtime processes
   - Operator entrypoint: daemon, session, attach, logs, status, recall
   - Connects to daemon via Unix socket HTTP client

2. **Daemon / supervisor** (`internal/daemon/`)
   - Long-lived process on Unix socket (`~/.belayer/daemon.sock`)
   - Session CRUD, event logging, graceful shutdown
   - Brokers session lifecycle transitions

3. **Session adapters** (`internal/vendor/`)
   - Claude adapter (stream-json parsing, token extraction)
   - Codex adapter (structured JSON, usage tracking)
   - Generic adapter (raw transcript, any CLI agent)
   - Registry pattern for vendor lookup

4. **Runtime storage** (`internal/store/`, `internal/memory/`)
   - SQLite + WAL for sessions and events with FTS5 search
   - Three-tier memory: core (in-context), archival (FTS5), recall (combined)
   - Markdown is authoritative; FTS5 is a derived index

5. **Runtime interface** (`internal/runtime/`)
   - `Runtime` interface with `Mode()`, `Containerized()`, `SupportsDynamicAgents()`
   - `LocalRuntime` — tmux sessions, no isolation, full host access
   - `DockerRuntime` — compose-based containers with network isolation (legacy)
   - `ClamshellRuntime` — deny-by-default sandboxes with host-owned credentials
   - `Select(useDocker, useClamshell)` dispatcher chooses backend

6. **Execution environments** (`internal/tmux/`, `internal/docker/`, `internal/clamshell/`)
   - tmux Runner interface with bracketed paste and pipe-pane capture
   - Docker sandboxes: compose generation, network isolation (none/limited/full)
   - Clamshell integration: `clamshell sandbox create/connect` for agent isolation
   - Per-agent worktrees via `git worktree add` for session isolation
   - Worktree cleanup wired into session stop and `belayer session clean`

7. **Communication** (`internal/broker/`)
   - Message broker: send, broadcast, subscribe, interrupt
   - 2s debounce coalescing for rapid messages
   - Urgent messages bypass debounce

8. **Agent framework** (`internal/agent/`, `internal/session/`, `internal/reflection/`)
   - YAML agent configs with role validation and tool registry
   - Session templates: climb, climb-fullstack, epic
   - Pilot-always-present invariant enforced in climb sessions
   - `Tier` field on AgentSpec (main, peripheral, ephemeral)
   - Sleep-time reflection for memory consolidation

## Package Dependency Graph

```
cli → daemon → store
cli → session (templates)
cli → runtime (Local/Docker/Clamshell selection)
cli → clamshell (sandbox connect for attach)
daemon → store
broker → store (message history)
reflection → memory + store
memory → (SQLite)
runtime → (interface only)
docker → (os/exec)
clamshell → (os/exec)
tmux → (os/exec)
vendor → (independent)
agent → (yaml.v3)
workspace → (os/exec, encoding/json)
```

## Security & Isolation Model

Belayer provides defense in depth through multiple isolation layers. Clamshell is the primary isolation backend.

### Clamshell Sandbox Architecture

```
Belayer Daemon (trusted control plane)
  - Pure Go binary, never runs LLM-generated code
  - Manages session lifecycle, messaging, events
  - Triggers reflection (launches sandbox, never runs LLM itself)

Clamshell Gateway (trusted, host-side)
  - Holds real credentials (API keys, tokens)
  - Runs managed proxy with policy enforcement
  - Provides inference.local routing (credential injection at proxy boundary)
  - Audit logging of all egress

Agent Sandboxes (untrusted, clamshell-managed)
  - Vendor CLI + git + belayer CLI
  - Worktree mounted at /workspace
  - NO real credentials (inference.local handles API auth)
  - Deny-by-default network (only proxy on loopback)
  - Per-binary egress policy
```

### Clamshell Isolation Properties

| Concern | How clamshell solves it |
|---------|----------------------|
| **Network isolation** | Deny-by-default iptables. Sandbox processes can only reach the managed proxy on loopback. |
| **Credential isolation** | Host-owned secrets, never mounted into sandbox. `inference.local` routing injects credentials at the proxy boundary. |
| **Egress control** | Per-binary policy — `claude` can reach `api.anthropic.com`; agent-written scripts cannot. |
| **Filesystem isolation** | Writable `/workspace`, read-only root. Host control plane never visible inside sandbox. |
| **Audit** | Deny event logs with binary identity, target, reason. `clamshell doctor` for health. |
| **Interactive access** | tmux-backed sessions. `belayer attach` wraps `clamshell sandbox connect`. |

### Runtime Comparison

| Aspect | Local (tmux) | Clamshell | Docker (legacy) |
|--------|-------------|-----------|-----------------|
| Network | Full host | Deny-by-default + per-binary policy | Internal Docker network + tinyproxy |
| Credentials | Environment variables | Host-owned, inference.local routing | Mounted .env file |
| Filesystem | Full host access | /workspace only, read-only root | Container overlayfs + mounts |
| Process | tmux session | Clamshell sandbox | Docker container |
| Use case | Development, trusted envs | Production default | Legacy deployments |

### Threat Model

| Threat | Mitigation |
|--------|------------|
| Agent escapes sandbox | Clamshell deny-by-default network + read-only root + no host paths |
| Agent accesses other sessions | Per-session sandbox names + session-scoped worktrees |
| Credential exfiltration | Credentials never enter sandbox (inference.local routing) + per-binary egress |
| Agent modifies host system | /workspace is only writable mount + no host filesystem access |
| Prompt injection via logs | Structured JSON logging + no shell interpolation of log content |
| Agent-written code phones home | Per-binary egress policy — only approved binaries reach approved endpoints |

---

## ASCII Architecture Reference

For environments without Mermaid rendering support:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           BELAYER v6 SYSTEM VIEW                            │
└─────────────────────────────────────────────────────────────────────────────┘

┌──────────────┐     HTTP/Unix      ┌─────────────────────────────────────────┐
│   CLI User   │◄─────Socket───────►│            BELAYER DAEMON               │
└──────────────┘                    │  ┌─────────┐  ┌─────────┐  ┌─────────┐  │
                                    │  │  HTTP   │  │ SQLite  │  │ Message │  │
┌──────────────┐                    │  │ Router  │  │ + FTS5  │  │ Broker  │  │
│   Agents     │◄───Agent IPC──────►│  └────┬────┘  └────┬────┘  └────┬────┘  │
│ (Claude,     │                    │       └────────────┴────────────┘       │
│  Codex, etc) │                    │              ┌─────────┐                │
└──────────────┘                    │              │ 3-Tier  │                │
                                    │              │ Memory  │                │
                                    │              └─────────┘                │
                                    └─────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                          SESSION TEMPLATES                                  │
└─────────────────────────────────────────────────────────────────────────────┘

    CLIMB               CLIMB-FULLSTACK          EPIC
   ┌─────────────┐     ┌─────────────────┐     ┌─────────────┐
   │   ┌─────┐   │     │     ┌─────┐     │     │   ┌─────┐   │
   │   │Pilot│   │     │     │Pilot│     │     │   │Pilot│   │
   │   └─┬─┬─┘   │     │     └─┬─┬─┘     │     │   └──┬──┘   │
   │     │ │     │     │       │ │       │     │      │      │
   │     ▼ ▼     │     │    ┌──┘ └──┐    │     │   Orchestrate│
   │  ┌───────┐  │     │    ▼       ▼    │     │   Sessions   │
   │  │Implmnt│  │     │ ┌─────┐ ┌─────┐│     └─────────────┘
   │  │   +   │  │     │ │ API │ │ App ││
   │  │Review │  │     │ │Impl │ │Impl ││
   │  └───────┘  │     │ └─────┘ └─────┘│
   └─────────────┘     │    + Reviewer   │
                       └─────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                     CLAMSHELL SANDBOX ARCHITECTURE                          │
└─────────────────────────────────────────────────────────────────────────────┘

┌───────────────────────────────┐
│   Clamshell Gateway (host)   │
│  ┌─────────────────────────┐ │
│  │  Managed Proxy          │ │         ┌──────────────┐
│  │  + inference.local      │─┼────────►│   Internet   │
│  │  + credential injection │ │         │  (per-binary  │
│  │  + egress policy        │ │         │   filtered)  │
│  └────────────┬────────────┘ │         └──────────────┘
│               │ loopback     │
│  ┌────────────▼────────────┐ │
│  │   Agent Sandbox         │ │
│  │   (deny-by-default)     │ │
│  │                         │ │
│  │   /workspace (RW)       │ │
│  │   vendor CLI (claude)   │ │
│  │   belayer CLI           │ │
│  │   NO real credentials   │ │
│  └─────────────────────────┘ │
└───────────────────────────────┘

Runtime Selection:
• local     → tmux sessions (no isolation)
• clamshell → deny-by-default sandbox (recommended)
• docker    → compose containers (legacy)
```

---

## API Reference

### Session Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/sessions` | Create new session |
| GET | `/sessions` | List all sessions |
| GET | `/sessions/{id}` | Get session by ID |
| PATCH | `/sessions/{id}` | Update session status |
| GET | `/sessions/{id}/events` | Get session events |
| POST | `/sessions/{id}/events` | Log event |
| POST | `/sessions/{id}/messages` | Send message to agent |
| POST | `/sessions/{id}/messages/broadcast` | Broadcast to all agents |

### Utility Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/search?q={query}` | FTS5 search across events |

### Planned Endpoints

| Method | Endpoint | Description | Issue |
|--------|----------|-------------|-------|
| POST | `/sessions/{id}/workbench` | Provision workbench stack | #43 |
| GET | `/sessions/{id}/workbench` | Workbench status + endpoints | #43 |
| DELETE | `/sessions/{id}/workbench` | Tear down workbench | #43 |
| POST | `/sessions/{id}/tools/{name}` | Execute tool | #44 |
| GET | `/sessions/{id}/tools` | List available tools | #44 |
| GET | `/sessions/{id}/events?after={id}&wait=30s` | Long-poll events | #53 |
| GET | `/events/stream?sessions=id1,id2` | SSE multi-session stream | #53 |

---

## Workspace Directory Structure

```
~/.belayer/
├── daemon.sock              # Unix socket (daemon listens here)
├── belayer.db               # SQLite database (WAL mode)
├── belayer.db-shm           # SQLite shared memory
├── belayer.db-wal           # SQLite write-ahead log
├── templates/
│   ├── pilot/               # Agent template (agent.yaml + system-prompt.md + agents.md)
│   ├── api-implementer/
│   ├── app-implementer/
│   ├── reviewer/
│   └── sprite/              # Ephemeral agent template
├── policies/
│   └── extend-fullstack.yaml  # Clamshell egress policy
├── environments/
│   └── extend-fullstack.yaml  # Environment config (repos, agents, policy, tools)
├── worktrees/
│   └── {sessionID}/
│       ├── extend-api/      # Per-repo per-session git worktree
│       └── extend-app/
└── repos.json               # Repository name → path mappings
```
