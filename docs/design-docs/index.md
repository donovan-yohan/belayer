# Design Documents

## Current Designs

| Document | Purpose | Status | Created |
|----------|---------|--------|---------|
| [summit-node-explorer-plugin-design.md](2026-03-23-summit-node-explorer-plugin-design.md) | Summit PR node, explorer plugin, `/explorer:send` skill | Current | 2026-03-23 |

**See also:** [Belayer v2 Temporal Orchestrator](../designs/temporal-orchestrator-reimagining.md) — implemented 2026-03-19

## Archived

| Document | Purpose | Status | Created |
|----------|---------|--------|---------|
| [2026-03-19-harness-audit-design](2026-03-19-harness-audit-design.md) | Harness plugin audit: workflow fix for stale docs, learning persistence, doc lifecycle | Implemented | 2026-03-19 |
| [2026-03-17-unified-agent-plugin-source-design](2026-03-17-unified-agent-plugin-source-design.md) | Unified agent plugin source design | Implemented | 2026-03-17 |
| [2026-03-16-review-deferred-items-design](2026-03-16-review-deferred-items-design.md) | Test coverage, typed enums, HandleApproval bug fix | Implemented | 2026-03-16 |
| [2026-03-16-review-loops-test-infra-design](2026-03-16-review-loops-test-infra-design.md) | Multi-persona review loops, test contracts, spotter shift, persistent learnings | Implemented | 2026-03-16 |
| [2026-03-13-belayer-marketplace-design](2026-03-13-belayer-marketplace-design.md) | Belayer marketplace: vendor harness + pr plugins, auto-install via `belayer init` | Implemented | 2026-03-13 |
| [2026-03-12-environment-provider-design](2026-03-12-environment-provider-design.md) | Environment provider interface for external tool integration (extend-cli) | Implemented | 2026-03-12 |
| [2026-03-11-planning-review-hats-design](2026-03-11-planning-review-hats-design.md) | Planning hat (tracker intake) and review hat (PR monitoring, CI fix, review reaction) | Implemented | 2026-03-11 |
| [2026-03-11-multi-provider-spawner-design](2026-03-11-multi-provider-spawner-design.md) | Multi-provider AgentSpawner: CodexSpawner + factory function + config wiring | Implemented | 2026-03-11 |
| [2026-03-11-instance-to-crag-complete-rename-design](2026-03-11-instance-to-crag-complete-rename-design.md) | Complete instance→crag rename: package, config file, internal vars, docs | Implemented | 2026-03-11 |
| [2026-03-10-crag-architecture-design](2026-03-10-crag-architecture-design.md) | Climbing terminology overhaul + per-role window layout with deferred activation | Implemented | 2026-03-10 |
| [2026-03-10-filesystem-mail-store-design](2026-03-10-filesystem-mail-store-design.md) | Replace beads/dolt mail backend with pure filesystem store | Implemented | 2026-03-10 |
| [2026-03-09-manage-session-context-design](2026-03-09-manage-session-context-design.md) | Manage session runtime .claude/ context with commands and CLAUDE.md | Implemented | 2026-03-09 |
| [2026-03-08-interactive-lead-sessions-design](2026-03-08-interactive-lead-sessions-design.md) | Replace claude -p with full interactive sessions for leads, spotters, anchors | Implemented | 2026-03-08 |
| [2026-03-07-context-aware-validation-design](2026-03-07-context-aware-validation-design.md) | Context-aware validation pipeline: spotter, anchor, config system | Implemented | 2026-03-07 |
| [2026-03-07-cli-data-layer-design](2026-03-07-cli-data-layer-design.md) | Goal 1: CLI and data layer — pure data publisher | Implemented | 2026-03-07 |
| [PRD-belayer-orchestrator](PRD-belayer-orchestrator.md) | Original project PRD: multi-repo coding agent orchestrator | Implemented | 2026-03-06 |
| [PRD-agent-friendly-architecture](PRD-agent-friendly-architecture.md) | Agent-friendly architecture PRD | Implemented | 2026-03-07 |
| [2026-03-09-manage-session-context](2026-03-09-manage-session-context.md) | Manage session context design (historical draft) | Superseded | 2026-03-09 |
| [2026-03-09-mail-system-design](2026-03-09-mail-system-design.md) | Beads-backed inter-agent mail system with tmux send-keys delivery | Superseded | 2026-03-09 |
| [2026-03-07-belayer-manage-design](2026-03-07-belayer-manage-design.md) | Goal 5: Belayer manage — interactive agent session for task creation | Superseded | 2026-03-07 |
| [2026-03-07-spotter-review-design](2026-03-07-spotter-review-design.md) | Goal 4: Spotter — cross-repo review with redistribution | Superseded | 2026-03-07 |
| [2026-03-07-lead-spawning-design](2026-03-07-lead-spawning-design.md) | Goal 3: Lead spawning — AgentSpawner interface and per-goal sessions | Superseded | 2026-03-07 |
| [2026-03-07-setter-daemon-design](2026-03-07-setter-daemon-design.md) | Goal 2: Setter daemon — DAG executor with tmux management | Superseded | 2026-03-07 |
| [2026-03-07-agent-friendly-architecture-design](2026-03-07-agent-friendly-architecture-design.md) | Agent-friendly architecture: Setter, Spotter, and Lead redesign | Superseded | 2026-03-07 |
| [2026-03-06-cross-repo-integration-design](2026-03-06-cross-repo-integration-design.md) | Goal 6: Cross-repo integration & alignment | Superseded | 2026-03-06 |
| [2026-03-06-task-intake-decomposition-design](2026-03-06-task-intake-decomposition-design.md) | Goal 5: Task intake & decomposition | Superseded | 2026-03-06 |
| [2026-03-06-coordinator-engine-design](2026-03-06-coordinator-engine-design.md) | Goal 4: Coordinator engine (state machine + agentic nodes) | Superseded | 2026-03-06 |
| [2026-03-06-lead-execution-loop-design](2026-03-06-lead-execution-loop-design.md) | Goal 3: Bundled lead execution loop | Superseded | 2026-03-06 |
| [2026-03-06-instance-repo-management-design](2026-03-06-instance-repo-management-design.md) | Goal 2: Crag & repository management (was "instance") | Superseded | 2026-03-06 |
| [2026-03-06-project-scaffolding-design](2026-03-06-project-scaffolding-design.md) | Goal 1: Project scaffolding & core architecture | Superseded | 2026-03-06 |
| [2026-03-06-tui-dashboard-design](2026-03-06-tui-dashboard-design.md) | Goal 7: TUI dashboard (archived — never implemented) | Stale | 2026-03-06 |
