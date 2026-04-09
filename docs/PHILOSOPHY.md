# Philosophy

Belayer v6 starts from a clean break.

Status: `active` — updated 2026-04-09 for the v6 baseline branch.

---

## The Core Bet

Belayer should orchestrate long-lived coding sessions, not YAML pipeline graphs.

The v5 system proved that orchestration matters, but it also showed where the weight accumulated:
Temporal workers, pipeline DSLs, framework installers, plugin registries, and file-polling contracts
made the system harder to evolve than the actual product problem.

v6 resets around a simpler idea:

- **Session runtime first** — the primary unit is a running agent session.
- **Local-first coordination** — runtime state should be inspectable from the repo and machine running the work.
- **Vendor delegation** — Belayer coordinates external coding agents instead of re-implementing them.
- **Minimal permanent surface area** — keep shared types, event envelopes, and the CLI shell; grow only what the runtime proves necessary.

---

## What Belayer Owns In v6

Belayer owns the runtime shell around autonomous work:

- launching or attaching to sessions
- tracking task/session state
- emitting structured events
- coordinating retries and handoffs
- persisting lightweight local metadata

Belayer does **not** need to own a general-purpose workflow engine, a separate harness product,
or a permanent YAML abstraction before the runtime model is proven.

---

## Why The Clean Break Matters

v5 and v6 are different mental models.

- **v5**: pipelines, gates, routers, Temporal activities, framework installation
- **v6**: daemon-managed sessions, local state, tmux/docker/runtime coordination, SQLite-backed metadata

Keeping both models alive in the same branch would create ambiguity for future agents and future design work.
The clean break is intentional: remove the old model first, then build the new one on an uncluttered base.

---

## Design Principles

1. **Prefer direct runtime primitives over meta-frameworks.**
2. **Keep state local, legible, and recoverable.**
3. **Make orchestration observable through structured events.**
4. **Add abstractions only after the runtime needs them twice.**
5. **Treat docs as navigation for future agents, not marketing residue from the old model.**

---

## Non-Goals For This Branch

This baseline branch is not trying to ship the full v6 runtime yet.
It only establishes a clean foundation by removing v5 assumptions and preserving the smallest useful scaffold.
