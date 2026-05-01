---
status: proposed
created: 2026-04-15
supersedes:
  - Layer 3 (tmux transport adapter) of 2026-04-15-belayer-run-model-for-nightshift-v1.md
  - 2026-04-15-harness-interface-and-sidecar-boundary.md (drops generic Harness interface)
  - Partially refines 2026-04-15-headless-hermes-daemon-architecture.md (preserves intent, changes ownership model)
implemented-by:
consulted-learnings:
  - docs/design-docs/2026-04-15-headless-hermes-daemon-architecture.md
  - docs/design-docs/2026-04-15-belayer-run-model-for-nightshift-v1.md
  - docs/ARCHITECTURE.md
  - docs/PHILOSOPHY.md
  - internal/daemon/agents.go (current tmux-based launch/deliver/watch)
  - internal/broker/broker.go (current Broker interface)
  - ~/.hermes/hermes-agent/run_agent.py (AIAgent constructor, callbacks, run_conversation)
  - ~/.hermes/hermes-agent/acp_adapter/session.py (SessionManager, _restore, _persist)
  - ~/.hermes/hermes-agent/tools/registry.py (ToolRegistry.register)
---

# Hermes Bridge

Belayer replaces tmux with a thin Python subprocess per agent (`hermes-bridge`) that wraps Hermes's `AIAgent` API. One process per agent. No custom protocol. The bridge talks to the daemon via its existing HTTP API. The daemon talks to the bridge via stdin.

## Design principle

Extend only where necessary. Hermes already handles:

- Headless agent execution (`AIAgent(quiet_mode=True)`)
- Tool calling loop with retries, context compression, token tracking
- Session persistence to SQLite + JSON logs
- Session resume (`hermes --resume <session_id>`)
- Tool registry and dispatch
- Interrupt mechanism (`agent.interrupt(message)`)
- 11 structured callbacks for observability

The bridge adds exactly what Hermes doesn't have: **coordination tools that talk to the Belayer session bus**. Everything else, we use as-is.

---

## Architecture

```
Belayer daemon (Go)
  |-- Session bus (messages, events, artifacts, roster)
  |-- HTTP API (Unix socket)
  |
  |-- hermes-bridge [planner] (Python subprocess)
  |     |-- stdin: receives interrupt/stop commands from daemon
  |     |-- AIAgent(quiet_mode=True)
  |     +-- HTTP calls to daemon API (events, tool handlers)
  |
  |-- hermes-bridge [api-specialist] (Python subprocess)
  |     |-- stdin: receives interrupt/stop commands from daemon
  |     |-- AIAgent(quiet_mode=True)
  |     +-- HTTP calls to daemon API
  |
  +-- hermes-bridge [reviewer] (Python subprocess)
        |-- ...same pattern
```

One Python process per agent. The daemon spawns and monitors each subprocess. No shared state between agents. No ThreadPoolExecutor. No bridge-side server or socket.

### Why one process per agent

1. **Crash isolation.** One agent segfaults, the others keep running. No "bridge is a single point of failure" risk.
2. **No registry collision.** Hermes's tool registry is a process-global singleton (`registry.py:437`). With one process per agent, each agent has its own registry. No handler overwrite problem.
3. **Simpler lifecycle.** Daemon spawns a subprocess, monitors it, kills it. Same pattern as the current tmux session management. No intra-process agent coordination.
4. **Simpler spawning.** Planner calls `belayer spawn` (same as today). Daemon spawns another subprocess. No need for "add an agent to a running bridge" RPC.

### Why no custom protocol

Two communication channels, both already exist:

**Bridge -> Daemon**: HTTP to the daemon's Unix socket API. The daemon already has endpoints for messages, events, artifacts, tools. The bridge is just another HTTP client, same as the CLI.

**Daemon -> Bridge**: stdin. The daemon writes JSON lines to the bridge's stdin. The bridge has a reader thread. Used only for interrupt (rare) and stop. Everything else the bridge pulls from the daemon via HTTP.

