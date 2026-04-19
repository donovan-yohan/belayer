"""Tests for trace-tier capture in callbacks.py (Tasks 3.3, 3.4, 3.5)."""

import os
from unittest.mock import patch

from hermes_bridge.callbacks import make_callbacks


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
    assert "BELAYER_FOO" in d["env_subset"]
    assert "SECRET" not in d["env_subset"]


def test_subprocess_exec_suppressed_at_standard():
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(log_level="standard")
        cbs["tool_complete_callback"]("c", "Bash", {"command": "ls"}, {"exit_code": 0, "stdout": "", "stderr": ""})
    exec_events = [c for c in mock_post.call_args_list if c.args[3] == "trace:subprocess_exec"]
    assert exec_events == []
