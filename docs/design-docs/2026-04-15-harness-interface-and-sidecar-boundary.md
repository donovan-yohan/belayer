---
status: proposed
created: 2026-04-15
supersedes:
  - Layer 3 (tmux transport adapter) of 2026-04-15-belayer-run-model-for-nightshift-v1.md
  - Partially refines 2026-04-15-headless-hermes-daemon-architecture.md (preserves intent, changes ownership model)
implemented-by:
consulted-learnings:
  - docs/design-docs/2026-04-15-headless-hermes-daemon-architecture.md
  - docs/design-docs/2026-04-15-belayer-run-model-for-nightshift-v1.md
  - docs/ARCHITECTURE.md
  - docs/PHILOSOPHY.md
  - internal/tmux/tmux.go (current Runner interface)
  - internal/broker/broker.go (current Broker interface)
  - internal/daemon/agents.go (current launch/deliver/watch code)
---

# Harness Interface and Sidecar Boundary

This document specifies the Go-side interface between Belayer's daemon and any agent runtime. It preserves the headless Hermes architecture's intent (structured communication, callbacks, tool injection) while keeping the Go daemon as the outer process that owns the session bus.

The headless Hermes doc proposes rewriting the daemon in Python. This doc proposes the opposite: keep the Go daemon, add a `Harness` interface, and implement the Hermes integration as a Python sidecar subprocess managed by Go.

---

## Why this ordering matters

Belayer's value is the session bus: messages, events, artifacts, roster, and the coordination model around them. The harness (Hermes today, possibly something else tomorrow) is the execution engine that runs agents. These are different concerns with different rates of change.

If the daemon is Python and tightly coupled to Hermes, swapping to a different runtime (A2A endpoint, Claude Code via ACP, a future Anthropic harness) means rewriting the daemon. If the daemon is Go with a pluggable interface, swapping runtimes means writing a new `Harness` implementation.

Hermes is MIT-licensed, actively developed, and its API changes across versions. Belayer should depend on Hermes's capabilities without being locked to its process model.

---

## The interface

### Harness

```go
// package harness

// Harness spawns and manages agent instances for a specific runtime.
// Implementations: TmuxHarness (current), HermesSidecar (proposed), A2AHarness (future).
type Harness interface {
    // Spawn starts an agent and returns a handle for interaction.
    // The handle is valid until the agent finishes or is killed.
    Spawn(ctx context.Context, cfg SpawnConfig) (AgentHandle, error)

    // Close shuts down the harness and all agents it manages.
    // Called once during daemon shutdown.
    Close() error
}
```

### SpawnConfig

```go
// SpawnConfig is everything the harness needs to start an agent.
// Runtime-agnostic: no tmux session names, no Python class references.
type SpawnConfig struct {
    // Identity
    SessionID string
    AgentID   string // logical name: "planner", "api-specialist"
    Role      string
    Profile   string // Hermes profile name (harness interprets this)

    // Workspace
    Workdir    string
    RepoScope  string
    RunDir     string // .belayer/runs/{session}/{agent}
    SocketPath string // daemon socket for callback registration

    // Coordination tools the agent should have access to.
    // The harness is responsible for making these available to the agent
    // in whatever form the runtime supports (injected tools, CLI, etc.)
    Tools []ToolSpec

    // Extra environment variables to pass through.
    ExtraEnv map[string]string
}
```

### AgentHandle

```go
// AgentHandle is the daemon's interface to a running agent.
// All methods are safe to call from any goroutine.
type AgentHandle interface {
    // ID returns the agent's logical name.
    ID() string

    // Send delivers a structured message to the agent.
    // How the message reaches the agent is the harness's problem.
    // For tmux: bracketed paste + enter.
    // For Hermes sidecar: queued for pre_llm_call injection.
    // For A2A: HTTP POST to the agent's task endpoint.
    Send(ctx context.Context, msg Message) error

    // Interrupt delivers an urgent message, preempting current work if possible.
    // For tmux: Ctrl+C + message.
    // For Hermes sidecar: interrupt flag + message queue.
    // For A2A: cancel current task + new task.
    Interrupt(ctx context.Context, msg Message) error

    // Events returns a channel that emits lifecycle events.
    // The channel is closed when the agent finishes (for any reason).
    // The daemon selects on this alongside other work.
    Events() <-chan Event

    // Status returns the agent's current status.
    Status() AgentStatus

    // Stop requests graceful shutdown with a timeout.
    // The agent should finish its current turn and exit.
    Stop(ctx context.Context) error

    // Kill forces immediate termination.
    Kill() error
}
```