No JSON-RPC. No bridge-side socket. No `protocol.py`. The daemon's HTTP API is the protocol.

---

## What the bridge does

### 1. Registers coordination tools

Three tools on the AIAgent, matching Belayer's three primitives:

| Tool | Purpose | Daemon endpoint (already exists) |
|---|---|---|
| `belayer_send_message` | Agent-to-agent messages | `POST /sessions/{id}/messages` |
| `belayer_create_artifact` | Register durable outputs | `POST /sessions/{id}/artifacts` |
| `belayer_report_status` | Progress/blocked/done | `POST /sessions/{id}/events` |

Registration requires two steps because of Hermes internals:

```python
from tools import registry

# Step 1: Register handler in the process-global registry (makes dispatch work)
registry.register(
    name="belayer_send_message",
    toolset="belayer",
    schema=SEND_MESSAGE_SCHEMA,
    handler=make_send_message_handler(agent_id, session_id, daemon_socket),
)

# Step 2: Patch the agent's tool snapshot (makes the model see the tool)
# AIAgent snapshots tools into self.tools at construction time (run_agent.py:1019).
# The API call sends self.tools to the model. Without this patch, the model
# never discovers our tool. Dispatch works dynamically via the registry,
# but discovery is static per-agent.
agent.tools.append(SEND_MESSAGE_TOOL_DEF)
agent.valid_tool_names.add("belayer_send_message")
```

With one process per agent, the registry is private. No collision across agents.

Tool handlers call the daemon's existing HTTP API:

```python
def make_send_message_handler(agent_id, session_id, daemon_socket):
    def handler(args):
        resp = http_post(daemon_socket, f"/sessions/{session_id}/messages",
                         {"to": args["to"], "content": args["content"], "from": agent_id})
        return f"Message sent to {args['to']}" if resp.ok else f"Failed: {resp.text}"
    return handler
```

### 2. Forwards events to the daemon

The bridge wires Hermes callbacks on the AIAgent and POSTs events to the daemon's event log endpoint (`POST /sessions/{id}/events`).

| Hermes callback | Event type | What it tells the daemon |
|---|---|---|
| `tool_start_callback` | `bridge:tool_started` | Tool name + args preview |
| `tool_complete_callback` | `bridge:tool_completed` | Tool name + duration + result preview |
| `step_callback` | `bridge:step_completed` | Iteration count + tools used |
| `status_callback` | `bridge:status_change` | Lifecycle or context pressure |
| `clarify_callback` | `bridge:clarification_needed` | Agent has a question |
| (periodic, every 30s) | `bridge:heartbeat` | Agent is alive (replaces capture-pane polling) |
| `run_conversation()` returns | `bridge:finished` / `bridge:failed` | Agent completed or errored |

Events use the `bridge:` prefix to avoid collision with existing daemon event types (`agent_finished`, `agent_spawned`, etc.).

**Note on `run_conversation()` return values**: The return dict has ~24 different exit paths. Not all include `"interrupted"` or `"failed"` keys. The bridge must use `result.get("interrupted", False)` and `result.get("failed", False)`, and handle `partial=True` returns (treat as "re-enter with a nudge message").

### 3. Runs the outer conversation loop

Hermes owns the inner tool-calling loop (`run_conversation()`). The bridge owns the outer loop: checking for pending messages between turns, re-entering the conversation.

