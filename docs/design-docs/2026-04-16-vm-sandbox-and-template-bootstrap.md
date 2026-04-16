---
status: superseded
created: 2026-04-16
supersedes:
implemented-by:
consulted-learnings:
  - docs/design-docs/2026-04-15-nightshift-v1-deployment-topology.md
  - docs/design-docs/2026-04-15-git-backed-agent-identity.md
  - docs/design-docs/2026-04-15-hermes-bridge-sidecar.md
---

# VM Sandbox, Network Policy, and Template Bootstrap

Prove that Belayer can run inside a Lima VM, agents execute through Hermes with managed network egress, and chalk-bag provides the permanent skill/template home for ephemeral sessions.

## Three goals

1. **VM testbed** — use the existing `devbox` Lima VM as the execution environment for belayer + hermes agents
2. **Network sandbox** — manage agent egress so only declared endpoints are reachable, with clamshell as an optional enforcement layer
3. **Template bootstrap** — use `chalk-bag` as the permanent, git-backed home for agent templates, skills, and tool definitions that get materialized into ephemeral hermes sessions

---

## Part 1: Lima VM as Testbed

### What we have

```
$ limactl list
NAME        STATUS   CPUS  MEMORY  DISK
devbox      Stopped  8     16GiB   100GiB
```

- Ubuntu 24.04 LTS (ARM64, Apple VZ with Rosetta)
- `~/Documents/Programs` mounted writable via virtiofs
- Port forwards: 3000-3100, 5432, 6379, 8080, 9000
- Provisioned with: build-essential, git, Node.js 20, Python 3, tmux, ripgrep, etc.

### What we need to install in the VM

```bash
# 1. Go (for building belayer)
sudo snap install go --classic  # or wget + /usr/local/go

# 2. Hermes agent
# Clone or pip install hermes-agent into ~/.hermes/hermes-agent/
python3 -m venv ~/.hermes/hermes-agent/venv
~/.hermes/hermes-agent/venv/bin/pip install hermes-agent

# 3. Hermes CLI (for profile/auth management)
# Same venv or separate
~/.hermes/hermes-agent/venv/bin/pip install hermes-cli

# 4. Docker (for clamshell, optional)
sudo apt-get install docker.io docker-compose-plugin
sudo usermod -aG docker $USER

# 5. Belayer binary
cd ~/Documents/Programs/personal/belayer
go build -o ~/.local/bin/belayer ./cmd/belayer

# 6. chalk-bag (clone if not already mounted)
# Already at ~/Documents/Programs/personal/chalk-bag via virtiofs mount
```

### VM bootstrap script

```bash
#!/usr/bin/env bash
# belayer-devbox-setup.sh — run inside limactl shell devbox
set -euo pipefail

echo "=== Installing Go ==="
if ! command -v go &>/dev/null; then
  sudo snap install go --classic
fi

echo "=== Setting up Hermes venv ==="
HERMES_HOME="$HOME/.hermes"
VENV="$HERMES_HOME/hermes-agent/venv"
mkdir -p "$HERMES_HOME/hermes-agent" "$HERMES_HOME/profiles"
if [ ! -d "$VENV" ]; then
  python3 -m venv "$VENV"
fi
"$VENV/bin/pip" install --upgrade pip
"$VENV/bin/pip" install hermes-agent hermes-cli

echo "=== Building Belayer ==="
BELAYER_ROOT="$HOME/Documents/Programs/personal/belayer"
cd "$BELAYER_ROOT"
go build -o "$HOME/.local/bin/belayer" ./cmd/belayer

echo "=== Verifying ==="
belayer --version || echo "belayer not on PATH, add ~/.local/bin"
"$VENV/bin/python3" -c "from hermes_agent import AIAgent; print('hermes OK')"

echo "=== Done ==="
```

### Why the VM matters

The devbox VM proves the deployment topology from the Nightshift v1 design doc:

- **Host (macOS)** = operator workstation
- **VM** = simulated Nightshift worker node
- **One request, one worker, one belayer session** — the v1 constraint

If belayer + hermes agents run correctly inside the VM, the same topology works on a real Linux worker node.

---

## Part 2: Network Sandbox — With or Without Clamshell

### Design: two enforcement modes

