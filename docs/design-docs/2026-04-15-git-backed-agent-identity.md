# Git-Backed Agent Identity: Soul + Capabilities

Status: `forward-looking` — design direction, not yet implemented

## The idea

A git repo becomes the canonical backing store for agent identities. Each agent type (planner, implementer, reviewer, QA, etc.) is defined by two co-authored halves:

1. **Soul** — who the agent is, how it thinks, what it cares about
2. **Capabilities** — what infrastructure the agent needs to operate

These live together in the same repo and are materialized at run time by Belayer when spawning agents.

## Why git

- Versionable. Identity changes are tracked, diffable, reviewable.
- Forkable. Teams can fork a base identity repo and customize.
- Composable. Shared traits (like "always validate visually") can live in base files and be inherited.
- Portable. The same identity repo works across local dev, CI, and remote Nightshift workers.
- Already the pattern. Hermes profiles, skills, and plugins are already files in a directory. This formalizes what already exists.

## Current state in the codebase

`AgentSpec` in `internal/session/template.go` already has:

```go
type AgentSpec struct {
    Name         string
    Vendor       string
    Model        string
    Role         string              // human-readable description
    SystemPrompt string              // proto-soul: flat string today
    MCPConfig    string              // proto-capabilities: path to MCP config
    Settings     string              // proto-capabilities: path to settings
    Env          map[string]string   // proto-capabilities: environment vars
}
```

This is the seed. `SystemPrompt` is a flat string doing the job of a soul. `MCPConfig`, `Settings`, and `Env` are doing the job of capabilities. The design doc below describes where this should go.

## Soul

The soul defines the agent's identity, dispositions, and behavioral leanings. It is NOT a task description or a checklist. It shapes how the agent approaches all work, not what specific work to do.

### What belongs in the soul

- **Role identity**: "You are the QA agent. You validate that what was built actually works."
- **Behavioral dispositions**: "You don't trust that frontend work is correct until you've seen it in a browser." / "You prefer to resolve versions from registries, not from memory."
- **Judgment calibration**: "Distinguish between blocking issues and suggestions." / "Be thorough but practical."
- **Coordination style**: "When you finish a task, summarize what you changed so the pilot can coordinate next steps."
- **Memory guidance**: "Write observations about effective patterns to your memory so future sessions benefit."

### What does NOT belong in the soul

- Task-specific instructions (those come from the planner via messages)
- Hardcoded workflows or checklists (those are brittle and prevent adaptation)
- Tool-specific instructions (those belong in capabilities or skill definitions)

### Soul file structure (proposed)

```
identities/
  base/
    soul.md              # shared dispositions (all agents inherit)
  planner/
    soul.md              # planner-specific identity + dispositions
  implementer/
    soul.md
  reviewer/
    soul.md
  qa/
    soul.md              # includes visual validation disposition
  merger/
    soul.md
```

Each `soul.md` is a markdown file that gets injected as the system prompt (or part of it) when Belayer spawns the agent via Hermes. The base soul is prepended to the role-specific soul.

### Example: QA agent soul (sketch)

```markdown
You are the QA agent. Your job is to verify that what was built actually works
for real users, not just that it compiles and passes unit tests.

You are skeptical by default. Code that compiles is not code that works.
Tests that pass are not features that ship correctly.

For frontend work, you validate visually. You use Playwright for automated
verification and chrome-dev-tools MCP for live inspection. You don't report
a frontend task as complete until you've seen it render correctly in a browser.

For backend work, you validate through integration. You call the actual endpoints,
check the actual responses, verify the actual error handling.

You report what you find honestly. If something looks wrong, say so. If something
looks right, say that too. Don't hedge.
```

## Capabilities

Capabilities declare what infrastructure the agent needs to operate. They are the machine-readable counterpart to the soul. The soul tells the agent who it is. Capabilities tell the worker control plane what to provision.

### What belongs in capabilities

- **MCP servers**: which MCP servers the agent needs (chrome-dev-tools, project-specific servers, etc.)
- **Runtime dependencies**: headless Chrome, specific CLI tools, language runtimes
- **Auth tokens**: which credentials need to be staged (GitHub, Vercel, Fly, npm, etc.)
- **Hermes configuration**: profile selection, plugin enablement, skill preloading
- **Environment variables**: beyond what Belayer already injects

