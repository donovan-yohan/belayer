# Design

Status: `implemented` — v6 session runtime (2026-04-09)

## Qualities Achieved

- **Session-first UX**: Users think in sessions and templates, not node graphs.
- **Observable state**: SQLite-backed events with FTS5 search; `belayer status` and `belayer logs`.
- **Low ceremony**: `belayer daemon` + `belayer session start --template climb --input "task"` — no pipeline YAML.
- **Recoverable operations**: SQLite WAL mode, daemon graceful shutdown, session state persisted.
- **Vendor-agnostic core**: Adapter interface with Claude/Codex/Generic implementations.
- **Isolation by default**: Clamshell sandboxes provide deny-by-default egress, host-owned credentials, per-binary policy. Belayer never builds its own isolation.

## Key Patterns

- **Three-tier memory** (Letta-inspired): **Core** — session-scoped key-value pairs, always injected into prompts, upsert semantics for fast updates. **Archival** — append-only long-term learnings with provenance (session, source file, date, tags), full-text searchable via FTS5. **Recall** — on-demand combined view: core entries for the current session + archival search results for a query. Markdown files on disk are authoritative; SQLite FTS5 is a derived index rebuilt from markdown via `RebuildIndex` on fresh clones.
- **Scion messaging**: Broker with bracketed paste delivery via tmux, 2s debounce for coalescing rapid messages, urgent bypass.
- **Pilot-always-present**: Climb sessions enforce pilot (opus) + implementer (sonnet) + reviewer (codex) trio. Pilot orchestrates, facilitates PR-based review loops, detects cross-repo drift in fullstack sessions. Review loops evolve via agent memory, not hardcoded rules.
- **PR-based review loop**: Implementer creates PR → pilot routes to reviewer with spec context → reviewer provides fresh-eyes pass/fail → on fail, pilot sends feedback to implementer → iterate until pass. The reviewer is a different vendor (codex) with no implementation context — genuinely independent review.
- **Agent memory & learning**: All agents get personal memory that persists across sessions. Pilot accumulates coordination patterns, implementers learn codebase conventions, reviewer's checklist evolves from experience. Post-session reflection consolidates both personal memory and shared institutional learnings.
- **Clamshell sandbox**: Deny-by-default network, host-owned credentials via `inference.local` routing, per-binary egress policy, audit logging. Replaces Docker sandbox + tinyproxy model.
- **Pluggable runtime**: `Runtime` interface in `internal/runtime/` with `LocalRuntime` (tmux), `DockerRuntime` (compose, legacy), `ClamshellRuntime` (sandbox). `Select()` dispatcher chooses backend based on CLI flags.
- **Multi-repo mapping**: AgentSpec YAML supports optional `repo` field for per-agent repository targeting. Environment config maps repo names to paths via `ResolveRepoPath`.
- **Per-session worktrees**: Each agent gets an isolated `git worktree` from `origin/main`, preventing agents from trampling each other's work or the user's checkout. Cleanup wired into session stop and `belayer session clean`.
- **Sleep-time compute**: Post-session Reflector consolidates core memory into archival entries.
- **Tiered agents** (data model): Main characters (persistent, peer-to-peer messaging), peripheral (session-scoped), ephemeral (task-scoped). `Tier` field on `AgentSpec`. Scion athenaeum pattern.
- **Epic sessions**: Pilot-only roster for workspace orchestration. Pilot creates/monitors/stops child sessions for epic decomposition.

## Planned Patterns

These are defined in the [sandbox runtime design doc](design-docs/2026-04-09-sandbox-runtime-architecture-design.md) and tracked as open issues:

- **Workbench provisioning** (#43, #52): On-demand test infrastructure (`belayer workbench up/down`) with health checks and readiness detection. Workbench is distinct from sandbox — it's the application stack agents test against.
- **Tool execution routing** (#44): User-defined tools in environment config, routed to agent/workbench/infra/host targets by the daemon with audit trail. Template variables auto-shell-quoted via `internal/shell/`.
- **Tiered agent behavior** (#49): Dynamic spawning of peripheral and ephemeral agents at runtime (`belayer session add-agent --tier ephemeral`). Auto-cleanup on ephemeral completion.
- **Pilot orchestration tools** (#50): Cross-session management — pilot calls `belayer session start/stop/list`, provisions workbenches, runs tools, monitors via event stream.
- **Event-driven monitoring** (#53): SSE/long-poll endpoints replacing 2s polling. `belayer watch --sessions id1,id2` for multi-session pilots.

## Observability

- **Streaming logs**: `belayer logs -f` polls events every 2s. `--since N` filters to last N minutes.
- **Debug command**: `belayer debug <id>` aggregates session metadata, recent events, container health, and logs from exited agents.
- **Agent self-observability**: Daemon socket available inside sandboxes. Agents can call `belayer recall`, `belayer note`, `belayer logs` from within their sandbox.
- **Error trapping**: Container entrypoint traps EXIT and logs agent exit code to the session event store via `belayer note`.
- **Restart context**: `belayer session wake --agent <name>` compiles event history into restart context for crashed agents. Vendor adapters provide `CompileRestartPrompt` for vendor-specific formatting.

## Session Templates

| Template | Agents | Purpose |
|----------|--------|---------|
| climb | 3 (pilot, implementer, reviewer) | Single-repo implementation with review loop |
| climb-fullstack | 4 (pilot, api-impl, app-impl, reviewer) | Multi-repo implementation (e.g. API + frontend) |
| epic | 1 (pilot) | Workspace orchestration — decomposes epics, creates parallel sessions |
