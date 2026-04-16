# Agent Architecture

How agents communicate, coordinate, and stay alive inside a Belayer run.

---

## Agent toolbox

Every Hermes agent starts with a base set of tools from the harness (file editing, bash, search, etc.). Belayer injects seven additional tools via the bridge at spawn time. These are the coordination primitives that let agents talk to each other through the session bus instead of through raw terminal output.

```mermaid
flowchart TB
    subgraph hermes["Hermes Base Tools (built-in to harness)"]
        direction LR
        Read
        Write
        Edit
        Bash
        Grep
        Glob
        Agent
        WebSearch
        WebFetch
        Notebook
        TodoWrite
        TaskCreate
    end

    subgraph belayer_tools["Belayer Coordination Tools (injected at spawn)"]
        direction LR
        send["belayer_send_message\nto · content"]
        artifact["belayer_create_artifact\nkind · path · summary?"]
        status["belayer_report_status\nstatus · detail?"]
        spawn["belayer_spawn_agent\nname · profile · message · branch?"]
    end

    subgraph completion_tools["Completion Gate Tools (injected at spawn)"]
        direction LR
        request["belayer_request_completion\nsummary · spec_artifact?"]
        approve["belayer_approve_completion\nverification_report"]
        reject["belayer_reject_completion\nverification_report · gap_list"]
    end

    style hermes fill:#161b22,stroke:#30363d,color:#c9d1d9
    style belayer_tools fill:#1a3a2a,stroke:#3fb950,color:#c9d1d9
    style send fill:#1a3a2a,stroke:#3fb950,color:#c9d1d9
    style artifact fill:#1a3a2a,stroke:#3fb950,color:#c9d1d9
    style status fill:#1a3a2a,stroke:#3fb950,color:#c9d1d9
    style spawn fill:#1a3a2a,stroke:#3fb950,color:#c9d1d9
    style completion_tools fill:#1a2a3a,stroke:#58a6ff,color:#c9d1d9
    style request fill:#1a2a3a,stroke:#58a6ff,color:#c9d1d9
    style approve fill:#1a2a3a,stroke:#58a6ff,color:#c9d1d9
    style reject fill:#1a2a3a,stroke:#58a6ff,color:#c9d1d9
```

### Tool details

| Tool | Parameters | What it does |
|------|-----------|--------------|
| `belayer_send_message` | `to` (agent name), `content` (string) | Direct message to another agent via the session bus |
| `belayer_create_artifact` | `kind`, `path`, `summary?` | Register a durable output (contract, report, task-graph, etc.) |
| `belayer_report_status` | `status` (working/blocked/done/needs-review), `detail?` | Publish lifecycle state to the session bus |
| `belayer_spawn_agent` | `name`, `profile`, `message`, `branch?` | Dynamically spawn a specialist agent (supervisor only) |
| `belayer_request_completion` | `summary`, `spec_artifact?` | Signal work is done, trigger PM verification (supervisor only) |
| `belayer_approve_completion` | `verification_report` | Approve the run after spec verification (PM only) |
| `belayer_reject_completion` | `verification_report`, `gap_list` | Reject the run with gaps for supervisor to fix (PM only) |

The base Hermes tools let an agent do work (read files, write code, run commands). The Belayer coordination tools let an agent coordinate (send messages, register outputs, report status, spawn teammates). The completion gate tools enforce spec verification before a run can close.

Each agent template declares which belayer tools it receives via the `belayer_tools` field in `agent.yaml`. Three baseline tools (send_message, report_status, create_artifact) are always registered. Role-specific tools are only available to agents whose templates declare them. The supervisor can spawn agents and request completion. Only the PM can approve or reject a run. This is enforced at registration time — agents never see tools they aren't authorized to use.

Agents never communicate by writing to shared files or reading each other's terminal output. All coordination flows through Belayer's session bus.

---

## The Belayer daemon: session bus

The daemon is a Go process running on a Unix socket. It owns the session state and routes all inter-agent communication.

