---
status: implemented
created: 2026-03-26
branch: master
supersedes:
implemented-by:
consulted-learnings: [L-20260321-score-then-route, L-20260320-rendezvous-attempt-scope, L-20260321-workflow-no-file-io]
---

# ExecSpawner + Framework Scaffolding

Decouple node spawning from belayer core. Make `TmuxSpawner` + Claude Code-specific hooks an installable framework rather than a hardcoded implementation. Introduce `belayer setup --framework` to scaffold orchestration config into target repos.

## Problem

Belayer's `DESIGN.md` declares "Belayer is plumbing" and "Agent-agnostic — Nodes are black boxes," but the implementation couples the pipeline engine to tmux + Claude Code:

- `session.TmuxSpawner` is the only `Spawner` implementation
- `session.WriteHooksConfig()` writes Claude Code-specific hooks JSON
- `buildClaudeCommand()` constructs a `claude --dangerously-skip-permissions` invocation
- Anyone wanting a different runtime (Codex, `claude -p`, shell scripts, Python agents) must fork the session package

The `Spawner` interface already exists as the right boundary — it just has only one implementation.

## Design

### Principle: Orchestration is owned by the environment being orchestrated

Pipeline config, node scripts, and framework files live in the target repo's `.belayer/` directory, not in belayer core. Single-repo and multi-repo both follow this pattern. Belayer provides the engine; the repo provides the orchestration definition.

### 1. ExecSpawner (core)

New `Spawner` implementation in `internal/v3/session/`. Replaces `TmuxSpawner` as the core spawner.

**`SpawnOpts` changes:** Add a `Command` field to `SpawnOpts`. `NodeActivity` populates it from `NodeConfig.Command` before calling `Spawn()`. Remove the `HooksPath` field — hook configuration is now the framework's responsibility, not core's.

```go
type SpawnOpts struct {
    NodeName    string
    TaskID      string
    Attempt     int
    WorkDir     string
    Description string
    Command     string  // NEW: shell command to exec (from pipeline YAML)
    InputPrompt string
}

type ExecSpawner struct{}

func (e *ExecSpawner) Spawn(ctx context.Context, opts SpawnOpts) error {
    // 1. Validate command exists (exec.LookPath)
    // 2. Set BELAYER_* env vars
    // 3. exec.Command(opts.Command).Start() — fire and forget
    // 4. Return immediately (polling handles the rest)
}
```

