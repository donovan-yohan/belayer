"""Tests for trace-tier capture in callbacks.py (Tasks 3.3, 3.4, 3.5)."""

import json
import os
from pathlib import Path
from unittest.mock import patch

from hermes_bridge.callbacks import make_callbacks, _coerce_plain


def _build(log_level="standard"):
    return make_callbacks(
        agent_id="test-agent",
        session_id="sess-1",
        socket_path="/tmp/fake.sock",
        log_level=log_level,
    )


# ---------- Task 3.3: full_input / full_result ----------

def test_tool_start_emits_full_input_at_trace():
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(log_level="trace")
        big = "A" * 70000
        cbs["tool_start_callback"]("call-1", "Write", {"path": "/tmp/x.txt", "content": big})
    started = [c for c in mock_post.call_args_list if c.args[3] == "bridge:tool_started"]
    assert len(started) == 1
    data = started[0].args[4]
    assert "full_input" in data
    assert data["full_input"]["content"] == big


def test_tool_start_omits_full_input_at_standard():
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(log_level="standard")
        cbs["tool_start_callback"]("call-1", "Write", {"path": "/tmp/x.txt", "content": "A" * 70000})
    started = [c for c in mock_post.call_args_list if c.args[3] == "bridge:tool_started"]
    assert "full_input" not in started[0].args[4]


def test_tool_complete_emits_full_result_at_trace():
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(log_level="trace")
        cbs["tool_complete_callback"]("call-2", "Bash", {"command": "echo"}, {"stdout": "ok", "exit_code": 0})
    completed = [c for c in mock_post.call_args_list if c.args[3] == "bridge:tool_completed"]
    assert "full_result" in completed[0].args[4]


# ---------- Task 3.4: trace:fs_snapshot before/after ----------

def test_fs_snapshot_before_and_after_write(tmp_path):
    target = tmp_path / "hello.txt"
    target.write_text("initial", encoding="utf-8")

    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(log_level="trace")
        cbs["tool_start_callback"]("c", "Write", {"path": str(target), "content": "updated"})
        target.write_text("updated", encoding="utf-8")
        cbs["tool_complete_callback"]("c", "Write", {"path": str(target), "content": "updated"}, {"ok": True})

    snaps = [c for c in mock_post.call_args_list if c.args[3] == "trace:fs_snapshot"]
    assert len(snaps) == 2
    before, after = snaps[0].args[4], snaps[1].args[4]
    assert before["phase"] == "before" and after["phase"] == "after"
    assert before["exists"] is True
    assert before["content"] == "initial"
    assert after["content"] == "updated"
    assert before["sha256"] != after["sha256"]


def test_fs_snapshot_suppressed_at_standard(tmp_path):
    target = tmp_path / "hello.txt"
    target.write_text("x", encoding="utf-8")
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(log_level="standard")
        cbs["tool_start_callback"]("c", "Write", {"path": str(target), "content": "y"})
        cbs["tool_complete_callback"]("c", "Write", {"path": str(target), "content": "y"}, {"ok": True})
    snaps = [c for c in mock_post.call_args_list if c.args[3] == "trace:fs_snapshot"]
    assert snaps == []


# ---------- Task 3.5: trace:subprocess_exec ----------

def test_subprocess_exec_emitted_at_trace():
    # Only allowlisted vars pass; prior prefix-based inclusion of BELAYER_* was
    # removed (it would leak any future BELAYER_ env var without updating
    # _ENV_SECRET_VARS). The explicit allowlist is the security contract now.
    with patch.dict(os.environ, {"PATH": "/usr/bin", "SECRET": "nope", "BELAYER_FOO": "yes"}, clear=True):
        with patch("hermes_bridge.callbacks.post_event") as mock_post:
            cbs = _build(log_level="trace")
            cbs["tool_complete_callback"](
                "c", "Bash",
                {"command": "ls -la"},
                {"exit_code": 0, "stdout": "hi", "stderr": ""},
            )
    exec_events = [c for c in mock_post.call_args_list if c.args[3] == "trace:subprocess_exec"]
    assert len(exec_events) == 1
    d = exec_events[0].args[4]
    assert d["cmd"] == "ls -la"
    assert d["exit_code"] == 0
    assert d["stdout"] == "hi"
    assert "PATH" in d["env_subset"]
    assert "BELAYER_FOO" not in d["env_subset"]
    assert "SECRET" not in d["env_subset"]