### Message and Event types

```go
// Message is a structured message from the daemon to an agent.
type Message struct {
    ID        string
    From      string // sender agent ID
    Content   string
    Type      MessageType // instruction, state-change, input-needed
    Timestamp time.Time
}

// Event is a lifecycle event from an agent to the daemon.
type Event struct {
    Type      EventType
    AgentID   string
    Timestamp time.Time
    Data      map[string]any
}

type EventType string

const (
    // Agent finished its task. Data: {"exit_reason": "...", "summary": "..."}
    EventFinished EventType = "finished"

    // Agent is blocked and needs help. Data: {"reason": "...", "needs_from": "..."}
    EventBlocked EventType = "blocked"

    // Agent started a tool call. Data: {"tool": "...", "preview": "..."}
    EventToolStarted EventType = "tool_started"

    // Agent completed a tool call. Data: {"tool": "...", "duration_ms": N}
    EventToolCompleted EventType = "tool_completed"

    // Agent needs clarification. Data: {"question": "...", "choices": [...]}
    EventClarification EventType = "clarification"

    // Agent completed a reasoning step. Data: {"api_calls": N, "tools_used": [...]}
    EventStepCompleted EventType = "step_completed"

    // Agent published an artifact. Data: {"kind": "...", "path": "...", "summary": "..."}
    EventArtifactPublished EventType = "artifact_published"

    // Agent sent a message to another agent. Data: {"to": "...", "content": "..."}
    EventMessageSent EventType = "message_sent"

    // Agent process exited unexpectedly. Data: {"exit_code": N, "stderr": "..."}
    EventExitedUnexpectedly EventType = "exited_unexpectedly"

    // Heartbeat / activity indicator. Data: {} (absence of this = stall)
    EventActivity EventType = "activity"
)

type AgentStatus string

const (
    StatusStarting AgentStatus = "starting"
    StatusRunning  AgentStatus = "running"
    StatusIdle     AgentStatus = "idle"  // finished a turn, waiting for next message
    StatusBlocked  AgentStatus = "blocked"
    StatusStopping AgentStatus = "stopping"
    StatusExited   AgentStatus = "exited"
)
```

### ToolSpec (coordination tools)

```go
// ToolSpec describes a coordination tool the daemon wants the agent to have.
// The harness translates this into whatever form the runtime supports.
type ToolSpec struct {
    Name        string         // "send_message", "publish_artifact", "signal_blocked"
    Description string
    Schema      map[string]any // JSON Schema for the tool's input
}
```

---

## What moves where

Today the daemon does five things related to agents:

1. **Launch** (`defaultLaunchAgent`): build Hermes command, create tmux session
2. **Deliver messages** (`defaultDeliverMessage`): SendKeys + SendEnter via tmux
3. **Watch for exit** (`watchAgentExit`): WaitForSession, check finish marker
4. **Watch for idle** (`watchAgentIdle`): CapturePane polling, normalizePaneForIdle
5. **Wire broker to delivery** (`handleSpawnAgent`): broker.Subscribe -> deliverMessage

With the new interface:

| Concern | Before (daemon) | After (harness) |
|---------|-----------------|-----------------|
| Launch | daemon builds command, calls runner.CreateSession | `harness.Spawn(cfg)` returns handle |
| Deliver messages | daemon calls runner.SendKeys | `handle.Send(msg)` |
| Exit detection | daemon goroutine polls runner.WaitForSession | harness sends `EventFinished` or `EventExitedUnexpectedly` on Events channel |
| Idle detection | daemon goroutine polls runner.CapturePane | harness sends `EventActivity` heartbeats; daemon infers stall from absence |
| Broker wiring | daemon subscribes closure that calls deliverMessage | daemon subscribes closure that calls `handle.Send` |

The daemon no longer has `watchAgentExit` or `watchAgentIdle` as daemon methods. Those behaviors are either built into the harness (Hermes sidecar gets callbacks natively) or implemented inside the harness (tmux harness wraps the existing polling logic).

---

## Implementation: TmuxHarness

Wraps the existing `tmux.Runner` code behind the new interface. This is a refactor, not a rewrite. All the existing tmux logic moves into this harness.

```go
type TmuxHarness struct {
    runner tmux.Runner
}

func (h *TmuxHarness) Spawn(ctx context.Context, cfg SpawnConfig) (AgentHandle, error) {
    // Build hermes launch command (existing hermesharness.BuildLaunchCmd logic)
    // Create tmux session via runner.CreateSession
    // Start exit watcher goroutine (existing watchAgentExit logic)
    // Start idle watcher goroutine (existing watchAgentIdle logic)
    // Return tmuxAgentHandle
}
```

