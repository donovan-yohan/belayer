---
name: explorer-send
description: Use when a spec.md is ready for implementation and needs to be submitted to the belayer pipeline
---

> Generated from Claude plugin command: plugins/explorer/commands/send.md
> Claude alias: /explorer:send

# Send Spec to Pipeline

Submit a spec document to the belayer pipeline for autonomous implementation.

## Usage

```
explorer-send path/to/spec.md
explorer-send path/to/spec.md --detach
```

## Invocation

**IMMEDIATELY execute this workflow:**

1. **Parse arguments:** Extract the file path from the command arguments. If no path is provided, ask the user which spec file to submit. Note any additional flags (e.g., `--detach`).

2. **Validate the spec file:**
   - Check that the file exists using the Read tool
   - Check that the file is non-empty
   - If validation fails, report the error and stop

3. **Resolve the absolute path:** Convert the spec file path to an absolute path so `belayer climb` can find it regardless of working directory.

4. **Submit to pipeline:** Run belayer climb via the Bash tool:
   ```bash
   belayer climb --file <absolute-path-to-spec> [--detach]
   ```
   Pass `--detach` if the user included it or asked to run in the background.

5. **Report result:** Relay the output to the user. On success, belayer reports the Temporal workflow ID. On failure, it reports what went wrong (e.g., worker not running, Temporal not reachable).

## Notes

- This command shells out to `belayer climb` — the belayer worker and Temporal must be running
- No spec validation or linting is performed — belayer is pipes, the spec goes in as-is
- The climb runs from the current working directory, so the pipeline operates on the current repo
- Use `--detach` to return immediately while the pipeline runs in the background
