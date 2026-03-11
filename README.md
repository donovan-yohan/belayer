# belayer

A Go CLI that orchestrates autonomous coding agents across multiple repositories. Give it a work item, and it decomposes it into per-repo climbs, spawns isolated agent sessions in git worktrees, monitors progress, validates cross-repo alignment, and creates PRs.

## How It Works

```
Problem Input (text or Jira ticket)
        |
        v
  Brainstorm + Decompose (interactive Claude session)
        |
        v
  Climbs DAG (per-repo, with dependencies)
        |
  +-----+------+------+
  v     v      v      v
Lead  Lead   Lead   Lead     (parallel, isolated worktrees)
  |     |      |      |
  +-----+------+------+
        |
        v
  Alignment Review (cross-repo spotter)
     |          |
   PASS       FAIL
     |          |
  Create PRs  Re-dispatch with corrections
```

**Three layers:**
- **User** — CLI commands
- **Belayer** — Deterministic Go daemon; polls SQLite, manages DAG execution, enforces concurrency limits
- **Lead** — Ephemeral Claude Code session per climb; runs in an isolated git worktree

## Prerequisites

- **Go 1.24+**
- **tmux** — process management for agent sessions (`brew install tmux`)
- **Claude Code CLI** — the `claude` binary must be on your PATH ([install guide](https://docs.anthropic.com/en/docs/claude-code))
- **SQLite** — bundled via `modernc.org/sqlite` (no C dependency)

## Install

```bash
go install github.com/donovan-yohan/belayer/cmd/belayer@latest
```

Or build from source:

```bash
git clone https://github.com/donovan-yohan/belayer.git
cd belayer
go build -o belayer ./cmd/belayer
```

## Quickstart

### 1. Initialize belayer

```bash
belayer init
```

Creates `~/.belayer/config.json`.

### 2. Create a crag

A crag is a long-lived workspace tied to one or more repos.

```bash
belayer crag create my-project \
  --repo https://github.com/you/frontend.git \
  --repo https://github.com/you/backend.git
```

This bare-clones the repos into `~/.belayer/crags/my-project/repos/`.

### 3. Brainstorm and create a problem

Launch an interactive Claude session that knows your crag's repos and can generate problem specs:

```bash
belayer setter -c my-project
```

Or create a problem directly from a spec and climbs file:

```bash
belayer problem create -c my-project --spec spec.md --climbs climbs.json
```

<details>
<summary>Example climbs.json</summary>

```json
{
  "climbs": [
    {
      "id": "setup",
      "repo": "frontend",
      "description": "Initialize project scaffolding with Vite + React",
      "depends_on": []
    },
    {
      "id": "api-layer",
      "repo": "backend",
      "description": "Create REST API endpoints for user management",
      "depends_on": []
    },
    {
      "id": "integration",
      "repo": "frontend",
      "description": "Connect frontend to backend API",
      "depends_on": ["setup", "api-layer"]
    }
  ]
}
```

</details>

### 4. Start the belayer daemon

The belayer daemon picks up pending problems and runs them:

```bash
belayer belayer start -i my-project --max-leads 4
```

This will:
- Build a DAG from the climbs
- Create isolated git worktrees per climb
- Spawn Claude Code sessions in tmux windows
- Monitor for completion (via mail messages)
- Run a cross-repo spotter review when all climbs finish
- Create PRs on approval, or re-dispatch corrections on rejection

### 5. Monitor progress

```bash
# Quick status
belayer status -c my-project

# View lead session logs
belayer logs -c my-project
```

## CLI Reference

| Command | Description |
|---------|-------------|
| `belayer init` | Initialize global config |
| `belayer crag create` | Create a new crag with repos |
| `belayer crag list` | List all crags |
| `belayer crag delete` | Delete a crag |
| `belayer setter` | Interactive agent session for problem creation |
| `belayer problem create` | Create a problem from spec + climbs files |
| `belayer problem list` | List problems for a crag |
| `belayer belayer start` | Start the daemon that executes problems |
| `belayer status` | Show problem and climb status |
| `belayer message <addr>` | Send a typed mail message to an agent |
| `belayer mail read` | Read unread messages and mark as read |
| `belayer mail inbox` | List unread messages without marking read |
| `belayer mail ack <id>` | Mark a specific message as read |
| `belayer logs` | View and manage lead session logs |

## Architecture

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full code map and data flow.

**Key design decisions:**
- **Stripe-style blueprint** — deterministic Go code handles orchestration; ephemeral Claude sessions handle judgment calls (decomposition, sufficiency checks, alignment reviews)
- **Bare repos + worktrees** — each climb gets an isolated git worktree so agents can work in parallel without conflicts
- **SQLite as single source of truth** — all state (problems, climbs, verdicts, events) lives in one database per crag
- **DAG execution** — climbs declare dependencies; the belayer daemon only spawns a climb when all its dependencies are complete
- **Crash recovery** — the belayer daemon resumes in-progress problems on restart by scanning SQLite and checking for `TOP.json` files
- **Inter-agent mail** — filesystem-backed messaging system (`belayer message` / `belayer mail read`) with typed messages and tmux send-keys delivery

## Development

```bash
# Run all tests
go test ./...

# Build
go build -o belayer ./cmd/belayer
```

## License

MIT