The user wants clamshell to be **optional**. This means belayer needs an abstraction layer:

```
┌─────────────────────────────────┐
│         Belayer Daemon          │
│                                 │
│  bridge.Spawn(cfg) ────────────┼──► Python subprocess
│                                 │
│  cfg.SandboxMode:              │
│    "none"     → direct exec    │
│    "clamshell" → clamshell     │
│                sandbox create  │
└─────────────────────────────────┘
```

### Mode: `none` (current behavior, zero overhead)

- Bridge subprocess runs directly via `python3 -m hermes_bridge`
- Agent inherits host/VM environment, network, credentials
- No isolation whatsoever
- Good for: local dev, trusted environments, quick iteration

### Mode: `clamshell` (sandboxed, managed egress)

- Belayer daemon calls `clamshell sandbox create` before spawning the bridge
- Bridge subprocess runs inside the sandbox container
- Network egress is deny-by-default, governed by policy YAML
- Credentials mediated through clamshell proxy (opaque handles)
- Good for: production workers, untrusted agent code, compliance

### Complexity assessment: how much does dual-mode add?

**Low complexity.** Here's why:

1. **The bridge interface doesn't change.** Whether running natively or in clamshell, the bridge is still `python3 -m hermes_bridge` with the same env vars. Clamshell wraps the execution environment, not the protocol.

2. **Config is a single field.** `SandboxMode` in the bridge config (already extensible). The spawn path branches once: direct exec vs clamshell create + exec-inside.

3. **Policy files are standalone.** They don't need to be compiled into belayer. They're YAML files shipped alongside templates. Clamshell reads them directly.

4. **No code in the hot path.** The sandbox setup happens once at agent spawn, not per-turn. The ongoing bridge<->daemon communication is identical in both modes (Unix socket HTTP).

**What it costs:**
- ~50 lines in `internal/bridge/bridge.go` for the clamshell spawn path
- A `sandbox_mode` field in `.belayer/config.yaml` and per-environment overrides
- Policy YAML files shipped in the belayer repo (already started: `.belayer/policies/`)
- Documentation for both modes

**What it does NOT cost:**
- No abstraction layer or plugin interface needed
- No runtime behavioral differences (bridge protocol is identical)
- No conditional logic in hermes_bridge Python code
- No changes to the daemon HTTP API

### Implementation sketch

```go
// internal/bridge/bridge.go

func (b *Bridge) Spawn(cfg Config) (*Process, error) {
    switch cfg.SandboxMode {
    case "clamshell":
        return b.spawnViaClamshell(cfg)
    default: // "none" or empty
        return b.spawnDirect(cfg)
    }
}

func (b *Bridge) spawnViaClamshell(cfg Config) (*Process, error) {
    // 1. Ensure clamshell gateway is running
    // 2. Create sandbox with policy from cfg.PolicyPath
    // 3. Bootstrap: clone chalk-bag, set up hermes profile
    // 4. Execute bridge inside sandbox
    // 5. Set up port forward for daemon Unix socket
    // Returns Process handle that wraps clamshell sandbox lifecycle
}
```

### Configuration

```yaml
# .belayer/config.yaml
sandbox:
  mode: none           # "none" | "clamshell"
  policy: .belayer/policies/default.yaml
  clamshell:
    gateway_url: http://127.0.0.1:8787
    providers:
      - github=github-main
      - anthropic=anthropic-main
```

Override per environment:

```yaml
# .belayer/environments/extend-fullstack.yaml
sandbox:
  mode: clamshell
  policy: .belayer/policies/extend-fullstack.yaml
```

---

## Part 3: Hermes Network Policy — What Agents Actually Need

### Hermes network surface (empirically derived)

From the hermes-agent source code, here are ALL the network endpoints a running agent might contact:

#### Tier 1: Always needed (LLM inference)

| Domain | Port | Purpose | Binary |
|--------|------|---------|--------|
| `api.anthropic.com` | 443 | Claude API | python3 |
| `api.openai.com` | 443 | OpenAI/GPT API | python3 |
| `auth.openai.com` | 443 | Codex OAuth token refresh | python3 |
| `chatgpt.com` | 443 | Codex inference endpoint | python3 |
| `ab.chatgpt.com` | 443 | Codex inference (alt) | python3 |
| `inference-api.nousresearch.com` | 443 | Nous/Hermes inference | python3 |
| `portal.nousresearch.com` | 443 | Nous OAuth + agent keys | python3 |

