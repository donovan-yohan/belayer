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
- **Sleep-time compute**: Post-session Reflector consolidates core memory into archival entries.

## Session Templates

| Template | Phase | Agents | Purpose |
|----------|-------|--------|---------|
| intake | Intake | 1 (explorer) | Idea to spec |
| implement | Implement | 3+ (pilot, implementer, reviewer) | Implementation with review loop |
| deliver | Deliver | 2 (QA, merger) | Validation and merge |
