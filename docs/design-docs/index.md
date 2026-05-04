# Design Documents

These files are design history. The current reference docs live one directory
up; start with `docs/README.md` before copying command examples or runtime
claims from a design note.

## Current Or Recently Implemented Designs

- [Belayer Hermes profiles spec](2026-05-03-belayer-hermes-profiles-spec.md) — Implemented (Phases 1–6, issues #133–#137, #142, #144). Base `blyr` profile + per-talent forks (`blyr-<crag>-<instance>/`). Runtime reference: `belayer auth ensure`, `belayer doctor`, `belayer prune`, `belayer uninstall`.
- [Org Mode Nightshift plan](2026-04-30-org-mode-nightshift-plan.md) — Stack plan for crag-mode rollout: filesystem contract, CLI, proof examples. Shipped via PRs #117–#129. Prefer `docs/CRAG_MODE.md` and `docs/CRAG_FILESYSTEM.md` for current behavior.
- [Talent lifecycle contract plan](2026-05-01-talent-lifecycle-contract-plan.md) — Defines `belayer-talent/v1` contract: `role`/`domain`, `activation`, `runtime.lifecycle`, typed `contract`, gate bindings, and lifecycle evidence in evaluations. Docs+schema step shipped; daemon wake-on-mail follow-up tracked separately.
- [Observability log tiers and API design](2026-04-19-observability-log-tiers-and-api-design.md) — Tiered logging, consolidated CLI+HTTP API, SSE with digests, per-agent spill files, Nightshift-friendly archives.
- [Template-team exit conditions, prompt discipline, and CI lint](2026-04-20-template-team-exit-conditions-and-prompt-discipline.md) — Implemented prompt/tool/doc-drift cleanup. This file already carries implementation deviations; check shipped prompts and current docs before copying examples.
- [Belayer-in-clamshell](2026-04-17-belayer-in-clamshell-design.md) — Current direction for container-per-climb sandboxing, but not the organization-mode execution adapter boundary.
- [Embed hermes_bridge + deployment docs](2026-04-17-embed-hermes-bridge-design.md) — Historical design behind embedded bridge distribution and deployment docs.

## Historical Or Superseded Designs

- [Belayer run model for Nightshift v1](2026-04-15-belayer-run-model-for-nightshift-v1.md) — Historical framing for session bus, Hermes harness driver, and transport adapter. Prefer `docs/AGENT_ARCHITECTURE.md` and `docs/PHILOSOPHY.md` for current wording.
- [Nightshift v1 deployment topology](2026-04-15-nightshift-v1-deployment-topology.md) — Historical deployment framing. Prefer `docs/DEPLOYMENT.md`.
- [Hermes bridge sidecar](2026-04-15-hermes-bridge-sidecar.md) — Historical bridge subprocess design. Prefer `docs/AGENT_ARCHITECTURE.md` plus the current Hermes plugin implementation.
- [Product manager agent](2026-04-16-product-manager-agent.md) — Historical PM-gate proposal. PM completion gates have shipped; prefer `docs/AGENT_ARCHITECTURE.md`.
- [Tool catalog and identity](2026-04-16-tool-catalog-and-identity.md) — Historical role-based tool gating proposal. Prefer `agents/README.md` and current `agent.yaml#belayer_tools` examples.
- [Sandbox, runtime, and crag proof](2026-04-16-sandbox-runtime-and-crag-proof.md) — Historical proof plan. Prefer `docs/DEPLOYMENT.md` for current trust/sandbox boundaries.
- ~~[VM sandbox and template bootstrap](2026-04-16-vm-sandbox-and-template-bootstrap.md)~~ — Superseded by later sandbox/runtime work.
- [Belayer-in-clamshell review](2026-04-17-belayer-in-clamshell-review.md) — Historical review notes for the clamshell direction.
- [Tool catalog and identity implementation plan](2026-04-16-tool-catalog-and-identity-plan.md) — Historical execution checklist; do not treat as current prompt guidance.

## Forward-Looking, Not Implemented

- [Crag daemon](2026-04-15-crag-daemon.md) — Forward-looking always-on worker control plane.
- [Git-backed agent identity](2026-04-15-git-backed-agent-identity.md) — Forward-looking identity/versioning direction.