Every run has two mandatory agents — the **supervisor** and the **PM** — plus zero or more specialist agents spawned by the supervisor at runtime. The supervisor orchestrates work; the PM gates completion. Everything else is dynamic.

```mermaid
flowchart TB
    subgraph daemon["Belayer Daemon (Go, Unix socket)"]
        direction TB
        store[("SQLite\nsessions, agent_runs,\nmessages, events, artifacts")]
        broker["Message Broker\n(debounce, fan-out)"]
        events["Event Log\n(append-only)"]
        roster["Agent Roster\n(name, status, transport)"]
    end

    subgraph supervisor_box["Supervisor (non-ephemeral, always-on)"]
        supervisor_hermes["Hermes + Bridge"]
        supervisor_tools["Base tools + Belayer coordination\n+ spawn + request_completion"]
    end

    subgraph pm_box["PM (ephemeral, auto-spawned by daemon)"]
        pm_hermes["Hermes + Bridge"]
        pm_tools["Base tools + Belayer baseline\n+ approve/reject_completion"]
    end

    subgraph specialists["Specialist 1..N (ephemeral, spawned by supervisor)"]
        spec_hermes["Hermes + Bridge"]
        spec_tools["Base tools + Belayer baseline"]
    end

    supervisor_hermes <-->|"stdin/stdout JSON\n+ HTTP over Unix socket"| daemon
    pm_hermes <-->|"stdin/stdout JSON\n+ HTTP over Unix socket"| daemon
    spec_hermes <-->|"stdin/stdout JSON\n+ HTTP over Unix socket"| daemon
```

The supervisor is always the first agent spawned. It reads the spec, decomposes work, and spawns specialists (frontend, backend, qa, reviewer, etc.) as needed. The PM is never spawned by the supervisor — the daemon auto-spawns it when the supervisor calls `belayer_request_completion`, creating an adversarial verification step that the supervisor cannot skip.

### What the daemon owns

| Concern | Storage | Access |
|---------|---------|--------|
| Sessions | SQLite `sessions` table | `POST /sessions`, `GET /sessions/{id}` |
| Agent roster | SQLite `agent_runs` table | `POST /sessions/{id}/agents`, `GET /sessions/{id}/agents` |
| Messages | SQLite `messages` table + in-memory broker | `POST /sessions/{id}/messages`, `GET /sessions/{id}/messages` |
| Events | SQLite `session_events` table | `POST /sessions/{id}/events`, `GET /sessions/{id}/events` |
| Artifacts | SQLite `artifacts` table | `POST /sessions/{id}/artifacts`, `GET /sessions/{id}/artifacts` |

---

## Message delivery

Two delivery paths depending on urgency:

### Non-urgent (pull-based)

Most messages. The agent picks them up between conversation turns.

```mermaid
sequenceDiagram
    participant A as Specialist A
    participant D as Daemon
    participant P as Supervisor

    A->>D: POST /messages {to: "supervisor", content: "done"}
    Note over D: Store in messages table,<br/>log message_sent event
    Note over P: ...finishes current turn...
    P->>D: GET /messages?for=supervisor&pending=true
    D->>P: [{from: "A", content: "done"}]
    Note over D: Mark as delivered
    Note over P: Inject as next user turn
```

The broker debounces non-urgent messages with a 2-second window. If a specialist fires three messages in quick succession, the supervisor sees them coalesced into one delivery. Reduces interruption noise during fast agent turns.

### Urgent (push via stdin)

For when an agent is blocked or the supervisor needs to redirect a specialist mid-turn.

```mermaid
sequenceDiagram
    participant P as Supervisor
    participant D as Daemon
    participant A as Specialist A

    P->>D: POST /messages {to: "A", content: "stop, wrong branch", interrupt: true}
    D->>A: stdin: {"type": "interrupt", "from": "supervisor", "content": "stop, wrong branch"}
    Note over A: Bridge calls agent.interrupt()<br/>Current LLM turn halts
    Note over A: Message injected as<br/>next user turn
    A->>D: POST /events {type: "bridge:status_change"}
```

