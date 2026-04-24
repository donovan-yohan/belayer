"""Belayer Hermes plugin.

Registers Belayer session-bus tools into the Hermes tool registry at plugin
discovery time, so they land in AIAgent.tools via get_tool_definitions()
rather than being appended after construction (the legacy path in
hermes_bridge/tools.py).

Phase 1 scope: belayer_broadcast only. Remaining tools (send_message,
check_mail, spawn_agent, request_completion, etc.) still registered by the
bridge's register_belayer_tools() until subsequent phases migrate them.

Env vars consumed at register() time (set by the Go daemon when spawning
the bridge subprocess — see internal/bridge/bridge.go:BuildEnv):

  BELAYER_SESSION_ID     — active session id
  BELAYER_AGENT_ID       — this agent's name within the session
  BELAYER_SOCKET         — UNIX socket path for daemon HTTP RPC
  BELAYER_AGENT_KIND     — "main" or "side" (gates mailbox tools)

Each bridge subprocess is a fresh Python process, so register() reads fresh
env vars each time — no cross-session leak.
"""

from __future__ import annotations

import json
import logging
import os
import socket
from typing import Any, Dict, Optional

logger = logging.getLogger(__name__)

KIND_MAIN = "main"
KIND_SIDE = "side"


def _unix_post(socket_path: str, path: str, payload: Dict[str, Any]) -> tuple[int, str]:
    """POST JSON to a UNIX-socket HTTP endpoint.

    Kept inline to avoid importing from hermes_bridge, which lives in a
    different runtime dir and is not guaranteed to be on PYTHONPATH when the
    plugin loads. Mirrors hermes_bridge/http_client.py:unix_post.
    """
    body = json.dumps(payload).encode("utf-8")
    request = (
        f"POST {path} HTTP/1.1\r\n"
        f"Host: localhost\r\n"
        f"Content-Type: application/json\r\n"
        f"Content-Length: {len(body)}\r\n"
        f"Connection: close\r\n"
        f"\r\n"
    ).encode("utf-8") + body

    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.settimeout(5.0)
    try:
        s.connect(socket_path)
        s.sendall(request)
        chunks: list[bytes] = []
        while True:
            chunk = s.recv(4096)
            if not chunk:
                break
            chunks.append(chunk)
    finally:
        s.close()

    raw = b"".join(chunks).decode("utf-8", errors="replace")
    header, _, body_str = raw.partition("\r\n\r\n")
    status_line = header.split("\r\n", 1)[0] if header else ""
    parts = status_line.split(" ", 2)
    status = int(parts[1]) if len(parts) >= 2 and parts[1].isdigit() else 0
    return status, body_str


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


def _make_broadcast_handler(agent_id: str, session_id: str, socket_path: str):
    def handler(args: Optional[Dict[str, Any]] = None, **_kwargs: Any) -> str:
        args = args or {}
        content = args.get("content", "")
        status, body = _unix_post(
            socket_path,
            f"/sessions/{session_id}/messages/broadcast",
            {"content": content, "from": agent_id, "type": "instruction"},
        )
        if status == 201:
            return "Broadcast sent."
        logger.warning("belayer_broadcast failed (%d): %s", status, body[:200])
        return f"[System] Failed to broadcast message. Error: {body[:200]}"

    return handler


def register(ctx) -> None:
    """Plugin entry point called by Hermes PluginManager.

    Reads per-session env vars set by the Go daemon and conditionally
    registers tools based on agent kind (main vs side). Side agents skip
    mailbox surfaces; side and main both get baseline tools in later phases.
    """
    session_id = os.environ.get("BELAYER_SESSION_ID", "")
    agent_id = os.environ.get("BELAYER_AGENT_ID", "")
    socket_path = os.environ.get("BELAYER_SOCKET", "")
    kind = (os.environ.get("BELAYER_AGENT_KIND", KIND_MAIN) or KIND_MAIN).strip().lower()
    if kind not in (KIND_MAIN, KIND_SIDE):
        kind = KIND_MAIN

    if not (session_id and agent_id and socket_path):
        # Not running inside a Belayer bridge — no-op. This makes the plugin
        # safe to enable globally: it registers nothing unless BELAYER_* env
        # vars signal a real session context.
        logger.debug(
            "belayer plugin: no BELAYER_* env vars; skipping registration (session=%r agent=%r)",
            session_id, agent_id,
        )
        return

    # belayer_broadcast — main agents only; side agents have no mailbox.
    if kind == KIND_MAIN:
        ctx.register_tool(
            name=BROADCAST_SCHEMA["name"],
            toolset="belayer",
            schema=BROADCAST_SCHEMA,
            handler=_make_broadcast_handler(agent_id, session_id, socket_path),
            description=BROADCAST_SCHEMA["description"],
        )
        logger.info(
            "belayer plugin: registered belayer_broadcast for agent=%s session=%s",
            agent_id, session_id,
        )
