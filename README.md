# belayer

Belayer is being rebuilt on a v6 clean-break branch.

## Current Status

This repository no longer contains the v5 Temporal + YAML pipeline implementation.
`feature/v6` is the base branch for rebuilding Belayer as a session-runtime orchestrator.

What is intentionally preserved right now:

- the CLI entrypoint scaffold
- shared model/event types
- lightweight event logging
- v6-oriented documentation

## Immediate Direction

Belayer v6 is targeting:

- daemon-managed coding sessions
- local runtime state backed by SQLite
- vendor CLI adapters
- tmux / local execution coordination
- a simpler, more legible operator model than v5

## Development

```bash
go build ./...
go test ./...
```

This branch is meant to be the clean foundation for the rest of the v6 epic.
