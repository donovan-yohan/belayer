---
description: View or modify belayer configuration
argument-hint: "[show | get <key> | set <key> <value>]"
allowed-tools: ["Bash", "Read"]
---

Manage belayer configuration via the CLI.

## Show full resolved config

```bash
belayer config show
```

## Get a specific value

```bash
belayer config get agents.provider
belayer config get agents.lead_provider
```

## Set a value (writes to crag config by default)

```bash
belayer config set agents.provider codex
belayer config set agents.lead_provider codex
belayer config set execution.max_leads 4
```

Use `--global` to write to global config instead:

```bash
belayer config set --global agents.provider codex
```

Format the output clearly for the user.
