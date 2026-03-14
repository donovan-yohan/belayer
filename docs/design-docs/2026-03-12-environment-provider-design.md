# Environment Provider Design

**Date:** 2026-03-12
**Status:** Draft
**Related:** [extend-cli env command design](counterpart in extend-cli repo)

## Problem

Belayer orchestrates autonomous coding agents across multiple repositories. For simple projects, belayer's built-in bare-repo + worktree system is sufficient. But for fullstack projects (like Extend), leads need more than just a git worktree — they need Docker containers, databases, port isolation, and snapshot restore capabilities.

Belayer should not own this infrastructure lifecycle. A separate tool (extend-cli) already knows how to provision these environments. But neither tool should depend on or import the other.

## Design

### Single Provider Model

Rather than maintaining two code paths (builtin vs. external), belayer uses a **single provider architecture**. The daemon always interacts with environments through the same JSON contract — regardless of whether the provider is belayer's own `belayer env` command or an external tool like `extend env`.

This means:
- **One code path in the daemon** — no `if builtin { ... } else { ... }` branching
- **The contract is tested by default usage** — every crag exercises the same JSON parsing and command invocation
- **Swapping providers is purely a config change** — same shape, same parsing, different binary
- **belayer dog-foods its own contract** — the default `belayer env` command conforms to the same interface that external tools implement

### How It Works

The daemon shells out to a configured command for all environment lifecycle operations. The command returns JSON on stdout. The daemon parses it and stores the results.

**Default config** (every crag gets this unless overridden):

```toml
[environment]
command = "belayer"
subcommand = "env"
```

**External provider config** (e.g., for an Extend fullstack crag):

```toml
[environment]
command = "extend"
subcommand = "env"
snapshot = "acme"        # optional: default snapshot for create/reset
```

### `belayer env` — The Default Provider

A new `belayer env` subcommand that implements the JSON contract using belayer's existing bare-repo + worktree machinery. This is what every crag uses by default.

**Behavior:**
- `belayer env create` — no-op for infrastructure (no Docker), returns env metadata
- `belayer env add-worktree` — calls `repo.WorktreeAdd()`, returns the computed path (`<cragDir>/tasks/<envName>/<repoName>-<branchSlug>/`)
- `belayer env remove-worktree` — calls `repo.WorktreeRemove()`
- `belayer env destroy` — calls `crag.CleanupProblemWorktrees()`
- `belayer env reset` — no-op (no DB to reset), returns success
- `belayer env status` — returns worktree git status (dirty/clean), empty services
- `belayer env logs` — returns empty result (no services to log)
- `belayer env list` — lists active environments from the environments table

The `--json` flag produces structured output. Without it, human-readable output for manual use.

### Daemon Integration

**Today's flow:**

```
problem → running → crag.CreateWorktree() → hardcoded path → spawn lead
```

**New flow:**

```
problem → running → shell out to `<command> <subcommand> create` → store env info in SQLite
  per climb → shell out to `<command> <subcommand> add-worktree` → store worktree path → spawn lead
  spotter failure → shell out to `<command> <subcommand> reset` → retry
  complete/failed → shell out to `<command> <subcommand> destroy`
```

Key principle: **worktree paths become data, not convention.** Belayer stops computing `<cragDir>/tasks/<problemID>/<repoName>/` and starts storing the path returned by the provider.

### SQLite Changes

**New `environments` table:**

| Column | Type | Purpose |
|--------|------|---------|
| `problem_id` | TEXT FK | Links to problem |
| `provider_command` | TEXT | Command that was used (e.g., "belayer env" or "extend env") |
| `env_name` | TEXT | Name passed to provider |
| `env_json` | TEXT | Full JSON response from create |
| `created_at` | TIMESTAMP | |

**New column on leads:** `worktree_path` — stores the actual path returned by the provider instead of deriving it.

### Config Changes

**`belayer.toml`** gets an `[environment]` section. Every crag has a default; crags can override:

```toml
[environment]
command = "belayer"       # default
subcommand = "env"        # default
snapshot = ""             # optional: default snapshot name
```

No changes to `crag.json` — the provider is purely a `belayer.toml` config concern, following the existing config resolution chain (crag > global > embedded defaults).

---

## JSON Contract

The contract that ALL providers must implement — including `belayer env`. All communication is via CLI flags (input) and JSON on stdout (output). Exit code 0 = success, non-zero = error.

### `create`

Creates a named environment. For simple providers (like `belayer env`), this may be a lightweight operation (no Docker). For fullstack providers (like `extend env`), this provisions containers, DB, and ports.

```bash
<command> <subcommand> create --name <name> [--snapshot <snapshot>] --json
```

Response:

```jsonc
{
  "status": "ok",
  "name": "prob-123",
  "index": 3,
  "env": {
    "DATABASE_URL": "postgres://localhost:5732/extend_api",
    "REDIS_URL": "redis://localhost:6679",
    "SPICEDB_ENDPOINT": "localhost:50351"
  },
  "services": {
    "database": { "status": "running", "port": 5732 },
    "redis": { "status": "running", "port": 6679 },
    "spicedb": { "status": "running", "port": 50351 }
  }
}
```

For `belayer env`, `env` and `services` are empty objects (no infrastructure to report).

Single-branch shortcut (creates env + one worktree per project):

