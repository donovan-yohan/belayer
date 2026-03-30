# Design Documents

## Current Designs

- [2026-03-29-quality-learning-loop-claude-codex-design](2026-03-29-quality-learning-loop-claude-codex-design.md) — Quality learning loop + claude-codex framework: carabiner quality patterns, agent nodes, gate-failure flywheel (2026-03-29)
- [2026-03-25-three-phase-architecture-design](2026-03-25-three-phase-architecture-design.md) — Three-phase architecture: Explore/Climb/Summit, multi-repo setter/spotter, config hierarchy (2026-03-25)

## Archived

### Implemented

- [2026-03-28-self-improving-harness-runtime-design](2026-03-28-self-improving-harness-runtime-design.md) — Self-improving .harness/ runtime: agent evolution, metrics-driven self-modification, stateless command protocol, retroactive bug tracing (2026-03-28)
- [2026-03-27-code-restructuring-v1-removal-design](2026-03-27-code-restructuring-v1-removal-design.md) — Remove v1 legacy code, flatten v3 to internal/, rename pipeline nodes (2026-03-27)
- [2026-03-26-exec-spawner-framework-scaffolding-design](2026-03-26-exec-spawner-framework-scaffolding-design.md) — Decouple node spawning from core, framework scaffolding via `belayer setup`, node protocol (2026-03-26)
- [2026-03-24-bug-architecture-review-design](2026-03-24-bug-architecture-review-design.md) — Architecture review step in bug flow + learnings enforcement across harness lifecycle (2026-03-24)
- [2026-03-23-summit-node-explorer-plugin-design](2026-03-23-summit-node-explorer-plugin-design.md) — Summit PR node, explorer plugin, `/explorer:send` skill (2026-03-23)
- [2026-03-19-harness-audit-design](2026-03-19-harness-audit-design.md) — Harness plugin audit: workflow fix for stale docs, learning persistence, doc lifecycle (2026-03-19)
- [2026-03-17-unified-agent-plugin-source-design](2026-03-17-unified-agent-plugin-source-design.md) — Unified agent plugin source design (2026-03-17)
- [2026-03-16-review-deferred-items-design](2026-03-16-review-deferred-items-design.md) — Test coverage, typed enums, HandleApproval bug fix (2026-03-16)
- [2026-03-16-review-loops-test-infra-design](2026-03-16-review-loops-test-infra-design.md) — Multi-persona review loops, test contracts, spotter shift, persistent learnings (2026-03-16)
- [2026-03-13-belayer-marketplace-design](2026-03-13-belayer-marketplace-design.md) — Belayer marketplace: vendor harness + pr plugins, auto-install via `belayer init` (2026-03-13)
- [2026-03-12-environment-provider-design](2026-03-12-environment-provider-design.md) — Environment provider interface for external tool integration (extend-cli) (2026-03-12)
- [2026-03-11-planning-review-hats-design](2026-03-11-planning-review-hats-design.md) — Planning hat (tracker intake) and review hat (PR monitoring, CI fix, review reaction) (2026-03-11)
- [2026-03-11-multi-provider-spawner-design](2026-03-11-multi-provider-spawner-design.md) — Multi-provider AgentSpawner: CodexSpawner + factory function + config wiring (2026-03-11)
- [2026-03-11-instance-to-crag-complete-rename-design](2026-03-11-instance-to-crag-complete-rename-design.md) — Complete instance→crag rename: package, config file, internal vars, docs (2026-03-11)
- [2026-03-10-crag-architecture-design](2026-03-10-crag-architecture-design.md) — Climbing terminology overhaul + per-role window layout with deferred activation (2026-03-10)
- [2026-03-10-filesystem-mail-store-design](2026-03-10-filesystem-mail-store-design.md) — Replace beads/dolt mail backend with pure filesystem store (2026-03-10)
- [2026-03-09-manage-session-context-design](2026-03-09-manage-session-context-design.md) — Manage session runtime .claude/ context with commands and CLAUDE.md (2026-03-09)
- [2026-03-08-interactive-lead-sessions-design](2026-03-08-interactive-lead-sessions-design.md) — Replace claude -p with full interactive sessions for leads, spotters, anchors (2026-03-08)
- [2026-03-07-context-aware-validation-design](2026-03-07-context-aware-validation-design.md) — Context-aware validation pipeline: spotter, anchor, config system (2026-03-07)
- [2026-03-07-cli-data-layer-design](2026-03-07-cli-data-layer-design.md) — Goal 1: CLI and data layer — pure data publisher (2026-03-07)
- [PRD-belayer-orchestrator](PRD-belayer-orchestrator.md) — Original project PRD: multi-repo coding agent orchestrator (2026-03-06)
- [PRD-agent-friendly-architecture](PRD-agent-friendly-architecture.md) — Agent-friendly architecture PRD (2026-03-07)

### Superseded

- [2026-03-29-adversarial-review-system-design](2026-03-29-adversarial-review-system-design.md) — Validate/Review split + blinded protocol for harness plugin (2026-03-29, superseded by carabiner separation)
- [2026-03-09-manage-session-context](2026-03-09-manage-session-context.md) — Manage session context design (historical draft) (2026-03-09)
- [2026-03-09-mail-system-design](2026-03-09-mail-system-design.md) — Beads-backed inter-agent mail system with tmux send-keys delivery (2026-03-09)
- [2026-03-07-belayer-manage-design](2026-03-07-belayer-manage-design.md) — Goal 5: Belayer manage — interactive agent session for task creation (2026-03-07)
- [2026-03-07-spotter-review-design](2026-03-07-spotter-review-design.md) — Goal 4: Spotter — cross-repo review with redistribution (2026-03-07)
- [2026-03-07-lead-spawning-design](2026-03-07-lead-spawning-design.md) — Goal 3: Lead spawning — AgentSpawner interface and per-goal sessions (2026-03-07)
- [2026-03-07-setter-daemon-design](2026-03-07-setter-daemon-design.md) — Goal 2: Setter daemon — DAG executor with tmux management (2026-03-07)
- [2026-03-07-agent-friendly-architecture-design](2026-03-07-agent-friendly-architecture-design.md) — Agent-friendly architecture: Setter, Spotter, and Lead redesign (2026-03-07)
- [2026-03-06-cross-repo-integration-design](2026-03-06-cross-repo-integration-design.md) — Goal 6: Cross-repo integration & alignment (2026-03-06)
- [2026-03-06-task-intake-decomposition-design](2026-03-06-task-intake-decomposition-design.md) — Goal 5: Task intake & decomposition (2026-03-06)
- [2026-03-06-coordinator-engine-design](2026-03-06-coordinator-engine-design.md) — Goal 4: Coordinator engine (state machine + agentic nodes) (2026-03-06)
- [2026-03-06-lead-execution-loop-design](2026-03-06-lead-execution-loop-design.md) — Goal 3: Bundled lead execution loop (2026-03-06)
- [2026-03-06-instance-repo-management-design](2026-03-06-instance-repo-management-design.md) — Goal 2: Crag & repository management (was "instance") (2026-03-06)
- [2026-03-06-project-scaffolding-design](2026-03-06-project-scaffolding-design.md) — Goal 1: Project scaffolding & core architecture (2026-03-06)

### Stale

- [2026-03-06-tui-dashboard-design](2026-03-06-tui-dashboard-design.md) — Goal 7: TUI dashboard (archived — never implemented) (2026-03-06)
