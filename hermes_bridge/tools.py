"""Belayer coordination tools for Hermes agents.

Registered tools are kind-aware, but the names are belayer-native:
  - mains: belayer_send_message, belayer_broadcast, belayer_check_mail
  - all agents: belayer_report_status, belayer_create_artifact
  - role-specific tools come from the daemon-provided BELAYER_TOOLS allowlist

Tool schemas follow the OpenAI function-calling format used by Hermes.
Handlers receive kwargs matching schema property names (Hermes calling convention).
"""

import json
import logging

from hermes_bridge.http_client import unix_get, unix_post

log = logging.getLogger("tools")

KIND_MAIN = "main"
KIND_SIDE = "side"


def _body_preview(body: str, limit: int = 200) -> str:
    return body[:limit]


def _json_messages(body: str) -> list[dict]:
    try:
        parsed = json.loads(body)
    except json.JSONDecodeError:
        return []
    if isinstance(parsed, list):
        return [msg for msg in parsed if isinstance(msg, dict)]
    return []


def _message_field(msg: dict, *keys: str) -> str:
    for key in keys:
        val = msg.get(key)
        if val is not None and val != "":
            return str(val)
    return ""


def _format_messages(messages: list[dict]) -> str:
    parts = []
    for msg in messages:
        sender = _message_field(msg, "SenderID", "sender_id") or "unknown"
        content = _message_field(msg, "Content", "content")
        parts.append(f"[Message from {sender}]: {content}")
    return "\n\n".join(parts)


def _filter_messages(messages: list[dict]) -> tuple[str, list[str]]:
    valid = []
    dropped = []
    ack_ids: list[str] = []
    for msg in messages:
        msg_id = _message_field(msg, "ID", "id")
        content = _message_field(msg, "Content", "content")
        if not content.strip():
            dropped.append({
                "sender": _message_field(msg, "SenderID", "sender_id") or "unknown",
                "message_id": msg_id,
            })
            if msg_id:
                ack_ids.append(msg_id)
            continue
        valid.append(msg)
        if msg_id:
            ack_ids.append(msg_id)
    return _format_messages(valid), ack_ids


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
        "stick to consistent kinds across a project (e.g. 'spec', 'design-doc', 'review-report')."
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
                    "Optional path to the spec or design-doc artifact (e.g. "
                    "'artifacts/checkout-flow.spec.md'). If omitted, the PM searches the "
                    "artifact registry for kinds 'spec' or 'design-doc'. Provide it "
                    "explicitly when there are multiple specs in play and you want the PM to "
                    "verify against a specific one."
                ),
            },
        },
        "required": ["summary"],
    },
}

