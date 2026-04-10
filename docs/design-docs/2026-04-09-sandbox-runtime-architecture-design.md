---
status: current
created: 2026-04-09
branch: feature/v6-phase4-proof-out
supersedes:
implemented-by:
consulted-learnings: []
---

# Sandbox & Runtime Architecture Design

Belayer v6 agent orchestration, workbench provisioning, and security model — built on extend-clamshell for sandbox isolation.

## Problem Statement

Belayer needs to support autonomous coding sessions that:

1. **Run overnight** ("night shift") on tasks ranging from a single spec to a Jira epic with dependent tickets
2. **Operate on multi-repo codebases** (e.g., extend-api + extend-app) with project-specific infrastructure (postgres, localstack, rabbitmq via `xt up`)
3. **Enforce security boundaries** — network egress control, filesystem isolation, credential masking, audit trail
4. **Support concurrent sessions** — two tasks against the same repos simultaneously, with full isolation

Current state: the daemon, session lifecycle, templates, prompt compilation, messaging, and memory/reflection systems are implemented. What's missing is the runtime integration with clamshell, workbench provisioning, and the agent topology for complex multi-session work.

## Reference Architecture Sources

| Source | Key Pattern Adopted |
|--------|-------------------|
| **extend-clamshell** | Deny-by-default egress, host-owned credentials with inference routing, per-binary network identity, process fingerprinting, audit logging, tmux-backed interactive sessions. **The sandbox runtime foundation.** |
| **Anthropic Managed Agents** | Environment as reusable template; each session gets its own container instance; sandbox as a tool the agent calls; credentials never in sandbox |
| **Anthropic Engineering Blog** | "The harness no longer lived inside the container. It called the container the way it called any other tool." |
| **NVIDIA OpenShell** | Placeholder credential tokens resolved at network boundary; fail-closed secret resolution; per-binary OPA policy. Clamshell independently implements the same patterns. |
| **Letta Framework** | Sleep-time compute (reflection), three-tier memory, progressive disclosure, shared memory blocks between agents |
| **Letta Code** | Primary agent as orchestrator with Task tool; subagents as child processes; one-level-deep spawning; git-backed memory |
| **Scion (Google Cloud)** | Git worktrees per agent, tmux send-keys messaging, game master pattern, tiered agents (main/peripheral/ephemeral) |

## Core Design

### Primitives

| Primitive | What it is | Lifecycle |
|-----------|-----------|-----------|
| **Agent** | A vendor CLI + model + system prompt + tools | Created once, referenced by sessions |
| **Environment** | Clamshell policy + repos + workbench spec + tools | Created once per project, reused across sessions |
| **Session** | A running set of agents within an environment, performing a task | Created per task, generates events, stopped when complete |
| **Workbench** | On-demand execution environment for building, testing, and running code | Provisioned when an agent calls `belayer workbench up`, destroyed on teardown |
| **Tool** | A user-defined command routed to a specific execution layer | Defined in environment config, executed by daemon |
| **Event** | Messages exchanged between the system, agents, and external callers | Persisted in SQLite, searchable via FTS5 |

### Agent Topology: Tiered Agents

Agents exist in three tiers, inspired by Scion's athenaeum pattern.

| Tier | Lifetime | Communication | Spawned by | Examples |
|------|----------|---------------|------------|---------|
| **Main character** | Persistent across session (or workspace, for pilot) | Peer-to-peer via `belayer message` | Template definition or pilot | Pilot, implementer, reviewer |
| **Peripheral** | Session-scoped, persistent within it | Receives instructions from main characters | Pilot at session start | Linter, type checker, doc generator |
| **Ephemeral** | Task-scoped, dismissed when done | Reports result to spawner, then exits | Any main character | "Research how auth works", "fix these lint errors" |

The **pilot** is a main character with dual scope:
- **Within a session**: participates as a peer, messages teammates, delegates work
- **Across the workspace**: creates/monitors/stops sessions, triggers integration testing, manages the full epic lifecycle

