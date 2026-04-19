# Design Documents

- [Belayer run model for Nightshift v1](2026-04-15-belayer-run-model-for-nightshift-v1.md) — Three-layer run model: session bus, Hermes harness driver, transport adapter
- [Nightshift v1 deployment topology](2026-04-15-nightshift-v1-deployment-topology.md) — One request per worker, two control planes, Belayer as intra-run session bus
- [Hermes bridge sidecar](2026-04-15-hermes-bridge-sidecar.md) — Bridge subprocess design: how Hermes agents are spawned and coordinated
- [Crag daemon](2026-04-15-crag-daemon.md) — Forward-looking: always-on worker control plane (outer layer)
- [Git-backed agent identity](2026-04-15-git-backed-agent-identity.md) — Forward-looking: soul + capabilities materialization
- [Product manager agent](2026-04-16-product-manager-agent.md) — PM completion gate: adversarial spec-vs-reality verification
- [Tool catalog and identity](2026-04-16-tool-catalog-and-identity.md) — Co-locate belayer tools with agent templates, role-based tool gating
- ~~[VM sandbox and template bootstrap](2026-04-16-vm-sandbox-and-template-bootstrap.md)~~ — Superseded by sandbox-runtime-and-crag-proof
- [Sandbox, runtime, and crag proof](2026-04-16-sandbox-runtime-and-crag-proof.md) — SandboxDriver + RuntimeProvider interfaces, lightweight crag, arielcharts E2E proof
- [Embed hermes_bridge + deployment docs](2026-04-17-embed-hermes-bridge-design.md) — Ship bridge embedded in binary, extract via belayer init; rewrite SANDBOXING.md with prod/dev topologies and known security gaps
- ~~[Clamshell apikey provider type](2026-04-17-clamshell-apikey-provider-design.md)~~ — Superseded by belayer-in-clamshell-design (upstream apikey spec still current, belayer integration changed)
- [Belayer-in-clamshell](2026-04-17-belayer-in-clamshell-design.md) — One-container-per-run model; belayer daemon + bridges run inside a single clamshell sandbox; apikey provider consumed at boot; arielcharts + `pnpm run dev` as E2E proof
