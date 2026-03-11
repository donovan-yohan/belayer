# Setter Daemon Management

> **Status**: Approved | **Created**: 2026-03-10

## Problem

The setter currently requires an active terminal (`belayer setter`). Users must keep a terminal open or manually manage backgrounding. There's no built-in way to start/stop/check the setter as a background daemon.

## Design

### CLI Structure

`belayer setter` becomes a parent command with three subcommands:

```
belayer setter start [flags]    # start the daemon (backgrounds by default)
belayer setter stop [flags]     # stop a running daemon
belayer setter status [flags]   # check if running
```

All three accept `--instance / -i` to target a specific instance.

Breaking change: bare `belayer setter` no longer runs inline. Use `belayer setter start --foreground` for the old behavior.

### Start

`belayer setter start`:

1. Resolve instance, check for existing PID file at `<instanceDir>/setter.pid`
2. If PID file exists and process is alive: error "setter already running (PID N)"
3. If PID file exists but process is dead: remove stale PID file, continue
4. Re-exec `belayer setter start --foreground` with stdout/stderr redirected to `<instanceDir>/logs/setter.log`
5. Write child PID to `<instanceDir>/setter.pid`
6. Print "Setter started (PID N), logs: <path>"

`belayer setter start --foreground`:

- Runs in the terminal with signal handling (current behavior)
- On startup, writes own PID to `setter.pid`
- On exit (graceful or crash), removes `setter.pid`

Flags inherited from current `belayer setter`: `--max-leads`, `--poll-interval`, `--stale-timeout`.

### Stop

`belayer setter stop`:

1. Read `<instanceDir>/setter.pid`
2. If no PID file: "no setter running"
3. If PID file exists but process dead: clean up stale PID, "no setter running"
4. Send SIGTERM to PID
5. Wait up to 10s for process to exit (poll with `kill -0`)
6. If still alive after 10s, SIGKILL
7. Remove PID file
8. Print "Setter stopped"

### Status

`belayer setter status`:

1. Read PID file
2. Check if process alive (`kill -0`)
3. Print "Setter running (PID N)" or "Setter not running"

### PID File Lifecycle

- Written by the foreground process itself (not the parent that re-execs) to avoid race conditions
- Removed in a `defer` in the foreground runner + in the signal handler cleanup path
- Stale PID detection: `syscall.Kill(pid, 0)` — if ESRCH, it's stale

### Files Changed

| File | Change |
|------|--------|
| `internal/cli/setter.go` | Rewrite: parent cmd + start/stop/status subcommands |
| `<instanceDir>/setter.pid` | PID file, created on start, removed on stop/exit |
| `<instanceDir>/logs/setter.log` | Daemon stdout/stderr output |

### Daemonization Approach

Self-re-exec pattern: `belayer setter start` calls `os.StartProcess` to re-launch itself with `--foreground`, redirecting stdout/stderr to the log file. No external dependencies (launchd, systemd, etc.).