### Execution Model

```
belayer daemon (orchestration layer)
  └── belayer session start
        └── clamshell sandbox create (per agent)    ← isolation layer
              ├── deny-by-default network
              ├── host-owned credentials (inference.local routing)
              ├── per-binary egress policy
              ├── process fingerprinting + audit
              └── vendor CLI runs inside (claude, opencode, etc.)

  └── belayer workbench up (on demand)              ← test infrastructure
        ├── extend-api (built from worktree)
        ├── extend-app (built from worktree)
        ├── postgres, localstack, rabbitmq
        └── health checks + endpoint reporting
```

An agent's workflow follows the natural developer loop:

1. Edit code in worktree (always available inside sandbox)
2. Compile/typecheck via tool call (daemon runs in workbench)
3. Run integration tests via `belayer workbench up` (on demand)
4. Verify via tool calls (`curl` the API, screenshot the app)

### Clamshell as Sandbox Runtime

Each agent runs inside a clamshell sandbox. Belayer does not build its own isolation — clamshell provides:

| Concern | How clamshell solves it |
|---------|----------------------|
| **Network isolation** | Deny-by-default iptables. Sandbox processes can only reach the managed proxy on loopback. |
| **Credential isolation** | Host-owned secrets, never mounted into sandbox. `inference.local` routing injects credentials at the proxy boundary. Agent sees no real API keys. |
| **Egress control** | Per-binary policy (which binary can reach which endpoint). `claude` can reach `api.anthropic.com`; a Python script the agent wrote cannot. |
| **Filesystem isolation** | Writable `/workspace`, read-only root. Host control plane state never visible inside sandbox. |
| **Audit** | Deny event logs with binary identity, target, reason. Runtime metadata. `clamshell doctor` for health. |
| **Interactive access** | tmux-backed sessions. `clamshell sandbox connect` for interactive attach. |

**What belayer adds on top of clamshell:**

| Concern | How belayer adds it |
|---------|-------------------|
| **Multi-agent orchestration** | Session templates, messaging plane, pilot coordination |
| **Workbench provisioning** | `belayer workbench up` — Docker Compose for extend-api + postgres + infra |
| **Tool execution routing** | Daemon routes `belayer tool run` to workbench/infra/host targets |
| **Memory & reflection** | Three-tier memory, sleep-time reflection agent, pilot memory across sessions |
| **Session lifecycle** | Create, monitor, stop, wake, logs, concurrent sessions |
| **Epic decomposition** | Pilot analyzes Jira tickets, creates parallel sessions, triggers integration at milestones |

### CLI Surface

```bash
# Session management (admin interface)
belayer session start --template implement --input spec.md
belayer session list
belayer session stop <id>
belayer status
belayer logs <id> [-f]
belayer attach <agent>           # wraps clamshell sandbox connect

# Inside agents (agent interface)
belayer message send --to <agent> "text"
belayer message broadcast "text"
belayer recall "query"
belayer note "observation"
belayer workbench up             # provision test infrastructure
belayer workbench status         # check readiness + endpoints
belayer tool run <name> --input '{...}'
```

No `--docker` or `--local` flags. Isolation via clamshell is the default. `belayer attach` wraps `clamshell sandbox connect` for interactive tmux access to any agent.

### Session Start Flow

```
belayer session start --environment extend-fullstack --input spec.md
  1. Load environment config (repos, agents, policy, workbench spec, tools)
  2. Load agent templates from .belayer/templates/{name}/
  3. Create session in daemon (SQLite, UUID)
  4. Create git worktrees per repo per session:
       extend-api → .belayer/worktrees/{sessionID}/extend-api
       extend-app → .belayer/worktrees/{sessionID}/extend-app
  5. For each agent in environment.agents:
     a. Compile prompt from template (system-prompt.md + agents.md + team roster + memory + task)
     b. Determine workspace mounts from agent.repo field:
          pilot:           (no workspace — orchestrates via messaging and tools)
          api-implementer: --workspace {api-worktree}:/workspace
          app-implementer: --workspace {app-worktree}:/workspace
          reviewer:        (no workspace — works from diffs via messaging)
     c. clamshell sandbox create \
          --name belayer-{session}-{agent} \
          --policy .belayer/policies/extend-fullstack.yaml \
          --workspace ... (per above)
     d. Log agent_launched event
  6. Update session status → running
  7. Print summary + attach instructions
```