The daemon writes directly to the bridge subprocess's stdin pipe. The bridge's `StdinReader` thread picks it up, queues it, and calls `agent.interrupt()` to halt the current LLM generation. The interrupted turn returns immediately, and the urgent message becomes the next user turn.

---

## Agent lifecycle: supervisor vs. specialists

Two distinct lifecycle models exist inside a single run:

### The supervisor (non-ephemeral, always-on)

The supervisor stays alive for the entire run. It is the only agent that can spawn other agents. When a specialist finishes and reports back, the supervisor is already there, waiting.

```mermaid
stateDiagram-v2
    [*] --> Starting: belayer spawn (non-ephemeral)
    Starting --> Running: bridge:started
    Running --> Idle: Task done, waiting for specialists
    Idle --> Running: Specialist reports in (message or interrupt)
    Idle --> Incomplete: Idle timeout (5 min), no response
    Running --> Complete: belayer finish → PM approval
    Running --> Blocked: Unrecoverable error
    Running --> Incomplete: Agent decides it cannot finish
    Idle --> Complete: Stop command
```

When the supervisor completes a task, it enters an **idle loop**: polling every 5 seconds for new messages, listening on stdin for interrupts. If a specialist sends a message, the supervisor wakes up and continues. If nothing happens for 5 minutes, the supervisor exits as **incomplete** — this is not a successful completion, it means no specialist reported back.

The **incomplete** state is distinct from both **complete** (work finished) and **blocked** (unrecoverable error). It means the agent made progress but could not finish — either due to idle timeout, getting stuck in a loop, or making a deliberate decision to escalate. When the supervisor reports incomplete, the daemon transitions the session to `needs_human_review`. Any agent can report incomplete via `belayer_report_status(status="incomplete")`.

The supervisor doesn't burn tokens while idling. It's not running inference. It's a Python process sleeping in a poll loop, ready to resume the Hermes conversation the moment work arrives.

### Specialists (ephemeral, spawn-and-complete)

Specialists are spawned by the supervisor for a specific task. They do the work, report back, and exit.

```mermaid
stateDiagram-v2
    [*] --> Starting: supervisor calls belayer_spawn_agent
    Starting --> Running: bridge:started
    Running --> Complete: Task done (bridge:finished)
    Running --> Blocked: Error or exit without finish
    Running --> Incomplete: Cannot finish, escalates to human
```

Specialists are ephemeral by default (`ephemeral=true`). When their task is done, the bridge posts `bridge:finished` and the process exits. If a specialist gets stuck, it can report `incomplete` to escalate — the daemon logs an `agent_escalated` event and the supervisor is notified.

But their **names persist**. The `agent_runs` table keeps the row. If the supervisor needs to assign more work to the same role, it spawns with the same name. The daemon detects the prior run, carries over the `HermesSessionID`, and the specialist resumes with its full conversation history.

```
First spawn:   supervisor → belayer_spawn_agent(name="api", message="implement POST /items")
                         → agent_runs row created, HermesSessionID saved on bridge:started
                         → specialist works, finishes, process exits

Second spawn:  supervisor → belayer_spawn_agent(name="api", message="now add validation")
                         → daemon finds prior row for "api", carries over HermesSessionID
                         → specialist resumes with full context from first assignment
```

This gives you persistent identity without persistent processes. The specialist sleeps (no process running, no tokens burning), but its name, conversation history, and worktree are preserved for reassignment.

---

## Telemetry: the event stream

Every significant thing that happens inside a run posts an event to the daemon's event log. This is the primary observability surface and the mechanism that enables session resume.

### Event flow