ESCALATE_TO_HUMAN_SCHEMA = {
    "name": "belayer_escalate_to_human",
    "description": (
        "Permanently stop the run and hand it to a human operator because a blocker exists that "
        "is outside your agency and cannot be resolved by further effort. This is a stop button, "
        "not a communication channel. Calling this tool immediately triggers session teardown: "
        "the session transitions to `needs_human_review`, the sandbox is torn down, all "
        "in-flight specialist work is abandoned, and a human operator must intervene before "
        "anything further happens. There is no resume, no follow-up turn, no way to undo.\n\n"
        "WHEN TO USE belayer_escalate_to_human (ALL of the following must be true):\n"
        "- You have made at least 3 attempts at the same approach and each has failed with the "
        "same or a materially similar error — not a different error, not a different symptom, "
        "the same root cause repeating despite your changes\n"
        "- You have exhausted alternative strategies: different libraries, different "
        "architectures, different tool orderings, different implementations of the same goal. "
        "'I tried twice with the same approach' does not qualify\n"
        "- The blocker is genuinely outside your agency: an infrastructure egress-policy denial "
        "that blocks every install path for a required package, a spec ambiguity that requires "
        "a human business decision to resolve, an external credential problem you cannot work "
        "around. If more effort, a different approach, or a different specialist could plausibly "
        "fix it, that condition is NOT met\n\n"
        "Example scenarios where using this tool IS appropriate:\n"
        "- After 5 attempts to install dependency X (pip install, conda, from source, vendored "
        "wheel, alternative package), each failing with the same egress-policy denial, and no "
        "alternative package provides the capability X offers\n"
        "- After 4 attempts with two different specialists to implement a spec section that "
        "contains a genuine contradiction (e.g. 'must be stateless' and 'must persist across "
        "sessions' with no guidance on which takes priority), where any implementation choice "
        "violates one of the constraints\n\n"
        "WHEN NOT TO USE (these are not grounds for escalation):\n"
        "- You are unsure how to proceed: think harder, re-read the spec, try a different "
        "approach. Uncertainty is not a blocker. This tool is not a way to ask for guidance\n"
        "- A specialist returned unsatisfactory work: use belayer_spawn_agent to retry with "
        "better, more specific instructions. Poor output quality from a specialist is a "
        "supervision problem, not an escalation trigger\n"
        "- You hit a dependency install error on your first attempt: try an alternative install "
        "path, read the docs, try a different library, consult the error message. One failure "
        "is not exhaustion\n"
        "- There are spec sections you haven't implemented yet: implement them. Incomplete work "
        "is not a blocker; it is the work\n"
        "- You want to ask for clarification on something ambiguous: there is no clarification "
        "mechanism — this tool terminates the run permanently. If the ambiguity has a reasonable "
        "default interpretation, take it and proceed; only escalate if every interpretation "
        "violates a hard constraint\n"
        "- You're stuck and frustrated: that is not a system blocker. Step back, retry with a "
        "fresh approach, spawn a specialist with different instructions\n"
        "- A single specialist failed: that is one data point. Try again with different "
        "instructions, or try a different specialist identity\n\n"
        "COST / CONSEQUENCES:\n"
        "Calling this tool permanently terminates the run. The session transitions to "
        "`needs_human_review`, the sandbox is torn down, all in-flight specialist work is "
        "abandoned, and a human operator must intervene before anything further happens. Do not "
        "call this tool as a way to 'check in' or ask for help — it is a stop button, not a "
        "communication channel. The operator will see your reason, your blocker, and the "
        "approaches you tried; they will use that context to decide whether to retry with a "
        "different strategy, adjust the spec, or abandon the run entirely."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "reason": {
                "type": "string",
                "description": (
                    "One-sentence description of why the run cannot proceed. Be direct and "
                    "specific: name the capability or outcome that is blocked, not just the "
                    "symptom. The operator reads this first to triage."
                ),
            },
            "blocker": {
                "type": "string",
                "description": (
                    "The specific obstacle that makes forward progress impossible. Describe "
                    "the concrete constraint: the infrastructure policy that blocks every "
                    "install path, the spec contradiction that makes any implementation "
                    "invalid, the external credential that is absent and cannot be substituted. "
                    "Be precise enough that the operator can act without asking follow-up "
                    "questions."
                ),
            },
            "what_tried": {
                "type": "array",
                "items": {"type": "string"},
                "minItems": 3,
                "description": (
                    "The approaches already attempted, in order. Each entry should name the "
                    "approach and its outcome (e.g. 'pip install cryptography — egress denied "
                    "by policy on all mirrors', 'vendored wheel from workspace — import failed, "
                    "wrong platform ABI'). Minimum 3 entries required — this matches the "
                    "\"at least 3 materially similar failed attempts\" guardrail in the tool's "
                    "WHEN TO USE block. The operator uses this list to understand what has "
                    "already been ruled out before deciding next steps."
                ),
            },
        },
        "required": ["reason", "blocker", "what_tried"],
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

BROADCAST_SCHEMA = {
    "name": "belayer_broadcast",
    "description": (
        "Broadcast a message to every main agent in the session except the sender. "
        "Broadcasts are for party-wide announcements, not private follow-up."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "content": {
                "type": "string",
                "description": "Message body to broadcast to the party.",
            },
        },
        "required": ["content"],
    },
}

