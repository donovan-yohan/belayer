---
status: current
created: 2026-03-20
branch: master
supersedes: v2-channels-wiring
---
# Design: Observer-as-Intake — The User Session IS the Pipeline Entry Point

## Summary

Replace `belayer run "prompt"` with `belayer start`. The user's Claude Code session becomes both the intake (brainstorm, research, draft specs) AND the observer (receive pipeline events, handle flares, approve risk gates). When work is ready, the user tells Claude to submit it — Claude calls a `submit` MCP tool — and a Temporal workflow starts. Events flow back while the user keeps working on the next thing.

## Goal

Make the pipeline entry point a conversation, not a command-line argument. Work emerges from brainstorming, not from a pre-formed prompt. The user can have multiple specs in-flight simultaneously, getting interrupted by events only when something needs attention.

## The Problem with `belayer run "prompt"`

The current model assumes work is defined upfront as a one-liner. But real work:
- Starts as a vague idea ("we need auth for all platforms")
- Gets refined through research and conversation
- Results in a structured spec after back-and-forth
- May spawn multiple independent pipeline runs from one brainstorm session

Forcing this into `belayer run "one liner"` loses the conversation. The setter session was supposed to handle intake, but it's a separate session from the user — creating an unnecessary hop.

## Approach

### `belayer start` — Your Session IS the Pipeline

```bash
belayer start                          # Opens Claude Code with belayer channel
belayer start --crag extend-platform   # Opens in a specific crag context
```

This opens a Claude Code session in the user's terminal with:
- The belayer MCP channel server (for receiving pipeline events)
- MCP tools: `submit`, `status`, `approve`, `override`
- System prompt explaining the user's role and available tools
- No Temporal workflow started — that happens when the user submits work

### The User Flow

```
USER                                    BELAYER
────                                    ───────

belayer start
  ↓
Claude Code session opens
"What would you like to work on?"

User: "I want to add auth to all
  our platforms"

Claude: researches, asks questions,
  drafts a spec...

User: "looks good, send it"

Claude calls submit({                  → Temporal workflow starts
  spec: "...",                            decomposer → leads → spotters → ...
  repos: ["extend-api", "extend-app"]
})

"Submitted! Pipeline running.           ← <channel event="pipeline_started">
 Working on anything else?"

User: "yeah, let's also think about
  the notification system..."

Claude: brainstorms notifications...    ← <channel event="role_completed"
                                           repo="extend-api">
                                          "extend-api lead finished!"

User: "nice. send the notifications
  spec when ready"

                                        ← <channel event="flare"
                                           repo="extend-app">
                                          "FLARE: lead-extend-app needs help
                                           with the OAuth redirect flow"

Claude: "Heads up — the auth pipeline
  needs your help with OAuth in
  extend-app. Want to look at it?"

User: "yeah, what's the issue?"

Claude: explains, user helps,
  Claude calls approve/intervene        → Temporal signal resumes workflow
```

### MCP Tools Exposed by the Channel Server

| Tool | Purpose | When Called |
|------|---------|------------|
| `submit` | Start a new pipeline run with a spec | User says "send it" / "submit this" |
| `status` | Query active pipeline runs | User asks "what's running?" |
| `approve` | Approve a risk gate | Risk gate event arrives, user approves |
| `override` | Override a decomposer decision | User wants to change repo selection |
| `flare_respond` | Send context to a flared session | User helps a stuck agent |

The `submit` tool connects to Temporal and starts a RouteWorkflow:

```typescript
// In channel server
mcp.setRequestHandler(CallToolRequestSchema, async req => {
  if (req.params.name === 'submit') {
    const { spec, repos } = req.params.arguments;
    // HTTP POST to worker's API to start workflow
    await fetch('http://127.0.0.1:WORKER_PORT/start', {
      method: 'POST',
      body: JSON.stringify({ spec, repos })
    });
    return { content: [{ type: 'text', text: 'Pipeline started' }] };
  }
});
```

### What This Eliminates

- **`belayer run "prompt"`** — replaced by `belayer start` + `submit` tool
- **Separate setter session** — the user IS the setter
- **The approach phase as a pipeline stage** — intake happens in the user's session, not in a Temporal workflow
- **Pre-formed descriptions** — work emerges from conversation

### What This Changes in the Pipeline

The pipeline phases become:

```
BEFORE:
  APPROACH (setter) → ASCENT (decomposer → lead → spotter → anchor) → SEND (PR)
  ↑ separate session    ↑ Temporal workflow                            ↑ Temporal

AFTER:
  USER SESSION (you)  →  ASCENT (decomposer → lead → spotter → anchor) → SEND (PR)
  ↑ belayer start         ↑ Temporal workflow starts on submit           ↑ Temporal
  ↑ brainstorm freely     ↑ events flow back to your session
  ↑ submit when ready
```

The Approach phase moves OUT of Temporal and INTO the user's session. The Temporal workflow only handles execution (Ascent) and output (Send).

### `belayer start` vs `belayer run`

| | `belayer start` (new default) | `belayer run "prompt"` (kept for automation) |
|---|---|---|
| Entry | Opens interactive session | Fire-and-forget CLI |
| Intake | Conversational, iterative | Pre-formed one-liner |
| Pipeline start | User calls submit tool | Immediate workflow start |
| Events | Stream into session | Logged, query via status |
| Multi-spec | Yes — submit multiple from one session | One per invocation |
| Use case | Interactive development | CI/automation/scripts |

`belayer run "prompt"` is kept with `--detach` behavior for automation/CI where interactive intake doesn't make sense.

### Worker Changes

The worker needs an HTTP API endpoint for the `submit` tool to call:

```
POST /start
{
  "spec": "Build user authentication...",
  "repos": ["extend-api", "extend-app"],
  "pipeline": "belayer-pipeline.yaml"  // optional
}
→ Starts RouteWorkflow, returns workflow ID
```

This is a thin wrapper around the existing `client.ExecuteWorkflow` call.

### Session Lifecycle

```
belayer start
  │
  ├── Opens Claude Code with belayer channel + tools
  │     Channel port: 8790
  │     Tools: submit, status, approve, override, flare_respond
  │     System prompt: "You are the user's brainstorming partner and
  │       pipeline observer. Help them research and draft specs. When
  │       work is ready, use the submit tool. Pipeline events arrive
  │       as <channel> tags — report status and handle interrupts."
  │
  ├── User brainstorms, Claude helps draft specs
  │
  ├── User: "submit this" → Claude calls submit tool
  │     → POST to worker /start endpoint
  │     → Worker starts Temporal workflow
  │     → Worker spawns lead sessions in tmux with channels + hooks
  │     → Events flow back to user session via channel push
  │
  ├── User keeps working on next spec
  │     Meanwhile: events stream in as <channel> tags
  │     Flares interrupt the conversation: "FLARE: lead-app needs help"
  │
  └── User can submit more specs, handle flares, approve gates
      Multiple pipelines can be in-flight simultaneously
```

## Key Decisions

- `belayer start` replaces `belayer run` as the primary entry point
- `belayer run "prompt"` kept for automation/CI (--detach behavior)
- The user session IS the approach phase — no separate setter
- `submit` MCP tool starts Temporal workflows from within the session
- Worker exposes HTTP /start endpoint for the submit tool
- Multiple pipelines can be in-flight from one session
- Events from all pipelines flow to the one user session
- Flares interrupt the brainstorming conversation
- The Temporal workflow starts at decomposer, not setter
