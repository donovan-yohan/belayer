"""Hermes callback -> daemon event forwarding.

Each callback posts a typed event to the daemon's event log so the
session bus can track agent progress without polling.
"""

import hashlib
import json
import os
import time
import logging
import threading

from hermes_bridge.http_client import unix_post

log = logging.getLogger("callbacks")

_HEARTBEAT_INTERVAL = 30  # seconds

_MUTATING_TOOLS = frozenset({"Write", "Edit", "NotebookEdit", "write_file", "edit_file", "create_file"})
_SHELL_TOOLS = frozenset({"Bash", "run_shell", "bash"})
_ENV_ALLOWLIST = frozenset({"PATH", "PWD", "HOME", "USER", "LANG", "TERM", "NODE_ENV", "CI"})
_ENV_SECRET_VARS = frozenset({
    "BELAYER_API_KEY",
    "BELAYER_BASE_URL",
    "BELAYER_PROVIDER",
    "BELAYER_HTTP_PROXY",
    "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY",
})


def _fs_snapshot(path: str, phase: str) -> dict:
    try:
        with open(path, "rb") as f:
            data = f.read()
        return {
            "phase": phase,
            "path": path,
            "exists": True,
            "size_bytes": len(data),
            "sha256": hashlib.sha256(data).hexdigest(),
            "content": data.decode("utf-8", "replace"),
        }
    except FileNotFoundError:
        return {"phase": phase, "path": path, "exists": False, "size_bytes": 0, "sha256": "", "content": ""}
    except OSError:
        # Permission or read errors — still record the attempt so the trace is complete.
        return {"phase": phase, "path": path, "exists": False, "size_bytes": 0, "sha256": "", "content": ""}


def _filtered_env() -> dict:
    out = {
        k: v for k, v in os.environ.items()
        if k not in _ENV_SECRET_VARS and (k in _ENV_ALLOWLIST or k.startswith("BELAYER_"))
    }
    return out


def _coerce_plain(v):
    """Best-effort convert arbitrary tool payloads to JSON-friendly values.

    Recursively normalizes dicts, lists, and leaf values so that
    json.dumps(_coerce_plain(x)) never raises.
    """
    if v is None or isinstance(v, (str, int, float, bool)):
        return v
    if isinstance(v, dict):
        return {k: _coerce_plain(val) for k, val in v.items()}
    if isinstance(v, (list, tuple)):
        return [_coerce_plain(item) for item in v]
    if isinstance(v, bytes):
        return v.decode("utf-8", errors="replace")
    # pathlib.Path and any other type with no obvious JSON mapping
    return str(v)


def post_event(socket_path: str, session_id: str, agent_id: str, event_type: str, data: dict | None = None) -> None:
    """POST a typed event to /sessions/{session_id}/events.

    Failures are logged at DEBUG — event delivery is best-effort so
    a transient daemon hiccup doesn't crash the agent.
    """
    if data is None:
        data = {}
    data["agent"] = agent_id

    # State-changing events drive session lifecycle; log failures at WARNING.
    _STATE_EVENTS = ("agent_status:", "bridge:finished", "bridge:failed")

    status, body = unix_post(
        socket_path,
        f"/sessions/{session_id}/events",
        {"type": event_type, "data": json.dumps(data)},
    )
    if status not in (200, 201):
        if any(event_type.startswith(prefix) for prefix in _STATE_EVENTS):
            log.warning("post_event %s -> %d %s", event_type, status, body[:120])
        else:
            log.debug("post_event %s -> %d %s", event_type, status, body[:120])


