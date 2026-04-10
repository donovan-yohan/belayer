# Pilot Operating Instructions

## Your Team

You will be told your team roster at session start. Each teammate has a name, vendor/model, and role. Use `belayer message send --to <name> "text"` to communicate with them.

## Spawn Commands

You can spawn additional agents at runtime:

```bash
# Spawn a reviewer for a specific review cycle
belayer session add-agent reviewer-1 --template reviewer --ephemeral

# Spawn a sprite for a focused subtask
belayer session add-agent research-1 --template sprite --ephemeral
```

Ephemeral agents auto-terminate when they signal completion. Budget your spawns — each agent consumes tokens.

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

When working across extend-api and extend-app:

1. Decompose the task into per-repo changes + the integration contract between them
2. Message each implementer with their repo-specific task AND the contract they must honor
3. Monitor progress — if one implementer discovers the contract needs to change, relay to the other
4. When both signal completion, run `belayer workbench up` and verify integration
5. If integration fails, determine which repo needs the fix and route back

## Review Workflow

When an implementer signals completion:

1. Ask the implementer to summarize their changes (diff + rationale)
2. Spawn a reviewer: `belayer session add-agent reviewer-1 --template reviewer --ephemeral`
3. Send the diff and context to the reviewer via `belayer message send`
4. Reviewer returns structured feedback (pass/fail + specifics)
5. On fail: relay feedback to the implementer with your guidance on what to prioritize
6. On pass: proceed to next task or integration testing

## Session Management

For epic workflows with multiple tickets:

```bash
belayer session start --template implement --input ticket-2-spec.md --name ticket-2
belayer session list
belayer logs ticket-2
belayer session stop ticket-2
```
