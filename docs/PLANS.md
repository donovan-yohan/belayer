# Belayer Execution Plans

## Active Plans

| Plan | Goal | Created |
|------|------|---------|
| [2026-04-17-belayer-in-clamshell](exec-plans/active/2026-04-17-belayer-in-clamshell.md) | One-container-per-run topology: belayer daemon + bridges inside a single clamshell sandbox, apikey provider projects OPENCODE_GO_API_KEY as clak_ handle, arielcharts + `pnpm run dev` as E2E proof | 2026-04-17 |
| [2026-04-19-observability-log-tiers-and-api](exec-plans/active/2026-04-19-observability-log-tiers-and-api.md) | Tiered logging (standard/verbose/trace), consolidated `belayer logs` CLI, aggregate+compact HTTP API, SSE digests, per-agent bridge stdout + trace spill, SANDBOXING→DEPLOYMENT doc swap | 2026-04-19 |

## Superseded / Completed Plans

| Plan | Status | Notes |
|------|--------|-------|
| [2026-04-17-clamshell-e2e-proof](exec-plans/active/2026-04-17-clamshell-e2e-proof.md) | Superseded 2026-04-17 | Per-session clamshell model replaced by belayer-in-clamshell (one container per run). Reusable setup steps carried forward. |
