# belayer

A Go CLI that orchestrates autonomous coding agents across multiple repositories. Give it a work item, and it decomposes it into per-repo goals, spawns isolated agent sessions in git worktrees, monitors progress, validates cross-repo alignment, and creates PRs.

## How It Works

```
Task Input (text or Jira ticket)
        |
        v
  Brainstorm + Decompose (interactive Claude session)
        |
        v
  Goals DAG (per-repo, with dependencies)
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
- **User** — CLI commands and TUI dashboard
- **Setter** — Deterministic Go daemon; polls SQLite, manages DAG execution, enforces concurrency limits
- **Lead** — Ephemeral Claude Code session per goal; runs in an isolated git worktree

## Prerequisites

- **Go 1.24+**
- **tmux** — process management for agent sessions (`brew install tmux`)
- **Claude Code CLI** — the `claude` binary must be on your PATH ([install guide](https://docs.anthropic.com/en/docs/claude-code))
- **SQLite** — bundled via `modernc.org/sqlite` (no C dependency)
- **Dolt** — versioned database engine used by beads (`brew install dolt`)
- **Beads** — git-backed issue tracker for inter-agent mail (`brew install beads`)

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

### 2. Create an instance

An instance is a long-lived workspace tied to one or more repos.

```bash
belayer instance create my-project \
  --repo https://github.com/you/frontend.git \
  --repo https://github.com/you/backend.git
```

This bare-clones the repos into `~/.belayer/instances/my-project/repos/`.

### 3. Brainstorm and create a task

Launch an interactive Claude session that knows your instance's repos and can generate task specs:

```bash
belayer manage -i my-project
```

Or create a task directly from a spec and goals file:

```bash
belayer task create -i my-project --spec spec.md --goals goals.json
```

<details>
<summary>Example goals.json</summary>

```json
{
  "goals": [
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

### 4. Start the setter daemon

The setter picks up pending tasks and runs them:

```bash
belayer setter -i my-project --max-leads 4
```

This will:
- Build a DAG from the goals
- Create isolated git worktrees per goal
- Spawn Claude Code sessions in tmux windows
- Monitor for completion (via mail messages)
- Run a cross-repo spotter review when all goals finish
- Create PRs on approval, or re-dispatch corrections on rejection

### 5. Monitor progress

```bash
# Quick status
belayer status -i my-project

# Interactive TUI dashboard
belayer tui -i my-project

# View lead session logs
belayer logs -i my-project
```

## CLI Reference

| Command | Description |
|---------|-------------|
| `belayer init` | Initialize global config |
| `belayer instance create` | Create a new instance with repos |
| `belayer instance list` | List all instances |
| `belayer instance delete` | Delete an instance |
| `belayer manage` | Interactive agent session for task creation |
| `belayer task create` | Create a task from spec + goals files |
| `belayer task list` | List tasks for an instance |
| `belayer setter` | Start the daemon that executes tasks |
| `belayer status` | Show task and goal status |
| `belayer tui` | Interactive bubbletea dashboard |
| `belayer message <addr>` | Send a typed mail message to an agent |
| `belayer mail read` | Read unread messages and mark as read |
| `belayer mail inbox` | List unread messages without marking read |
| `belayer mail ack <id>` | Mark a specific message as read |
| `belayer logs` | View and manage lead session logs |

## Architecture

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full code map and data flow.

**Key design decisions:**
- **Stripe-style blueprint** — deterministic Go code handles orchestration; ephemeral Claude sessions handle judgment calls (decomposition, sufficiency checks, alignment reviews)
- **Bare repos + worktrees** — each goal gets an isolated git worktree so agents can work in parallel without conflicts
- **SQLite as single source of truth** — all state (tasks, goals, verdicts, events) lives in one database per instance
- **DAG execution** — goals declare dependencies; the setter only spawns a goal when all its dependencies are complete
- **Crash recovery** — the setter resumes in-progress tasks on restart by scanning SQLite and checking for `DONE.json` files
- **Inter-agent mail** — beads-backed messaging system (`belayer message` / `belayer mail read`) replaces signal files with typed messages and tmux send-keys delivery

## Development

```bash
# Run all tests
go test ./...

# Build
go build -o belayer ./cmd/belayer
```

## License

MIT
