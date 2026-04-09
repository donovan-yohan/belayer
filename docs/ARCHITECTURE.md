# Belayer v6 Architecture

Status: `implemented` — v6 session runtime (2026-04-09)

> Many robots, bring your own pilots.

Belayer v6 is a daemon-based session runtime for orchestrating multiple AI coding agents through a structured three-phase workflow. This document provides both high-level diagrams and implementation details for technical audiences.

---

## System Architecture Diagram

```mermaid
flowchart TB
    subgraph Client["CLI Client"]
        CLI["belayer CLI<br/>(cobra commands)"]
    end

    subgraph Daemon["Belayer Daemon (HTTP/Unix Socket)"]
        HTTP["HTTP Router<br/>POST /sessions<br/>GET /sessions/{id}<br/>POST /events<br/>POST /messages"]
        Store[("SQLite Store<br/>• sessions<br/>• events<br/>• FTS5 search<br/>WAL mode")]
        Broker["Message Broker<br/>• Send/Broadcast<br/>• Debounced delivery<br/>• Agent IPC"]
        Memory[("Three-Tier Memory<br/>├─ Core (in-context)<br/>├─ Archival (FTS5)<br/>└─ Recall (combined)")]
    end

    subgraph SessionTemplates["Session Templates"]
        Intake["Intake Phase<br/>├─ 1 Agent: explorer<br/>└─ Idea → Spec"]
        Implement["Implement Phase<br/>├─ pilot (opus)<br/>├─ implementer (sonnet)<br/>└─ reviewer (codex)"]
        Deliver["Deliver Phase<br/>├─ qa<br/>└─ merger"]
    end

    subgraph Execution["Agent Execution"]
        subgraph Local["Local Mode"]
            Tmux["tmux Runner<br/>• CreateSession<br/>• SendKeys<br/>• CapturePane"]
        end
        
        subgraph DockerMode["Docker Mode"]
            Sandbox["Docker Sandbox<br/>• Network isolation<br/>• tinyproxy allowlist<br/>• Volume mounts"]
            Proxy["tinyproxy<br/>Limited → allowlisted<br/>None → internal only<br/>Full → unrestricted"]
        end
    end

    subgraph Vendors["Vendor Adapters"]
        Claude["Claude Adapter<br/>claude-code"]
        Codex["Codex Adapter<br/>codex"]
        Generic["Generic Adapter<br/>Any terminal program"]
    end

    subgraph Workspaces["Workspace Config"]
        WSStruct["~/.belayer/<br/>├─ daemon.sock<br/>├─ belayer.db<br/>├─ templates/*.yaml<br/>├─ sandboxes/{id}/<br/>└─ repos.json"]
    end

    CLI -->|Unix Socket| HTTP
    HTTP --> Store
    HTTP --> Broker
    HTTP --> Memory
    
    Store -->|Session CRUD| SessionTemplates
    Broker -->|Route messages| Execution
    Memory -->|Recall context| Execution
    
    Execution -->|Launch| Vendors
    
    SessionTemplates -.->|Load config| Workspaces
    
    Tmux -->|exec| Vendors
    Sandbox -->|container| Vendors
    Sandbox --> Proxy

    style Daemon fill:#e1f5ff,stroke:#01579b,stroke-width:2px
    style Execution fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px
    style Vendors fill:#fff3e0,stroke:#ef6c00,stroke-width:2px
    style Client fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px
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

## Security Model

- **Shell safety**: All YAML template values pass through `internal/shell.Quote` before shell interpolation. Template validation rejects agent names and env keys with unsafe characters.
- **Directory permissions**: All `.belayer/` directories created with 0700. Daemon socket chmod'd to 0600. Compose files and templates written with 0600.
- **Network isolation**: Docker `internal: true` networks prevent direct internet access. Limited mode uses tinyproxy with anchored regex patterns. Host validation rejects broad patterns (`.*`, `.`, `*`) and non-hostname characters.
- **Auth isolation**: Vendor credentials forwarded via mounted `.env` file (0600), never embedded in compose YAML or shell commands.
- **Compose safety**: All values in generated docker-compose.yml are YAML double-quoted to prevent YAML injection.

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
