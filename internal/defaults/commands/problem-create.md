---
description: Create a new belayer problem from spec and climbs
argument-hint: "[--jira TICKET]"
allowed-tools: ["Bash", "Read", "Write"]
---

Guide the user through creating a belayer problem:

1. If spec.md doesn't exist in the current directory, help the user write one
2. If climbs.json doesn't exist, help decompose the spec into per-repo climbs
3. Validate that climb repo names match available crag repos
4. Run:

```bash
belayer problem create --spec spec.md --climbs climbs.json
```

If the user provides a `--jira` argument, append it:

```bash
belayer problem create --spec spec.md --climbs climbs.json --jira <ticket>
```

After creation, show the problem ID and suggest running `belayer status` to monitor.