```mermaid
flowchart LR
    subgraph bridge["Bridge Process (Python)"]
        callbacks["Hermes Callbacks\n(tool_start, tool_complete,\nstep, status, clarify)"]
        lifecycle["Lifecycle Events\n(started, finished,\nfailed, idle)"]
        heartbeat["Heartbeat Thread\n(every 30s)"]
    end

    subgraph daemon["Daemon"]
        events_api["POST /sessions/{id}/events"]
        event_store[("Event Log\n(SQLite)")]
        side_effects["Side Effects\n(status update,\nmessage routing)"]
    end

    subgraph consumers["Consumers"]
        crag["Crag Web UI\n(dashboard, run detail)"]
        cli["belayer events\n(CLI tail)"]
        supervisor_obs["Supervisor\n(reads via messages)"]
    end

    callbacks --> events_api
    lifecycle --> events_api
    heartbeat --> events_api
    events_api --> event_store
    events_api --> side_effects
    event_store --> crag
    event_store --> cli
    side_effects --> supervisor_obs
```

### Event types

Hermes callbacks are wired onto the agent instance at construction time. They fire automatically as the agent works:

| Event | Source | Payload | Side Effect |
|-------|--------|---------|-------------|
| `bridge:started` | Bridge main | `{agent, hermes_session_id, role, profile}` | Saves HermesSessionID for resume |
| `bridge:finished` | Bridge main | `{agent, reason?, final_response?}` | Sets agent status to `complete` |
| `bridge:failed` | Bridge main | `{agent, error}` | Sets agent to `blocked`, urgent message to supervisor |
| `bridge:idle` | Bridge main | `{agent, final_response?}` | Informational (supervisor entering idle loop) |
| `bridge:heartbeat` | Heartbeat thread | `{agent}` | Liveness signal (every 30s) |
| `bridge:step_completed` | Step callback | `{agent, step}` | Tracks conversation turn count |
| `bridge:tool_started` | Tool start callback | `{agent, tool, input_preview}` | Audit: what tool, what input |
| `bridge:tool_completed` | Tool complete callback | `{agent, tool, duration_ms, result_preview}` | Audit: how long, what result |
| `bridge:status_change` | Status callback | `{agent, status_type}` | Informational |
| `bridge:clarification_needed` | Clarify callback | `{agent, question}` | Routes question to supervisor |
| `bridge:turn_usage` | Bridge main (after each turn) | `{agent, input_tokens, output_tokens, cache_read_tokens, estimated_cost_usd, cost_status}` | Per-turn token/cost tracking |
| `bridge:session_usage` | Bridge main (on exit) | `{agent, session_total_tokens, session_estimated_cost_usd, session_api_calls}` | Cumulative session totals |
| `bridge:completion_requested` | Request completion tool | `{agent, summary, spec_artifact?}` | Auto-spawns PM agent for verification |
| `bridge:completion_approved` | Approve completion tool | `{agent, verification_report}` | Marks session complete, registers report artifact |
| `bridge:completion_rejected` | Reject completion tool | `{agent, verification_report, gap_list}` | Sends gap list to supervisor, tracks rejection cycle |

Daemon-internal events (not from bridge):

| Event | Source | Side Effect |
|-------|--------|-------------|
| `message_sent` | Message handler | Audit trail |
| `message_delivered` | Broker | Delivery confirmation |
| `agent_spawned` | Spawn handler | Roster update |
| `agent_finished` | Finish handler | Status update |
| `agent_exited_without_finish` | Exit watcher | Marks agent `blocked` |
| `agent_escalated` | Status event handler | Agent reported `incomplete`, logged for monitoring |
| `artifact_created` | Artifact handler | Registry update |
| `session_created` | Session handler | — |
| `session_completed` | Completion approved handler | Session status → complete |
| `session_stalled` | Bridge finished/failed handler | All agents exited without completion → session status → stalled |
| `warning:supervisor_exited_early` | Bridge finished handler | Supervisor exited while specialists still running |
| `completion_rejected` | Completion rejected handler | Tracks cycle count |
| `completion_escalated` | Rejection limit handler | Session status → needs_human_review |
| `pm_spawn_failed` | PM spawn error | Notifies supervisor to retry |
| `pm_spawn_failed` | PM spawn error | Notifies supervisor to retry |

### How telemetry enables resume