The `tmuxAgentHandle` internally:
- `Send()` calls `runner.SendKeys` + `runner.SendEnter` (existing deliverMessage logic)
- `Interrupt()` sends Ctrl+C then the message (existing broker.Interrupt pattern)
- `Events()` channel fed by the exit/idle watcher goroutines
- `Status()` tracks state transitions from watcher goroutines

This preserves 100% of existing behavior. Tests pass without changes to test assertions (only to wiring).

---

## Implementation: HermesSidecar

The Go daemon manages a Python subprocess (`hermes-bridge`) that wraps Hermes's `AIAgent` API. Communication over a Unix socket using JSON-RPC.

### Process model

```
Belayer daemon (Go)
  ├── Session bus (messages, events, artifacts, roster)
  ├── HTTP API (Unix socket, for CLI and outer control plane)
  └── HermesSidecar implements Harness
        └── hermes-bridge (Python subprocess)
              ├── JSON-RPC server on Unix socket
              ├── AIAgent("planner", quiet_mode=True)
              ├── AIAgent("api-specialist", quiet_mode=True)
              └── AIAgent("reviewer", quiet_mode=True)
```

### Why a sidecar subprocess, not in-process

1. **Crash isolation.** A Python segfault or OOM in one agent doesn't kill the Go daemon or the session bus. The sidecar can be restarted.

2. **Language boundary.** Belayer is Go. Hermes is Python. CGo-to-Python or embedding is fragile. A subprocess with JSON-RPC is a well-understood boundary.

3. **Restart semantics.** If the sidecar crashes, the Go daemon can detect it, mark agents as blocked, and optionally restart the sidecar. Agent sessions in Hermes are persistent (SQLite-backed), so restarting the sidecar can resume agents where they left off.

4. **Testing.** The Go side tests against the `Harness` interface with mocks. The Python side tests `AIAgent` wiring independently. No cross-language test harness needed.

### JSON-RPC protocol

The sidecar exposes these methods on a Unix socket:

```
hermes.spawn(config) -> {agent_id: string}
hermes.send(agent_id, message) -> {}
hermes.interrupt(agent_id, message) -> {}
hermes.status(agent_id) -> {status: string}
hermes.stop(agent_id) -> {}
hermes.kill(agent_id) -> {}
```

Events flow from sidecar to daemon as JSON-RPC notifications (server-initiated):

```
hermes.event({type: "finished", agent_id: "planner", data: {...}})
hermes.event({type: "tool_started", agent_id: "api-specialist", data: {...}})
hermes.event({type: "activity", agent_id: "reviewer", data: {}})
```

The Go `HermesSidecar` implementation:
- On `Spawn()`: calls `hermes.spawn`, creates a `hermesAgentHandle`, starts listening for events for that agent
- On `handle.Send()`: calls `hermes.send`
- On `handle.Events()`: returns a channel fed by incoming `hermes.event` notifications filtered by agent_id
- On `Close()`: sends SIGTERM to sidecar, waits for clean shutdown

### Python sidecar internals

The sidecar (`hermes-bridge`) is a standalone Python process. It:

1. Constructs `AIAgent` instances with `quiet_mode=True` per spawn request
2. Registers coordination tools (send_message, publish_artifact, signal_blocked) that call back to the Go daemon's HTTP API
3. Registers `pre_llm_call` hooks that check for pending messages from the Go daemon
4. Registers callbacks (tool_start, tool_complete, step, clarify) that emit events via JSON-RPC notifications
5. Runs agents on a `ThreadPoolExecutor` via `run_conversation()`

The sidecar does NOT own the session bus. Messages, events, and artifacts are owned by the Go daemon. The sidecar's coordination tools call the daemon's HTTP API to route messages and register artifacts. This means the coordination model works identically regardless of which harness is active.

### Tool injection flow

When the daemon spawns an agent with `Tools: []ToolSpec{sendMessage, publishArtifact, signalBlocked}`:

1. Go daemon sends `hermes.spawn(config)` including tool specs
2. Python sidecar receives tool specs
3. For each tool, sidecar calls `registry.register(name, schema, handler)` on the `AIAgent`
4. The handler for `send_message` does: `requests.post(f"http+unix://{daemon_socket}/sessions/{session_id}/messages", json={...})`
5. The agent discovers tools naturally in its tool list

This replaces the Belayer communication skill (a Markdown document teaching CLI commands) with native tool registration. The agent doesn't need to learn syntax. It just calls tools.

