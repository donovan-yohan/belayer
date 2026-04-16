# Philosophy

A portable specification for multi-agent coding runtimes.

## The Thesis

A multi-agent coding session, where multiple AI agents collaborate to write, review, and ship software, requires six infrastructure interfaces. These interfaces are the same regardless of which LLMs, vendors, or orchestration patterns power the agents.

This document defines those interfaces and the invariants that bind them. It is a specification, not a product description. Belayer is one implementation. The specification is language-agnostic and could be realized on any stack.

---

## The OS Analogy

An operating system virtualizes hardware into stable abstractions (processes, IPC, filesystems, permissions) so applications can focus on domain logic rather than managing resources directly. A multi-agent coding runtime does the same for AI coding agents: it virtualizes the infrastructure that every multi-agent coding session needs, so agents can focus on writing software.

| OS Concept | Runtime Interface | What it virtualizes |
|---|---|---|
| Process | **Session** | Agent lifecycle, state, event history |
| Scheduler | **Orchestration** | Team composition, coordination |
| Container / VM | **Sandbox** | Network isolation, credentials, filesystem |
| IPC | **Communication** | Agent-to-agent messaging, delivery |
| Filesystem | **Memory** | Knowledge persistence across sessions |
| Syscalls / Drivers | **Tools** | Capabilities agents invoke, execution routing |

The key property: applications don't need to know about each other's implementation details. A runtime that correctly virtualizes these six interfaces lets you swap any agent, model, vendor, or isolation backend without changing the contracts.

```mermaid
flowchart TB
    S["<b>SESSION</b><br/>Lifecycle - Events - State<br/><i>The central primitive</i>"]
    O["<b>ORCHESTRATION</b><br/>Roster - Phases - Coordination<br/><i>Planner reads team, adapts strategy</i>"]
    M["<b>MEMORY</b><br/>Core - Archival - Recall<br/><i>Knowledge that outlives sessions</i>"]
    C["<b>COMMUNICATION</b><br/>Messaging - Delivery - Artifacts<br/><i>Transport between agents</i>"]
    T["<b>TOOLS</b><br/>Registry - Routing - Execution<br/><i>Capabilities agents invoke</i>"]
    SB["<b>SANDBOX</b><br/>Isolation - Credentials - Network<br/><i>Security boundary around agents</i>"]

    S --> O
    S --> M
    O --> C
    O --> T
    C --> SB
    T --> SB
    M -.->|"consolidation"| S
```

---

## Topology

A multi-agent coding system needs **two control planes** at different scopes:

1. **Outer daemon** — always-on, manages a pool of workers, queues requests, persists long-lived data (logs, memory, artifacts) that outlive any individual run.
2. **Inner daemon** — ephemeral, lives inside one worker for one run, coordinates agent-to-agent communication, records telemetry and artifacts to a run-local database.

The inner daemon and everything it manages are **ephemeral** — when the worker dies, the run-local state dies with it. Anything that matters beyond the run (logs, learned knowledge, output artifacts) must be exported to the outer daemon before the worker is reclaimed.

```mermaid
flowchart LR
    User["User / ticket / spec"] --> OD["Outer daemon\n(always-on)"]
    OD --> Queue[("Request queue\nlong-lived storage")]
    OD --> W1["Worker 1"]
    OD --> W2["Worker 2"]
    OD --> W3["Worker 3"]

    subgraph WorkerRun["One run on one worker (ephemeral)"]
        ID["Inner daemon\n(agent control plane)"]
        H["Harness driver"]
        SB["Sandbox / workspace"]
        DB[("Run-local DB\nevents, telemetry, artifacts")]

        ID --> H
        H --> SB
        ID --> DB
    end

    W1 --> WorkerRun
    DB -.->|"export: logs,\nartifacts, memory"| Queue
```

### How the six interfaces map to the architecture

Each box in the architecture implements one of the six runtime interfaces. The diagram below is color-coded: boxes with the same color implement the same interface.