CHECK_MAIL_SCHEMA = {
    "name": "belayer_check_mail",
    "description": (
        "Explicitly poll your mailbox now. This is optional because the bridge "
        "already polls pending mail before every turn, but it is available for "
        "mid-narrative checks between tool calls."
    ),
    "parameters": {
        "type": "object",
        "properties": {},
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
        interrupt = bool(args.get("interrupt", False))
        status, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/messages",
            {"to": to, "content": content, "from": agent_id, "interrupt": interrupt},
        )
        if status == 201:
            prefix = "Interrupt sent" if interrupt else "Message sent"
            return f"{prefix} to {to}."
        log.warning("send %s to %s failed (%d): %s", "interrupt" if interrupt else "message", to, status, body[:200])
        if status in (410, 404):
            return f"[System] Agent '{to}' is not available. Choose another target or respawn it."
        return f"[System] Daemon unavailable — message to {to} not delivered. Continue local work. Error: {body[:200]}"

    return handler


def make_broadcast_handler(agent_id: str, session_id: str, socket_path: str):
    """Return a handler for belayer_broadcast."""

    def handler(args: dict, **kwargs) -> str:
        content = args.get("content", "")
        status, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/messages/broadcast",
            {"content": content, "from": agent_id, "type": "instruction"},
        )
        if status == 201:
            return "Broadcast sent."
        log.warning("broadcast failed (%d): %s", status, body[:200])
        return f"[System] Failed to broadcast message. Error: {body[:200]}"

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


def make_check_mail_handler(
    agent_id: str,
    session_id: str,
    socket_path: str,
    turn_mail_ids: list[str] | None = None,
):
    """Return a handler for belayer_check_mail."""

    def handler(args: dict, **kwargs) -> str:
        status, body = unix_get(
            socket_path,
            f"/sessions/{session_id}/messages?for={agent_id}&pending=true",
        )
        if status != 200:
            log.warning("check_mail failed (%d): %s", status, body[:200])
            return f"[System] Failed to poll mail. Error: {body[:200]}"
        messages = _json_messages(body)
        if not messages:
            return "[System] No pending mail."
        formatted, ack_ids = _filter_messages(messages)
        if turn_mail_ids is not None:
            turn_mail_ids.extend(ack_ids)
        return formatted or "[System] No pending mail."

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


def make_escalate_to_human_handler(agent_id: str, session_id: str, socket_path: str):
    """Return a handler for belayer_escalate_to_human."""

    def handler(args: dict, **kwargs) -> str:
        reason = args.get("reason", "")
        blocker = args.get("blocker", "")
        what_tried = args.get("what_tried", [])

        if not isinstance(reason, str) or not reason.strip():
            return "[System] 'reason' is required and must be a non-empty string."
        if not isinstance(blocker, str) or not blocker.strip():
            return "[System] 'blocker' is required and must be a non-empty string."
        if not isinstance(what_tried, list) or len(what_tried) < 3:
            return "[System] 'what_tried' must be a list of at least 3 strings documenting prior attempts (matches the 3-attempt guardrail in the tool description)."
        if not all(isinstance(item, str) and item.strip() for item in what_tried):
            return "[System] Each entry in 'what_tried' must be a non-empty string."

        # Keep the single-line detail short but informative — the daemon
        # surfaces it verbatim in the agent_escalated log entry, which is
        # what an on-call operator sees first. Full `blocker` and the
        # `what_tried` list are preserved as structured fields alongside.
        first_tried = what_tried[0].strip()
        last_tried = what_tried[-1].strip()
        detail = (
            f"Escalated by agent: {reason.strip()} | blocker: {blocker.strip()[:200]} | "
            f"tried ({len(what_tried)}): first='{first_tried[:120]}' … "
            f"last='{last_tried[:120]}'"
        )
        event_data = json.dumps({
            "agent": agent_id,
            # Mirror belayer_report_status' shape so both escalation paths
            # look identical to the daemon's processAgentStatusEvent handler.
            "status": "incomplete",
            "detail": detail,
            "escalated_by_tool": True,
            "reason": reason,
            "blocker": blocker,
            "what_tried": what_tried,
        })
        status_code, body = unix_post(
            socket_path,
            f"/sessions/{session_id}/events",
            {"type": "agent_status:incomplete", "data": event_data},
        )
        if status_code not in (200, 201):
            log.warning("escalate_to_human failed (%d): %s", status_code, body[:200])
            return f"[System] Failed to post escalation event. Error: {body[:200]}"

        # The tool is documented as a "stop button" — don't rely on the
        # model to stop generating or on the daemon to race a sandbox
        # teardown against the next tool call. Post bridge:finished for
        # observability and raise SystemExit so the bridge process exits
        # cleanly on this tool call. Best-effort on bridge:finished: if
        # the daemon is unreachable we still want to exit, so we log and
        # proceed rather than bail back to the agent.
        finished_data = json.dumps({
            "agent": agent_id,
            "reason": "escalate_to_human",
        })
        finished_status, finished_body = unix_post(
            socket_path,
            f"/sessions/{session_id}/events",
            {"type": "bridge:finished", "data": finished_data},
        )
        if finished_status not in (200, 201):
            log.warning(
                "escalate_to_human posted, but bridge:finished failed (%d): %s",
                finished_status,
                finished_body[:200],
            )
        log.info("escalate_to_human: exiting bridge cleanly after event post")
        raise SystemExit(0)

    return handler


