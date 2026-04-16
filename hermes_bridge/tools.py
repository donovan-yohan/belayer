"""Belayer coordination tools for Hermes agents.

Baseline tools (always registered on every agent):
  - belayer_send_message        — agent-to-agent messaging via session bus
  - belayer_report_status       — publish agent status events (working/blocked/done)
  - belayer_create_artifact     — register a durable output with the artifact registry

Role-specific tools (only registered when declared in agent.yaml):
  - belayer_spawn_agent         — supervisor spawns specialists into the session
  - belayer_request_completion  — supervisor signals "work is done, verify before closing"
  - belayer_approve_completion  — PM approves the run after spec verification
  - belayer_reject_completion   — PM rejects the run with a gap list for remediation

Tool schemas follow the OpenAI function-calling format used by Hermes.
Handlers receive kwargs matching schema property names (Hermes calling convention).
"""

import json
import logging

from hermes_bridge.http_client import unix_post

log = logging.getLogger("tools")

# ---------------------------------------------------------------------------
# Tool schemas
# ---------------------------------------------------------------------------

SEND_MESSAGE_SCHEMA = {
    "name": "belayer_send_message",
    "description": (
        "Send a message to another agent in this Belayer session. "
        "Use this to communicate with the supervisor or other specialists."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "to": {
                "type": "string",
                "description": "The recipient agent name (e.g. 'supervisor', 'backend', 'reviewer')",
            },
            "content": {
                "type": "string",
                "description": "The message content",
            },
        },
        "required": ["to", "content"],
    },
}

CREATE_ARTIFACT_SCHEMA = {
    "name": "belayer_create_artifact",
    "description": (
        "Register a durable output artifact with Belayer. "
        "Use this for shared contracts, reports, task graphs, etc."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "kind": {
                "type": "string",
                "description": "Artifact kind (e.g. 'shared-contract', 'specialist-report', 'task-graph')",
            },
            "path": {
                "type": "string",
                "description": "Path to the artifact file (relative to workspace)",
            },
            "summary": {
                "type": "string",
                "description": "Brief summary of the artifact contents",
            },
        },
        "required": ["kind", "path"],
    },
}

REPORT_STATUS_SCHEMA = {
    "name": "belayer_report_status",
    "description": (
        "Report your current status to the Belayer session bus. "
        "Use this for progress updates, marking yourself blocked, signaling completion, "
        "or escalating to a human when you've made progress but cannot finish."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "status": {
                "type": "string",
                "enum": ["working", "blocked", "done", "needs-review", "incomplete"],
                "description": (
                    "Your current status. Use 'incomplete' when you've made progress "
                    "but are stuck in a loop or cannot finish — this escalates to a human."
                ),
            },
            "detail": {
                "type": "string",
                "description": "Details about your status (what you're working on, why you're blocked, etc.)",
            },
        },
        "required": ["status"],
    },
}

SPAWN_AGENT_SCHEMA = {
    "name": "belayer_spawn_agent",
    "description": (
        "Spawn a new specialist agent in this Belayer session. "
        "Use this to bring up backend, frontend, qa, or reviewer agents when you need them. "
        "The agent starts with the given message as its first instruction. "
        "Implementer agents (backend, frontend) should be given a branch name for worktree isolation."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "name": {
                "type": "string",
                "description": "Agent name (e.g. 'backend', 'frontend', 'qa', 'reviewer')",
            },
            "profile": {
                "type": "string",
                "description": "Hermes profile to use (e.g. 'backend', 'frontend', 'qa', 'reviewer')",
            },
            "message": {
                "type": "string",
                "description": "Initial instruction/assignment for the agent",
            },
            "branch": {
                "type": "string",
                "description": (
                    "Git branch name for worktree isolation. When set, the agent works in its own "
                    "git worktree on this branch, isolated from other agents. Use for implementer "
                    "agents (backend, frontend) to prevent conflicts. Not needed for reviewers or QA."
                ),
            },
        },
        "required": ["name", "profile", "message"],
    },
}

REQUEST_COMPLETION_SCHEMA = {
    "name": "belayer_request_completion",
    "description": (
        "Signal that the run is complete and ready to close. This is a terminal "
        "workflow action — it ends the active work phase and spawns the PM agent "
        "for adversarial spec-vs-reality verification. The PM will independently "
        "verify every spec item against the code and either approve (closing the "
        "session) or reject (returning gaps for remediation). "
        "Do NOT call this until: all specialist agents have reported done, all "
        "implementation branches have been merged or are ready, you have independently "
        "verified the work (tests pass, builds succeed), and review/QA feedback has "
        "been addressed. Once called, you must wait for the PM's verdict. You cannot "
        "approve or reject the run yourself."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "summary": {
                "type": "string",
                "description": "Summary of what was accomplished, which spec items were implemented, and any deviations from the spec",
            },
            "spec_artifact": {
                "type": "string",
                "description": "Path to the spec/design-doc artifact (optional, PM will search if not provided)",
            },
        },
        "required": ["summary"],
    },
}