```python
def agent_loop(agent, agent_id, session_id, daemon_socket, stdin_queue):
    user_message = initial_message
    conversation_history = None

    while True:
        result = agent.run_conversation(
            user_message=user_message,
            conversation_history=conversation_history,
        )
        conversation_history = result.get("messages", [])

        # Check for urgent interrupt (delivered via stdin -> stdin_queue)
        if result.get("interrupted"):
            interrupt_msg = stdin_queue.get_nowait_or_none()
            if interrupt_msg:
                user_message = f"[Urgent from {interrupt_msg['from']}]: {interrupt_msg['content']}"
                continue

        # Check for pending non-urgent messages (pull from daemon)
        pending = fetch_pending_messages(daemon_socket, session_id, agent_id)
        if pending:
            user_message = format_messages(pending)
            continue

        # Check terminal states
        if result.get("completed") or result.get("failed"):
            post_event(daemon_socket, session_id, agent_id,
                       "bridge:finished" if result.get("completed") else "bridge:failed",
                       result)
            break

        # Partial/unknown return: nudge agent to continue
        user_message = "[System] Your previous turn ended without completion. Continue your work."
```

**Message delivery is pull-based by default.** Between turns, the bridge calls `GET /sessions/{id}/messages?for={agent_id}&after={last_seen}` on the daemon. No push needed for non-urgent messages. Agents finish their work, then read mail.

**Urgent delivery uses stdin.** For the rare case where the planner needs to abort a specialist mid-work, the daemon writes a JSON line to the bridge's stdin:

```json
{"type": "interrupt", "from": "planner", "content": "Stop. Requirements changed."}
```

The bridge has a reader thread on stdin. On receiving an interrupt command, it calls `agent.interrupt(message)` (public API at `run_agent.py:2923`), which sets the interrupt flag AND signals the per-thread tool interrupt to abort in-flight operations. The agent's `run_conversation()` exits, and the outer loop re-enters with the urgent message.

**Stop uses stdin too:**

```json
{"type": "stop"}
```

Bridge calls `agent.interrupt()`, waits for `run_conversation()` to exit (it persists session on the interrupt path), then exits cleanly.

---

## Daemon-to-bridge communication: stdin protocol

The daemon writes one JSON object per line to the bridge's stdin. Two command types:

```json
{"type": "interrupt", "from": "planner", "content": "Stop. Requirements changed."}
{"type": "stop"}
```

That's the entire protocol. The bridge reads lines from stdin on a background thread.

For everything else, the bridge is a client of the daemon's HTTP API. No bridge-side server.

### Broker integration

The daemon's current broker sends interrupts by invoking the handler twice: first with `\x03` (Ctrl+C, a tmux convention), then with the real message (`memory.go:196-204`). This tmux-specific behavior needs to change.

Two options:
- **(a)** Refactor `Broker.Interrupt()` to skip the `\x03` when transport is bridge. Cleanest.
- **(b)** Bypass the broker for interrupts entirely. The daemon calls bridge stdin directly. The broker only handles non-urgent `Send()`. Simpler, since non-urgent messages are pull-based anyway (the bridge fetches them via HTTP).

Option (b) is recommended. The broker's `Subscribe` / `Send` / `Interrupt` model was designed around tmux delivery. With bridge agents, the broker's role shrinks: it's just the persistence and routing layer for messages. Delivery is either pull-based (bridge polls) or push-via-stdin (daemon writes directly). The broker doesn't need to "deliver" to bridge agents at all.

---

## Crash recovery

### What Hermes persists

- **JSON session log** (`_save_session_log`): Incremental, after each tool iteration (`run_agent.py:10249`). Always current.
- **SQLite session DB** (`_flush_messages_to_session_db`): Written on exit paths only (completion, errors, interrupts). Lags during normal operation.

The bridge passes Hermes a shared `SessionDB` and relies on Hermes' normal
session creation/flush paths. `step_callback` is used for Belayer events and
transcript records, not for calling Hermes private persistence methods.

### Bridge process dies

1. Daemon detects child exit via subprocess monitoring
2. Daemon marks that agent's run as `blocked`, logs stderr
3. Daemon optionally restarts: spawns a new bridge subprocess with the same config + the Hermes session ID
4. New bridge loads conversation history from Hermes SQLite (same mechanism as `hermes --resume <id>`), reconstructs AIAgent with full config, re-registers Belayer tools, re-wires callbacks, resumes

