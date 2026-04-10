# Design

Status: `implemented` — v6 session runtime (2026-04-09)

## Qualities Achieved

- **Session-first UX**: Users think in sessions and templates, not node graphs.
- **Observable state**: SQLite-backed events with FTS5 search; `belayer status` and `belayer logs`.
- **Low ceremony**: `belayer daemon` + `belayer implement --input "task"` — no pipeline YAML.
- **Recoverable operations**: SQLite WAL mode, daemon graceful shutdown, session state persisted.
- **Vendor-agnostic core**: Adapter interface with Claude/Codex/Generic implementations.

## Key Patterns

- **Three-tier memory** (Letta-inspired): core (always in context) / archival (FTS5 search) / recall (combined). Agent-driven, markdown is authoritative.
- **Scion messaging**: Broker with bracketed paste delivery via tmux, 2s debounce for coalescing rapid messages, urgent bypass.
- **Pilot-always-present**: Implement sessions enforce pilot (opus) + implementer (sonnet) + reviewer (codex) trio.
- **Docker sandbox**: Per-session compose with internal network isolation, mounted .env for auth.
- **Multi-repo mapping**: AgentSpec YAML supports optional `repo` field for per-agent repository targeting. Environment config maps repo names to paths via `ResolveRepoPath`.
- **Per-session worktrees**: Each agent gets an isolated `git worktree` from `origin/main`, preventing agents from trampling each other's work or the user's checkout.
- **Sleep-time compute**: Post-session Reflector consolidates core memory into archival entries.
- **Sandbox as tool**: Agents don't run code in their own containers — they call `belayer sandbox up` to provision an execution environment on demand (Anthropic managed agents pattern).
- **Pluggable runtime**: Six OS-like primitives (agent, environment, session, sandbox, tool, event) with a Runtime interface that maps to Local/Docker/Kubernetes backends.
- **Tiered agents**: Main characters (persistent, peer-to-peer messaging), peripheral (session-scoped), ephemeral (task-scoped, spawned by any main character). Scion athenaeum pattern.
- **Tool execution routing**: User-defined tools in environment config, routed to agent/sandbox/infra/host targets by the daemon with audit trail.

## Observability

- **Streaming logs**: `belayer logs -f` polls events every 2s. `--since N` filters to last N minutes.
- **Debug command**: `belayer debug <id>` aggregates session metadata, recent events, Docker container health (`docker compose ps`), and logs from exited containers.
- **Agent self-observability**: Daemon socket mounted into Docker containers at `/belayer/daemon.sock`. Agents can call `belayer recall`, `belayer note`, `belayer logs` from inside sandboxes.
- **Error trapping**: Container entrypoint traps EXIT and logs agent exit code to the session event store via `belayer note`.
- **Restart context**: `belayer session wake --agent <name>` compiles event history into restart context for crashed agents. Vendor adapters provide `CompileRestartPrompt` for vendor-specific formatting.

## Session Templates

| Template | Phase | Agents | Purpose |
|----------|-------|--------|---------|
| intake | Intake | 1 (explorer) | Idea to spec |
| implement | Implement | 3+ (pilot, implementer, reviewer) | Implementation with review loop |
| deliver | Deliver | 2 (QA, merger) | Validation and merge |
