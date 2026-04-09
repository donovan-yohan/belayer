# Philosophy

Status: `implemented` — v6 session runtime (2026-04-09)

## The Core Bet

Belayer orchestrates long-lived coding sessions, not YAML pipeline graphs.

## What Belayer Owns

- Launching and attaching to agent sessions
- Tracking session state in SQLite
- Emitting and querying structured events (FTS5)
- Coordinating agent communication via message broker
- Managing three-tier memory (core/archival/recall)
- Providing Docker sandbox isolation
- Enforcing the pilot-always-present invariant

## What Belayer Does Not Own

- Agent intelligence (delegated to Claude, Codex, etc.)
- Code editing or testing (agents do this inside sessions)
- General-purpose workflow engines
- Framework installation or plugin systems

## Design Principles

1. **Session runtime first** — the primary unit is a running agent session.
2. **Local-first coordination** — SQLite + Unix socket, inspectable from the repo.
3. **Vendor delegation** — Belayer coordinates; agents do the work.
4. **Pilot is an LLM, not a state machine** — coordination requires judgment, not heuristics.
5. **Markdown is truth** — FTS5 is a derived index, rebuilt from authoritative sources.
6. **Add abstractions only after the runtime needs them twice.**

## Quality Anchors

Implementation correctness is measured against three reference systems:

1. **Letta** — Three-tier memory, agent-driven (not API blocks), sleep-time compute
2. **Scion** — tmux delivery, bracketed paste + debounce, same-binary CLI/IPC
3. **Anthropic Harness** — Daemon + CLI model, session lifecycle, event streaming
