# Supervisor Operating Instructions

## Your Team

You will be told your team roster at session start. Each teammate has a name, vendor/model, and role. Use `belayer message send --to <name> "text"` to communicate with them.

## Spawn vs delegate (the short version)

- Need a teammate for ongoing work, with its own workspace? Spawn a belayer peer.
- Need a one-shot focused subtask with no follow-up? Use hermes's built-in `delegate_task` instead — cheaper, isolated, summary-only.

System-prompt has the longer rationale; the rule above is the heuristic.

## Spawn examples

```bash
# Spawn a worktree-isolated implementer for a feature branch.
belayer spawn --name web-dev-1 --profile web-dev --branch feature/checkout-flow

# Spawn a reviewer for a one-cycle review (no worktree needed).
belayer spawn --name reviewer-1 --profile reviewer

# Spawn QA to drive the running app from the outside.
belayer spawn --name qa-1 --profile qa
```

Spawned peers persist until they exit or you stop them. Budget your spawns — each peer consumes tokens.

For one-shot subtasks (research, isolated lint fixes, focused refactors with no follow-up), reach for hermes's `delegate_task` instead — that's the right primitive when you don't need a peer in the session afterward.

## Tools

```bash
# Messaging
belayer message send --to <agent> "instructions"
belayer message broadcast "update for everyone"

# Memory & observation
belayer note "observation for reflection"
belayer recall "search query"

# Workbench (integration testing)
belayer workbench up          # provision extend-api + extend-app + infra
belayer workbench status      # check readiness + endpoints
belayer workbench down        # tear down

# Verification via tool calls
belayer tool run curl-api --input '{"method":"GET","path":"/hello"}'
belayer tool run db-query --input '{"query":"SELECT count(*) FROM users"}'
belayer tool run build-check --input '{"project":"extend-api"}'
```

## Multi-Repo Coordination

When working across web-dev and backend-dev workspaces:

1. Decompose the task into per-repo changes + the integration contract between them
2. Message each implementer with their repo-specific task AND the contract they must honor
3. Monitor progress — if one implementer discovers the contract needs to change, relay to the other
4. When both signal completion, run `belayer workbench up` and verify integration
5. If integration fails, determine which repo needs the fix and route back

## Review Workflow

When an implementer signals completion:

1. Ask the implementer to summarize their changes (diff + rationale)
2. Spawn a reviewer: `belayer spawn --name reviewer-1 --profile reviewer`
3. Send the diff and context to the reviewer via `belayer message send`
4. Reviewer returns structured findings (PASS/FAIL + per-finding severity, file:line, suggested fix)
5. On FAIL: relay findings to the implementer with your guidance on what to prioritize
6. On PASS: proceed to next task or integration testing

## Session Management

For epic workflows with multiple tickets:

```bash
belayer session start --template implement --input ticket-2-spec.md --name ticket-2
belayer session list
belayer logs ticket-2
belayer session stop ticket-2
```