```mermaid
flowchart TB
    subgraph legend [" "]
        direction LR
        L1["SESSION"]:::session
        L2["ORCHESTRATION"]:::orch
        L3["COMMUNICATION"]:::comm
        L4["SANDBOX"]:::sandbox
        L5["MEMORY"]:::memory
        L6["TOOLS"]:::tools
    end

    OD["Outer daemon\n(always-on)"]
    MEM[("Durable memory\nlogs, learnings")]:::memory

    OD --> S1
    OD --> S2
    OD --> S3

    subgraph S1["Session A (Worker 1)"]
        ID1["Session bus / inner daemon\nmessages, events, artifacts"]:::comm
        SUP1["Supervisor agent\ndecomposes, delegates, decides"]:::orch
        SP1["Specialist agents\n(frontend, backend, QA, ...)"]:::orch
        H1["Harness + tool catalog\nexecution routing"]:::tools
        SB1["Sandbox\nisolation boundary"]:::sandbox
        DB1[("Run-local DB\ntelemetry, events")]:::session

        ID1 <--> SUP1
        ID1 <--> SP1
        SUP1 --> H1
        SP1 --> H1
        H1 --> SB1
        ID1 --> DB1
    end

    subgraph S2["Session B (Worker 2)"]
        ID2["Session bus"]:::comm
        SUP2["Supervisor"]:::orch
        SP2["Specialists"]:::orch
        H2["Harness + tools"]:::tools
        SB2["Sandbox"]:::sandbox
        DB2[("Run-local DB")]:::session

        ID2 <--> SUP2
        ID2 <--> SP2
        SUP2 --> H2
        SP2 --> H2
        H2 --> SB2
        ID2 --> DB2
    end

    subgraph S3["Session C (Worker 3)"]
        ID3["Session bus"]:::comm
        SUP3["Supervisor"]:::orch
        SP3["Specialists"]:::orch
        H3["Harness + tools"]:::tools
        SB3["Sandbox"]:::sandbox
        DB3[("Run-local DB")]:::session

        ID3 <--> SUP3
        ID3 <--> SP3
        SUP3 --> H3
        SP3 --> H3
        H3 --> SB3
        ID3 --> DB3
    end

    MEM -.->|"inject context\nat session start"| ID1
    MEM -.->|"inject"| ID2
    MEM -.->|"inject"| ID3
    DB1 -.->|"export logs,\nartifacts, learnings"| MEM
    DB2 -.->|"export"| MEM
    DB3 -.->|"export"| MEM

    classDef session fill:#4A90D9,stroke:#2E5C8A,color:#fff
    classDef orch fill:#E8A838,stroke:#B07A1A,color:#fff
    classDef comm fill:#50B86C,stroke:#2D8A45,color:#fff
    classDef sandbox fill:#E05555,stroke:#A83232,color:#fff
    classDef memory fill:#9B6FCF,stroke:#6B3FA0,color:#fff
    classDef tools fill:#3ABAB4,stroke:#1F8A85,color:#fff
```

| Interface | Color | What it maps to | Key property |
|---|---|---|---|
| **Session** | Blue | Each worker run + its run-local DB | Ephemeral — dies with the worker |
| **Orchestration** | Amber | Supervisor + specialist agents (LLMs) | Judgment, not code — adapts to roster |
| **Communication** | Green | Inner daemon / session bus | Routes messages, events, artifacts between agents |
| **Sandbox** | Red | Isolation boundary around each worker | Deny-by-default, agents cannot self-impose |
| **Memory** | Purple | Durable storage on outer daemon | Injected at start, exported at end — outlives sessions |
| **Tools** | Teal | Harness driver + registered tool catalog | Execution routing into sandboxed environments |

The key invariant: **run-local state is disposable, outer state is durable.** The inner daemon optimizes for low-latency coordination between agents during the run. The outer daemon optimizes for persistence — logs for debugging, artifacts for delivery, memory for future runs. The export path between them is what makes ephemeral runs useful beyond their lifetime.

---

## The Six Interfaces

### 1. Session

The session is the central primitive. It is the unit of work, the scope for state, and the recovery boundary.

- Append-only event log that survives agent crashes
- Lifecycle management (create, run, stop, resume)
- Queryable state (events, status, agent health)
- Session-scoped identity for all agents in the run
- Artifact registration for durable outputs

What is outside this interface: what events mean (agent judgment), when a session is "done" (orchestration judgment), where events are stored (implementation choice).

### 2. Orchestration

Orchestration determines who does what. The orchestrator is an LLM that reads the team roster and adapts its coordination strategy to the task.

- Declarative team rosters (role, profile, scope, tier)
- Dynamic agent spawning
- Roster-adaptive task assignment
- Completion and blockage signaling

What is outside this interface: the coordination logic itself (that's the planner's judgment), exact workflow sequences (not hardcoded), cluster-wide scheduling (belongs to the outer control plane).

### 3. Sandbox

The sandbox is the security boundary between the runtime (trusted) and agents (untrusted). Agents cannot self-impose isolation.

- Network isolation (deny-by-default, allowlisted egress)
- Credential isolation (agents never see real keys)
- Filesystem boundaries
- Pluggable enforcement backend

### 4. Communication

Communication is the transport layer between agents. Agents don't know each other's runtime. They send messages through the session bus, which handles delivery.

- Point-to-point messaging and broadcast
- Delivery guarantees (coalescing, urgent bypass)
- Transport abstraction (agents don't care how delivery works)
- Durable coordination artifacts

What is outside this interface: message content or meaning (that's between the agents), when to send messages (orchestration judgment).

### 5. Memory

Memory is knowledge that persists beyond any single agent invocation or session. The runtime owns memory infrastructure (storage, indexing, injection, consolidation triggers). Agents own their memory content (what to remember, how to organize it, when to prune).

- Core, archival, and recall layers or equivalents
- Agent-managed content, runtime-managed plumbing
- Background consolidation / sleep-time support
- Provenance and staleness awareness

### 6. Tools

Tools are capabilities that agents invoke through the runtime. The runtime routes execution to the correct target.

- Declarative tool registry
- Execution routing (which environment runs the command)
- Safety constraints (read-only, audit, target restrictions)

---

## Agent Identity

An agent is not a process. It's an identity. The process (Hermes, Claude Code, Codex, whatever) is the runtime detail. The identity is what persists.

An agent identity is a portable directory of files: config, system prompt, operating instructions. The harness loads it at spawn time. The identity should be transferable across harnesses in principle, even if a given implementation only supports one.

---

## Design principle

> Keep the philosophy broad, keep the implementation narrow enough to finish.