```python
# Recovery: same as normal startup, but with conversation_history from SQLite
from hermes_state import SessionDB
db = SessionDB()
history = db.get_messages_as_conversation(hermes_session_id)
history = [m for m in history if m.get("role") != "session_meta"]

agent = AIAgent(model=model, quiet_mode=True, session_id=hermes_session_id,
                session_db=db, ...)
register_belayer_tools(agent, config)
wire_callbacks(agent, agent_id)

agent.run_conversation(
    user_message="[System] Process restarted. Review your prior work and continue.",
    conversation_history=history,
)
```

Other agents are unaffected (separate processes).

### Daemon dies

1. Coordination tool handlers get `ConnectionRefusedError` from daemon socket
2. Bridge wraps tool handlers: on failure, returns `"[System] Daemon unavailable. Continue local work (file editing, testing). Coordination tools are offline."` The agent sees this as a tool result and adapts.
3. Bridge keeps running. The agent continues doing local work.
4. When daemon restarts, bridge's next HTTP call succeeds. No special reconnection logic needed. The bridge is just an HTTP client that retries.

### Individual agent failure

`run_conversation()` catches unhandled exceptions, returns `{failed: True}`. Bridge POSTs `bridge:failed` event and exits. Other agents unaffected (separate processes). Daemon marks agent as `blocked`.

---

## What changes in the daemon

### Removed

- `tmux.Runner` and all tmux code (`internal/tmux/`)
- `defaultLaunchAgent` (builds Hermes CLI command, creates tmux session)
- `defaultDeliverMessage` (tmux send-keys with bracketed paste)
- `watchAgentExit` goroutine (polls tmux wait-for + finish marker)
- `watchAgentIdle` goroutine (polls capture-pane, diffs output)
- `normalizePaneForIdle`
- Finish marker file convention
- Capture-pane idle polling config (`idlePollInterval`, `idleTimeout`, `idleNudgeCooldown`)

### Added

- `internal/bridge/` package:
  - `bridge.Spawn(config) -> *Process`: spawns Python subprocess, returns handle with stdin pipe
  - `bridge.Process`: wraps `exec.Cmd` + stdin writer + exit monitoring
- Pending messages endpoint: `GET /sessions/{id}/messages?for={agent_id}&after={last_seen}` (if not already supported by existing messages endpoint)
- `AgentRun.HermesSessionID` column (for crash recovery; replaces semantic use of `TmuxSession`)

### Modified

- `handleSpawnAgent`: replaces tmux launch with `bridge.Spawn()`. The seven steps in the current flow (agents.go:31-117):
  1. Validate request, resolve workdir (stays)
  2. Create `store.AgentRun` with `Transport: "bridge"` (was `"tmux"`)
  3. Spawn bridge subprocess (replaces `d.launchAgent` / tmux)
  4. Store `HermesSessionID` (replaces `UpdateAgentRunTmuxSession`)
  5. `UpdateAgentRunStatus("running")` (stays)
  6. No broker subscription needed (bridge pulls messages via HTTP; daemon writes stdin for interrupts)
  7. Start exit monitoring goroutine on subprocess (replaces `watchAgentExit` + `watchAgentIdle`)

- Broker role shrinks: broker stores and routes messages, but doesn't "deliver" to bridge agents. The bridge pulls. For interrupt, daemon writes stdin directly, bypassing broker delivery.

- `handleFinishAgent` endpoint: still works (agents can still call `belayer finish` via CLI) but completion is also detected automatically when the bridge process exits after `run_conversation()` returns `completed=True`. Both paths update agent status. The bridge path is canonical; the CLI path is a fallback.

### Event type alignment

Bridge events use `bridge:` prefix. Daemon maps them to status transitions:

| Bridge event | Daemon action |
|---|---|
| `bridge:finished` | `UpdateAgentRunStatus("complete")` |
| `bridge:failed` | `UpdateAgentRunStatus("blocked")` |
| `bridge:step_completed` | Reset idle timer (replaces capture-pane) |
| `bridge:heartbeat` | Reset idle timer |

