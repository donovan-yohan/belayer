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

## Commands

### Messaging
```bash
belayer message send --to supervisor "Blocked on API auth contract"
belayer message broadcast "Shared contract updated in artifacts/shared-contract.md"
```

### Notes
```bash
belayer note "extend-api tests pass locally after migration change"
```

### Artifacts
```bash
belayer artifact create --kind specialist-report --path artifacts/api-specialist-report.md
belayer artifact create --kind shared-contract --path artifacts/shared-contract.md
belayer artifact list
```

### Completion
You must call one of these before you are done:

```bash
belayer finish "Summary of work completed"
belayer finish --blocked "Reason I cannot continue"
```

If you do not call `belayer finish`, a hook will mark your run blocked automatically when your Hermes session ends.

## Planner-only commands

### Spawn specialists
```bash
belayer spawn --profile default --name api --role api --workdir /path/to/repo
belayer spawn --profile default --name reviewer --role reviewer --workdir /path/to/repo
belayer roster
```

Use `belayer spawn` whenever you decide a new specialist is needed.

## Communication etiquette

- Use **messages** for direct questions/instructions
- Use **notes** for progress and observations
- Use **artifacts** for durable outputs others should read later
- Use **finish** to explicitly signal completion or blockage

Do not assume the supervisor can infer your state from silence.