def test_subprocess_exec_suppressed_at_standard():
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(log_level="standard")
        cbs["tool_complete_callback"]("c", "Bash", {"command": "ls"}, {"exit_code": 0, "stdout": "", "stderr": ""})
    exec_events = [c for c in mock_post.call_args_list if c.args[3] == "trace:subprocess_exec"]
    assert exec_events == []


# ---------- BLOCKER 1: secret env var filtering ----------

def test_subprocess_exec_filters_belayer_api_key():
    env = {"PATH": "/usr/bin", "BELAYER_API_KEY": "sk-secret", "BELAYER_SESSION_ID": "sess-1"}
    with patch.dict(os.environ, env, clear=True):
        with patch("hermes_bridge.callbacks.post_event") as mock_post:
            cbs = _build(log_level="trace")
            cbs["tool_complete_callback"](
                "c", "Bash",
                {"command": "echo hi"},
                {"exit_code": 0, "stdout": "hi", "stderr": ""},
            )
    exec_events = [c for c in mock_post.call_args_list if c.args[3] == "trace:subprocess_exec"]
    assert len(exec_events) == 1
    env_subset = exec_events[0].args[4]["env_subset"]
    assert "BELAYER_API_KEY" not in env_subset
    # BELAYER_SESSION_ID was incidentally included under the old prefix rule;
    # the allowlist doesn't cover it. Session/agent context is already on the
    # event envelope, so there's no reason to echo it into env_subset too.
    assert "BELAYER_SESSION_ID" not in env_subset


def test_subprocess_exec_filters_belayer_base_url():
    env = {"PATH": "/usr/bin", "BELAYER_BASE_URL": "https://api.secret.com", "BELAYER_AGENT_ID": "agent-1"}
    with patch.dict(os.environ, env, clear=True):
        with patch("hermes_bridge.callbacks.post_event") as mock_post:
            cbs = _build(log_level="trace")
            cbs["tool_complete_callback"](
                "c", "Bash",
                {"command": "echo hi"},
                {"exit_code": 0, "stdout": "hi", "stderr": ""},
            )
    exec_events = [c for c in mock_post.call_args_list if c.args[3] == "trace:subprocess_exec"]
    env_subset = exec_events[0].args[4]["env_subset"]
    assert "BELAYER_BASE_URL" not in env_subset
    assert "BELAYER_AGENT_ID" not in env_subset


def test_subprocess_exec_filters_belayer_provider():
    env = {"PATH": "/usr/bin", "BELAYER_PROVIDER": "openai", "BELAYER_ROLE": "supervisor"}
    with patch.dict(os.environ, env, clear=True):
        with patch("hermes_bridge.callbacks.post_event") as mock_post:
            cbs = _build(log_level="trace")
            cbs["tool_complete_callback"](
                "c", "Bash",
                {"command": "echo hi"},
                {"exit_code": 0, "stdout": "hi", "stderr": ""},
            )
    exec_events = [c for c in mock_post.call_args_list if c.args[3] == "trace:subprocess_exec"]
    env_subset = exec_events[0].args[4]["env_subset"]
    assert "BELAYER_PROVIDER" not in env_subset
    assert "BELAYER_ROLE" not in env_subset


# ---------- CONCERN 1: recursive _coerce_plain ----------

def test_coerce_plain_recursive_bytes():
    result = _coerce_plain({"outer": {"inner": b"binary data"}})
    # Must serialize without raising
    serialized = json.dumps(result)
    parsed = json.loads(serialized)
    assert "binary data" in parsed["outer"]["inner"]


def test_coerce_plain_recursive_path():
    result = _coerce_plain({"args": [Path("/tmp/x")]})
    serialized = json.dumps(result)
    parsed = json.loads(serialized)
    assert parsed["args"][0] == "/tmp/x"


def test_coerce_plain_catchall_custom_object():
    class SomeCustomClass:
        def __str__(self):
            return "custom-repr"

    result = _coerce_plain({"obj": SomeCustomClass()})
    serialized = json.dumps(result)
    parsed = json.loads(serialized)
    assert parsed["obj"] == "custom-repr"