---

## Implementation: A2AHarness (future)

The same interface supports Google A2A protocol. An A2A agent is a remote HTTP endpoint with an Agent Card:

```go
type A2AHarness struct {
    httpClient *http.Client
}

func (h *A2AHarness) Spawn(ctx context.Context, cfg SpawnConfig) (AgentHandle, error) {
    // Discover agent via Agent Card URL
    // Create A2A Task with initial instructions
    // Return a2aAgentHandle that wraps task lifecycle
}
```

The `a2aAgentHandle`:
- `Send()` creates a new A2A Message on the task
- `Events()` channel fed by polling or streaming the task's event log
- `Status()` maps A2A task states (submitted, working, input-needed, completed, failed) to AgentStatus

No changes to the daemon, session bus, or broker needed.

---

## What changes in the daemon

### Daemon struct

```go
type Daemon struct {
    store   *store.Store
    broker  broker.Broker
    harness harness.Harness  // replaces: runner tmux.Runner

    // These function fields are removed. The harness handles launch and delivery.
    // launchAgent    func(req agentSpawnRequest) (string, error)     // REMOVED
    // deliverMessage func(run store.AgentRun, msg broker.Message) error // REMOVED

    // These are removed. The harness handles idle/exit detection internally.
    // idlePollInterval  time.Duration  // REMOVED (or moved to TmuxHarness config)
    // idleTimeout       time.Duration  // REMOVED
    // idleNudgeCooldown time.Duration  // REMOVED

    handles   map[string]harness.AgentHandle  // keyed by sessionID-agentName
    handlesMu sync.RWMutex

    // ... rest unchanged
}
```

### handleSpawnAgent (simplified)

```go
func (d *Daemon) handleSpawnAgent(w http.ResponseWriter, r *http.Request) {
    // ... validation unchanged ...

    handle, err := d.harness.Spawn(r.Context(), harness.SpawnConfig{
        SessionID:  sessionID,
        AgentID:    req.Name,
        Role:       req.Role,
        Profile:    req.Profile,
        Workdir:    req.Workdir,
        RepoScope:  req.Repo,
        RunDir:     runDir,
        SocketPath: d.config.SocketPath,
        Tools:      d.coordinationTools(),
        ExtraEnv:   map[string]string{},
    })
    if err != nil {
        // ... error handling ...
    }

    // Store handle for later interaction
    d.storeHandle(sessionID, req.Name, handle)

    // Wire broker to handle
    d.broker.Subscribe(sessionID, req.Name, func(msg broker.Message) {
        if msg.Urgent {
            handle.Interrupt(context.Background(), toHarnessMessage(msg))
        } else {
            handle.Send(context.Background(), toHarnessMessage(msg))
        }
    })

    // Drain events from handle into daemon event log + broker
    go d.drainAgentEvents(sessionID, req.Name, handle)

    // ... store agent run, log event, respond ...
}
```

### drainAgentEvents (replaces watchAgentExit + watchAgentIdle)

```go
func (d *Daemon) drainAgentEvents(sessionID, agentName string, handle harness.AgentHandle) {
    for event := range handle.Events() {
        // Log every event to the session event store
        d.store.LogEvent(store.SessionEvent{
            SessionID: sessionID,
            Type:      string(event.Type),
            Data:      mustJSON(event.Data),
        })

        switch event.Type {
        case harness.EventFinished:
            d.store.UpdateAgentRunStatus(sessionID, agentName, "complete")
            if agentName != "planner" {
                // Notify planner
                d.broker.Send(sessionID, "planner", broker.Message{...})
            }

        case harness.EventExitedUnexpectedly:
            d.store.UpdateAgentRunStatus(sessionID, agentName, "blocked")
            if agentName != "planner" {
                d.broker.Interrupt(sessionID, "planner", broker.Message{...})
            }

        case harness.EventBlocked:
            d.store.UpdateAgentRunStatus(sessionID, agentName, "blocked")
            // Route to planner

        case harness.EventClarification:
            // Route question to planner as a message

        case harness.EventMessageSent:
            // Agent used send_message tool; route through broker
            to := event.Data["to"].(string)
            content := event.Data["content"].(string)
            d.broker.Send(sessionID, to, broker.Message{...})

        case harness.EventArtifactPublished:
            // Agent used publish_artifact tool; register in store
            d.store.CreateArtifact(store.Artifact{...})
        }
    }
    // Channel closed = agent is done (for any reason)
}
```

This is cleaner than the current code. No polling goroutines, no CapturePane, no finish-marker files. The harness tells us what happened.

---

