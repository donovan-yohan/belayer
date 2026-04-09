# CLAUDE.md

Belayer v6 — session runtime for autonomous coding agents.

## Architecture

Daemon-based session runtime. One global daemon (`belayer daemon`) serves all workspaces via Unix socket. Sessions are the primitive — each session has a template (intake/implement/deliver), agents, events, and messages stored in SQLite.

Three-phase model:
- **intake** — single agent generates spec from idea
- **implement** — pilot (opus) + implementer (sonnet) + reviewer (codex) trio
- **deliver** — QA validation and merge

## CLI Surface

```
belayer daemon                     # Start supervisor
belayer setup [--global]           # Bootstrap .belayer/ workspace (cwd or global)
belayer implement --input "task"   # Launch implementation session with agent trio
belayer session create/list/stop   # Low-level session CRUD
belayer attach <id> [--agent name] # Attach to agent tmux panes
belayer status                     # Daemon health + active sessions
belayer logs <id>                  # Session event stream
belayer recall "query"             # FTS5 search across events
belayer message send/broadcast     # Agent-to-agent messaging
belayer context                    # Session info (messaging plane)
belayer note "text"                # Log observation to session
```

## Workspace Scoping

Workspaces are cwd-derived: belayer walks up from the current directory looking for `.belayer/`. Falls back to `~/.belayer/` if none found. The daemon is global; workspaces are local.

## Key Packages

- `internal/daemon/` — HTTP server on Unix socket, session/event CRUD
- `internal/store/` — SQLite with WAL mode, FTS5 for event search
- `internal/session/` — Template definitions (intake/implement/deliver)
- `internal/agent/` — Config, prompt compilation, memory, tool registry
- `internal/broker/` — Message send/broadcast with 2s debounce
- `internal/tmux/` — Local tmux runner for agent execution
- `internal/vendor/` — Claude, Codex, Generic adapters
- `internal/memory/` — Three-tier: core (in-context), archival (FTS5), recall
- `internal/workspace/` — repos.json loading and path resolution
- `internal/docker/` — Compose generation for sandboxed execution (not yet wired)

## Development

```bash
go build ./cmd/belayer
go test ./...
go install ./cmd/belayer
```

## Guidance

- The daemon handles plumbing. The pilot agent handles judgment.
- Do not replace LLM judgment with deterministic heuristics.
- Pilot is always present in implement sessions, even single-repo.
- Keep the phase naming clear: intake/implement/deliver in code (explore/climb/summit is marketing only).
