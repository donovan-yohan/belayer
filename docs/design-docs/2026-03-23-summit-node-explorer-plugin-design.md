---
status: current
created: 2026-03-23
branch: master
supersedes:
implemented-by:
consulted-learnings: [L-007, L-009]
---

# Summit Node & Explorer Plugin

Complete the default belayer pipeline template by adding a PR-creation node and a convenience plugin for submitting specs from any interactive session.

## Context

The default pipeline (`default-climb`) today has three nodes: setter (plan), lead (implement), spotter (review gate). The pipeline ends after the spotter gate passes — no PR is created. Meanwhile, there's no standardized way for users to submit a spec.md into the pipeline from an interactive coding session.

## Deliverables

### 1. Summit Node (Pipeline Template)

A fourth node appended after the spotter gate in `DefaultPipelineYAML`:

```yaml
- name: summit
  type: node
  description: |
    You are the summit. The code has passed review. Your job is to create
    a pull request for the completed work.

    Run /pr:author to create the PR.
    After /pr:author completes, verify the PR exists with `gh pr view`.
    If the PR was created successfully, write the PR URL and number to
    .belayer/output/pr.json.
  input:
    type: gate_result
    key: spotter
  output:
    type: pr
    path: .belayer/output/pr.json
  on_pass: stop
  on_retry: self
  on_fail: stop
  max_retries: 2
```

Summit is the terminal node — `on_pass: stop` ends the pipeline successfully.

**Output type `pr`**: New valid output type for nodes that produce pull requests. The output file is a JSON object containing `{ url, number, branch }` — all three fields are required.

**Completion verification**: Summit verifies the PR was created by running `gh pr view` after `/pr:author`. If PR creation failed, the node writes a failure completion and retries (up to 2 retries). On retry, summit checks `gh pr view` *before* running `/pr:author` to avoid duplicate PR creation (idempotency).

### 2. Explorer Plugin

New plugin at `plugins/explorer/` in the belayer marketplace with one command.

**Plugin structure:**

```
plugins/explorer/
  .claude-plugin/
    plugin.json
  commands/
    send.md
```

**`plugin.json`:**

```json
{
  "name": "explorer",
  "version": "0.1.0",
  "description": "Submit specs to the belayer pipeline from any interactive coding session.",
  "author": { "name": "donovanyohan" },
  "keywords": ["explorer", "submit", "spec", "pipeline", "intake"],
  "agents": [],
  "commands": ["./commands/send.md"],
  "skills": []
}
```

**`/explorer:send` command:**

1. Accept a path argument: `/explorer:send path/to/spec.md`
2. Validate: file exists, non-empty
3. Read the spec.md contents
4. Call the belayer-channel MCP `submit` tool with `{ spec: <contents> }`
5. Relay the channel's response verbatim to the user (contains workflow ID and pipeline name, or an error if the channel/worker is not reachable)

No repo detection, no spec linting, no transformation — belayer is pipes, the spec goes in as-is. This is a convenience wrapper that any interactive coding session can use when a spec is ready for implementation. Error handling is delegated to the channel server — if the worker is down, the channel returns a descriptive error string that the command surfaces to the user.

### 3. Go Code Changes

**`internal/v3/pipeline/validate.go`**:
- Add `"pr"` to `validOutputTypes` map.
- Extend the gate/non-gate consistency check: `pr` output type must be rejected on gate nodes (same pattern as `gate_result` being rejected on non-gate nodes).
- Update the error message string (line 51) to include `"pr"` in the list of valid output types.

**`internal/v3/pipeline/defaults.go`**: Append the summit node to `DefaultPipelineYAML`.

**`internal/plugins/registry.go`**: Add `ExplorerVersion = "0.1.0"` constant. The explorer plugin follows the same `InstallPlugin` call site as harness and pr (called during `belayer init`).

**`.claude-plugin/marketplace.json`**: Add explorer entry alongside harness and pr:
```json
{
  "name": "explorer",
  "source": "./plugins/explorer",
  "description": "Submit specs to the belayer pipeline from any interactive session"
}
```

### 4. Updated Default Pipeline

```
setter → lead → spotter (gate) → summit → done
```

Single-repo, linear chain. No fan-out (future concern). Pipeline terminates after summit passes.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Summit node type | `node` (not `gate`) | PR creation is pass/fail, no scoring dimensions needed. `/pr:author` already uses pr-review-toolkit agents internally. |
| Output type | New `pr` type | Semantically distinct from `file`/`commit`/`gate_result`. Enables future PR monitoring integration. |
| Completion verification | `gh pr view` after `/pr:author` | Catches silent failures where `/pr:author` didn't actually create a PR. |
| Explorer plugin scope | Single skill, no validation | Belayer is pipes — doesn't own spec creation or quality. The plugin is just a convenience wrapper around `submit`. |
| Fan-out | Not included | Single-repo linear pipeline for now. Fan-out is a future concern. |

## Scope Boundaries

- **In scope**: Pipeline template update, `pr` output type validation, explorer plugin with `/explorer:send` skill, marketplace registration
- **Out of scope**: Temporal workflow changes, fan-out execution, spec validation/linting, PR monitoring after creation, input type/key validation (no `validInputTypes` map exists today; runtime concern)