## Store changes

The `AgentRun.Transport` field currently defaults to `"tmux"`. It should store the harness type:

```go
type AgentRun struct {
    // ...
    Transport   string // "tmux", "hermes-sidecar", "a2a"
    TmuxSession string // only populated for tmux transport
    // ...
}
```

`TmuxSession` becomes transport-specific metadata. For Hermes sidecar, it's unused. For A2A, we might store the task URL. This could evolve into a generic `TransportMeta string` (JSON) field, but for now the existing columns are fine.

---

## Migration path

### Phase 1: Extract the interface

Create `internal/harness/harness.go` with the types above. Create `internal/harness/tmux.go` that implements `Harness` by wrapping the existing `tmux.Runner` + the daemon's current launch/deliver/watch code. Update the daemon to use `harness.Harness` instead of `tmux.Runner` + function fields.

All existing tests pass. Behavior is identical. This is a mechanical refactor.

### Phase 2: Build the sidecar protocol

Define the JSON-RPC protocol between Go and Python. Build a minimal Python `hermes-bridge` that can spawn one `AIAgent` and relay events. Test with a single agent in quiet mode.

### Phase 3: Build HermesSidecar harness

Implement the Go side of the sidecar communication. The daemon can now be configured with either `TmuxHarness` (default, backwards compatible) or `HermesSidecar` (opt-in via flag or config).

### Phase 4: Coordination tool injection

Replace the Belayer communication skill with injected tools. The sidecar registers `send_message`, `publish_artifact`, `signal_blocked` as native tools on each `AIAgent`. Tool handlers call back to the Go daemon's HTTP API.

### Phase 5: Cut over

Once the sidecar path is validated end-to-end, make it the default for `belayer run start`. Keep `TmuxHarness` available for debugging and for non-Hermes harnesses.

---

## What the headless Hermes doc got right

Nearly everything about the execution model:

- `AIAgent(quiet_mode=True)` as headless execution
- Callbacks for tool start/complete/step/clarify
- `pre_llm_call` hook for message injection
- `pre_tool_call` hook for security enforcement
- Tool injection via `registry.register()`
- `delegate_task` for ACP dispatch to Claude Code
- ThreadPoolExecutor for concurrent agents

All of this is preserved. It just runs inside the sidecar instead of being the daemon itself.

## What the headless Hermes doc got wrong

The ownership model. Making the Python process the outer daemon means:

1. The session bus, which is the core of Belayer's value, lives inside a Hermes-specific process. Replacing Hermes means rewriting the bus.
2. A crash in any agent's Python code takes down the entire daemon, including the session bus and HTTP API.
3. Testing the coordination model requires a Python environment with Hermes installed. Today, `go test ./...` covers the full daemon including spawn/deliver/watch.
4. The "thin adapter layer" mitigation for API instability is optimistic. When the adapter IS the daemon, every Hermes change is a daemon change.

The sidecar model preserves all the execution benefits while keeping the coordination model independent of the runtime.

---

## Risks

### Sidecar startup latency

Spawning a Python subprocess adds 1-2 seconds to daemon startup. For a long-running Nightshift worker, this is negligible. For interactive `belayer run start`, it's noticeable but acceptable.

**Mitigation**: start the sidecar lazily on first `Spawn()` call, not at daemon startup.

### Two-process debugging

Debugging issues across the Go/Python boundary is harder than debugging a single process. JSON-RPC adds a serialization layer.

**Mitigation**: structured logging on both sides with correlated request IDs. The JSON-RPC messages are the debugging artifact, not a hindrance.

### Sidecar crash recovery

If the sidecar dies, all agents managed by it are gone. The Go daemon needs to detect this and mark them blocked.

**Mitigation**: the Go side monitors the sidecar subprocess. On unexpected exit, all handles emit `EventExitedUnexpectedly` and close their Events channels. The daemon's `drainAgentEvents` loop handles the rest.

### Protocol versioning

The JSON-RPC protocol between Go and Python needs to evolve without breaking compatibility.

**Mitigation**: version the protocol. The sidecar reports its version on startup. The Go side checks compatibility. For v1, keep the protocol minimal and extend it only when needed.

---

## What this does not cover

- **Clamshell sandbox integration**: orthogonal to the harness boundary.
- **Outer worker control plane**: Nightshift worker talks to the Go daemon's HTTP API regardless of harness.
- **Agent identity/profiles**: still Hermes profiles. The sidecar respects `HERMES_HOME` per agent.
- **Memory and session persistence**: Hermes handles this internally. The sidecar just needs to point each agent at the right session DB.
