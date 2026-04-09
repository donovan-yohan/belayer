# Design

Status: `active` — v6 clean-break baseline (2026-04-09)

Belayer v6 design work should optimize for operator clarity and runtime simplicity.

## Desired Qualities

- **Session-first UX**: users think in tasks and live sessions, not node graphs.
- **Observable state**: operators can tell what is running, blocked, failed, or waiting.
- **Low ceremony**: the common case should not require framework setup or pipeline authoring.
- **Recoverable operations**: crashes and restarts should preserve enough local state to resume.
- **Vendor-agnostic core**: Belayer coordinates external agents without hard-coding a single provider model.

## Branch Guidance

On `feature/v6`, prefer building the smallest runtime slice that proves the session model.
Do not reintroduce Temporal, the v5 YAML pipeline engine, or framework/plugin scaffolding unless a v6 design explicitly calls for it.
