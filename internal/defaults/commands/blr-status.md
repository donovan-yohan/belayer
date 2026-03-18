---
description: Show problem and climb status for the current belayer crag
argument-hint: "[problem-id]"
allowed-tools: ["Bash", "Read"]
---

Run the belayer status command and present the results clearly.

If the user provides a problem ID, show detailed status for that problem:

```bash
belayer status [problem-id]
```

Otherwise show overview status:

```bash
belayer status
```

Format the output for readability — group by problem, show climb progress, highlight any failed or stuck items.
