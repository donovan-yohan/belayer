# Belayer v6 Architecture

Status: `implemented` — v6 session runtime (2026-04-09)

> Many robots, bring your own pilots.

Belayer v6 is a daemon-based session runtime for orchestrating multiple AI coding agents through a structured three-phase workflow. This document provides both high-level diagrams and implementation details for technical audiences.

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
        Intake["Intake Phase"]
        Implement["Implement Phase"]
        Deliver["Deliver Phase"]
    end

    subgraph Execution["Agent Execution"]
        subgraph Local["Local Mode"]
            Tmux["tmux Runner"]
        end
        
        subgraph DockerMode["Docker Mode"]
            Sandbox["Docker Sandbox"]
            Proxy["tinyproxy"]
        end
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
    Broker --> Execution
    Memory --> Execution
    
    Execution --> Vendors
    
    Templates -.-> Workspaces
    
    Tmux --> Vendors
    Sandbox --> Vendors
    Sandbox --> Proxy
```

---

## Session Lifecycle (Implement Phase)

```mermaid
sequenceDiagram
    participant U as User
    participant C as belayer CLI
    participant D as Daemon
    participant S as SQLite Store
    participant P as Pilot Agent
    participant I as Implementer
    participant R as Reviewer

    U->>C: belayer session start --template implement --input "task"
    C->>D: POST /sessions {name, template}
    D->>S: INSERT session
    D-->>C: {session_id, status: pending}
    
    Note over D: Load template (pilot, implementer, reviewer)
    
    D->>P: Launch via tmux/Docker
    D->>I: Launch via tmux/Docker
    D->>R: Launch via tmux/Docker
    
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

5. **Execution environments** (`internal/tmux/`, `internal/docker/`)
   - tmux Runner interface with bracketed paste and pipe-pane capture
   - Docker sandboxes: compose generation, network isolation (none/limited/full), tinyproxy allowlisting
   - Per-agent worktrees via `git worktree add` for session isolation
   - Daemon socket mounted into containers for agent self-observability
   - Container entrypoint with PID 1 init (UID/GID sync, EXIT trap, two-window tmux)

6. **Communication** (`internal/broker/`)
   - Message broker: send, broadcast, subscribe, interrupt
   - 2s debounce coalescing for rapid messages
   - Urgent messages bypass debounce

7. **Agent framework** (`internal/agent/`, `internal/session/`, `internal/reflection/`)
   - YAML agent configs with role validation and tool registry
   - Intake/Implement/Deliver session templates
   - Pilot-always-present invariant enforced in Implement
   - Sleep-time reflection for memory consolidation

## Package Dependency Graph

```
cli → daemon → store
cli → session (templates)
daemon → store
broker → store (message history)
reflection → memory + store
memory → (SQLite)
docker → (os/exec)
tmux → (os/exec)
vendor → (independent)
agent → (yaml.v3)
workspace → (os/exec, encoding/json)
```

## Security & Isolation Model

Belayer provides defense in depth through multiple isolation layers. Understanding these boundaries is critical for security-conscious deployments.

### Network Isolation

#### Docker Network Architecture

```
Host Network Namespace
└── Docker Daemon
    ├── Session Network A (internal: true, isolated)
    │   ├── Container: pilot ──┐
    │   ├── Container: implementer │──► tinyproxy ──► Internet (if allowed)
    │   └── Container: reviewer ──┘
    │
    ├── Session Network B (internal: true, isolated)
    │   └── Container: explorer ──► tinyproxy ──► Internet (if allowed)
    │
    └── tinyproxy (per-session, optional)
        └── Allowlist: ["api.anthropic.com", "*.github.com"]
```

**Key Properties:**
- **Internal networks**: Docker networks marked `internal: true` cannot reach the host network or internet directly
- **Per-session isolation**: Each session gets its own Docker network; agents cannot communicate across sessions
- **tinyproxy filtering**: In "limited" mode, all outbound traffic goes through tinyproxy with regex-based host allowlisting
- **Host validation**: Broad patterns (`.*`, `.`, `*`) and non-hostname characters are rejected to prevent accidental over-allowlisting

#### Network Modes

| Mode | Implementation | Use Case |
|------|----------------|----------|
| `none` | Internal Docker network only | Air-gapped environments, offline work |
| `limited` | tinyproxy with allowlist | Production default — vendor APIs + package managers only |
| `full` | Direct bridge network | Trusted environments, maximum flexibility |

### Filesystem Isolation

#### Container Mounts

Each Docker container receives carefully scoped filesystem access:

| Mount | Path | Mode | Purpose |
|-------|------|------|---------|
| Workspace | `/workspace` | Read-write | Project files agent works on |
| Daemon Socket | `/belayer/daemon.sock` | Read-only | Agent self-observability API |
| Credentials | `/belayer/.env` | Read-only (0600) | Vendor API keys (never in env vars) |
| Git Config | `~/.gitconfig` | Read-only | Git identity for commits |
| SSH Keys | `~/.ssh` | Read-only (if mounted) | Git authentication |

