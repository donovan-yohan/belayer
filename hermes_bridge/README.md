# hermes_bridge

Per-agent bridge subprocess for Belayer. Spawned by the Go daemon via
`python -m hermes_bridge`. Wraps a Hermes `AIAgent` and pipes its activity
back to the daemon over a Unix socket (or HTTP CONNECT proxy inside a
sandboxed deployment).

## Runtime dependencies

The bridge itself is stdlib-only (`http.client`, `json`, `threading`, ...).
Hermes-specific deps (`openai`, `httpx`, ...) come from the parent Hermes
venv at `~/.hermes/hermes-agent/venv/`. The daemon's spawn logic resolves
that path and points `PYTHONPATH` at the hermes-agent source tree (see
`internal/bridge/bridge.go`).

**Hermes 0.11+** is required: the `belayer_*` tool surface is registered by
the Belayer Hermes plugin (`plugins/belayer/`) at plugin discovery time,
which depends on the plugin manager added in 0.11. The bridge no longer
calls a `register_belayer_tools()` shim — when `from run_agent import AIAgent`
runs, the plugin loader populates `agent.tools` for us. The bridge then uses
the plugin's `pop_turn_mail_ids()` helper to drain `check_mail` ack ids
between turns.

This package declares `dependencies = []` so it can be installed into any
venv for development or test without pulling down the full LLM stack.

## Development

From the repo root:

```bash
uv sync              # creates .venv/ with pytest
uv run pytest        # runs hermes_bridge/tests/
```

Or use the Makefile: `make test-python`.
