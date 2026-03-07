# Design: Instance & Repository Management (Goal 2)

**Date**: 2026-03-06
**Goal**: Implement instance lifecycle and git repository management (bare clones + worktrees)

## Overview

Belayer instances are long-lived workspaces that persist repos and configuration. Each instance contains bare git clones and creates per-task worktrees for isolation. This goal implements the full instance lifecycle and repository management layer.

## Directory Structure

```
~/.belayer/
  config.json                         # Global config (already exists from Goal 1)
  instances/
    <name>/
      instance.json                   # Instance-specific config
      belayer.db                      # SQLite database
      repos/
        <repo-name>.git/              # Bare clones
      tasks/
        <task-id>/
          <repo-name>/               # Git worktrees
```

## Design Decisions

### 1. Instance location: `~/.belayer/instances/<name>/`
Instances live inside the global belayer directory. The instance path is deterministic from the name, stored in the global config registry (`config.json`'s `instances` map).

### 2. Bare repos for shared object storage
Following the extend-cli pattern: `git clone --bare <url> repos/<name>.git`. Worktrees reference the bare repo's objects, making creation fast and storage-efficient.

### 3. Repo name extraction
Repo name derived from the URL: strip `.git` suffix, take the last path segment. E.g., `https://github.com/org/my-repo.git` -> `my-repo`.

### 4. Instance config (`instance.json`)
```json
{
  "name": "my-instance",
  "repos": [
    {
      "name": "my-repo",
      "url": "https://github.com/org/my-repo.git",
      "bare_path": "repos/my-repo.git"
    }
  ],
  "created_at": "2026-03-06T12:00:00Z"
}
```
Stores the repo registry for the instance. Paths are relative to the instance directory.

### 5. Worktree management
- Create: `git worktree add <path> -b <branch>` from the bare repo
- The branch name follows: `belayer/<task-id>/<repo-name>`
- Worktree path: `tasks/<task-id>/<repo-name>/`
- Cleanup: `git worktree remove <path>` + prune

### 6. Package structure
- `internal/instance/` — instance lifecycle (create, load, delete, list)
- `internal/repo/` — git operations (bare clone, worktree add/remove)

### 7. CLI commands
- `belayer init` — already creates `~/.belayer/`, no changes needed
- `belayer instance create <name> --repos <url1,url2>` — creates instance, clones repos
- `belayer instance list` — lists all instances
- `belayer instance delete <name>` — removes instance directory and config entry

### 8. Database integration
On instance creation, also initialize the SQLite database (`belayer.db`) with migrations. The instance record is inserted into the `instances` table.

## Error Handling

- Repo clone failure: clean up partial instance directory, return error
- Instance name conflict: return error (names must be unique)
- Worktree creation failure: return error with context (branch conflicts, etc.)

## Testing Strategy

- Instance creation/loading with temp directories (no real git clones in unit tests)
- Repo name extraction from various URL formats
- Worktree lifecycle (requires a real git repo in temp dir for integration tests)
- CLI integration via cobra command execution