### Capabilities file structure (proposed)

```
identities/
  qa/
    soul.md
    capabilities.yaml
```

```yaml
# identities/qa/capabilities.yaml
mcp_servers:
  - name: chrome-dev-tools
    required: true
    note: "Visual validation of frontend work"

runtime:
  - name: chromium
    required: true
    note: "Headless browser for Playwright and DevTools MCP"
  - name: playwright
    required: true

auth:
  - name: github
    required: false
    note: "For reading PR status, not for pushing"

hermes:
  plugins:
    - belayer-communication
  skills:
    - belayer-finish
```

### Who consumes capabilities

- **Belayer** reads the soul and injects it as the system prompt when spawning via Hermes.
- **Worker control plane** reads the capabilities and provisions the infrastructure before Belayer starts the run. This is the bootstrapping step.
- **Hermes profile builder** reads the hermes section of capabilities and configures the profile accordingly.

This is the key architectural split: the soul goes to the harness (Hermes), the capabilities go to the infrastructure (worker control plane + Hermes config).

## The bootstrapping problem

Today, Hermes runs locally and inherits whatever the developer has: MCP servers, auth tokens, browser, CLI tools. When deploying remotely on a Nightshift worker, none of that exists.

The sequence for a remote run becomes:

1. Worker control plane assigns a request to a worker
2. Worker reads the session template (which agents are needed)
3. For each agent, worker reads its `capabilities.yaml`
4. Worker provisions: installs runtimes, starts MCP servers, stages auth tokens
5. Belayer daemon starts, reads soul files, spawns agents via Hermes with the provisioned environment

Step 4 is new. Today it doesn't exist because everything runs locally. This is the gap that capabilities.yaml fills.

### What this does NOT solve yet

- How auth tokens get from the user's vault to the worker (credential mediation, probably via clamshell)
- How MCP servers are started and health-checked on the worker
- How capabilities are validated before a run starts (preflight check)
- How capabilities compose when an agent needs project-specific MCP servers in addition to its base set

These are real problems but they're solvable incrementally. The first step is having a declarative format for what's needed.

## Linking to a git repo

The identity repo is referenced by the Belayer workspace configuration:

```yaml
# .belayer/config.yaml (or equivalent)
identity_repo: git@github.com:org/nightshift-identities.git
identity_ref: main  # branch or tag
```

At run start, Belayer clones or pulls the identity repo and materializes the relevant souls + capabilities. For local dev, the identity repo can be a local path instead of a remote.

### Default identities

Belayer ships with built-in default identities (the current `builtinTemplates` in `template.go`). The git-backed repo overrides these. If no repo is configured, the built-ins work as they do today. This preserves zero-config local dev.

## Relationship to existing AgentSpec

The current `AgentSpec` evolves to reference identity files rather than inline everything:

```go
type AgentSpec struct {
    Name     string            `yaml:"name"`
    Identity string            `yaml:"identity"`       // e.g., "qa" — resolves to identities/qa/
    Vendor   string            `yaml:"vendor"`
    Model    string            `yaml:"model"`
    Env      map[string]string `yaml:"env,omitempty"`  // run-specific overrides

    // These become computed from the identity at spawn time:
    // - SystemPrompt from soul.md
    // - MCPConfig from capabilities.yaml
    // - Settings from capabilities.yaml
}
```

The `Identity` field is the link. It resolves to a directory in the identity repo. The soul and capabilities are loaded from there. Run-specific overrides (vendor, model, env) can still be set in the session template.

## Migration path

1. **Now**: document the direction (this doc), keep current AgentSpec working as-is
2. **Soon**: extract current `SystemPrompt` strings from `builtinTemplates` into `identities/*/soul.md` files in the Belayer repo itself. AgentSpec gains `Identity` field, falls back to inline `SystemPrompt` if not set.
3. **Later**: add `capabilities.yaml` parsing and wire it into the Hermes launch builder. Worker control plane reads capabilities for provisioning.
4. **Eventually**: support external git repos as identity sources. Teams fork and customize.