### Workbench Provisioning

The workbench is the test infrastructure stack, provisioned on demand when an agent calls `belayer workbench up`. It is separate from the agent sandboxes.

```yaml
# .belayer/environments/extend-fullstack/workbench.yaml
services:
  postgres:
    image: postgres:16
    env:
      POSTGRES_DB: extend_{{.SessionID}}
  localstack:
    image: localstack/localstack
  rabbitmq:
    image: rabbitmq:3-management
  extend-api:
    build:
      context: "{{.Worktree.extend-api}}"
    env:
      SPRING_DATASOURCE_URL: "jdbc:postgresql://postgres:5432/extend_{{.SessionID}}"
    depends_on: [postgres, localstack, rabbitmq]
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/actuator/health"]
      interval: 5s
      timeout: 3s
      start_period: 90s
  extend-app:
    build:
      context: "{{.Worktree.extend-app}}"
    env:
      API_URL: "http://extend-api:8080"
    depends_on: [extend-api]
```

Daemon endpoint: `POST /sessions/{id}/workbench` → generates compose from spec, runs `docker compose up`, blocks until health checks pass, returns endpoints.

Agents reach workbench results through tool calls — the daemon runs `docker exec` in workbench containers and returns output. Agent sandboxes and workbench containers are on separate networks.

### Tool Execution Model

User-defined tools in the environment config, routed by the daemon:

```yaml
tools:
  - name: db-query
    description: "Read-only SQL query against session database"
    input: { query: string }
    exec:
      target: infra
      command: 'psql -U belayer_ro -d extend_{{.SessionID}} -c "{{.query}}"'
    constraints: { read_only: true, audit: true }

  - name: build-check
    description: "Compile the project and return build errors"
    input: { project: string }
    exec:
      target: workbench
      command: "cd /workspace/{{.project}} && ./gradlew build 2>&1"

  - name: curl-api
    description: "Make an HTTP request to the running API"
    input: { method: string, path: string }
    exec:
      target: workbench
      command: 'curl -s -X {{.method}} http://extend-api:8080{{.path}}'
```

| Target | Where it runs | Use case |
|--------|--------------|----------|
| `agent` | Inside the calling agent's clamshell sandbox | File operations, git commands |
| `workbench` | Docker exec in workbench container | Compile, test, curl, verify |
| `infra` | Docker exec in infra container | DB queries, log inspection |
| `host` | Host machine (opt-in, dangerous) | xt operations, debugging |