def make_callbacks(agent_id: str, session_id: str, socket_path: str,
                   transcript_writer=None, log_level: str = "standard") -> dict:
    """Return a dict of callback_name -> callback_fn for AIAgent.

    Wire these onto the agent instance with setattr after construction:

        callbacks = make_callbacks(agent_id, session_id, socket_path, transcript_writer=writer)
        for attr, fn in callbacks.items():
            setattr(agent, attr, fn)

    Pass transcript_writer (a _TranscriptWriter instance) to enable verbose
    reasoning/narration capture. When None, reasoning_callback and
    interim_assistant_callback are no-ops so there is zero overhead for
    standard (non-verbose) runs.

    log_level controls how much detail is captured in tool events:
      "standard" — input_preview/result_preview only (default)
      "verbose"  — same as standard (transcript_writer enables reasoning capture)
      "trace"    — additionally includes full_input/full_result on tool events,
                   trace:fs_snapshot events before/after mutating file tools,
                   and trace:subprocess_exec events after shell tools
    """
    # Mutable state captured in closure — avoids a class for a handful of callbacks.
    state = {"step_count": 0, "last_heartbeat": 0.0, "tool_starts": {}, "reasoning_buffer": []}

    def _heartbeat_if_due() -> None:
        now = time.monotonic()
        if now - state["last_heartbeat"] >= _HEARTBEAT_INTERVAL:
            state["last_heartbeat"] = now
            post_event(socket_path, session_id, agent_id, "bridge:heartbeat", {})

    # Tools that accept a file path via dict-style 'path' or 'file_path' kwarg.
    _FILE_TOOLS = frozenset({"read_file", "write_file", "edit_file", "create_file"})

    def _extract_path(tool_name: str, tool_args) -> str | None:
        """Extract file path from tool args for file operation events."""
        if tool_name not in _FILE_TOOLS:
            return None
        if isinstance(tool_args, dict):
            return tool_args.get("path") or tool_args.get("file_path")
        return None

    def tool_start_callback(tool_call_id, tool_name, tool_args, **kwargs):
        state["tool_starts"][tool_call_id] = time.monotonic()
        event_data = {
            "tool": tool_name,
            "input_preview": str(tool_args)[:200],
        }
        path = _extract_path(tool_name, tool_args)
        if path:
            event_data["path"] = path
        if log_level == "trace":
            event_data["full_input"] = _coerce_plain(tool_args)
        post_event(
            socket_path, session_id, agent_id,
            "bridge:tool_started",
            event_data,
        )
        if log_level == "trace" and tool_name in _MUTATING_TOOLS:
            snap_path = None
            if isinstance(tool_args, dict):
                snap_path = tool_args.get("path") or tool_args.get("file_path")
            if snap_path:
                post_event(
                    socket_path, session_id, agent_id,
                    "trace:fs_snapshot",
                    _fs_snapshot(snap_path, "before"),
                )

    def tool_complete_callback(tool_call_id, tool_name, tool_args, tool_result, **kwargs):
        started = state["tool_starts"].pop(tool_call_id, None)
        duration_ms = int((time.monotonic() - started) * 1000) if started else 0
        event_data = {
            "tool": tool_name,
            "duration_ms": duration_ms,
            "result_preview": str(tool_result)[:200],
        }
        path = _extract_path(tool_name, tool_args)
        if path:
            event_data["path"] = path
        if log_level == "trace":
            event_data["full_result"] = _coerce_plain(tool_result)
        post_event(
            socket_path, session_id, agent_id,
            "bridge:tool_completed",
            event_data,
        )
        if log_level != "trace":
            return
        if tool_name in _MUTATING_TOOLS:
            snap_path = None
            if isinstance(tool_args, dict):
                snap_path = tool_args.get("path") or tool_args.get("file_path")
            if snap_path:
                post_event(
                    socket_path, session_id, agent_id,
                    "trace:fs_snapshot",
                    _fs_snapshot(snap_path, "after"),
                )
        if tool_name in _SHELL_TOOLS:
            cmd = ""
            if isinstance(tool_args, dict):
                cmd = tool_args.get("command") or tool_args.get("cmd") or ""
            exit_code = None
            stdout = ""
            stderr = ""
            if isinstance(tool_result, dict):
                exit_code = tool_result.get("exit_code")
                stdout = tool_result.get("stdout", "")
                stderr = tool_result.get("stderr", "")
            else:
                stdout = str(tool_result)
            post_event(
                socket_path, session_id, agent_id,
                "trace:subprocess_exec",
                {
                    "cmd": str(cmd),
                    "exit_code": exit_code,
                    "stdout": str(stdout),
                    "stderr": str(stderr),
                    "env_subset": _filtered_env(),
                },
            )

    def reasoning_callback(text, **kwargs):
        if transcript_writer is None:
            return
        if not text:
            return
        state["reasoning_buffer"].append(str(text))

    def step_callback(messages=None, **kwargs):
        # Flush any buffered reasoning before incrementing step_count so the
        # turn number matches the turn whose reasoning we are reporting.
        if transcript_writer is not None and state["reasoning_buffer"]:
            full_text = "".join(state["reasoning_buffer"])
            state["reasoning_buffer"] = []
            turn = state["step_count"] + 1  # this turn, not yet incremented
            transcript_writer.write_turn({
                "kind": "reasoning",
                "turn": turn,
                "text": full_text,
            })
            post_event(
                socket_path, session_id, agent_id,
                "bridge:agent_reasoning",
                {"text": full_text, "turn": turn},
            )
        state["step_count"] += 1
        post_event(
            socket_path, session_id, agent_id,
            "bridge:step_completed",
            {"step": state["step_count"]},
        )
        _heartbeat_if_due()

    def interim_assistant_callback(visible, already_streamed=False, **kwargs):
        if transcript_writer is None:
            return
        if not visible:
            return
        text = str(visible)
        transcript_writer.write_turn({
            "kind": "narration",
            "text": text,
            "already_streamed": bool(already_streamed),
        })
        post_event(
            socket_path, session_id, agent_id,
            "bridge:agent_narration",
            {"text": text, "already_streamed": bool(already_streamed)},
        )

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
        "reasoning_callback": reasoning_callback,
        "interim_assistant_callback": interim_assistant_callback,
    }


class _TranscriptWriter:
    """Append-only JSONL transcript for a single agent.

    Thread-safe; one line per write_turn call with the supplied dict merged
    with timestamp and agent_id. Flush on every write to reduce loss if
    the bridge process crashes, but without fsync this does not guarantee
    durability across OS or host crashes.
    """

    def __init__(self, path: str, agent_id: str):
        self._path = path
        self._agent_id = agent_id
        self._lock = threading.Lock()
        os.makedirs(os.path.dirname(path), exist_ok=True)
        self._fh = open(path, "a", encoding="utf-8")

    def write_turn(self, data: dict) -> None:
        payload = {
            "ts": time.time(),
            "agent_id": self._agent_id,
            **data,
        }
        line = json.dumps(payload, ensure_ascii=False)
        with self._lock:
            self._fh.write(line)
            self._fh.write("\n")
            self._fh.flush()

    def close(self) -> None:
        with self._lock:
            try:
                self._fh.close()
            except Exception:
                pass


def make_transcript_writer(path: str | None, agent_id: str):
    """Return a _TranscriptWriter, or None if path is falsy.

    Call sites should gate on the return value: `if writer: writer.write_turn(...)`.
    """
    if not path:
        return None
    try:
        return _TranscriptWriter(path, agent_id)
    except OSError as exc:
        log.warning("transcript writer init failed for %s: %s", path, exc)
        return None


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
