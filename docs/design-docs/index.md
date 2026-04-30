# Design Documents

These files are design history. The current reference docs live one directory
up; start with `docs/README.md` before copying command examples or runtime
claims from a design note.

## Current Or Recently Implemented Designs

- [Observability log tiers and API design](2026-04-19-observability-log-tiers-and-api-design.md) — Tiered logging, consolidated CLI+HTTP API, SSE with digests, per-agent spill files, Nightshift-friendly archives.
- [Template-team exit conditions, prompt discipline, and CI lint](2026-04-20-template-team-exit-conditions-and-prompt-discipline.md) — Implemented prompt/tool/doc-drift cleanup. This file already carries implementation deviations; check shipped prompts and current docs before copying examples.
- [Belayer-in-clamshell](2026-04-17-belayer-in-clamshell-design.md) — Current direction for container-per-run sandboxing, but not the organization-mode execution adapter boundary.
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
