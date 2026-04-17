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
        "Send a message to another live agent in this Belayer session. "
        "The recipient receives the content as a turn in their conversation; the daemon records "
        "the message in the session event log. This is the primary way agents talk to each other "
        "for coordination, hand-off, and follow-up dialogue.\n\n"
        "WHEN TO USE belayer_send_message:\n"
        "- Peer-to-peer dialogue with another agent in the session (questions, hand-offs, "
        "follow-ups, requests for review)\n"
        "- Routing a finding or instruction to a specific teammate by their session-local name\n"
        "- Continuing a conversation you started with a peer who is still alive\n\n"
        "WHEN NOT TO USE (use these instead):\n"
        "- Reporting your own progress or state -> use belayer_report_status (it's the right "
        "channel for status events; messages aren't)\n"
        "- Bringing up a new teammate -> use belayer_spawn_agent first; messaging an agent that "
        "isn't in the session yet will fail\n"
        "- Publishing a durable output other agents will reference later -> use "
        "belayer_create_artifact (artifacts are addressable; messages scroll past)\n\n"
        "IMPORTANT:\n"
        "- The recipient must be a currently-running agent in this session. Messaging an exited "
        "agent fails with a clear error directing you to belayer_spawn_agent for re-spawn.\n"
        "- Messages are visible in session logs and to the operator; treat them as on-the-record.\n"
        "- 'to' is the session-local agent name (e.g. 'reviewer-1'), not a profile name."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "to": {
                "type": "string",
                "description": (
                    "The recipient's session-local agent name as shown in your team roster "
                    "(e.g. 'supervisor', 'reviewer-1'). This is the per-session identifier "
                    "assigned at spawn time, not the identity profile name. Consult the roster "
                    "if you're unsure who is currently live."
                ),
            },
            "content": {
                "type": "string",
                "description": (
                    "The message body. Be specific and self-contained — the recipient sees "
                    "this as a single turn, so include enough context (what you're asking for, "
                    "what file/diff/issue you're referring to, what success looks like) that "
                    "they can act without a clarifying round-trip."
                ),
            },
        },
        "required": ["to", "content"],
    },
}

CREATE_ARTIFACT_SCHEMA = {
    "name": "belayer_create_artifact",
    "description": (
        "Register a durable output with the session's artifact registry. The artifact is recorded "
        "with a kind, a path on the workspace, the producing agent, and a summary; other agents "
        "and the operator can list it, look it up, and reference it by path in later messages.\n\n"
        "WHEN TO USE belayer_create_artifact:\n"
        "- Publishing a spec, design doc, or task graph the supervisor or PM will need to verify "
        "against later\n"
        "- Recording a shared contract between repos (e.g. an API schema two implementers must "
        "honour)\n"
        "- Filing a structured report (review findings, QA results, verification reports) that a "
        "later agent — possibly in a future session — needs to read\n\n"
        "WHEN NOT TO USE (use these instead):\n"
        "- Scratch files, intermediate work, or transient logs -> just write to the workspace "
        "without registering\n"
        "- A short status update -> use belayer_report_status\n"
        "- A direct hand-off to one peer -> use belayer_send_message and reference the file path "
        "inline\n\n"
        "IMPORTANT:\n"
        "- The file at 'path' must already exist on disk before you call this — the daemon does "
        "not write content for you, it only registers metadata.\n"
        "- Artifacts are append-only in spirit: don't overwrite a registered artifact in place. "
        "For revisions, write to a new path (e.g. add a version suffix or bump the kind) and "
        "register that.\n"
        "- 'kind' is a free-form tag but downstream consumers (PM, reflection) match on it — "
        "stick to consistent kinds across a project (e.g. 'spec', 'design-doc', 'review-report').\n"
        "- By convention, the pair (kind='spec', producer='operator') is used for the run-level "
        "SPEC.md registered by `belayer run start` from the operator's --spec text. The "
        "artifacts API does not currently enforce that reservation, so don't register your own "
        "artifact under that pair; use a more specific kind (e.g. 'design-spec', 'feature-spec') "
        "or set producer to your own agent name."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "kind": {
                "type": "string",
                "description": (
                    "A short tag describing what the artifact is. Common kinds: 'spec', "
                    "'design-doc', 'task-graph', 'shared-contract', 'review-report', "
                    "'qa-report', 'verification-report'. Downstream tools (PM gate, reflection) "
                    "look artifacts up by kind, so reuse existing kinds in your project rather "
                    "than inventing new ones for similar concepts."
                ),
            },
            "path": {
                "type": "string",
                "description": (
                    "Path to the artifact file, relative to your workspace root (e.g. "
                    "'artifacts/checkout-flow.spec.md' or 'reports/review-2026-04-16.md'). "
                    "The file must already exist."
                ),
            },
            "summary": {
                "type": "string",
                "description": (
                    "One- to three-sentence summary so other agents can decide whether to read "
                    "the full artifact. State what's in it and who would care."
                ),
            },
        },
        "required": ["kind", "path"],
    },
}

