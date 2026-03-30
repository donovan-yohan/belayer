# belayer

> Carabiners and ropes for your agentic harness.

Belayer is a pipeline orchestrator for autonomous coding agents. Define your workflow in YAML, install a framework, and belayer handles the sequencing, retries, gate scoring, and completion signaling via Temporal.

**Agent-agnostic.** Your nodes are black boxes. Claude Code, Codex, a shell script, a Python agent — anything that reads `node-context.json` and writes a completion file.

**You own your orchestration.** Pipeline config and scripts live in your repo's `.belayer/` directory, not in belayer. `belayer setup --framework` scaffolds the starter files; you customize freely.

## Quickstart

```bash
# Install
go install github.com/donovan-yohan/belayer/cmd/belayer@latest

# Set up a framework in your project
cd your-project
belayer setup --framework claude-tmux

# Start Temporal (in another terminal)
temporal server start-dev

# Run a pipeline
belayer climb "add user authentication"
```

## How It Works

```
belayer climb "add auth"
    │
    ├── Resolves .belayer/pipeline.yaml
    ├── Creates git worktree
    ├── Starts Temporal workflow
    │
    ├── For each node:
    │   ├── Writes .belayer/.internal/input/node-context.json
    │   ├── Execs the node's command (from pipeline YAML)
    │   ├── Polls for .belayer/.internal/completion/<id>.json
    │   └── Routes to next node (on_pass / on_retry / on_fail)
    │
    └── Pipeline complete → branch with commits ready for PR
```

## Pipeline YAML

Pipelines define nodes (constructive steps) and gates (adversarial quality checks):

```yaml
name: my-pipeline
nodes:
  - name: implement
    type: node
    command: .belayer/scripts/run.sh
    description: |
      Implement the feature. Write tests. Commit when done.
    input: { type: description }
    output: { type: commit }
    on_pass: review
    on_retry: self
    max_retries: 3

  - name: review
    type: gate
    command: .belayer/scripts/run.sh
    description: |
      Review the code for spec compliance and test coverage.
    input: { type: commit }
    dimensions:
      - { name: spec_compliance, weight: 0.35, description: "Changes match spec?" }
      - { name: test_coverage, weight: 0.3, description: "Tests are meaningful?" }
      - { name: correctness, weight: 0.35, description: "Works in production?" }
    thresholds: { pass: 7.0, retry: 4.0 }
    output: { type: gate_result }
    on_pass: stop
    on_retry: implement
    max_retries: 2
```

**Score-then-route:** Gate nodes produce structured scores per dimension. Belayer computes the weighted average and applies thresholds — the gate session never decides its own outcome. Anti-gaming by design.

## Three Phases

```
EXPLORE: intake sources → spec.md
            │
            v
CLIMB:   spec.md → plan → implement → review(gate) → pr-author → PR
            │
            v
SUMMIT:  PR manifest → auto-merge → monitoring (not yet implemented)
```

**Multi-repo is additive.** The per-repo pipeline runs identically whether you have one repo or ten. Multi-repo adds two coordination layers:
- **Setter** — decomposes spec.md into per-repo specs (fan-out)
- **Spotter** — validates cross-repo alignment after all repos complete (fan-in)

Neither changes how work happens inside a repo.

## Frameworks

A framework is a directory with `pipeline.yaml` + scripts. Belayer ships two built-in frameworks:

```bash
belayer setup --framework claude-tmux        # interactive tmux sessions
belayer setup --framework gstack             # headless: claude implements, codex reviews
belayer setup --framework ./my-framework     # custom
```

After setup, `.belayer/` contains:
- `pipeline.yaml` — committed orchestration config
- `scripts/run.sh` — committed node runner (your customization point)
- `.internal/` — gitignored runtime state (completion files, node context, gate outputs)

Create your own framework by making a directory with at least `pipeline.yaml`.

## Node Protocol

Every node command receives context through:

1. **Environment variables:** `BELAYER_TASK_ID`, `BELAYER_NODE`, `BELAYER_ATTEMPT`, `BELAYER_WORK_DIR`
2. **Context file:** `.belayer/.internal/input/node-context.json` with full task details

And signals completion by writing:
```
.belayer/.internal/completion/<task-id>-<node>-attempt-<N>.json
```

The `claude-tmux` framework handles this via a Claude Code Stop hook that calls `belayer node-complete`. Custom frameworks can write the file directly.

## Why Belayer

| belayer | alternatives |
|---------|-------------|
| **Agent-agnostic** — use any agent that fulfills the contract | Model-locked to one provider |
| **Pipeline-as-YAML** — fully customizable node sequences | Hardcoded workflows |
| **You own your nodes** — bring your own implementations | Platform owns your agents |
| **Multi-repo as additive layer** — same per-repo pipeline | Multi-repo as an agent feature |
| **Score-then-route gates** — deterministic quality scoring | Trust the agent's self-assessment |

## CLI

| Command | Description |
|---------|-------------|
| `belayer setup --framework <name>` | Scaffold a framework into the current repo |
| `belayer climb [description]` | Start a pipeline run |
| `belayer worker` | Start the Temporal worker daemon |
| `belayer status` | Check pipeline progress |
| `belayer node-complete` | Record node completion (called by framework hooks) |

## Prerequisites

- **Go 1.24+**
- **Temporal** — `temporal server start-dev` for local development
- **Framework dependencies** — varies by framework (claude-tmux needs `tmux`, `claude`, `jq`)

## Development

```bash
go build -o belayer ./cmd/belayer
go test ./...
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the code map and [docs/DESIGN.md](docs/DESIGN.md) for design patterns.

## Companion Tools

- **[carabiner](https://github.com/donovan-yohan/carabiner)** — Agent-agnostic harness for quality patterns and domain knowledge
- **gstack** — vendored in `.claude/skills/gstack/`, provides /review, /ship, /office-hours, and 25+ other skills

## License

MIT