#### Tier 2: Common providers (model routing)

| Domain | Port | Purpose | Binary |
|--------|------|---------|--------|
| `openrouter.ai` | 443 | OpenRouter multi-model | python3 |
| `api.deepseek.com` | 443 | DeepSeek | python3 |
| `api.moonshot.cn` | 443 | Moonshot/Kimi | python3 |
| `api.kimi.com` | 443 | Kimi coding API | python3 |
| `generativelanguage.googleapis.com` | 443 | Google Gemini | python3 |

#### Tier 3: Agent tools (used by hermes built-in tools)

| Domain | Port | Purpose | Binary |
|--------|------|---------|--------|
| `api.github.com` | 443 | GitHub API (skills, repos) | python3, git, gh |
| `github.com` | 443 | Git clone/fetch | git |
| `api.tavily.com` | 443 | Web search tool | python3 |
| `api.firecrawl.dev` | 443 | Web crawl tool | python3 |
| `api.osv.dev` | 443 | Vulnerability scanning | python3 |

#### Tier 4: Package managers (workspace setup)

| Domain | Port | Purpose | Binary |
|--------|------|---------|--------|
| `registry.npmjs.org` | 443 | npm packages | npm, npx |
| `pypi.org` | 443 | Python packages | pip, pip3 |
| `files.pythonhosted.org` | 443 | Python package files | pip, pip3 |

#### Tier 5: Skills hub (hermes-specific)

| Domain | Port | Purpose | Binary |
|--------|------|---------|--------|
| `hermes-agent.nousresearch.com` | 443 | Skills index | python3 |
| `skills.sh` | 443 | Skills.sh marketplace | python3 |
| `clawhub.ai` | 443 | ClawHub marketplace | python3 |

#### Tier 6: Chat platforms (NOT needed for belayer)

Discord, Slack, Telegram, etc. — these are gateway features, not relevant for coding agents.

### Default policy set for Belayer

We ship three policies:

#### 1. `belayer-minimal.yaml` — Anthropic-only, no tools

For: testing, simple single-provider runs.

```yaml
version: 5

sandbox:
  user: sandbox
  group: sandbox
  work_dir: /workspace
  runtime_dir: /run/agent
  artifact_outbox: /run/agent/outbox
  read_only_paths: [/usr, /lib, /lib64, /bin, /sbin, /etc, /var/log, /app]
  read_write_paths: [/tmp, /workspace, /run/agent, /run/agent/outbox]
  masked_paths: [/proc/kcore, /proc/latency_stats, /proc/timer_list, /sys/firmware]
  tmpfs_paths: [/tmp]

process:
  seccomp_profile: ./etc/seccomp-default.json
  no_new_privileges: true
  drop_capabilities: ['ALL']
  memory_limit: 2048m
  pids_limit: 256
  cpu_quota: 200000
  read_only_rootfs: true

network:
  mode: proxy
  proxy_listen: 3128
  allow_http: false
  providers:
    anthropic_api: true
    github: true
  localhost: []
```

#### 2. `belayer-standard.yaml` — Multi-provider + packages

For: typical development runs with multiple LLM providers and package managers.

