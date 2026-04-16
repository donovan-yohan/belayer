# Design Documents

## Current Designs

- [2026-04-15-headless-hermes-daemon-architecture](2026-04-15-headless-hermes-daemon-architecture.md) — Replaces tmux (Layer 3) with Hermes's programmatic AIAgent API for headless inter-agent communication. Covers ecosystem survey (Scion, Gastown, Letta, A2A, LangGraph), Hermes API capabilities (hooks, callbacks, tool injection, ACP dispatch), daemon architecture with session bus, and implementation sequence (2026-04-15)
- [2026-04-15-belayer-run-model-for-nightshift-v1](2026-04-15-belayer-run-model-for-nightshift-v1.md) — Defines Belayer's three-layer run model for Nightshift v1: session bus, Hermes harness driver, and tmux transport adapter, plus the Belayer-mediated communication pattern for Hermes agents (2026-04-15)
- [2026-04-15-nightshift-v1-deployment-topology](2026-04-15-nightshift-v1-deployment-topology.md) — Nightshift v1 deployment model with one request per worker, two control planes, and Belayer positioned as the intra-run agent session bus (2026-04-15)
- [2026-04-15-nightshift-extend-first-implementation-delta](2026-04-15-nightshift-extend-first-implementation-delta.md) — Concrete package-by-package change plan from current Belayer toward an Extend-first Nightshift system, including likely code deletion targets (2026-04-15)
- [2026-04-15-nightshift-extend-first-architecture](2026-04-15-nightshift-extend-first-architecture.md) — Extend-first Nightshift architecture; updates Belayer for Hermes-backed specialists, extend-localenv workbench integration, and extend-clamshell as the primary runtime boundary (2026-04-15)
- [2026-04-09-sandbox-runtime-architecture-design](2026-04-09-sandbox-runtime-architecture-design.md) — Sandbox provisioning, pluggable runtime, agent topology, and security boundaries (2026-04-09)