**Container Filesystem:**
- Overlayfs for container root — changes outside mounted paths are ephemeral
- No access to host `/proc`, `/sys`, or other system paths
- No ability to mount new volumes or escape via privileged operations

#### Local Mode (tmux) Isolation

When Docker is not used, agents run as tmux sessions:

| Aspect | Isolation Level |
|--------|-----------------|
| Process | Separate tmux session with unique name |
| Filesystem | Full host access (runs in CWD) |
| Network | Full host network access |
| Use case | Trusted environments, no Docker overhead |

**Process Isolation:**
- Session naming: `belayer-{session-id}-{agent-name}`
- Separate process group for signal management
- Bracketed paste mode to prevent injection attacks

### Access Control Matrix

| Resource | Docker Mode | Local Mode |
|----------|-------------|------------|
| Workspace files | RW via mount | RW in CWD |
| Daemon API | RO via socket mount | Unix socket (same) |
| Internet | filtered/none/full | Full access |
| Host filesystem | No access | Full access |
| Other sessions | No access | No access (via session scoping) |
| Credentials | Mounted file only | Environment variables |

### Security Mechanisms

- **Shell safety**: All YAML template values pass through `internal/shell.Quote` before shell interpolation. Template validation rejects agent names and env keys with unsafe characters.
- **Directory permissions**: All `.belayer/` directories created with 0700. Daemon socket chmod'd to 0600. Compose files and templates written with 0600.
- **Compose safety**: All values in generated docker-compose.yml are YAML double-quoted to prevent YAML injection.
- **Auth isolation**: Vendor credentials forwarded via mounted `.env` file (0600), never embedded in compose YAML or shell commands.

### Threat Model

| Threat | Mitigation |
|--------|------------|
| Agent escapes container | Docker `internal` networks + no privileged mode + read-only rootfs for system paths |
| Agent accesses other sessions | Per-session Docker networks + session-scoped API tokens |
| Credential exfiltration | Credentials in mounted files (not env vars) + network filtering |
| Agent modifies host system | Container overlayfs + limited mount scope |
| Prompt injection via logs | Structured JSON logging + no shell interpolation of log content |
| Privilege escalation | Non-root container user (UID/GID sync) + no sudo |

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
│                          THREE-PHASE WORKFLOW                               │
└─────────────────────────────────────────────────────────────────────────────┘

    INTAKE              IMPLEMENT                 DELIVER
   ┌─────────┐         ┌─────────────┐           ┌─────────────┐
   │         │         │   ┌─────┐   │           │   ┌─────┐   │
   │ Explorer│         │   │Pilot│◄──┼──coord───►│   │ QA  │   │
   │    │    │         │   └─┬─┬─┘   │           │   └──┬──┘   │
   │    ▼    │         │     │ │     │           │      │      │
   │  Spec   │         │     │ │     │           │   Validate   │
   └─────────┘         │     ▼ ▼     │           │      │      │
                       │  ┌───────┐  │           │   ┌──┴──┐   │
                       │  │Implmnt│  │           │   │Merge│   │
                       │  │   +   │  │           │   │ /PR │   │
                       │  │Review │  │           │   └─────┘   │
                       │  └───────┘  │           └─────────────┘
                       └─────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                      DOCKER SANDBOX ARCHITECTURE                            │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────┐
│   Docker Network    │
│  (per-session isol) │
│  ┌───────────────┐  │         ┌──────────────┐
│  │  Agent Cont.  │  │◄───────►│   tinyproxy  │
│  │  (claude-code)│  │         │  (optional)  │
│  └───────┬───────┘  │         └──────┬───────┘
│          │ mount    │                │
│          ▼          │                ▼
│  ┌───────────────┐  │         ┌──────────────┐
│  │  Unix Socket  │  │         │   Internet   │
│  │ daemon.sock   │◄─┼─────────│  (filtered)  │
│  └───────────────┘  │         └──────────────┘
└─────────────────────┘

Network Modes:
• none    → Internal Docker network only (air-gapped)
• limited → Allowlisted hosts via tinyproxy
• full    → Unrestricted internet access
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

---

## Workspace Directory Structure

```
~/.belayer/
├── daemon.sock              # Unix socket (daemon listens here)
├── belayer.db               # SQLite database (WAL mode)
├── belayer.db-shm           # SQLite shared memory
├── belayer.db-wal           # SQLite write-ahead log
├── templates/
│   ├── intake.yaml          # Custom template overrides
│   ├── implement.yaml
│   └── deliver.yaml
├── sandboxes/
│   └── {session-id}/
│       └── docker-compose.yml
├── environments/
│   └── {env-name}.yaml      # Docker environment configs
└── repos.json               # Repository mappings
```
