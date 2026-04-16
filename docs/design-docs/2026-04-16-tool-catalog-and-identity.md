# Tool Catalog Co-located with Agent Identity

**Status:** Accepted
**Date:** 2026-04-16
**Motivation:** In the `mermaidflow-full` run (session `44a7b50e`), the supervisor called `belayer_approve_completion` to override a PM rejection because all seven belayer tools were registered on every agent. The design assumption that "which tools an agent uses is governed by its soul, not tool gating" proved wrong — the supervisor rationalized around the PM gate and self-approved.

## Problem

Three issues surfaced:

1. **No tool gating.** Every agent gets every belayer tool. The supervisor can approve its own work. Specialists can request completion. The PM can spawn agents.
2. **Templates are stale.** The system spawns `planner`, `frontend`, `backend`, `qa`, `reviewer`, `pm`. The templates define `pilot`, `app-implementer`, `api-implementer`, `reviewer`, `sprite`, `pm`. Half the agents have no matching template and get no system prompt from belayer.
3. **Tools are invisible from identity.** To understand what an agent can do, you have to read Python code in `hermes_bridge/tools.py`. The template directory — the intended home for agent identity — says nothing about capabilities.

## Invariant

Two agents are structurally guaranteed in every belayer run: the **supervisor** and the **PM**. The supervisor orchestrates work (spawning specialists, coordinating branches, verifying outputs) and the PM independently validates the result before the session can close. Tool gating enforces separation — the supervisor cannot approve its own work.

Everything else is flexible. For a small task the supervisor does the work itself and calls `request_completion` directly. For a complex multi-repo job it spawns frontend, backend, reviewer, and QA agents. The specialist roster is determined at runtime by the supervisor based on the work — belayer doesn't prescribe it. Templates exist for common roles but are not required; any agent name that has a matching `templates/<name>/` directory gets its identity injected.

## Design

### 1. Rename templates to match actual agent roster

| Template | Replaces | Spawned by |
|---|---|---|
| `supervisor` | `pilot` | `run start` CLI |
| `frontend` | `app-implementer` | supervisor via `spawn_agent` |
| `backend` | `api-implementer` | supervisor via `spawn_agent` |
| `reviewer` | `reviewer` | supervisor via `spawn_agent` |
| `qa` | _(new)_ | supervisor via `spawn_agent` |
| `pm` | `pm` | daemon auto-spawn on completion request |
| `sprite` | `sprite` | supervisor via `spawn_agent` |

The old `pilot`, `app-implementer`, `api-implementer` template directories are removed.

### 2. Add `belayer_tools` to `agent.yaml`

Each template's `agent.yaml` gains a `belayer_tools:` field listing the tool names that agent receives beyond the baseline.

**Baseline tools (always registered, not listed):**
- `belayer_send_message` — agent-to-agent messaging
- `belayer_report_status` — status events (working/blocked/done)
- `belayer_create_artifact` — register durable outputs

**Role-specific tool assignments:**

```yaml
# supervisor/agent.yaml
belayer_tools:
  - belayer_spawn_agent
  - belayer_request_completion

# pm/agent.yaml
belayer_tools:
  - belayer_approve_completion
  - belayer_reject_completion

# frontend/agent.yaml, backend/agent.yaml, reviewer/agent.yaml, qa/agent.yaml, sprite/agent.yaml
belayer_tools: []
```

The supervisor can spawn agents and request completion review. Only the PM can approve or reject. Everyone else gets the baseline.

### 3. Daemon reads `belayer_tools` and passes to bridge

The daemon already reads `templates/<name>/system-prompt.md` at spawn time. It will also read `templates/<name>/agent.yaml` and extract `belayer_tools`.

The tool list is passed to the bridge subprocess via a new `BELAYER_TOOLS` environment variable (comma-separated tool names). Example:

```
BELAYER_TOOLS=belayer_spawn_agent,belayer_request_completion
```

An empty value means baseline-only.

### 4. Bridge filters tool registration

`register_belayer_tools()` gains a `allowed_tools` parameter. It always registers the three baseline tools, then only registers additional tools that appear in the allowed list.

```python
BASELINE_TOOLS = {
    "belayer_send_message",
    "belayer_report_status",
    "belayer_create_artifact",
}

def register_belayer_tools(agent, agent_id, session_id, socket_path, allowed_tools=None):
    tools_to_register = set(BASELINE_TOOLS)
    if allowed_tools:
        tools_to_register |= set(allowed_tools)
    for schema, make_handler in _HANDLER_FACTORIES:
        if schema["name"] not in tools_to_register:
            continue
        # ... existing registration logic
```

### 5. Improve `belayer_request_completion` description

The current description is too casual. The new description makes the finality explicit:

```
Signal that the run is complete and ready to close. This is a terminal
workflow action — it ends the active work phase and spawns the PM agent
for adversarial spec-vs-reality verification. The PM will independently
verify every spec item against the code and either approve (closing the
session) or reject (returning gaps for remediation).

Do NOT call this until:
- All specialist agents have reported done
- All implementation branches have been merged or are ready
- You have independently verified the work (tests pass, builds succeed)
- Review and QA feedback has been addressed

Once called, you must wait for the PM's verdict. You cannot approve
or reject the run yourself.
```

### 6. Rename `planner` → `supervisor` in daemon code

The daemon hardcodes `"planner"` in several places:
- `bridge_events.go` — routing state-change messages, checking agent name for blocked-status handling
- `agents.go` — ephemeral default check (`req.Role == "planner"` → non-ephemeral)

All references change to `"supervisor"`.

The `run start` CLI command changes from spawning an agent named `"planner"` to `"supervisor"`.

### 7. Update `AGENT_ARCHITECTURE.md`

Replace the line:

> All seven Belayer tools are registered on every agent. Which tools an agent actually uses is governed by its soul, not tool gating.

With:

> Each agent template declares which belayer tools it receives via the `belayer_tools` field in `agent.yaml`. Three baseline tools (send_message, report_status, create_artifact) are always registered. Role-specific tools are only available to agents whose templates declare them. The supervisor can spawn agents and request completion. Only the PM can approve or reject a run.

Update the tool table and mermaid diagram to reflect the baseline/role-specific split.

## Files changed

| File | Change |
|---|---|
| `templates/supervisor/` | New directory (content migrated from `pilot/`) |
| `templates/frontend/` | New directory (content migrated from `app-implementer/`) |
| `templates/backend/` | New directory (content migrated from `api-implementer/`) |
| `templates/qa/` | New directory |
| `templates/pilot/` | Removed |
| `templates/app-implementer/` | Removed |
| `templates/api-implementer/` | Removed |
| `templates/*/agent.yaml` | Add `belayer_tools:` field to all |
| `hermes_bridge/tools.py` | Filter registration by allowed_tools param |
| `hermes_bridge/__main__.py` | Read `BELAYER_TOOLS` env var, pass to registration |
| `internal/daemon/agents.go` | Read `agent.yaml`, pass tools list via env var |
| `internal/daemon/bridge_events.go` | `"planner"` → `"supervisor"` |
| `internal/cli/run.go` | Spawn `"supervisor"` instead of `"planner"` |
| `docs/AGENT_ARCHITECTURE.md` | Update tool gating section |

## What doesn't change

- Tool schemas and handler implementations stay in `hermes_bridge/tools.py`
- The daemon does not interpret or enforce tool semantics — it passes the declared list through
- System prompt content is updated for the rename but the injection mechanism is unchanged
- The PM completion gate flow is unchanged — only the tool access is restricted