The `bridge:started` event is the key. When a bridge process starts, it posts its `hermes_session_id` to the daemon. The daemon persists this in `agent_runs.hermes_session_id`.

When the supervisor re-spawns a specialist with the same name:

1. Daemon looks up prior `AgentRun` for that name
2. Finds `HermesSessionID` from the last `bridge:started` event
3. Passes it to the new bridge process via `BELAYER_HERMES_SESSION_ID` env var
4. Bridge constructs the Hermes agent with `--resume <session_id>`
5. Agent picks up with full conversation history, tool results, and context

This also enables crash recovery. If a bridge process dies unexpectedly:

1. Exit watcher detects process death without `bridge:finished` event
2. Marks agent `blocked`
3. Sends urgent message to supervisor: "agent X exited without finishing"
4. Supervisor can re-spawn with same name, daemon carries over `HermesSessionID`
5. Agent resumes from where it crashed

The heartbeat thread provides the liveness signal. If heartbeats stop arriving for an agent that's supposedly `running`, something has gone wrong. This is the daemon's dead-man switch.

---

## Spawn flow: end to end

A complete picture of what happens when the supervisor spawns a specialist:

```mermaid
sequenceDiagram
    participant P as Supervisor (Hermes)
    participant D as Daemon (Go)
    participant B as Bridge (Python)
    participant S as Specialist (Hermes)

    P->>D: belayer_spawn_agent(name="api", profile="...", message="...", branch="feat/x")
    Note over D: 1. Check for prior AgentRun "api"<br/>2. Carry over HermesSessionID if exists<br/>3. Create git worktree if branch specified<br/>4. Create run directory<br/>5. Insert/update agent_runs row (status: starting)

    D->>B: Start Python subprocess<br/>(env: BELAYER_*, HERMES_*)
    Note over D: Update status → running<br/>Log agent_spawned event<br/>Start exit watcher

    B->>B: Construct AIAgent<br/>Register Belayer tools<br/>Wire callbacks<br/>Start stdin reader<br/>Start heartbeat thread

    B->>D: POST bridge:started {hermes_session_id: "..."}
    Note over D: Persist HermesSessionID

    B->>S: agent.run_conversation(message)
    Note over S: Agent works...<br/>Uses base tools + Belayer tools

    loop Every tool call
        S->>D: bridge:tool_started {tool, input}
        Note over S: Execute tool...
        S->>D: bridge:tool_completed {tool, duration, result}
    end

    loop Every 30s
        B->>D: bridge:heartbeat
    end

    S->>D: belayer_send_message(to="supervisor", content="done, PR #42 ready")
    Note over D: Store message, log event

    S->>D: bridge:finished {final_response: "..."}
    Note over D: Set agent status → complete

    Note over P: ...idle, polling...
    P->>D: GET /messages?for=supervisor&pending=true
    D->>P: [{from: "api", content: "done, PR #42 ready"}]
    Note over P: Resume conversation,<br/>process specialist report
```

---

## Bridge architecture

The bridge is the process boundary between Go (daemon) and Python (Hermes). Each agent gets its own bridge process.

```mermaid
flowchart TB
    subgraph bridge_proc["Bridge Process (Python, one per agent)"]
        direction TB
        stdin_reader["StdinReader Thread\n(reads daemon commands:\ninterrupt, stop)"]
        main_loop["Main Loop\n(outer conversation driver)"]
        hermes_agent["Hermes AIAgent\n(LLM conversation,\ntool execution)"]
        callbacks_mod["Callbacks Module\n(tool_start, tool_complete,\nstep, status, clarify)"]
        heartbeat_thread["Heartbeat Thread\n(30s periodic ping)"]
        tools_mod["Belayer Tools\n(send_message, create_artifact,\nreport_status, spawn_agent,\nrequest/approve/reject_completion)"]

        stdin_reader -->|"interrupt queue"| main_loop
        main_loop -->|"user_message"| hermes_agent
        hermes_agent -->|"result"| main_loop
        callbacks_mod -.->|"wired via setattr"| hermes_agent
        tools_mod -.->|"registered in tool registry"| hermes_agent
    end

    subgraph daemon_side["Daemon (Go)"]
        stdin_pipe["stdin pipe\n(interrupt/stop)"]
        http_api["HTTP API\n(Unix socket)"]
    end

    stdin_pipe -->|"JSON lines"| stdin_reader
    tools_mod -->|"HTTP POST"| http_api
    callbacks_mod -->|"HTTP POST"| http_api
    heartbeat_thread -->|"HTTP POST"| http_api
    main_loop -->|"HTTP GET (poll)"| http_api
```