Tool call safety: all `{{.variable}}` template vars auto-shell-quoted by the template engine. SQL tools use dedicated read-only DB users per session, not `psql --readonly` (which doesn't exist). Every call logged to session events.

### Security Model

```
Belayer Daemon (trusted control plane)
  - Pure Go binary, never runs LLM-generated code
  - Manages session lifecycle, messaging, events, workbench provisioning
  - Triggers reflection (launches clamshell sandbox, never runs LLM itself)

Clamshell Gateway (trusted, host-side)
  - Holds real credentials (API keys, tokens)
  - Runs managed proxy with policy enforcement
  - Provides inference.local routing (credential injection at proxy boundary)
  - Audit logging of all egress

Agent Sandboxes (untrusted, clamshell-managed)
  - Vendor CLI + git + belayer CLI
  - Worktree mounted at /workspace
  - NO real credentials (inference.local handles API auth)
  - Deny-by-default network (only proxy on loopback)
  - Per-binary egress policy

Workbench Containers (untrusted, runs agent-written code)
  - Application runtimes (JDK, Node)
  - Built from agent worktrees
  - Session-scoped database credentials (read-only user)
  - No internet access
  - Ephemeral — destroyed on workbench down

Reflection Agent (untrusted, runs in clamshell sandbox like any other agent)
  - Launched by daemon post-session or on idle
  - Reads exported session events + memory files
  - Writes updated memory, then exits
```

**Honest trade-off:** Workbench containers run agent-written code with database access. The agent can write code that reads data through the application's interfaces. Credential isolation (via clamshell) prevents direct secret exfiltration. Mitigations: session-scoped databases with test data, no-internet workbench egress, audit logging.

### Night Shift: Single Spec to Jira Epic

**Single spec:**
```
belayer session start --template implement --input spec.md
→ Pilot + implementer + reviewer in clamshell sandboxes
→ Implement, review, iterate
→ belayer workbench up for integration testing
→ QA passes → PR created
→ Reflection runs on idle
```

**Jira epic:**
```
belayer session start --template epic --input JIRA-1234
→ Pilot fetches epic, analyzes dependency graph (LLM judgment)
→ Creates parallel sessions for independent tickets
→ Monitors via belayer logs / event stream
→ Milestone complete → belayer workbench up → integration test
→ Next batch of dependent tickets
→ All done → PR(s), final QA, reflection
```

### Multi-Session Concurrency

- **Code**: git worktrees per session (independent branches)
- **Infrastructure**: per-session databases
- **Sandboxes**: session-scoped clamshell sandbox names
- **Events**: scoped by session ID in SQLite
- **Memory**: intentionally shared (learnings accumulate across sessions)

### Pilot Prompt System (Implemented)

Lean: role + team roster + compiled memory + tools + task. No hardcoded workflows. Pilot adapts orchestration to team composition. Review loops emerge from pilot judgment + accumulated memory.

### Reflection System (Implemented)

Deterministic trigger (post-session, idle detection). Reflection agent in its own clamshell sandbox. Reads events + memory, writes updated memory files. LLM judgment, not deterministic code.

### Environment Config

```yaml
name: extend-fullstack

repos:
  - name: extend-api
    path: ~/repos/extend-api
  - name: extend-app
    path: ~/repos/extend-app

clamshell:
  policy: policies/presets/extend-fullstack.yaml   # deny-by-default + anthropic + git + npm + maven

workbench:
  spec: ./workbench-extend.yaml

tools:
  - name: db-query
    description: "Read-only SQL query"
    input: { query: string }
    exec:
      target: infra
      command: 'psql -U belayer_ro -d extend_{{.SessionID}} -c "{{.query}}"'
    constraints: { read_only: true, audit: true }
  - name: curl-api
    description: "HTTP request to running API"
    input: { method: string, path: string }
    exec:
      target: workbench
      command: 'curl -s -X {{.method}} http://extend-api:8080{{.path}}'

reflection:
  vendor: claude
  model: sonnet
  trigger: post-session
  limits:
    max_review_loops: 10
    max_session_duration: 4h
```

### What We're NOT Building

| Concern | Who handles it |
|---------|---------------|
| Network isolation | Clamshell (deny-by-default iptables + managed proxy) |
| Credential masking | Clamshell (inference.local routing, host-owned secrets) |
| Per-binary egress policy | Clamshell (process fingerprinting + policy engine) |
| Audit logging | Clamshell (deny events, binary attestation) |
| Container scheduling | Docker / K8s |
| Secret management | Clamshell gateway / K8s Secrets |
| Custom orchestration DSL | The pilot is an LLM. Natural language + belayer CLI. |

Belayer builds the **orchestration layer**: sessions, messaging, memory, reflection, workbench, tools, epic decomposition. Clamshell builds the **isolation layer**. They compose cleanly.

## Implementation Sequence

1. **Clamshell integration** — `session_start.go` calls `clamshell sandbox create` instead of raw `docker run`/compose
2. **Workbench provisioning** — daemon endpoint + `belayer workbench up/down/status` CLI
3. **Tool execution routing** — daemon routes tool calls to workbench/infra/host via `docker exec`
4. **Runtime interface** — extract `Runtime` interface, clamshell as default backend
5. **Tiered agents** — main character / peripheral / ephemeral support
6. **Epic template** — pilot with session management tools
7. **Worktree cleanup** — wire into session teardown
8. **Workbench health checks** — block until ready, timeout, structured response
9. **Event-driven monitoring** — SSE/long-poll for pilot multi-session monitoring

## Resolved Questions

1. **Clamshell multi-workspace mount**: **Resolved.** Clamshell now supports multiple `--workspace` flags:
   ```bash
   clamshell sandbox create --name dev \
     --workspace ~/repos/extend-api:/workspace/extend-api \
     --workspace ~/repos/extend-app:/workspace/extend-app
   ```
   Full backward compatibility with single `--workspace /path`. Validates host directories exist, rejects duplicate container paths. This means agents that need multiple repos (e.g., a pilot reviewing both) can mount both. Per-repo agents mount only their repo.

2. **Workbench networking**: **Resolved.** Agents reach workbench results through daemon-mediated tool calls. The daemon runs `docker exec` in workbench containers and returns output via the tool API. No direct network path from agent sandbox to workbench. Tool definitions in the environment config specify `target: workbench` or `target: infra`.

3. **Clamshell policy for extend**: **Resolved.** Created `.belayer/policies/extend-fullstack.yaml` — extends git-and-anthropic with openai API, npm, and Maven/Gradle providers. Custom providers for `repo1.maven.org` and `plugins.gradle.org` need to be added to clamshell's provider catalog.

4. **Agent templates**: **Resolved.** Five templates created at `.belayer/templates/`:
   - `pilot/` — claude/opus, 200 turns, 4h, no workspace (orchestrates via messaging)
   - `api-implementer/` — opencode/kimi-2.5, 100 turns, 2h, workspace: extend-api
   - `app-implementer/` — opencode/kimi-2.5, 100 turns, 2h, workspace: extend-app
   - `reviewer/` — claude/sonnet, 20 turns, 30m, ephemeral (works from diffs, no workspace)
   - `sprite/` — claude/haiku, 10 turns, 10m, ephemeral (inherits spawner's workspace)

   Each template follows the Scion athenaeum pattern: `agent.yaml` (config), `system-prompt.md` (identity), `agents.md` (operations).

5. **Multi-repo coordination**: **Resolved.** One clamshell sandbox per agent, one repo per agent. Cross-repo coordination through pilot messaging. The reviewer works from diffs sent by the pilot, not mounted repos. The pilot's `agents.md` is the orchestration playbook — hardcoded roster, spawn commands, coordination patterns.

## Open Questions

1. ~~**Pilot persistence**~~: **Resolved.** Relaunched with compiled memory each session. Pilot writes workspace artifacts (memory files), reflection consolidates post-session. Next session boots with richer memory. Same pattern as ralph loops — each iteration starts fresh with accumulated state. For epics, sequential sessions get progressively smarter pilots — each session's reflection enriches the next launch's compiled memory.

2. **Jira integration**: MCP server vs custom tool vs CLI wrapper? Depends on Extend's Jira setup (Cloud vs Server) and existing tooling.

3. **opencode/kimi in clamshell image**: Clamshell sandbox image currently has claude + codex. Need to add opencode to support kimi-2.5 as implementer model. Mechanical change to `images/sandbox-rootfs/Dockerfile`.

4. **Maven/Gradle provider in clamshell**: The extend-fullstack policy references Maven Central and Gradle Plugin Portal, but these aren't in clamshell's built-in provider catalog yet. Need to add `maven_central` and `gradle_plugins` providers.