### Schema migration

Add `hermes_session_id` column to `agent_runs`. SQLite `ALTER TABLE` with idempotency guard:

```sql
-- Check if column exists before adding (SQLite doesn't support IF NOT EXISTS for columns)
ALTER TABLE agent_runs ADD COLUMN hermes_session_id TEXT NOT NULL DEFAULT '';
```

The `tmux_session` column stays for now (no migration infrastructure for column removal). It's just unused for bridge agents.

---

## Bridge structure

```
hermes_bridge/
  __main__.py       # Entry point: read env vars, construct AIAgent, run loop (~80 lines)
  tools.py          # Belayer tool registration (3 tools, ~60 lines)
  callbacks.py      # Hermes callbacks -> HTTP POST events (~40 lines)
  stdin_reader.py   # Background thread reading stdin for interrupt/stop (~30 lines)
```

~200 lines of Python. Lives in the Belayer repo. Imports from `hermes-agent` (AIAgent, registry, SessionDB). No third-party deps beyond Hermes itself.

---

## How specialist spawning works

Unchanged from today. The planner calls `belayer spawn` via the bash/code_execution tool:

```
belayer spawn --name api --role specialist --profile nightshift-api --workdir /workspace/repos/extend-api
```

The CLI hits `POST /sessions/{id}/agents`. The daemon's `handleSpawnAgent` spawns a new bridge subprocess. The planner doesn't know or care that the backend changed from tmux to a Python subprocess.

Config resolution: `name`, `role`, `profile`, `workdir` come from the caller (planner via CLI), same as today. Model and system prompt come from the Hermes profile, same as today. The bridge doesn't add new config knobs.

---

## Migration path

### Phase 1: Single-agent bridge

Python package. Daemon spawns one bridge subprocess for the planner. Bridge constructs AIAgent, registers tools, wires callbacks, runs conversation loop. Daemon monitors subprocess, receives events via HTTP.

**Validates**: AIAgent runs headlessly, coordination tools work, events flow to daemon.

### Phase 2: Multi-agent coordination

Planner spawns specialists via `belayer spawn`. Daemon spawns additional bridge subprocesses. Test planner -> specialist -> planner message flow through the session bus.

**Validates**: agents communicate through Belayer. The pull-based message delivery works.

### Phase 3: Delete tmux

Remove `internal/tmux/`, tmux-specific daemon code, finish markers, capture-pane polling. Update ARCHITECTURE.md.

---

## Risks

### Hermes API changes
Bridge is ~200 lines wrapping Hermes internals. When Hermes changes, we update
the wrapper. Belayer requires Hermes 0.12+ and relies on the public
`session_db` constructor contract for persistence.

### Memory overhead
Each Python process is ~50-100MB. Three agents = 150-300MB. For a Nightshift worker on a dedicated machine, this is negligible. If it becomes a problem, we can share a Python process (the original ThreadPoolExecutor model), but we'd need to solve the registry collision problem.

### Message delivery latency
Non-urgent messages wait for the current turn. Could be minutes if the agent is deep in a tool loop. This is intentional. Interrupt is the escape hatch for truly urgent cases.

### Startup latency
Each agent spawn = new Python process = 1-2 seconds. For a run that takes minutes to hours, negligible. Agents are spawned sequentially by the planner anyway, not in a burst.

---

## What this does not cover

- **Clamshell sandbox**: orthogonal. Bridge runs inside or outside, same pattern.
- **Outer worker control plane**: Nightshift worker talks to daemon HTTP API regardless.
- **Agent identity/profiles**: Hermes profiles, configured at spawn time. Bridge applies them at AIAgent construction.
- **Memory persistence**: Hermes handles it.
- **Generic Harness interface**: dropped. One runtime, one integration.
- **A2A protocol**: not needed inside a single worker run. If we need it later, the daemon's HTTP API is the adaptation point.