REPORT_STATUS_SCHEMA = {
    "name": "belayer_report_status",
    "description": (
        "Publish a status event to the session bus. Most statuses are observability-only — they "
        "go in the event log so the operator and reflection can see what you're up to, but they "
        "do not change daemon state. The exception is 'incomplete', which is the canonical "
        "escalation signal: when you report 'incomplete' the daemon treats the run as needing "
        "human review and wakes the operator.\n\n"
        "WHEN TO USE belayer_report_status:\n"
        "- A real state transition the operator should see in the timeline (started, hit a "
        "wall, finished a phase)\n"
        "- Escalating to a human after you've made progress but cannot finish — use 'incomplete' "
        "and explain why in 'detail'\n"
        "- Marking your own work 'done' so the supervisor and event stream reflect that you've "
        "wrapped your assigned task\n\n"
        "WHEN NOT TO USE (use these instead):\n"
        "- Asking a peer for help or sending coordination instructions -> use "
        "belayer_send_message\n"
        "- Logging every micro-step (\"reading file\", \"thinking\") -> don't; status events "
        "are for transitions, not narration\n"
        "- Signalling the whole run is complete and the spec is satisfied -> that's "
        "belayer_request_completion (supervisor only), not 'done'\n\n"
        "IMPORTANT:\n"
        "- 'incomplete' is the only status that triggers daemon-side action (operator "
        "escalation). Use it when truly stuck — looping, unable to make progress, missing "
        "credentials or context you can't recover.\n"
        "- 'working', 'blocked', 'done', and 'needs-review' are log-only. They give telemetry "
        "but do not move the session forward on their own.\n"
        "- Don't spam status events; the event log is shared and noisy events drown out real "
        "transitions."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "status": {
                "type": "string",
                "enum": ["working", "blocked", "done", "needs-review", "incomplete"],
                "description": (
                    "Your current status. 'incomplete' is the only value that triggers "
                    "daemon-side action (it escalates to a human) — use it when you have made "
                    "progress but cannot finish. The others ('working', 'blocked', 'done', "
                    "'needs-review') are recorded in the event log for observability but do not "
                    "change session state."
                ),
            },
            "detail": {
                "type": "string",
                "description": (
                    "What this status means right now. For 'blocked' or 'incomplete', explain "
                    "the cause concretely (file you can't read, credential missing, conflicting "
                    "instructions). For 'done', say what you finished. The operator reads this "
                    "to decide whether to intervene."
                ),
            },
        },
        "required": ["status"],
    },
}

