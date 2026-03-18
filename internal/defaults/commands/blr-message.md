---
description: Send a message to a running belayer agent
argument-hint: "<address> --type <type> --body <body>"
allowed-tools: ["Bash"]
---

Send a message to a belayer agent using the mail system.

```bash
belayer message <address> --type <type> --body "<body>"
```

Message types: climb_assignment, done, spot_result, verdict, feedback, instruction

Address format: `problem/<problem-id>/lead/<repo>/<climb-id>`

If the user doesn't specify all arguments, ask for:
1. Which agent to message (show running agents if possible via `belayer status`)
2. Message type (default: instruction)
3. Message body
