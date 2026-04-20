---
name: belayer-communication
description: How to communicate with other agents and the Belayer control plane during a Nightshift run. Load this skill in every Nightshift specialist profile.
version: 1.0.0
author: Nightshift
license: MIT
metadata:
  hermes:
    tags: [nightshift, belayer, communication, multi-agent, coordination]
---

# Belayer Communication

You are operating inside a **Belayer-managed Nightshift run**.

Multiple specialist agents are working on the same ticket. You are one of them. A supervisor coordinates the work. Belayer is the session bus that connects everyone.

## The rules

1. **Never communicate with other agents directly.** No raw tmux. No writing to shared files as a messaging hack. No printing text hoping someone reads your terminal.
2. **Always use `belayer` commands** to send messages, log observations, publish artifacts, and mark work complete.
3. **Belayer is your only coordination interface.** If you need something from another agent, go through Belayer. If you learn something the run should know, tell Belayer.

---

## Your environment

Every Nightshift agent receives these environment variables:

- `BELAYER_SESSION_ID` — the current run session
- `BELAYER_AGENT_ID` — your role identity (e.g. `supervisor`, `api`, `app`, `reviewer`, `qa`)
- `BELAYER_SOCKET` — daemon socket (usually auto-resolved)
- `BELAYER_RUN_DIR` — root directory for this run's artifacts and state

You do not need to pass `--session` or `--agent` to most commands — they are inferred from the environment.

---

## Commands you should use

### Messaging

Send a message to a specific agent:

```bash
belayer message send --to supervisor "I'm blocked on the auth token shape — need the shared contract updated"
```

Broadcast to all agents in the session:

```bash
belayer message broadcast "Shared contract v2 is now in artifacts/shared-contract.md — please re-read before continuing"
```

### Artifacts

Register a durable artifact:

```bash
belayer artifact create --kind specialist-report --path artifacts/api-specialist-report.md
belayer artifact create --kind shared-contract --path artifacts/shared-contract.md
```

List current artifacts:

```bash
belayer artifact list
```

### Finishing your work

When your assigned work is complete, you **must** call:

```bash
belayer finish "Summary of what I did and what the supervisor should know"
```

This:
- marks your agent run as complete in the session
- logs your summary to the event stream
- notifies the supervisor that you are done

**Do not** just stop producing output and wait. **Do not** say "I'm done" in your terminal without calling `belayer finish`. The system cannot detect completion unless you explicitly mark it.

If you are blocked and cannot continue, use:

```bash
belayer finish --blocked "Cannot proceed — need clarification on X from the supervisor"
```

This marks you as blocked rather than complete, so the supervisor knows to intervene.

---

## When to message vs note vs artifact

| You want to... | Use |
|---|---|
| Ask another agent a question | `belayer message send --to <agent>` |
| Tell the supervisor you're done | `belayer finish "..."` |
| Tell the supervisor you're blocked | `belayer finish --blocked "..."` |
| Broadcast a change everyone should know | `belayer message broadcast "..."` |
| Publish a durable output | `belayer artifact create ...` |
| Read what artifacts exist | `belayer artifact list` |

---

## Planner-only commands

If your role is **supervisor**, you have additional capabilities.

### Spawning specialists

When you determine which specialists are needed for the task, spawn them:

```bash
belayer spawn --profile nightshift-extend-api --name api --repo extend-api
belayer spawn --profile nightshift-extend-app --name app --repo extend-app
belayer spawn --profile nightshift-reviewer --name reviewer
belayer spawn --profile nightshift-qa --name qa
```

Each `spawn` command:
- creates a new Hermes session in its own tmux window
- registers the agent with Belayer's session roster
- makes it addressable via `belayer message send --to <name>`

You can check who is running:

```bash
belayer roster
```

### Coordination patterns

As supervisor, your job is to:

1. Read the ticket/spec
2. Decide which repos and specialists are needed
3. Spawn the right agents
4. Send them their task assignments via `belayer message send`
5. Monitor progress via `belayer logs` or event stream
6. Intervene when agents are blocked or drifting
7. Route review requests
8. Trigger verification/QA
9. Assemble the handoff when all work passes

You do **not** write code yourself. You coordinate.

### Checking status

```bash
belayer roster          # who exists and their status
belayer logs            # recent session events
belayer artifact list   # current artifacts
```

---

## What happens if you forget to call `belayer finish`

The system will notice.

If your agent session becomes idle without calling `belayer finish`, the system will prompt you:

> "Your session appears idle. Is your work complete? If so, please run `belayer finish \"summary\"` to mark completion. If you are blocked, run `belayer finish --blocked \"reason\"`. If you are still working, continue."

This is a safety net, not the primary mechanism. You should call `belayer finish` proactively when your work is done.

---

## Communication etiquette

### Do
- Be specific in messages — include file paths, artifact names, exact questions
- Publish artifacts as soon as they are meaningful
- Call `belayer finish` as soon as your work is complete or you are blocked
- Respond to supervisor messages promptly

### Don't
- Send vague messages like "I'm working on it"
- Forget to call `belayer finish` — it is the most important coordination signal
- Try to read another agent's terminal output directly
- Assume the supervisor can see your terminal — communicate through Belayer
- Wait silently if you are blocked — always surface blockers explicitly

---

## Quick reference

```bash
# Messaging
belayer message send --to <agent> "text"
belayer message broadcast "text"

# Artifacts
belayer artifact create --kind <kind> --path <path>
belayer artifact list

# Completion
belayer finish "summary of work done"
belayer finish --blocked "reason I cannot continue"

# Planner only
belayer spawn --profile <profile> --name <name> [--repo <repo>]
belayer roster

# Observation
belayer logs
```