SPAWN_AGENT_SCHEMA = {
    "name": "belayer_spawn_agent",
    "description": (
        "Spawn a specialist agent into this Belayer session. The new agent becomes a first-class "
        "peer: visible in session logs, addressable via belayer_send_message, with its own "
        "conversation, workspace, and tool budget. Available agent identities are configured "
        "in .belayer/agents/ — query session state for the current roster.\n\n"
        "WHEN TO USE belayer_spawn_agent:\n"
        "- Bringing up a teammate for ongoing work (an implementer for a feature, a reviewer "
        "for a diff, QA for verification)\n"
        "- Work that needs bidirectional dialogue: you send instructions, they report back, "
        "you respond, they iterate\n"
        "- Roles that need their own workspace (worktree-isolated implementers)\n"
        "- Multi-turn collaboration where the peer's intermediate state matters\n\n"
        "WHEN NOT TO USE (use these instead):\n"
        "- One-shot focused subtask, research, or analysis with no follow-up "
        "-> use delegate_task (cheaper, isolated, summary-only)\n"
        "- Simple file read or shell command -> call the tool directly\n"
        "- Tasks where you don't need a peer in the session afterward "
        "-> delegate_task is the right primitive\n"
        "- Triggering the run-completion gate -> use belayer_request_completion; the daemon "
        "spawns the PM itself, you do not spawn it manually\n\n"
        "IMPORTANT:\n"
        "- 'name' is the session-local handle (e.g. 'reviewer-1'); 'identity' is the template "
        "under .belayer/agents/<identity>/ (e.g. 'reviewer') and defaults to 'name' when "
        "omitted. Set 'identity' explicitly when you spawn multiple peers off the same "
        "template (e.g. name='reviewer-1', identity='reviewer').\n"
        "- 'identity' must match a directory under .belayer/agents/. An unknown identity "
        "spawns an agent with no system prompt and no belayer tool gating — almost certainly "
        "not what you want. Consult the roster shown at session start (or the "
        ".belayer/agents/ directory) for available identities in this project.\n"
        "- 'profile' is a separate concern: it selects the Hermes runtime profile "
        "(BELAYER_PROFILE / HERMES_HOME), not the identity. Most spawns can leave it as "
        "'default'; only override when a particular agent needs a non-default Hermes "
        "configuration (custom tool inventory, alternative model defaults, etc.).\n"
        "- Implementer-style identities need 'branch' for git worktree isolation; review/QA "
        "identities do not. Spawning an implementer without a branch makes it share the "
        "workspace with you, which is rarely what you want.\n"
        "- Spawned agents persist until they exit or you stop them — budget spawns "
        "intentionally; each peer consumes tokens for as long as it lives."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "name": {
                "type": "string",
                "description": (
                    "The session-local identifier for the new agent (e.g. 'reviewer-1', "
                    "'web-dev-checkout'). This is how other agents will address it via "
                    "belayer_send_message. Must be unique within the session. By convention, "
                    "use '<identity>-<sequence>' or '<identity>-<purpose>' so the role is "
                    "obvious at a glance."
                ),
            },
            "identity": {
                "type": "string",
                "description": (
                    "The identity template to load — must match a directory under "
                    ".belayer/agents/ (e.g. 'reviewer', 'web-dev', 'qa'). The daemon reads "
                    "the system prompt and belayer_tools allowlist from that directory at "
                    "spawn time. Optional: defaults to 'name' when omitted, which works for "
                    "single-instance roles like 'supervisor' and 'pm' where the handle and "
                    "the template share a name."
                ),
            },
            "profile": {
                "type": "string",
                "description": (
                    "Hermes runtime profile to launch the agent under (sets BELAYER_PROFILE "
                    "/ HERMES_HOME). Independent of 'identity' — 'profile' chooses the "
                    "Hermes config (tool inventory, model defaults, credentials) while "
                    "'identity' chooses the soul. Most spawns use 'default'; only change "
                    "this when an agent needs a non-default Hermes configuration."
                ),
            },
            "message": {
                "type": "string",
                "description": (
                    "First instruction the new agent receives. Treat this like a prompt — "
                    "include the goal, relevant file paths or artifact references, the "
                    "constraints they must honour, and what success looks like. A vague "
                    "first message produces a vague first turn."
                ),
            },
            "branch": {
                "type": "string",
                "description": (
                    "Git branch for worktree isolation. When set, the daemon creates (or "
                    "reuses) a git worktree on this branch under .belayer/worktrees/ and the "
                    "agent works there, isolated from other agents and the main checkout. "
                    "Use for implementer-style identities that will write code. Omit for "
                    "review/QA/research identities that read but don't commit."
                ),
            },
        },
        "required": ["name", "profile", "message"],
    },
}