### Two communication channels

| Channel | Direction | Transport | Used for |
|---------|-----------|-----------|----------|
| **stdin pipe** | Daemon → Bridge | Newline-delimited JSON | Urgent interrupts, stop commands |
| **Unix socket HTTP** | Bridge → Daemon | HTTP over Unix socket | Tool calls, events, message polling |

The stdin pipe is push (daemon writes when it needs to interrupt). The HTTP channel is pull (bridge calls when it has something to send or needs to check for messages).

---

## Worktree isolation

When the supervisor spawns a specialist with a `branch` parameter, the daemon creates a git worktree:

```
repo/
├── .belayer/
│   └── worktrees/
│       ├── api/          ← specialist A's isolated checkout
│       └── frontend/     ← specialist B's isolated checkout
├── src/                  ← supervisor works on main branch
└── ...
```

Each specialist gets its own filesystem checkout on its own branch. They can make commits without conflicting with each other or the supervisor. The supervisor can later merge branches or create PRs from them.

Worktree path: `<repoRoot>/.belayer/worktrees/<agentName>`

If no branch is specified, the specialist works in the same directory as the supervisor (shared workdir). This is simpler but means only one agent should be writing code at a time.

---

## Putting it together

The full picture of a Nightshift run:

```
Crag (always-on daemon, owns queue + targets + web UI)
│
├── submits request to available target
│
└── Target directory: ~/Projects/my-app
    │
    ├── Belayer daemon (Go, Unix socket, one per run)
    │   ├── SQLite store (sessions, agents, messages, events, artifacts)
    │   ├── Message broker (debounce, fan-out, urgent interrupt)
    │   └── Bridge process manager (spawn, monitor, stdin pipes)
    │
    ├── Supervisor (non-ephemeral)
    │   ├── Hermes agent (Opus, profile: nightshift-supervisor)
    │   ├── Base tools (Read, Write, Edit, Bash, Grep, Glob, ...)
    │   ├── Belayer tools (send_message, create_artifact, report_status, spawn_agent)
    │   ├── Completion tool: belayer_request_completion (signals "work done, verify")
    │   ├── Callbacks → daemon event log
    │   └── Lifecycle: running → idle (waiting) → running → ... → request completion
    │
    ├── Specialist "api" (ephemeral, spawned by supervisor)
    │   ├── Hermes agent (Sonnet, profile: nightshift-api)
    │   ├── Git worktree: .belayer/worktrees/api/ (branch: feat/api-items)
    │   ├── Base tools + Belayer tools (no spawn_agent)
    │   ├── Callbacks → daemon event log
    │   ├── Lifecycle: running → complete (process exits)
    │   └── Name persists in roster for re-assignment with session resume
    │
    ├── PM "pm" (ephemeral, auto-spawned by daemon on completion request)
    │   ├── Hermes agent (Sonnet, profile: default)
    │   ├── Completion tools: belayer_approve_completion, belayer_reject_completion
    │   ├── Reads spec artifact, git diff, artifact registry
    │   ├── Lifecycle: spawned → verify → approve (session complete) or reject (gaps to supervisor)
    │   └── Bounded: max 3 rejection cycles before escalating to human
    │
    └── Specialist "qa" (ephemeral, spawned by supervisor)
        ├── Hermes agent (Sonnet, profile: nightshift-qa)
        ├── Same workdir as supervisor (no worktree, reads but doesn't write code)
        ├── Base tools + Belayer tools (no spawn_agent)
        ├── Callbacks → daemon event log
        └── Lifecycle: running → complete
```

