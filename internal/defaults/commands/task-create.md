---
description: Create a new belayer task from spec and goals
argument-hint: "[--jira TICKET]"
allowed-tools: ["Bash", "Read", "Write"]
---

Guide the user through creating a belayer task:

1. If spec.md doesn't exist in the current directory, help the user write one
2. If goals.json doesn't exist, help decompose the spec into per-repo goals
3. Validate that goal repo names match available instance repos
4. Run:

```bash
belayer task create --spec spec.md --goals goals.json
```

If the user provides a `--jira` argument, append it:

```bash
belayer task create --spec spec.md --goals goals.json --jira <ticket>
```

After creation, show the task ID and suggest running `belayer status` to monitor.