REQUEST_COMPLETION_SCHEMA = {
    "name": "belayer_request_completion",
    "description": (
        "Signal that the run is complete and ready for the spec-vs-reality gate. This ends the "
        "active work phase and triggers the daemon to spawn the PM agent for adversarial "
        "verification. The PM independently checks every spec item against the actual code and "
        "either approves (closing the session) or rejects (returning a gap list for "
        "remediation). You cannot approve or reject the run yourself.\n\n"
        "WHEN TO USE belayer_request_completion:\n"
        "- All assigned work is done and you believe the spec is satisfied — you want the PM "
        "gate to confirm before the session closes\n"
        "- After all specialist agents have reported done, all implementation branches are "
        "merged or ready, and review/QA feedback has been addressed\n"
        "- When you have independently verified the work (builds pass, tests pass, behaviour "
        "matches spec) and want the human's last automated check\n\n"
        "WHEN NOT TO USE (use these instead):\n"
        "- 'I'm done with my piece' (you're a specialist, not the supervisor) -> use "
        "belayer_report_status with 'done'\n"
        "- The run is partially complete and you want to hand off mid-flight -> use "
        "belayer_report_status with 'incomplete' to escalate to a human\n"
        "- You want to retract or amend a previous request -> you cannot; the gate is "
        "one-shot, the PM is already running\n\n"
        "IMPORTANT:\n"
        "- This is a terminal action for the active work phase. Do NOT call it until: all "
        "specialist agents have reported done; all implementation branches are merged or "
        "ready to merge; you have independently verified the work (tests pass, builds "
        "succeed); and review/QA feedback has been addressed.\n"
        "- Once called, you must wait for the PM's verdict. You cannot approve or reject the "
        "run yourself — that authority belongs only to the PM.\n"
        "- The PM has up to three remediation cycles. Use the rejection feedback to address "
        "real gaps, not to argue with the PM."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "summary": {
                "type": "string",
                "description": (
                    "Plain-language summary of what was accomplished. List the spec items you "
                    "implemented, any deviations from the spec (with reasons), and anything "
                    "the PM should know to verify efficiently. The PM treats your summary as "
                    "a starting point, not as truth — they will independently check the code."
                ),
            },
            "spec_artifact": {
                "type": "string",
                "description": (
                    "Optional path to the spec or design-doc artifact. If omitted, the PM "
                    "defaults to the operator-written SPEC.md (registered as kind='spec', "
                    "producer='operator' at run start) and falls back to other 'spec' / "
                    "'design-doc' artifacts in the registry. Provide it explicitly when "
                    "the supervisor expanded the operator spec into a more detailed design "
                    "doc and wants the PM to verify against that instead."
                ),
            },
        },
        "required": ["summary"],
    },
}

