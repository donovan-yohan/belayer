---
description: Use when a spec.md is ready for implementation and needs to be submitted to the belayer pipeline
---

# Send Spec to Pipeline

Submit a spec document to the belayer pipeline for autonomous implementation.

## Usage

```
/explorer:send path/to/spec.md
```

## Invocation

**IMMEDIATELY execute this workflow:**

1. **Parse arguments:** Extract the file path from the command arguments. If no path is provided, ask the user which spec file to submit.

2. **Validate the spec file:**
   - Check that the file exists using the Read tool
   - Check that the file is non-empty
   - If validation fails, report the error and stop

3. **Read the spec contents:** Read the full contents of the spec file.

4. **Submit to pipeline:** Call the belayer-channel MCP `submit` tool with:
   ```json
   { "spec": "<full contents of the spec file>" }
   ```

5. **Report result:** Relay the channel's response verbatim to the user. The response contains the workflow ID and pipeline name on success, or an error message if the worker is not reachable.

## Notes

- This command is a thin wrapper around the belayer-channel MCP `submit` tool
- No spec validation or linting is performed — belayer is pipes, the spec goes in as-is
- The belayer worker must be running (`belayer worker`) for submission to succeed
- If the worker is not running, the channel server returns a descriptive error