Behavior:
- Reads command from `opts.Command` (populated by `NodeActivity` from node config's `command:` field)
- Sets environment variables: `BELAYER_TASK_ID`, `BELAYER_NODE`, `BELAYER_ATTEMPT`, `BELAYER_WORK_DIR` (new — not in current `buildEnvExports`)
- Starts the command via `exec.Command("sh", "-c", opts.Command).Start()` — shell expansion is intended so commands can include arguments and pipes. A goroutine calls `cmd.Wait()` to reap the process and avoid zombies. If the process exits non-zero before a completion file appears, the goroutine writes a failure sentinel to a channel that `pollForCompletion` can check, enabling fast-fail instead of waiting for the 2h timeout.
- `NodeActivity` polls for the completion file as it does today

**`NodeActivity` changes:** Remove the `session.WriteHooksConfig()` call — hook configuration is now the framework script's job. Remove `HooksPath` from `SpawnOpts`. Instead, `NodeActivity` writes `node-context.json` (see Section 3) before calling `Spawn()`.

Error handling:
- Command not found: `exec.LookPath` check before spawn, return error
- Command crashes without writing completion: the `Wait()` goroutine detects non-zero exit and signals via a channel. `pollForCompletion` checks both the completion file and the exit channel on each tick — fast-fail on process death instead of waiting 2h
- Disk/permission errors on `node-context.json` write: return activity error, Temporal retries
- Zombie prevention: `cmd.Wait()` in goroutine reaps every spawned process

### 2. Pipeline YAML `command:` field

Every node in the pipeline YAML must specify a `command:` field — the shell command ExecSpawner will exec.

```yaml
nodes:
  - name: implement
    type: node
    command: .belayer/scripts/run-node.sh
    description: |
      Implement the spec. Write tests. Commit when done.
    input: { type: description }
    output: { type: commit }
    on_pass: review
    on_retry: self
    max_retries: 3
```

Validation: a node without `command:` is a validation error. Explicit over magic defaults.

**`DefaultPipelineYAML` migration:** The existing `DefaultPipelineYAML` in `defaults.go` has no `command:` fields and uses legacy node names (setter/lead/spotter/summit). Rather than patching it, retire it — after this refactor, pipelines come from frameworks via `belayer setup`. The default pipeline constant becomes a fallback for backward compat only and will be removed alongside the P1 code restructuring. Integration tests that use `defaultInput()` with the default pipeline will switch to an explicit test pipeline YAML with `command:` fields pointing to the test's fake command.

### 3. Node Protocol (`node-context.json`)

Written by `NodeActivity` before calling `Spawner.Spawn()` — ownership is unambiguous: core writes the context, the framework reads it. Unconditionally overwritten on each attempt (idempotent — no stale-read risk on retry). This is the typed contract between belayer core and any framework implementation.

```json
{
  "task_id": "climb-1711234567890",
  "node_name": "implement",
  "node_type": "node",
  "attempt": 0,
  "work_dir": "/path/to/worktree",
  "description": "Implement the spec. Write tests. Commit when done.",
  "input_prompt": "Your input artifact is at: spec.md",
  "artifacts": {
    "design_doc": "spec.md"
  },
  "dimensions": null,
  "thresholds": null
}
```

For gate nodes, `dimensions` and `thresholds` are populated from the pipeline YAML so the command knows what to score.

Location: `.belayer/.internal/input/node-context.json` (gitignored runtime state).

The command writes a completion file to signal it's done:

```
.belayer/.internal/completion/<task-id>-<node>-attempt-<N>.json
```

This is the existing completion contract — unchanged. For gates, the command also writes `.belayer/.internal/output/gate-result.json` and `.belayer/.internal/output/rationale.md`.

### 4. Framework model

A framework is a directory with a known layout that provides everything needed to run pipeline nodes:

```
frameworks/claude-tmux/
├── framework.yaml          # metadata
├── pipeline.yaml           # default pipeline with command: fields
├── scripts/
│   ├── run-node.sh         # node runner: reads context, opens tmux + claude
│   └── run-gate.sh         # gate runner: reads context, opens tmux + claude for review
└── README.md               # usage and customization guide
```

`framework.yaml`:
```yaml
name: claude-tmux
description: Interactive Claude Code sessions in tmux windows
version: 0.1.0
```

Belayer ships `claude-tmux` as the first built-in framework via Go `//go:embed frameworks/` on an `embed.FS` in a new `internal/v3/frameworks/` package. `belayer setup` reads from the embedded FS for built-in names, or from disk for paths. Users can create custom frameworks — they're just directories.

**Required files:** `pipeline.yaml` and `scripts/` are required. `framework.yaml` and `README.md` are optional metadata. `belayer setup` validates that the source directory contains at least `pipeline.yaml` before copying — missing `pipeline.yaml` is an error, missing `scripts/` is a warning (some frameworks may not need scripts if commands are standalone binaries).

### 5. `belayer setup --framework <name-or-path>`

New CLI command that scaffolds a framework into the current repo's `.belayer/` directory.

```bash
belayer setup --framework claude-tmux           # built-in
belayer setup --framework ./my-custom-framework # local path
```

Resolution order:
1. If the argument is a path that exists on disk, use it directly
2. Otherwise, look for a built-in framework in belayer's embedded `frameworks/` directory

What it does:
1. Copies `pipeline.yaml` to `.belayer/pipeline.yaml`
2. Copies `scripts/` to `.belayer/scripts/`
3. Ensures `.belayer/.internal/` exists with a `.gitignore` containing `*`
4. Prints next steps: "Framework installed. Customize `.belayer/pipeline.yaml`, then run `belayer climb`."

If `.belayer/pipeline.yaml` already exists, prompt before overwriting. Use `--force` to skip the prompt (for CI/headless use).

### 6. `.belayer/` directory structure (in target repo)

```
.belayer/
├── pipeline.yaml           # committed — orchestration config
├── scripts/                # committed — node/gate runner scripts
│   ├── run-node.sh
│   └── run-gate.sh
└── .internal/              # gitignored — runtime state
    ├── .gitignore          # contains: *
    ├── worktrees/          # git worktrees for pipeline runs
    ├── completion/         # completion files (node protocol output)
    ├── input/              # node-context.json, diff.txt, diff-stat.txt, feedback.md
    ├── output/             # gate-result.json, rationale.md
    └── hooks.json          # framework-written hooks config (e.g., Claude Code Stop hook)
```

Committed files are the orchestration definition — project config like `Dockerfile` or `Makefile`. Runtime state is ephemeral.

### 7. What moves out of core

| Current location | Moves to | Notes |
|-----------------|----------|-------|
| `session.TmuxSpawner` | `frameworks/claude-tmux/` (as shell scripts) | No longer Go code — becomes `run-node.sh` |
| `session.WriteHooksConfig()` | `frameworks/claude-tmux/scripts/run-node.sh` | Script configures Claude Stop hook |
| `buildClaudeCommand()` | `frameworks/claude-tmux/scripts/run-node.sh` | Script builds the claude CLI invocation |
| `buildEnvExports()` | Removed — ExecSpawner sets all `BELAYER_*` env vars natively; no separate export function needed | |

### 8. What stays in core

| Code | Why |
|------|-----|
| `ClimbWorkflow` | Pipeline sequencing is core |
| `NodeActivity` (minus Claude-specific code) | Spawn + poll + route is core |
| `ExecSpawner` | Generic exec is core |
| `buildInputPrompt()` | Prompt assembly from artifacts is core |
| `materializeCodeInput()` | Git diff materialization is core |
| `pollForCompletion()` | Completion polling is core |
| `processGateResult()` | Score-then-route is core (L-20260321-score-then-route) |
| `cleanStaleCompletionFiles()` | Attempt scoping is core (L-20260320-rendezvous-attempt-scope) |
| Gate scoring (`gate/` package) | Deterministic scoring is core |
| Pipeline parser + validator | YAML config is core |
| Event logger | Observability is core |

### 9. `claude-tmux` framework: `run-node.sh` sketch

```bash
#!/usr/bin/env bash
set -euo pipefail

# Read belayer context from env vars
TASK_ID="${BELAYER_TASK_ID:?}"
NODE="${BELAYER_NODE:?}"
ATTEMPT="${BELAYER_ATTEMPT:?}"
WORK_DIR="${BELAYER_WORK_DIR:?}"

# Read rich context
CONTEXT_FILE="$WORK_DIR/.belayer/.internal/input/node-context.json"
DESCRIPTION=$(jq -r '.description' "$CONTEXT_FILE")
INPUT_PROMPT=$(jq -r '.input_prompt' "$CONTEXT_FILE")

# Configure Claude Code Stop hook to call belayer node-complete
# Use jq to safely construct JSON (avoids shell injection from TASK_ID/NODE values)
HOOKS_DIR="$WORK_DIR/.belayer/.internal"
HOOK_CMD="belayer node-complete --task-id ${TASK_ID} --node ${NODE} --attempt ${ATTEMPT}"
jq -n --arg cmd "$HOOK_CMD" '{
  hooks: {
    Stop: [{ hooks: [{ type: "command", command: $cmd }] }]
  }
}' > "$HOOKS_DIR/hooks.json"

# Ensure tmux session exists
SESSION="belayer-v3"
tmux has-session -t "$SESSION" 2>/dev/null || tmux new-session -d -s "$SESSION"

# Create window and launch Claude
WINDOW="${NODE}-${TASK_ID:0:8}"
tmux new-window -t "$SESSION" -n "$WINDOW"
tmux send-keys -t "$SESSION:$WINDOW" \
  "cd '$WORK_DIR' && claude --dangerously-skip-permissions --settings '$HOOKS_DIR/hooks.json' '$DESCRIPTION' '$INPUT_PROMPT'" Enter
```

### 10. `.belayer/.internal/` path migration

Current code writes to `.belayer/completion/`, `.belayer/input/`, etc. These paths move to `.belayer/.internal/completion/`, `.belayer/.internal/input/`, etc. to separate runtime state from committed config.

Affected code in core:
- `readCompletionFile()` / `cleanStaleCompletionFiles()` in `activity.go` — update path prefix
- `materializeCodeInput()` in `activity.go` — write to `.internal/input/`
- `completionFilePath()` / `writeCompletionFile()` in `node_complete.go` — update path prefix
- Remove `WriteHooksConfig()` and `HooksConfigPath()` from `session/hooks.go` — these move to the framework script

**Not affected:** `WriteHooksConfig()` no longer exists in core, so no path update needed for it. The framework script (`run-node.sh`) writes hooks directly to `.belayer/.internal/hooks.json`.

Introduce an `internalDir` helper: `filepath.Join(workDir, ".belayer", ".internal")` used across `activity.go` and `node_complete.go`. Also add `WriteFeedbackActivity` to the migration — it currently hardcodes `.belayer/input/feedback.md` and must change to `.belayer/.internal/input/feedback.md`.

Update `node-complete` to prefer `BELAYER_WORK_DIR` env var over `os.Getwd()` when resolving the working directory. This prevents silent failures when the hook runs in a directory different from the worktree (e.g., Claude Code Stop hooks may run from the agent's cwd, not the project root).

Keep `resolveNodeConfig()` in `node_complete.go` — its `belayer-pipeline.yaml` (repo root) and `.belayer/pipeline.yaml` lookup candidates remain for backward compat, plus the `DefaultPipelineYAML` fallback until the P1 cleanup removes it.

## Testing

| Test | Type | Description |
|------|------|-------------|
| `ExecSpawner` spawn success | Unit | Exec `echo hello`, verify process starts |
| `ExecSpawner` command not found | Unit | Missing command → error |
| `node-context.json` round-trip | Unit | Write context, read back, verify fields |
| Pipeline `command:` parsing | Unit | YAML with command field parses correctly |
| Pipeline missing `command:` | Unit | Validation rejects node without command |
| Integration: all pass | Integration | `fakeSpawner` unchanged — already tests the contract |
| Integration: retry then pass | Integration | `fakeSpawner` unchanged |
| `belayer setup` scaffolding | Integration | Setup writes expected files to temp dir |

Existing integration tests with `fakeSpawner` are the safety net — they prove the pipeline engine works regardless of spawner implementation.

## NOT in scope

- Multi-language Node SDKs (Python, TS, Go) — P2 TODO, depends on this refactor
- Remote framework fetching (`belayer setup --framework github.com/...`) — future
- P1 code restructuring (node renaming) — independent work
- Multi-repo runtime — independent P2
- Summit operations — independent P2
- Boulderer — P3
