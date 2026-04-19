# hermes_bridge

Per-agent bridge subprocess for Belayer. Spawned by the Go daemon via
`python -m hermes_bridge`. Wraps a Hermes `AIAgent` and pipes its activity
back to the daemon over a Unix socket (or HTTP CONNECT proxy inside a
clamshell sandbox).

## Runtime dependencies

The bridge itself is stdlib-only (`http.client`, `json`, `threading`, ...).
Hermes-specific deps (`openai`, `httpx`, ...) come from the parent Hermes
venv at `~/.hermes/hermes-agent/venv/`. The daemon's spawn logic resolves
that path and points `PYTHONPATH` at the hermes-agent source tree (see
`internal/bridge/bridge.go`).

This package declares `dependencies = []` so it can be installed into any
venv for development or test without pulling down the full LLM stack.

## Development

From the repo root:

```bash
uv sync              # creates .venv/ with pytest
uv run pytest        # runs hermes_bridge/tests/
```

Or use the Makefile: `make test-python`.