```bash
<command> <subcommand> create --name <name> --single-branch <branch> --projects api,app [--snapshot <snapshot>] --json
```

Additional fields in response:

```jsonc
{
  "worktrees": [
    { "repo": "extend-api", "branch": "feat/my-feature", "path": "/path/to/worktree", "env_file": "/path/to/.extend-worktree.env" },
    { "repo": "extend-app", "branch": "feat/my-feature", "path": "/path/to/worktree", "env_file": "/path/to/.extend-worktree.env" }
  ]
}
```

### `add-worktree`

Adds a git worktree to an existing environment. The worktree's env file points at the env's shared containers (if applicable).

```bash
<command> <subcommand> add-worktree --name <name> --repo <repo> --branch <branch> [--base-ref <ref>] --json
```

`--base-ref` specifies which branch the worktree should be created from (e.g., `main`, `develop`). Defaults to the repo's default branch if omitted.

Response:

```jsonc
{
  "status": "ok",
  "repo": "extend-app",
  "branch": "belayer/prob-123/app-climb-2",
  "path": "/path/to/worktree",
  "env_file": "/path/to/.extend-worktree.env"
}
```

For `belayer env`, `env_file` is empty (no env file generated).

### `remove-worktree`

Removes a worktree from an environment. Infrastructure stays running.

```bash
<command> <subcommand> remove-worktree --name <name> --repo <repo> --branch <branch> --json
```

Response:

```jsonc
{
  "status": "ok"
}
```

### `reset`

Restores a snapshot into the environment's DB. Worktrees are untouched. For `belayer env`, this is a no-op that returns success.

```bash
<command> <subcommand> reset --name <name> [--snapshot <snapshot>] --json
```

Response:

```jsonc
{
  "status": "ok",
  "duration_ms": 18500,
  "snapshot": "acme"
}
```

### `destroy`

Tears down all containers (if any) and removes all worktrees for the environment.

```bash
<command> <subcommand> destroy --name <name> --json
```

Response:

```jsonc
{
  "status": "ok"
}
```

### `status`

Reports health of an environment.

```bash
<command> <subcommand> status --name <name> --json
```

Response:

```jsonc
{
  "status": "ok",
  "name": "prob-123",
  "index": 3,
  "services": {
    "database": { "status": "running", "port": 5732, "uptime": "2h34m" },
    "redis": { "status": "running", "port": 6679, "uptime": "2h34m" },
    "spicedb": { "status": "unhealthy", "port": 50351, "uptime": "2h34m" }
  },
  "snapshot": { "name": "acme", "restored_at": "2026-03-12T...", "stale": false },
  "worktrees": [
    { "repo": "extend-api", "branch": "belayer/prob-123/api", "path": "/path/...", "dirty": false },
    { "repo": "extend-app", "branch": "belayer/prob-123/app-1", "path": "/path/...", "dirty": true }
  ]
}
```

For `belayer env`, `services` and `snapshot` are empty/null.

### `logs`

Returns recent log output for a service. For `belayer env`, returns empty lines array.

```bash
<command> <subcommand> logs --name <name> [--service <service>] --json
```

Response:

```jsonc
{
  "status": "ok",
  "service": "database",
  "lines": [
    { "timestamp": "2026-03-12T...", "message": "LOG: checkpoint complete" },
    { "timestamp": "2026-03-12T...", "message": "LOG: autovacuum launcher started" }
  ]
}
```

### `list`

Lists all environments.

```bash
<command> <subcommand> list --json
```

Response:

```jsonc
{
  "status": "ok",
  "environments": [
    { "name": "prob-123", "index": 3, "worktree_count": 3, "created_at": "2026-03-12T..." },
    { "name": "my-feature", "index": 1, "worktree_count": 2, "created_at": "2026-03-11T..." }
  ]
}
```

### Error Shape (all commands)

Exit code non-zero. Stdout:

```jsonc
{
  "status": "error",
  "error": "env 'prob-123' not found",
  "code": "ENV_NOT_FOUND"
}
```

---

## Ownership Boundaries

| Concern | belayer daemon owns | provider command owns |
|---------|--------------------|-----------------------|
| Problems, climbs, DAG | Yes | No |
| Branch naming | Yes (decides names) | No (takes what it's told) |
| When to create/destroy | Yes | No |
| Docker, DB, snapshots | No | Yes (if applicable) |
| Port allocation | No | Yes (if applicable) |
| Git worktree mechanics | No | Yes |
| Worktree paths | Stores them | Returns them |
| Lead spawning, tmux | Yes | No |

## Key Decisions

- **Single provider model** — no builtin vs. external split. The daemon always shells out to a configured command. `belayer env` is the default provider, conforming to the same contract as any external tool.
- **Dog-food the contract** — `belayer env` implements the JSON contract using existing bare-repo + worktree logic. This ensures the contract is exercised by default and any external provider is a drop-in replacement.
- **Worktree paths as data** — belayer stores paths returned by the provider rather than computing them from convention.
- **Per-problem environments** — leads within a problem share Docker/DB. Simultaneous problems get separate environments.
- **Config-only provider swap** — switching from `belayer env` to `extend env` is a `belayer.toml` change. No code changes, no crag recreation.
- **Snapshots are a provider concern** — belayer passes snapshot names as opaque strings. Snapshot creation, listing, and management are handled entirely by the provider (e.g., extend-cli's `extend db snapshot` commands).
