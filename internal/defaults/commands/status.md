---
description: Show task and goal status for the current belayer instance
argument-hint: "[task-id]"
allowed-tools: ["Bash", "Read"]
---

Run the belayer status command and present the results clearly.

If the user provides a task ID, show detailed status for that task:

```bash
belayer status [task-id]
```

Otherwise show overview status:

```bash
belayer status
```

Format the output for readability — group by task, show goal progress, highlight any failed or stuck items.
