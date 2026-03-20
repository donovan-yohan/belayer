---
status: current
created: 2026-03-20
branch: master
---
# Design: Claude Code Channels + Hooks — Reliable Session Communication

## Summary

Integrate Claude Code Channels (inbound events) and Hooks (outbound insurance) to replace belayer's brittle tmux send-keys messaging. Sessions are strongly prompted to call finish/flare/fail CLI commands. Hooks act as deterministic insurance: if a session reaches Stop without having signaled, the hook calls it. Channels push pipeline events INTO sessions (observer pattern). Phased rollout.

## Goal

Make session communication reliable and observable. The agent is prompted to signal via CLI (primary path). Hooks guarantee the signal happens even if the agent forgets (insurance). Channels enable pushing events into sessions — enabling the observer pattern and real-time pipeline awareness.

## Phase 1 Scope (this implementation)

### Inbound: Channels push events INTO sessions
- Observer session receives ALL pipeline events (phase transitions, repo completions, flares)
- Worker sessions receive dependency notifications ("extend-api finished, you can test against it")
- Risk gate notifications pushed to observer session

### Outbound: Prompt (primary) + Hooks (insurance)
- System prompt strongly instructs the agent to call `belayer <role> finish/flare/fail --task-id <id>`
- `belayer <role> finish` CLI is idempotent (second call is a no-op)
- **Stop hook (insurance):** If the session ends without having called finish/flare/fail, the hook detects this and calls `belayer <role> finish` automatically
- **Notification hook:** Routes `permission_prompt` events to the observer so the user knows when a session is stuck waiting for approval

### Observer Session
- `belayer run "description"` opens a Claude Code session in your terminal with `--channels`
- This session receives all pipeline events as `<channel>` tags
- Flare alerts, risk gate decisions, and dependency updates arrive here
- User intervenes by responding naturally in the observer session

## Architecture

```
belayer run "build auth for all platforms"
    │
    ├── OBSERVER SESSION (your terminal)
    │     Claude Code with --channels server:belayer-channel
    │     Receives ALL pipeline events via channel push
    │     Risk gates arrive here for user decision
    │
    ├── SETTER (tmux window, autonomous)
    │     Claude Code with --channels server:belayer-channel
    │     Prompted to call: belayer setter finish --task-id <id>
    │     Stop hook insurance: auto-finishes if agent forgot
    │     Notification hook: routes permission prompts to observer
    │
    ├── LEAD-EXTEND-API (tmux window, autonomous)
    │     Receives dependency notifications via channel push
    │     Prompted to call: belayer lead finish --task-id <id> --repo extend-api
    │     Stop hook insurance: auto-finishes if agent forgot
    │
    └── All sessions viewable via belayer attach <role>
```

## Hooks Architecture

### SessionStart Hook
Sets environment variables for task context:
```bash
#!/bin/bash
INPUT=$(cat)
# Set belayer context for this session
if [ -n "$CLAUDE_ENV_FILE" ]; then
  echo "export BELAYER_TASK_ID=${BELAYER_TASK_ID}" >> "$CLAUDE_ENV_FILE"
  echo "export BELAYER_ROLE=${BELAYER_ROLE}" >> "$CLAUDE_ENV_FILE"
  echo "export BELAYER_REPO=${BELAYER_REPO}" >> "$CLAUDE_ENV_FILE"
fi
exit 0
```

### Stop Hook (Insurance)
Fires when Claude's session ends. Checks if finish/flare/fail was already called. If not, calls it.

```bash
#!/bin/bash
INPUT=$(cat)
LAST_MSG=$(echo "$INPUT" | jq -r '.last_assistant_message')
TASK_ID="$BELAYER_TASK_ID"
ROLE="$BELAYER_ROLE"
REPO="$BELAYER_REPO"

# Check if finish was already called (idempotent — second call is a no-op)
REPO_FLAG=""
if [ -n "$REPO" ]; then
  REPO_FLAG="--repo $REPO"
fi

# Always call finish as insurance. The CLI is idempotent.
belayer "$ROLE" finish --task-id "$TASK_ID" $REPO_FLAG 2>/dev/null || true
exit 0
```

Since `belayer <role> finish` is idempotent:
- If the agent already called it → no-op
- If the agent forgot → the hook calls it → workflow advances
- No risk of double-advancing (Temporal Signal is idempotent too)

### Notification Hook
Routes permission prompts to the observer so the user knows a session is stuck:

```bash
#!/bin/bash
INPUT=$(cat)
NOTIFICATION_TYPE=$(echo "$INPUT" | jq -r '.notification_type')

if [ "$NOTIFICATION_TYPE" = "permission_prompt" ]; then
  # Push to observer via channel HTTP endpoint
  curl -s -X POST "http://localhost:${BELAYER_OBSERVER_PORT}" \
    -d "{\"event\":\"permission_needed\",\"role\":\"${BELAYER_ROLE}\",\"repo\":\"${BELAYER_REPO}\",\"message\":$(echo "$INPUT" | jq '.message')}" \
    2>/dev/null || true
fi
exit 0
```

## Channel Server

A thin Bun/TypeScript MCP server that bridges HTTP → Claude Code notifications:

```typescript
// belayer-channel.ts — thin adapter, no business logic
const mcp = new Server(
  { name: 'belayer-channel', version: '0.0.1' },
  {
    capabilities: { experimental: { 'claude/channel': {} } },
    instructions: `Pipeline events arrive as <channel source="belayer-channel" event="...">.
      Report status to the user. Alert on flares. Help with risk gate decisions.`,
  },
)

// HTTP listener: Temporal worker POSTs events here
Bun.serve({
  port: parseInt(process.env.BELAYER_CHANNEL_PORT || '8790'),
  hostname: '127.0.0.1',
  async fetch(req) {
    const body = await req.json()
    await mcp.notification({
      method: 'notifications/claude/channel',
      params: { content: JSON.stringify(body.content), meta: body.meta },
    })
    return new Response('ok')
  },
})
```

**Key principle:** The channel server is a thin adapter. All routing logic, state management, and business logic stays in the Go worker and Temporal workflow. The Bun script just bridges HTTP → MCP notifications.

## Pipeline Event Types

Events pushed from Temporal workflow → HTTP POST → channel server → Claude session:

| Event | Pushed to | Content |
|-------|-----------|---------|
| `pipeline_started` | Observer | Pipeline name, repo list |
| `phase_started` | Observer | Phase name, roles |
| `role_completed` | Observer + dependent workers | Role, repo, output summary |
| `dependency_ready` | Target worker | "extend-api finished, you can test" |
| `risk_gate` | Observer | Gate details, approval options |
| `flare` | Observer | Role, repo, help message |
| `permission_needed` | Observer | Role, repo, what permission |
| `pipeline_completed` | Observer | Final status, PR links |

## Phased Rollout

### Phase 1 (this implementation)
- Channel server for push events (one-way)
- Observer session pattern
- Stop hook insurance for finish
- Notification hook for permission prompts
- CLI finish/flare/fail unchanged (idempotent)
- Pipeline event streaming

### Phase 2 (after channel proves reliable)
- MCP tools for finish/flare/fail (replace CLI in system prompt)
- Per-session client interface with stable IDs (Codex review finding #1)
- Session registry persisted in Temporal workflow state (Codex finding #2)

### Phase 3 (after MCP tools work)
- Cross-session messaging via send_to_session tool
- Risk gates as in-session approve/override tools
- Plugin packaging for easy installation

## Codex Review Findings (addressed)

| Finding | Resolution |
|---------|-----------|
| SessionTransport wrong abstraction | Deferred to Phase 2 — proper per-session client with IDs |
| In-memory registry lost on restart | Deferred to Phase 2 — persist in Temporal workflow state |
| Research preview dependency | Acknowledged — use `--dangerously-load-development-channels` |
| No auth on HTTP listener | localhost-only binding + per-session token (Phase 1) |
| Overbuilt for first cut | Phased rollout: Phase 1 is channels-in + hooks-out only |
| Codex app-server is different model | Not forced under same interface — separate integration |
| Bun as second runtime | Thin adapter only, no routing/state logic in Bun |

## Key Decisions

- Prompt is primary signaling mechanism, hooks are deterministic insurance
- `belayer <role> finish` is idempotent — safe to call twice
- Channels for inbound (push events), CLI for outbound (signals)
- Observer session opens in user's terminal, workers in tmux
- Channel server is a thin Bun adapter — no business logic
- Deterministic port assignment via BELAYER_CHANNEL_PORT env var
- Notification hook routes permission_prompt to observer
- Phased: prove channels reliable before replacing CLI with MCP tools
- Stop hook always calls finish as insurance (idempotent = safe)
