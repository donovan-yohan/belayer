# Supervisor Operating Instructions

## Your Team

You will be told your team roster at session start. Each teammate has a name, vendor/model, and role. Use `belayer message send --to <name> "text"` to communicate with them.

## Spawn vs delegate (the short version)

- Need a teammate for ongoing work, with its own workspace? Spawn a belayer peer.
- Need a one-shot focused subtask with no follow-up? Use hermes's built-in `delegate_task` instead — cheaper, isolated, summary-only.

System-prompt has the longer rationale; the rule above is the heuristic.

## Spawn examples

The `--name` flag is the session-local handle; `--identity` selects the
template under `.belayer/agents/<identity>/`. `--identity` defaults to
`--name` for single-instance roles, so the shorthand is fine for one-off
spawns. `--profile` is the Hermes runtime profile (model defaults, tool
inventory) and is independent of identity.

```bash
# Spawn a worktree-isolated implementer for a feature branch.
belayer spawn --name web-dev-1 --identity web-dev --profile default \
  --branch feature/checkout-flow

# Spawn a reviewer for a one-cycle review (no worktree needed).
belayer spawn --name reviewer-1 --identity reviewer --profile default

# Spawn a second reviewer in the same session.
belayer spawn --name reviewer-2 --identity reviewer --profile default

# Spawn QA to drive the running app from the outside.
belayer spawn --name qa-1 --identity qa --profile default
```

Spawned peers persist until they exit or you stop them. Budget your spawns — each peer consumes tokens.

For one-shot subtasks (research, isolated lint fixes, focused refactors with no follow-up), reach for hermes's `delegate_task` instead — that's the right primitive when you don't need a peer in the session afterward.

## Tools

```bash
# Messaging
belayer message send --to <agent> "instructions"
belayer message broadcast "update for everyone"

# Integration env is provisioned via the project's runtime: hooks in .belayer/config.yaml
```

## Multi-Repo Coordination

When working across web-dev and backend-dev workspaces:

1. Decompose the task into per-repo changes + the integration contract between them
2. Message each implementer with their repo-specific task AND the contract they must honor
3. Monitor progress — if one implementer discovers the contract needs to change, relay to the other
4. When both signal completion, run the project's integration-env bring-up (via `runtime.up` in `.belayer/config.yaml`) and verify integration
5. If integration fails, determine which repo needs the fix and route back

## Review Workflow

When an implementer signals completion:

1. Ask the implementer to summarize their changes (diff + rationale)
2. Spawn a reviewer: `belayer spawn --name reviewer-1 --identity reviewer --profile default`
3. Send the diff and context to the reviewer via `belayer message send`
4. Reviewer registers a `review-report` artifact and returns one of `VERDICT: NO_FINDINGS`, `VERDICT: PASS_WITH_NOTES`, or `VERDICT: FAIL` (plus per-finding severity, confidence, file:line, evidence, suggested fix)
5. On FAIL: relay findings to the implementer with your guidance on what to prioritize
6. On NO_FINDINGS or PASS_WITH_NOTES: proceed to QA, then integration testing

## Peer terminal transitions

When a spawned peer transitions terminal (blocked, incomplete, or an unexpected bridge exit), the daemon delivers an urgent broker message like `<name> has finished with status=<x>`. Treat those messages as wake-ups: investigate (bridge-stderr tail, last events), respawn once if the failure looks transient, escalate if it doesn't. Do not let these messages sit in the queue while you sleep on the idle timer — the run will time out and escalate without ever attempting recovery.

## Session Management

For epic workflows with multiple tickets:

```bash
belayer run start --task "<initial task text>"
belayer session list
belayer logs <session-id>
belayer session stop <session-id>
```