```yaml
version: 5

sandbox:
  user: sandbox
  group: sandbox
  work_dir: /workspace
  runtime_dir: /run/agent
  artifact_outbox: /run/agent/outbox
  read_only_paths: [/usr, /lib, /lib64, /bin, /sbin, /etc, /var/log, /app]
  read_write_paths: [/tmp, /workspace, /run/agent, /run/agent/outbox]
  masked_paths: [/proc/kcore, /proc/latency_stats, /proc/timer_list, /sys/firmware]
  tmpfs_paths: [/tmp]

process:
  seccomp_profile: ./etc/seccomp-default.json
  no_new_privileges: true
  drop_capabilities: ['ALL']
  memory_limit: 2048m
  pids_limit: 512
  cpu_quota: 200000
  read_only_rootfs: true

network:
  mode: proxy
  proxy_listen: 3128
  allow_http: false
  providers:
    # LLM APIs
    anthropic_api: true
    openai_api: true
    # Version control
    github: true
    # Package managers
    npm: true
    pypi: true
  custom_endpoints:
    # Hermes-specific: Nous portal (OAuth + inference)
    - host: portal.nousresearch.com
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Hermes Nous OAuth and agent key minting"
    - host: inference-api.nousresearch.com
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Hermes Nous inference API"
    # Hermes-specific: Kimi/Moonshot (if using kimi models)
    - host: api.kimi.com
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Kimi coding inference (Hermes provider)"
    - host: api.moonshot.cn
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Moonshot inference (Hermes provider)"
    # OpenRouter (multi-model gateway, common for hermes users)
    - host: openrouter.ai
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "OpenRouter multi-model inference"
    # Web search (hermes built-in tool)
    - host: api.tavily.com
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Tavily search API (hermes web_search tool)"
    # Skills hub
    - host: hermes-agent.nousresearch.com
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Hermes skills index"
  localhost: []
```

#### 3. `belayer-workbench.yaml` — Standard + sidecar services

For: runs with database, localstack, etc. Extends standard with tcp_endpoints.

```yaml
version: 5

sandbox:
  user: sandbox
  group: sandbox
  work_dir: /workspace
  runtime_dir: /run/agent
  artifact_outbox: /run/agent/outbox
  read_only_paths: [/usr, /lib, /lib64, /bin, /sbin, /etc, /var/log, /app]
  read_write_paths: [/tmp, /workspace, /run/agent, /run/agent/outbox]
  masked_paths: [/proc/kcore, /proc/latency_stats, /proc/timer_list, /sys/firmware]
  tmpfs_paths: [/tmp]

process:
  seccomp_profile: ./etc/seccomp-default.json
  no_new_privileges: true
  drop_capabilities: ['ALL']
  memory_limit: 2048m
  pids_limit: 512
  cpu_quota: 200000
  read_only_rootfs: true

network:
  mode: proxy
  proxy_listen: 3128
  allow_http: false
  providers:
    anthropic_api: true
    openai_api: true
    github: true
    npm: true
    pypi: true
  custom_endpoints:
    # Same hermes endpoints as standard...
    - host: portal.nousresearch.com
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Hermes Nous OAuth and agent key minting"
    - host: inference-api.nousresearch.com
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Hermes Nous inference API"
    - host: api.kimi.com
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Kimi coding inference"
    - host: api.moonshot.cn
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Moonshot inference"
    - host: openrouter.ai
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "OpenRouter multi-model inference"
    - host: api.tavily.com
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Tavily search API"
    - host: hermes-agent.nousresearch.com
      ports: [443]
      protocol: rest
      enforcement: enforce
      tls: auto
      note: "Hermes skills index"
  localhost:
    - host: "127.0.0.1"
      ports: [8080]
      note: "Local dev server"
  tcp_endpoints:
    - host: belayer-postgres
      ports: [5432]
      allowed_ips: ["172.16.0.0/12", "192.168.0.0/16"]
      materialization: deferred
      note: "Session-local Postgres"
    - host: belayer-redis
      ports: [6379]
      allowed_ips: ["172.16.0.0/12", "192.168.0.0/16"]
      materialization: deferred
      note: "Session-local Redis"
    - host: belayer-localstack
      ports: [4566]
      allowed_ips: ["172.16.0.0/12", "192.168.0.0/16"]
      materialization: deferred
      note: "LocalStack AWS mock"
```

### Policy discovery and validation workflow

The proof-of-concept should:

1. Start a belayer session in the VM with `sandbox: clamshell` and `belayer-standard.yaml`
2. Spawn a supervisor agent that uses Anthropic Claude
3. Have the supervisor spawn a specialist that uses a different provider (Kimi, OpenRouter, etc.)
4. Watch for deny events in `~/.extend-clamshell/runtime/*/host/events/deny-events.json`
5. Iterate on the policy until all legitimate requests succeed and nothing else leaks

This is the "loop to see network requests block" workflow the user asked for.

---

## Part 4: chalk-bag as Permanent Template Home

### The problem

Belayer templates currently live in `belayer/templates/`. This works for the belayer repo itself, but:

1. **Templates are reusable across projects.** The supervisor soul, PM gate, reviewer prompt — these aren't belayer-specific. They're general agent archetypes.
2. **Skills need a home.** Hermes skills (from chalk-bag's harness/pr plugins) need to be discoverable by agents at runtime.
3. **Ephemeral sessions need bootstrapping.** When a hermes agent spawns inside a clamshell sandbox, it starts with a bare environment. It needs skills, templates, and capabilities injected.
4. **chalk-bag already has the infrastructure.** Plugin manifests, command files, agent definitions, reference docs — all in markdown with YAML frontmatter. Already has a `docs/hermes.md` integration guide.

### The solution

chalk-bag becomes the **canonical template repository** referenced by belayer. Belayer content lives at the top level — not inside `plugins/` — because it's infrastructure consumed by the daemon, not a Claude Code marketplace plugin.

```
chalk-bag/
  plugins/
    harness/          # existing: Claude Code marketplace plugin
    pr/               # existing: Claude Code marketplace plugin
  belayer/            # NEW: top-level domain, NOT a plugin
    templates/
      supervisor/
        soul.md           # was: system-prompt.md
        capabilities.yaml # was: agent.yaml (expanded)
        agents.md         # operating instructions
      pm/
        soul.md
        capabilities.yaml
        agents.md
      backend/
        ...
      frontend/
        ...
      reviewer/
        ...
      qa/
        ...
      sprite/
        ...
    policies/
      belayer-minimal.yaml
      belayer-standard.yaml
      belayer-workbench.yaml
    references/
      hermes-network-surface.md
```

**Why top-level, not a plugin:** The `plugins/` directory is for Claude Code marketplace distribution — harness and pr belong there because users invoke them as `/harness:brainstorm` or `/pr:review`. Belayer templates are infrastructure that the daemon reads at spawn time. They don't have commands, agents in the plugin sense, or skill triggers. Forcing them into the plugin format would add a `plugin.json` and `.claude-plugin/` scaffolding that nothing consumes.

### Migration: templates/ → chalk-bag

| Current (belayer) | New (chalk-bag) | Notes |
|---|---|---|
| `templates/supervisor/system-prompt.md` | `belayer/templates/supervisor/soul.md` | Rename to match identity design doc |
| `templates/supervisor/agent.yaml` | `belayer/templates/supervisor/capabilities.yaml` | Expand with hermes profile, MCP, etc. |
| `templates/supervisor/agents.md` | `belayer/templates/supervisor/agents.md` | Move as-is |
| `.belayer/policies/*.yaml` | `belayer/policies/*.yaml` | Centralize policies |

Belayer repo keeps a **fallback copy** of minimal templates for zero-config local dev (the existing `builtinTemplates` pattern). But the canonical source is chalk-bag.

### capabilities.yaml evolution

The current `agent.yaml` is minimal:

```yaml
# Current (belayer/templates/supervisor/agent.yaml)
belayer_tools:
  - belayer_spawn_agent
  - belayer_request_completion
```

The new `capabilities.yaml` in chalk-bag covers the full identity spec:

```yaml
# New (chalk-bag/belayer/templates/supervisor/capabilities.yaml)

# Identity metadata
vendor: anthropic
model: claude-sonnet-4-20250514
tier: orchestrator
ephemeral: false

# Belayer tools (role-gated)
belayer_tools:
  - belayer_spawn_agent
  - belayer_request_completion

# Hermes profile configuration (materialized at spawn time)
hermes:
  plugins:
    - harness   # resolved from chalk-bag/plugins/harness
  skills:
    - brainstorm
    - plan
    - orchestrate

# MCP servers needed
mcp_servers: []

# Runtime dependencies
runtime: []

# Network policy tier (references policy file)
network_tier: standard  # maps to belayer-standard.yaml
```

### Bootstrapping into ephemeral hermes sessions

When belayer spawns an agent (with or without clamshell), it needs to materialize a hermes profile. The flow:

```
1. Belayer reads capabilities.yaml from chalk-bag
2. Belayer creates a temporary Hermes profile directory:
   ~/.hermes/profiles/belayer-{sessionID}-{agentName}/
3. Belayer writes:
   - config.yaml (model, provider, api_key/base_url)
   - plugins/ symlinks to chalk-bag/plugins/ (harness, pr)
   - skills/ loaded from capabilities.yaml declarations
4. Belayer sets HERMES_HOME to the materialized profile dir
5. Bridge subprocess uses the materialized profile
6. On agent exit, profile directory is cleaned up (ephemeral)
```

This solves the **profile bootstrap TODO** from AGENT_ARCHITECTURE.md.

### chalk-bag reference in belayer config

```yaml
# .belayer/config.yaml
templates:
  source: git                          # "builtin" | "local" | "git"
  repo: git@github.com:donovan-yohan/chalk-bag.git
  ref: main
  path: belayer/templates                # within the repo
  local_path: ~/Documents/Programs/personal/chalk-bag/belayer/templates  # for dev

# For local dev, shortcut:
templates:
  source: local
  local_path: ~/Documents/Programs/personal/chalk-bag/belayer/templates
```

At run start, belayer resolves templates from the configured source. The `builtin` source falls back to the compiled-in defaults (current behavior).

---

## Part 5: Proof-of-Concept Plan

### Phase 1: VM Setup (day 1)

1. Start the devbox VM: `limactl start devbox`
2. Run the bootstrap script (Go, Hermes, Belayer binary)
3. Configure hermes auth inside the VM (`hermes auth` for at least one provider)
4. Run `belayer daemon` + `belayer run start` inside the VM with `sandbox: none`
5. **Success criteria**: supervisor spawns, sends a message, agents communicate through the session bus inside the VM

### Phase 2: Network Policy Discovery (day 1-2)

6. Install clamshell inside the VM (clone extend-clamshell, `pip install -e .`)
7. Start clamshell gateway: `clamshell gateway start`
8. Create a provider: `clamshell provider create --name anthropic-main --type anthropic ...`
9. Create a sandbox with `belayer-minimal.yaml` policy
10. Run a simple hermes agent inside the sandbox (not through belayer yet — direct)
11. **Watch deny events**: `tail -f ~/.extend-clamshell/runtime/*/host/events/deny-events.json`
12. Iterate: find blocked requests, add to policy, recreate sandbox, retest
13. **Success criteria**: agent completes a simple task (e.g., "write hello world to a file") with no unexpected deny events

### Phase 3: Belayer + Clamshell Integration (day 2-3)

14. Add `SandboxMode` field to bridge Config
15. Implement `spawnViaClamshell()` in bridge.go
16. Wire up `.belayer/config.yaml` sandbox configuration
17. Run `belayer run start` with `sandbox: clamshell`
18. Verify: supervisor spawns inside sandbox, can reach Anthropic API, can communicate with daemon
19. Verify: specialists spawn in separate sandboxes (or same sandbox, TBD)
20. **Success criteria**: full belayer run completes inside clamshell with policy-managed egress

### Phase 4: chalk-bag Template Bootstrap (day 3-4)

21. Create `belayer/` directory in chalk-bag (top-level, not under plugins/)
22. Migrate templates from belayer repo to chalk-bag
23. Write capabilities.yaml files (expanded from agent.yaml)
24. Implement profile materialization in belayer (write temp hermes profile from capabilities)
25. Configure belayer to read templates from local chalk-bag path
26. Run a session using chalk-bag templates
27. **Success criteria**: agent spawns with soul loaded from chalk-bag, hermes profile materialized from capabilities.yaml

### Phase 5: End-to-End Validation (day 4-5)

28. Full integration: VM + clamshell + chalk-bag templates + multi-agent session
29. Run a real task: e.g., "create a simple REST API with tests"
30. Supervisor (Anthropic) spawns backend specialist (Kimi or other)
31. Watch network: only declared endpoints reachable, credentials mediated
32. PM gate fires, verifies work
33. **Success criteria**: complete session with sandboxed agents, managed egress, and chalk-bag-sourced templates

---

## Architecture Diagram

```
┌─────────────────────────────── macOS Host ──────────────────────────────┐
│                                                                         │
│  ~/Documents/Programs/personal/                                         │
│    ├── belayer/          (source, mounted in VM via virtiofs)            │
│    └── chalk-bag/        (templates + skills, mounted in VM)            │
│                                                                         │
│  limactl start devbox                                                   │
│  ┌───────────────────── Lima VM (devbox) ────────────────────────────┐  │
│  │                                                                    │  │
│  │  belayer daemon (Go, Unix socket)                                  │  │
│  │    │                                                               │  │
│  │    ├─── sandbox: none ──────────────────────────┐                  │  │
│  │    │    python3 -m hermes_bridge                 │                  │  │
│  │    │    (direct exec, full network access)       │                  │  │
│  │    │                                             │                  │  │
│  │    └─── sandbox: clamshell ─────────────────┐    │                  │  │
│  │         clamshell gateway (localhost:8787)   │    │                  │  │
│  │         │                                    │    │                  │  │
│  │         ├── sandbox: supervisor              │    │                  │  │
│  │         │   ├── proxy (deny-by-default)      │    │                  │  │
│  │         │   ├── python3 -m hermes_bridge     │    │                  │  │
│  │         │   └── policy: belayer-standard     │    │                  │  │
│  │         │                                    │    │                  │  │
│  │         ├── sandbox: backend-specialist      │    │                  │  │
│  │         │   ├── proxy (deny-by-default)      │    │                  │  │
│  │         │   ├── python3 -m hermes_bridge     │    │                  │  │
│  │         │   └── policy: belayer-standard     │    │                  │  │
│  │         │                                    │    │                  │  │
│  │         └── sandbox: pm                      │    │                  │  │
│  │             ├── proxy (deny-by-default)      │    │                  │  │
│  │             ├── python3 -m hermes_bridge     │    │                  │  │
│  │             └── policy: belayer-minimal      │    │                  │  │
│  │                                                                    │  │
│  │  chalk-bag/ (mounted from host)                                    │  │
│  │    ├── belayer/templates/   (top-level, not a plugin)              │  │
│  │    │    ├── supervisor/soul.md + capabilities.yaml                 │  │
│  │    │    ├── pm/soul.md + capabilities.yaml                         │  │
│  │    │    └── backend/soul.md + capabilities.yaml                    │  │
│  │    └── plugins/             (harness, pr — marketplace plugins)    │  │
│  │                                                                    │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Open Questions

### 1. One sandbox per agent or one sandbox per session?

**Per-agent** (superlemmings model): each agent gets its own sandbox with its own policy. More isolation, more overhead, cleaner credential separation.

**Per-session** (simpler): all agents in one sandbox, share workspace. Less overhead, but agents can interfere with each other's files.

**Recommendation**: Start with per-session for the PoC (simpler). Evolve to per-agent when credential separation matters (e.g., different providers per agent need different opaque handles).

### 2. How does the daemon communicate with sandboxed agents?

The daemon runs outside the sandbox. The bridge inside the sandbox needs to reach the daemon's Unix socket.

**Options**:
- `clamshell forward` — forward Unix socket into sandbox
- Mount the socket read-write into the sandbox (Docker volume mount)
- TCP socket on localhost with port forward

**Recommendation**: Use clamshell's bootstrap mechanism to mount the daemon socket into the sandbox at a known path. Add a localhost rule for the daemon socket if needed.

### 3. How do credentials flow to sandboxed agents?

Today: agents inherit the host's `~/.hermes/auth.json` and environment API keys.

With clamshell: credentials should flow through the proxy's `inference.local` routing or through clamshell provider attachment (opaque handles).

**For the PoC**: use environment variable injection at sandbox creation time (`ANTHROPIC_API_KEY` passed through clamshell). This is less secure but proves the path. Migrate to opaque handles later.

### 4. How does chalk-bag get into the sandbox?

**Options**:
- Git clone during sandbox bootstrap (`--bootstrap-repo`)
- Mount from host (Docker volume, read-only)
- Pre-bake into sandbox image

**Recommendation**: Mount from host read-only for dev. Clone via `--bootstrap-repo` for production workers.

---

## What This Spec Does NOT Cover

- Crag (outer daemon) integration — that's a separate design doc
- Multi-worker orchestration — v1 is one request per worker
- Production credential management (vault, rotating tokens) — deferred
- Custom MCP server provisioning inside sandboxes — deferred
- Hermes gateway mode inside sandbox — not needed for v1