The supervisor orchestrates. Specialists execute. The PM verifies. The daemon routes. Events record everything. Names persist across spawns. Sessions resume from where they left off.

---

## Cost observability

Hermes exposes per-turn token usage and cost estimates from `run_conversation()`. The bridge posts these as events so the daemon can aggregate per-agent costs and Crag can display per-run cost breakdowns.

### Data available from Hermes

Per turn (from `run_conversation()` return dict):

| Field | Type | Description |
|-------|------|-------------|
| `input_tokens` | int | Non-cached input tokens |
| `output_tokens` | int | Completion tokens |
| `cache_read_tokens` | int | Tokens served from prompt cache |
| `cache_write_tokens` | int | Tokens written to prompt cache |
| `reasoning_tokens` | int | Extended thinking tokens |
| `total_tokens` | int | Total (prompt + completion) |
| `api_calls` | int | Number of API calls this turn |
| `estimated_cost_usd` | float | USD cost estimate |
| `cost_status` | string | "actual", "estimated", "included", or "unknown" |

Per session (from `agent` instance properties):

| Property | Description |
|----------|-------------|
| `agent.session_total_tokens` | Cumulative tokens across all turns |
| `agent.session_estimated_cost_usd` | Cumulative cost estimate |
| `agent.session_api_calls` | Total API calls |

### Event flow

```mermaid
flowchart LR
    subgraph bridge["Bridge (per agent)"]
        turn["run_conversation()\nreturns usage data"]
        exit["Agent exit\nsession totals"]
    end

    subgraph daemon["Daemon"]
        turn_event["bridge:turn_usage\n(per turn, per agent)"]
        session_event["bridge:session_usage\n(on exit, per agent)"]
        store[("Event log\naggregate by agent")]
    end

    subgraph crag["Crag Web UI"]
        breakdown["Cost breakdown\nby agent, by run"]
    end

    turn --> turn_event --> store
    exit --> session_event --> store
    store --> breakdown
```

### What this enables

- **Per-agent cost breakdown**: see that the supervisor used $0.80 (Opus, orchestration overhead) while the implementer used $2.10 (Sonnet, heavy code generation)
- **Per-run total cost**: sum across all agents for one Nightshift run
- **Cost trending**: Crag can show cost-per-run over time, catch cost regressions
- **Budget enforcement**: future feature, Crag could set cost limits per run and pause/escalate when approaching them

### Implementation status

Wired in `hermes_bridge/__main__.py`. After each `run_conversation()` call, the bridge posts `bridge:turn_usage` with per-turn token counts and cost. On exit, it posts `bridge:session_usage` with cumulative totals from the `agent.session_*` properties. The daemon stores these in the event log like any other bridge event. Aggregation in Crag is not yet built.

---

## The completion gate: Product Manager agent

See [PM Agent design doc](design-docs/2026-04-16-product-manager-agent.md) for the full design rationale.

The supervisor and specialists have a structural bias toward reporting "done." Nobody in the roster has the incentive to say "wait, you skipped half the spec." The PM agent fixes this.

### How it works (implemented)

```mermaid
flowchart LR
    supervisor["Supervisor:\ncalls belayer finish"]
    daemon["Daemon:\nintercepts finish,\ntriggers PM gate"]
    pm["PM Agent:\nreads spec, reads diff,\nverifies line by line"]
    approve["belayer_approve_completion\n→ session marked complete"]
    reject["belayer_reject_completion\n→ gap list to supervisor"]

    supervisor --> daemon
    daemon -->|"auto-spawns PM"| pm
    pm -->|"APPROVE"| approve
    pm -->|"REJECT"| reject
    reject -->|"fix gaps, try again"| supervisor

    style pm fill:#1a2a3a,stroke:#58a6ff,color:#c9d1d9
```

### The flow