APPROVE_COMPLETION_SCHEMA = {
    "name": "belayer_approve_completion",
    "description": (
        "Mark the run complete after verifying the spec is fully satisfied. This is the PM's "
        "terminal action — it closes the session and ends the run. Only the PM agent has "
        "access to this tool; no other agent should ever see it.\n\n"
        "WHEN TO USE belayer_approve_completion:\n"
        "- You are the PM, you have read the spec end-to-end, and you have evidence that every "
        "spec item is implemented in the code (not just claimed by the supervisor's summary)\n"
        "- Your verification report enumerates each spec item with the file/test/behaviour "
        "that satisfies it\n\n"
        "WHEN NOT TO USE (use these instead):\n"
        "- You found gaps, even small ones -> use belayer_reject_completion with a specific "
        "gap list so the supervisor can remediate\n"
        "- You are not the PM -> you do not have authority to close a run; the supervisor "
        "calls belayer_request_completion to invoke the gate, you don't bypass it\n"
        "- You want to close the session for an unrelated reason (cancel, abandon) -> that's "
        "an operator action, not an agent action\n\n"
        "IMPORTANT:\n"
        "- This is irreversible: it closes the session. There is no 'undo approve'.\n"
        "- Your verification_report MUST reference the specific spec items you confirmed, by "
        "name or number, with the evidence (code path, test, observable behaviour). "
        "'Everything looks good' is not a verification report.\n"
        "- 'The supervisor said it was done' is not evidence. Code in the repo, tests that "
        "run, and observable behaviour are evidence."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "verification_report": {
                "type": "string",
                "description": (
                    "Full verification report enumerating each spec item with evidence "
                    "(file:line, test name, observable behaviour). For deferred items, name "
                    "what was deferred and why the deferral is acceptable. The operator and "
                    "reflection both read this report to understand what was actually shipped."
                ),
            },
        },
        "required": ["verification_report"],
    },
}

REJECT_COMPLETION_SCHEMA = {
    "name": "belayer_reject_completion",
    "description": (
        "Reject the run because the spec is not fully satisfied. This sends a structured gap "
        "list back to the supervisor for remediation; the run does not close. Like "
        "approve_completion, this is a PM-only terminal action — no other agent should see it.\n\n"
        "WHEN TO USE belayer_reject_completion:\n"
        "- You are the PM and your verification found at least one spec item that is not "
        "implemented or is implemented incorrectly\n"
        "- You found a gap that the supervisor can address — you have a concrete fix in mind, "
        "not just a vague concern\n\n"
        "WHEN NOT TO USE (use these instead):\n"
        "- The spec is fully satisfied -> use belayer_approve_completion\n"
        "- You are not the PM -> only the PM has authority to reject the gate; specialists "
        "send findings via belayer_send_message instead\n"
        "- You want to flag a stylistic issue or a non-blocking concern -> that's reviewer "
        "territory, not PM territory; the PM gates on spec satisfaction, not code quality\n\n"
        "IMPORTANT:\n"
        "- This is irreversible in the sense that you cannot un-reject; the supervisor must "
        "now remediate and re-request completion. There is a hard cap of three remediation "
        "cycles before the daemon escalates to a human.\n"
        "- Your gap_list MUST reference specific spec items, by name or number, with what you "
        "expected to find and what you found instead. 'Tests are missing' is not actionable; "
        "'spec section 3.2 requires a regression test for the rate-limit path; no test exists "
        "in tests/rate_limit_test.go' is.\n"
        "- Be specific enough that the supervisor can fix the gap without coming back to you "
        "for clarification."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "verification_report": {
                "type": "string",
                "description": (
                    "Full verification report — the same shape as in approve_completion. "
                    "Enumerate each spec item with what you found (or didn't find). The "
                    "operator and reflection use this to understand the verification, "
                    "independent of the gap list."
                ),
            },
            "gap_list": {
                "type": "string",
                "description": (
                    "Specific, actionable list of gaps. Each gap should name the spec item, "
                    "what the spec requires, what you found (or didn't find) in the code, and "
                    "the concrete next step the supervisor can take to close it. The "
                    "supervisor reads this and routes work to remediate; vague gap lists "
                    "produce wasted remediation cycles."
                ),
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
        identity = args.get("identity", "") or name  # default to name for single-instance roles
        profile = args.get("profile", "")
        message = args.get("message", "")
        branch = args.get("branch", "")
        payload: dict = {
            "name": name,
            "identity": identity,
            "role": identity,  # role tracks the identity template by default
            "profile": profile,
            "message": message,
        }
        if branch:
            payload["branch"] = branch
        status, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/agents",
            payload,
        )
        if status == 201:
            extra = f" on branch '{branch}'" if branch else ""
            id_suffix = f" (identity '{identity}')" if identity != name else ""
            return f"Agent '{name}'{id_suffix} spawned with profile '{profile}'{extra}."
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
