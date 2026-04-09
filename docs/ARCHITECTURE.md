# Architecture

Status: `target` — v6 clean-break baseline (2026-04-09)

Belayer v6 is being rebuilt around a session runtime rather than a pipeline engine.
This document describes the intended architectural shape for the new branch baseline.

## Planned Runtime Layers

1. **CLI shell**
   - Starts and inspects runtime processes
   - Provides the operator entrypoint

2. **Daemon / supervisor**
   - Owns long-lived runtime coordination
   - Tracks active sessions and task state
   - Brokers retries, recovery, and operator actions

3. **Session adapters**
   - Integrate external coding vendors/CLIs
   - Normalize launch, status, and completion behavior

4. **Runtime storage**
   - SQLite for local metadata and resumable state
   - File artifacts for logs, transcripts, and generated outputs

5. **Execution environments**
   - Local tmux panes by default
   - Optional Docker isolation where needed

## Baseline State

This branch intentionally preserves only the minimal buildable scaffold while the runtime is reintroduced.
Legacy v5 orchestration packages were removed to avoid mixing two incompatible architecture models.
