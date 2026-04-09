# belayer

Session runtime for autonomous coding agents. Many robots, bring your own pilots.

## What It Does

Belayer provides the infrastructure for multi-agent coding sessions: a daemon that manages sessions, messaging, memory, and execution environments. You bring the AI agents (Claude, Codex, or any terminal program); belayer provides the coordination layer.

## Quick Start

```bash
# Build and install
go install ./cmd/belayer

# Bootstrap a workspace
belayer setup

# Start the daemon
belayer daemon

# Launch an implementation session
belayer implement --input "Add rate limiting to /api/v1/cards"

# Attach to the pilot agent
belayer attach <session-name> --agent pilot

# Monitor
belayer status
belayer logs <session-id>
```

## Three Phases

| Phase | Command | Agents | Purpose |
|---|---|---|---|
| **Intake** | `belayer intake` | 1 (explorer) | Idea to spec |
| **Implement** | `belayer implement` | 3 (pilot, implementer, reviewer) | Code with review loop |
| **Deliver** | `belayer deliver` | 2 (QA, merger) | Validate, merge, monitor |

## Architecture

- **Daemon** — always-on supervisor on Unix socket (`~/.belayer/daemon.sock`)
- **SQLite** — session store, event log, FTS5 search (WAL mode)
- **tmux** — agent execution substrate (local; Docker containers planned)
- **Three-tier memory** — core (in-context), archival (FTS5), recall (combined)
- **Message broker** — agent-to-agent IPC with debounce
- **Vendor adapters** — Claude, Codex, Generic (extensible)

## Development

```bash
go build ./cmd/belayer
go test ./...
```

Tracked in [Epic #21](https://github.com/donovan-yohan/belayer/issues/21).