# ---------------------------------------------------------------------------
# Registration
# ---------------------------------------------------------------------------

_TOOL_SPECS = {
    "belayer_send_message": (SEND_MESSAGE_SCHEMA, make_send_message_handler),
    "belayer_broadcast": (BROADCAST_SCHEMA, make_broadcast_handler),
    "belayer_check_mail": (CHECK_MAIL_SCHEMA, make_check_mail_handler),
    "belayer_report_status": (REPORT_STATUS_SCHEMA, make_report_status_handler),
    "belayer_create_artifact": (CREATE_ARTIFACT_SCHEMA, make_create_artifact_handler),
    "belayer_spawn_agent": (SPAWN_AGENT_SCHEMA, make_spawn_agent_handler),
    "belayer_request_completion": (REQUEST_COMPLETION_SCHEMA, make_request_completion_handler),
    "belayer_approve_completion": (APPROVE_COMPLETION_SCHEMA, make_approve_completion_handler),
    "belayer_reject_completion": (REJECT_COMPLETION_SCHEMA, make_reject_completion_handler),
    "belayer_escalate_to_human": (ESCALATE_TO_HUMAN_SCHEMA, make_escalate_to_human_handler),
}


def _register(agent, tool_name: str, agent_id: str, session_id: str, socket_path: str, turn_mail_ids: list[str] | None = None) -> None:
    try:
        from tools.registry import registry  # type: ignore[import]
    except ImportError as exc:
        raise RuntimeError(
            "Hermes 'tools' package not found. Ensure hermes-agent is installed."
        ) from exc

    schema, factory = _TOOL_SPECS[tool_name]
    if tool_name == "belayer_check_mail":
        handler = factory(agent_id, session_id, socket_path, turn_mail_ids=turn_mail_ids)
    else:
        handler = factory(agent_id, session_id, socket_path)
    registry.register(
        name=schema["name"],
        toolset="belayer",
        schema=schema,
        handler=handler,
    )
    agent.tools.append(
        {
            "type": "function",
            "function": {
                "name": schema["name"],
                "description": schema["description"],
                "parameters": schema["parameters"],
            },
        }
    )
    agent.valid_tool_names.add(schema["name"])


def register_belayer_tools(
    agent,
    agent_id: str,
    session_id: str,
    socket_path: str,
    allowed_tools: list[str] | None = None,
    *,
    agent_kind: str = KIND_MAIN,
    turn_mail_ids: list[str] | None = None,
) -> None:
    """Register Belayer coordination tools on an AIAgent instance.

    Kind determines the baseline mail surface; allowed_tools grants
    role-specific capabilities like spawn_agent or PM approval tools.
    """
    try:
        allowed_set = {str(tool) for tool in (allowed_tools or [])}
        allowed_preview = sorted(allowed_set)
    except TypeError:
        allowed_set = set()
        allowed_preview = []

    kind = (agent_kind or KIND_MAIN).strip().lower() or KIND_MAIN
    if kind not in (KIND_MAIN, KIND_SIDE):
        kind = KIND_MAIN

    baseline_tools = ["belayer_report_status", "belayer_create_artifact"]
    if kind == KIND_MAIN:
        baseline_tools.extend(["belayer_send_message", "belayer_broadcast", "belayer_check_mail"])

    registered = []
    seen = set()
    for tool_name in baseline_tools + allowed_preview:
        if tool_name in seen:
            continue
        seen.add(tool_name)
        if tool_name not in _TOOL_SPECS:
            continue
        if tool_name in {"belayer_check_mail", "belayer_broadcast", "belayer_send_message"} and kind != KIND_MAIN:
            continue
        _register(agent, tool_name, agent_id, session_id, socket_path, turn_mail_ids=turn_mail_ids)
        registered.append(tool_name)

    log.info(
        "Registered Belayer tools for agent=%s session=%s kind=%s tools=%s allowed=%s",
        agent_id,
        session_id,
        kind,
        registered,
        allowed_preview,
    )