1. **Supervisor signals completion**: calls `belayer finish "summary"` (CLI) or `belayer_request_completion(summary="...")` (bridge tool). Both paths converge in the daemon, which intercepts the finish and triggers the PM gate instead of marking the session complete.

2. **Daemon auto-spawns PM**: the event handler in `bridge_events.go` looks up the spec artifact (by kind: `spec` or `design-doc`), gathers the artifact registry, and spawns the PM via the bridge with a message containing full context. The PM's system prompt is loaded from `templates/pm/system-prompt.md` and injected as `ephemeral_system_prompt`.

3. **PM verifies**: reads the spec, reads the git diff, walks through the spec line by line. Produces a structured verification report (Passed / Failed / Deferred).

4. **PM decides**:
   - **APPROVE**: calls `belayer_approve_completion(verification_report="...")`. Daemon registers the report as an artifact, marks the session status as `complete`, and logs `session_completed`.
   - **REJECT**: calls `belayer_reject_completion(verification_report="...", gap_list="...")`. Daemon sends the gap list to the supervisor as an urgent message. Supervisor addresses gaps and calls `belayer finish` again.

5. **Bounded cycles**: after 3 rejections, the daemon marks the session as `needs_human_review` and sends an escalation message to the supervisor. No more automated retries.

### Key design decisions

- **The PM controls run completion, not the supervisor.** The supervisor calls `belayer finish`, but the daemon intercepts it and spawns the PM for verification. The PM calls `belayer_approve_completion` to actually close the session. There is no way for the supervisor to directly complete the run.
- **The daemon enforces the gate.** The PM is auto-spawned by the daemon when the supervisor finishes. The supervisor can't skip or forget the gate.
- **The spec is the source of truth, not the supervisor's summary.** The PM reads the original spec artifact directly. It receives the supervisor's summary for context but verifies against the spec.
- **Tool access is declared in agent templates.** Each template's `agent.yaml` declares which belayer tools that role receives. The supervisor gets spawn and request_completion. The PM gets approve and reject. Specialists get baseline only. This is enforced at bridge registration time.
- **PM is ephemeral.** It spawns, verifies, decides, and exits. If rejected, a new PM process spawns on the next `belayer finish` call.
- **PM identity lives in `templates/pm/`.** The system prompt is injected via Hermes's `ephemeral_system_prompt` at spawn time. The Hermes profile stays `default` for now.

> **TODO: Hermes profile bootstrap.** Currently all bridge agents use the `default` Hermes profile, with identity injected via `ephemeral_system_prompt` and model overridden via `BELAYER_MODEL`. This works for local testing because every agent shares the machine's auth context. But a Hermes profile controls more than the soul: provider selection, API keys, OAuth token state, model routing, skills, plugins, and MCP server configs. When agents need different providers (e.g. PM on Anthropic sonnet, implementer on OpenAI codex) or deploy to Crag where there's no interactive `hermes auth`, the default profile can't cover it. Belayer needs a way to construct or materialize per-agent Hermes profiles at spawn time, either from `templates/` declarations or from daemon-held credential sets.

### Implementation files

| File | What it does |
|------|--------------|
| `templates/pm/` | PM identity: `agent.yaml`, `system-prompt.md`, `agents.md` |
| `hermes_bridge/tools.py` | Tool schemas and handlers for `request_completion`, `approve_completion`, `reject_completion` |
| `hermes_bridge/__main__.py` | Reads `BELAYER_SYSTEM_PROMPT` and injects as `ephemeral_system_prompt` |
| `internal/daemon/bridge_events.go` | Event handlers: `handleBridgeCompletionRequested`, `handleBridgeCompletionApproved`, `handleBridgeCompletionRejected` |
| `internal/daemon/agents.go` | `spawnAgentInternal` for auto-spawning PM; `handleFinishAgent` intercepts supervisor finish to trigger PM gate; template system prompt resolution from `templates/<name>/system-prompt.md` |
| `internal/bridge/bridge.go` | `Config.SystemPrompt` field, passed as `BELAYER_SYSTEM_PROMPT` env var |
