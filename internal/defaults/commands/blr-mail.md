---
description: Check mail inbox for messages
argument-hint: "[read|inbox|ack <id>]"
allowed-tools: ["Bash"]
---

Check the belayer mail inbox:

- `belayer mail read` — Read and display all unread messages
- `belayer mail inbox` — Show unread message count
- `belayer mail ack <id>` — Acknowledge a specific message

Default to `belayer mail read` if no subcommand specified.