APPROVE_COMPLETION_SCHEMA = {
    "name": "belayer_approve_completion",
    "description": (
        "Approve the run after verifying the spec is fully satisfied. "
        "This marks the session as complete. Only call this after thorough verification "
        "confirms all spec items have been implemented."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "verification_report": {
                "type": "string",
                "description": "The full verification report showing which spec items passed, failed, or were deferred",
            },
        },
        "required": ["verification_report"],
    },
}

REJECT_COMPLETION_SCHEMA = {
    "name": "belayer_reject_completion",
    "description": (
        "Reject the run because the spec is not fully satisfied. "
        "This sends the gap list back to the supervisor for remediation. "
        "Be specific about what's missing so the supervisor can fix it."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "verification_report": {
                "type": "string",
                "description": "The full verification report showing which spec items passed, failed, or were deferred",
            },
            "gap_list": {
                "type": "string",
                "description": "Specific list of gaps: what the spec requires vs what was found (or not found) in the code",
            },
        },
        "required": ["verification_report", "gap_list"],
    },
}

# ---------------------------------------------------------------------------
# Handler factories
# ---------------------------------------------------------------------------


def make_send_message_handler(agent_id: str, session_id: str, socket_path: str):
    """Return a handler for belayer_send_message."""

    def handler(args: dict, **kwargs) -> str:
        to = args.get("to", "")
        content = args.get("content", "")
        status, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/messages",
            {"to": to, "content": content, "from": agent_id},
        )
        if status == 201:
            return f"Message sent to {to}."
        log.warning("send_message to %s failed (%d): %s", to, status, body[:200])
        if status == 410:
            return f"[System] Agent '{to}' has exited. Use belayer_spawn_agent to re-spawn with conversation history."
        return f"[System] Daemon unavailable — message to {to} not delivered. Continue local work. Error: {body[:200]}"

    return handler


def make_create_artifact_handler(agent_id: str, session_id: str, socket_path: str):
    """Return a handler for belayer_create_artifact."""

    def handler(args: dict, **kwargs) -> str:
        kind = args.get("kind", "")
        path = args.get("path", "")
        summary = args.get("summary", "")
        status, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/artifacts",
            {"kind": kind, "path": path, "producer": agent_id, "summary": summary},
        )
        if status == 201:
            return f"Artifact registered: {kind} at {path}."
        log.warning("create_artifact %s@%s failed (%d): %s", kind, path, status, body[:200])
        return (
            f"[System] Daemon unavailable — artifact not registered centrally. "
            f"Artifact saved locally at {path}. Error: {body[:200]}"
        )

    return handler


def make_report_status_handler(agent_id: str, session_id: str, socket_path: str):
    """Return a handler for belayer_report_status."""

    def handler(args: dict, **kwargs) -> str:
        status = args.get("status", "")
        detail = args.get("detail", "")
        event_data = json.dumps({"agent": agent_id, "status": status, "detail": detail})
        status_code, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/events",
            {"type": f"agent_status:{status}", "data": event_data},
        )
        if status_code in (200, 201):
            return f"Status reported: {status}."
        log.warning("report_status %s failed (%d): %s", status, status_code, body[:200])
        return f"[System] Daemon unavailable — status not broadcast. Continue local work. Error: {body[:200]}"

    return handler


def make_spawn_agent_handler(agent_id: str, session_id: str, socket_path: str):
    """Return a handler for belayer_spawn_agent."""

    def handler(args: dict, **kwargs) -> str:
        name = args.get("name", "")
        profile = args.get("profile", "")
        message = args.get("message", "")
        branch = args.get("branch", "")
        payload: dict = {"name": name, "role": name, "profile": profile, "message": message}
        if branch:
            payload["branch"] = branch
        status, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/agents",
            payload,
        )
        if status == 201:
            extra = f" on branch '{branch}'" if branch else ""
            return f"Agent '{name}' spawned with profile '{profile}'{extra}."
        log.warning("spawn_agent %s failed (%d): %s", name, status, body[:200])
        return f"[System] Failed to spawn agent '{name}'. Error: {body[:200]}"

    return handler


