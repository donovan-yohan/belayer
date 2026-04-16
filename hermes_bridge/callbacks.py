"""Hermes callback -> daemon event forwarding.

Each callback posts a typed event to the daemon's event log so the
session bus can track agent progress without polling.
"""

import json
import time
import logging
import threading

from hermes_bridge.http_client import unix_post

log = logging.getLogger("callbacks")

_HEARTBEAT_INTERVAL = 30  # seconds


def post_event(socket_path: str, session_id: str, agent_id: str, event_type: str, data: dict | None = None) -> None:
    """POST a typed event to /sessions/{session_id}/events.

    Failures are logged at DEBUG — event delivery is best-effort so
    a transient daemon hiccup doesn't crash the agent.
    """
    if data is None:
        data = {}
    data["agent"] = agent_id

    status, body = unix_post(
        socket_path,
        f"/sessions/{session_id}/events",
        {"type": event_type, "data": json.dumps(data)},
    )
    if status not in (200, 201):
        log.debug("post_event %s -> %d %s", event_type, status, body[:120])


def make_callbacks(agent_id: str, session_id: str, socket_path: str) -> dict:
    """Return a dict of callback_name -> callback_fn for AIAgent.

    Wire these onto the agent instance with setattr after construction:

        callbacks = make_callbacks(agent_id, session_id, socket_path)
        for attr, fn in callbacks.items():
            setattr(agent, attr, fn)
    """
    # Mutable state captured in closure — avoids a class for a handful of callbacks.
    state = {"step_count": 0, "last_heartbeat": 0.0, "tool_starts": {}}

    def _heartbeat_if_due() -> None:
        now = time.monotonic()
        if now - state["last_heartbeat"] >= _HEARTBEAT_INTERVAL:
            state["last_heartbeat"] = now
            post_event(socket_path, session_id, agent_id, "bridge:heartbeat", {})

    def tool_start_callback(tool_call_id, tool_name, tool_args, **kwargs):
        state["tool_starts"][tool_call_id] = time.monotonic()
        post_event(
            socket_path, session_id, agent_id,
            "bridge:tool_started",
            {
                "tool": tool_name,
                "input_preview": str(tool_args)[:200],
            },
        )

    def tool_complete_callback(tool_call_id, tool_name, tool_args, tool_result, **kwargs):
        started = state["tool_starts"].pop(tool_call_id, None)
        duration_ms = int((time.monotonic() - started) * 1000) if started else 0
        post_event(
            socket_path, session_id, agent_id,
            "bridge:tool_completed",
            {
                "tool": tool_name,
                "duration_ms": duration_ms,
                "result_preview": str(tool_result)[:200],
            },
        )

    def step_callback(messages=None, **kwargs):
        state["step_count"] += 1
        post_event(
            socket_path, session_id, agent_id,
            "bridge:step_completed",
            {"step": state["step_count"]},
        )
        _heartbeat_if_due()

    def status_callback(status_type=None, **kwargs):
        post_event(
            socket_path, session_id, agent_id,
            "bridge:status_change",
            {"status_type": str(status_type)},
        )

    def clarify_callback(question=None, **kwargs):
        post_event(
            socket_path, session_id, agent_id,
            "bridge:clarification_needed",
            {"question": str(question)[:500] if question else ""},
        )

    return {
        "tool_start_callback": tool_start_callback,
        "tool_complete_callback": tool_complete_callback,
        "step_callback": step_callback,
        "status_callback": status_callback,
        "clarify_callback": clarify_callback,
    }


def start_heartbeat_thread(socket_path: str, session_id: str, agent_id: str, interval: int = 30) -> threading.Event:
    """Start a daemon thread that sends periodic heartbeats.

    Returns the stop Event; call .set() to terminate the thread.
    This is a fallback for agents whose step_callback fires infrequently.
    """
    stop_event = threading.Event()

    def _loop():
        while not stop_event.wait(interval):
            post_event(socket_path, session_id, agent_id, "bridge:heartbeat", {})

    t = threading.Thread(target=_loop, daemon=True, name=f"heartbeat-{agent_id}")
    t.start()
    return stop_event
