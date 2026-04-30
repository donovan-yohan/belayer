# Belayer Docs

This directory separates current operator/reference docs from historical design
notes. When docs disagree, prefer the current references below and treat
`docs/design-docs/` as decision history unless its index marks a design as
current.

## Current References

- [Agent Architecture](AGENT_ARCHITECTURE.md) - agent kinds, tools, mail, spawn, and PM completion gate
- [Organization Mode](ORG_MODE.md) - talent catalogs, gate contracts, org events, and proof-run framing
- [Organization Filesystem](ORG_FILESYSTEM.md) - repo, user catalog, and user org directory contracts
- [Artifact Schemas](ARTIFACT_SCHEMAS.md) - content contracts for durable artifacts
- [Log Format](LOG_FORMAT.md) - external event, archive, search, and SSE contract
- [Observability](OBSERVABILITY.md) - operator recipes for logs, events, traces, and archives
- [Deployment](DEPLOYMENT.md) - runtime topologies, trust model, sockets, credentials, and Landlock
- [Philosophy](PHILOSOPHY.md) - portable runtime interfaces and identity/runtime separation

## Historical Design Notes

`docs/design-docs/` records how Belayer got here. These files are valuable for
context, but many contain old command shapes, older sandbox assumptions, or
forward-looking proposals that have not shipped. Check
[docs/design-docs/index.md](design-docs/index.md) before copying examples from
an older design note into prompts, docs, or tests.