def make_request_completion_handler(agent_id: str, session_id: str, socket_path: str):
    """Return a handler for belayer_request_completion."""

    def handler(args: dict, **kwargs) -> str:
        summary = args.get("summary", "")
        spec_artifact = args.get("spec_artifact", "")
        event_data = json.dumps({
            "agent": agent_id,
            "summary": summary,
            "spec_artifact": spec_artifact,
        })
        status, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/events",
            {"type": "bridge:completion_requested", "data": event_data},
        )
        if status in (200, 201):
            return (
                "Completion review requested. The product manager agent will be "
                "spawned to verify the spec against the implementation. "
                "Wait for the PM's verdict before taking further action."
            )
        log.warning("request_completion failed (%d): %s", status, body[:200])
        return f"[System] Failed to request completion review. Error: {body[:200]}"

    return handler


def make_approve_completion_handler(agent_id: str, session_id: str, socket_path: str):
    """Return a handler for belayer_approve_completion."""

    def handler(args: dict, **kwargs) -> str:
        report = args.get("verification_report", "")
        event_data = json.dumps({
            "agent": agent_id,
            "verification_report": report,
        })
        status, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/events",
            {"type": "bridge:completion_approved", "data": event_data},
        )
        if status in (200, 201):
            return "Run approved. Session marked as complete."
        log.warning("approve_completion failed (%d): %s", status, body[:200])
        return f"[System] Failed to approve completion. Error: {body[:200]}"

    return handler


def make_reject_completion_handler(agent_id: str, session_id: str, socket_path: str):
    """Return a handler for belayer_reject_completion."""

    def handler(args: dict, **kwargs) -> str:
        report = args.get("verification_report", "")
        gap_list = args.get("gap_list", "")
        event_data = json.dumps({
            "agent": agent_id,
            "verification_report": report,
            "gap_list": gap_list,
        })
        status, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/events",
            {"type": "bridge:completion_rejected", "data": event_data},
        )
        if status in (200, 201):
            return "Completion rejected. Gap list sent to supervisor for remediation."
        log.warning("reject_completion failed (%d): %s", status, body[:200])
        return f"[System] Failed to reject completion. Error: {body[:200]}"

    return handler


# ---------------------------------------------------------------------------
# Registration
# ---------------------------------------------------------------------------

BASELINE_TOOLS = {
    "belayer_send_message",
    "belayer_report_status",
    "belayer_create_artifact",
}

_HANDLER_FACTORIES = {
    "belayer_send_message": (SEND_MESSAGE_SCHEMA, make_send_message_handler),
    "belayer_report_status": (REPORT_STATUS_SCHEMA, make_report_status_handler),
    "belayer_create_artifact": (CREATE_ARTIFACT_SCHEMA, make_create_artifact_handler),
    "belayer_spawn_agent": (SPAWN_AGENT_SCHEMA, make_spawn_agent_handler),
    "belayer_request_completion": (REQUEST_COMPLETION_SCHEMA, make_request_completion_handler),
    "belayer_approve_completion": (APPROVE_COMPLETION_SCHEMA, make_approve_completion_handler),
    "belayer_reject_completion": (REJECT_COMPLETION_SCHEMA, make_reject_completion_handler),
}


def register_belayer_tools(agent, agent_id: str, session_id: str, socket_path: str, allowed_tools: list[str] | None = None) -> None:
    """Register Belayer coordination tools on an AIAgent instance.

    Baseline tools (send_message, report_status, create_artifact) are always
    registered. Additional tools are only registered if they appear in
    allowed_tools (read from BELAYER_TOOLS env var, set by the daemon from
    the agent template's agent.yaml).
    """
    try:
        from tools.registry import registry  # type: ignore[import]
    except ImportError as exc:
        raise RuntimeError(
            "Hermes 'tools' package not found. Ensure hermes-agent is installed."
        ) from exc

    tools_to_register = set(BASELINE_TOOLS)
    if allowed_tools:
        tools_to_register |= set(allowed_tools)

    registered = 0
    for tool_name, (schema, make_handler) in _HANDLER_FACTORIES.items():
        if tool_name not in tools_to_register:
            continue
        handler = make_handler(agent_id, session_id, socket_path)
        registry.register(
            name=schema["name"],
            toolset="belayer",
            schema=schema,
            handler=handler,
        )
        tool_def = {
            "type": "function",
            "function": {
                "name": schema["name"],
                "description": schema["description"],
                "parameters": schema["parameters"],
            },
        }
        agent.tools.append(tool_def)
        agent.valid_tool_names.add(schema["name"])
        registered += 1

    log.info(
        "Registered %d/%d Belayer tools for agent=%s session=%s (allowed: %s)",
        registered,
        len(_HANDLER_FACTORIES),
        agent_id,
        session_id,
        tools_to_register,
    )
