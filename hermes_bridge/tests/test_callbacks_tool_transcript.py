"""Tests for tool_start/tool_complete/step transcript writes in callbacks.py.

Covers:
- tool_start_callback writes a "tool_start" record when transcript_writer is attached.
- tool_complete_callback writes a "tool_complete" record when transcript_writer is attached.
- step_callback writes a "step" record (even without reasoning).
- No transcript writes happen when writer is None.
- full_input/full_result gated on log_level=="trace".
- make_transcript_writer posts bridge:warning when init fails at verbose/trace.
"""

import os
import json
from unittest.mock import MagicMock, patch

import pytest

from hermes_bridge.callbacks import make_callbacks, make_transcript_writer


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_writer():
    return MagicMock()


def _build(writer=None, log_level="standard"):
    return make_callbacks(
        agent_id="test-agent",
        session_id="sess-1",
        socket_path="/tmp/fake.sock",
        transcript_writer=writer,
        log_level=log_level,
    )


# ---------------------------------------------------------------------------
# tool_start_callback
# ---------------------------------------------------------------------------


def test_tool_start_writes_record_to_transcript():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=writer)
        cbs["tool_start_callback"]("call-1", "Write", {"path": "/tmp/x.txt"})

    writer.write_turn.assert_called_once()
    record = writer.write_turn.call_args[0][0]
    assert record["kind"] == "tool_start"
    assert record["tool"] == "Write"
    assert record["tool_call_id"] == "call-1"
    assert "input_preview" in record


def test_tool_start_no_full_input_at_standard():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=writer, log_level="standard")
        cbs["tool_start_callback"]("call-1", "Write", {"path": "/tmp/x.txt", "content": "A" * 500})

    record = writer.write_turn.call_args[0][0]
    assert "full_input" not in record


def test_tool_start_includes_full_input_at_trace():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=writer, log_level="trace")
        cbs["tool_start_callback"]("call-1", "Write", {"path": "/tmp/x.txt", "content": "hello"})

    record = writer.write_turn.call_args[0][0]
    assert "full_input" in record
    assert record["full_input"]["content"] == "hello"


def test_tool_start_no_writer_no_transcript():
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=None)
        # Should not raise; post_event is still called (daemon event).
        cbs["tool_start_callback"]("call-1", "Write", {"path": "/tmp/x.txt"})
    # Nothing to assert on writer — just verifying no crash.


# ---------------------------------------------------------------------------
# tool_complete_callback
# ---------------------------------------------------------------------------


def test_tool_complete_writes_record_to_transcript():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=writer)
        cbs["tool_start_callback"]("call-2", "Bash", {"command": "ls"})
        cbs["tool_complete_callback"]("call-2", "Bash", {"command": "ls"}, {"stdout": "ok", "exit_code": 0})

    # write_turn called twice: once for tool_start, once for tool_complete
    assert writer.write_turn.call_count == 2
    complete_record = writer.write_turn.call_args_list[1][0][0]
    assert complete_record["kind"] == "tool_complete"
    assert complete_record["tool"] == "Bash"
    assert complete_record["tool_call_id"] == "call-2"
    assert "duration_ms" in complete_record
    assert "result_preview" in complete_record


def test_tool_complete_no_full_result_at_standard():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=writer, log_level="standard")
        cbs["tool_complete_callback"]("call-3", "Read", {}, "file contents here")

    complete_record = writer.write_turn.call_args[0][0]
    assert "full_result" not in complete_record


def test_tool_complete_includes_full_result_at_trace():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=writer, log_level="trace")
        cbs["tool_complete_callback"]("call-4", "Read", {}, {"data": "full content"})

    complete_record = writer.write_turn.call_args[0][0]
    assert "full_result" in complete_record
    assert complete_record["full_result"]["data"] == "full content"


def test_tool_complete_no_writer_no_transcript():
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=None)
        cbs["tool_complete_callback"]("call-5", "Read", {}, "result")
    # No crash; daemon event posted normally.


# ---------------------------------------------------------------------------
# step_callback
# ---------------------------------------------------------------------------


def test_step_writes_step_record_without_reasoning():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=writer)
        # Fire step_callback with no prior reasoning_callback calls.
        cbs["step_callback"](messages=[])

    # One write_turn: the step record (no reasoning to flush).
    writer.write_turn.assert_called_once()
    record = writer.write_turn.call_args[0][0]
    assert record["kind"] == "step"
    assert record["turn"] == 1


def test_step_writes_step_record_each_step():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=writer)
        cbs["step_callback"](messages=[])
        cbs["step_callback"](messages=[])
        cbs["step_callback"](messages=[])

    calls = [c[0][0] for c in writer.write_turn.call_args_list]
    step_records = [r for r in calls if r["kind"] == "step"]
    assert len(step_records) == 3
    assert [r["turn"] for r in step_records] == [1, 2, 3]


def test_step_no_writer_no_transcript():
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=None)
        cbs["step_callback"](messages=[])
    # No crash; bridge:step_completed event posted normally.


def test_step_writes_both_reasoning_and_step_when_reasoning_buffered():
    """When reasoning was buffered, step_callback writes reasoning then step."""
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer=writer)
        cbs["reasoning_callback"]("some thinking")
        cbs["step_callback"](messages=[])

    calls = [c[0][0] for c in writer.write_turn.call_args_list]
    kinds = [r["kind"] for r in calls]
    # reasoning flushed first, then step
    assert kinds == ["reasoning", "step"]
    assert calls[0]["kind"] == "reasoning"
    assert calls[1]["kind"] == "step"
    assert calls[1]["turn"] == 1


# ---------------------------------------------------------------------------
# make_transcript_writer: warning event on failure at verbose/trace
# ---------------------------------------------------------------------------


def test_make_transcript_writer_posts_warning_on_failure_at_verbose(tmp_path):
    bad_path = str(tmp_path / "nonexistent_dir" / "sub" / "agent.jsonl")
    # Remove dirs so open() will fail (makedirs would succeed, so patch it).
    with patch("hermes_bridge.callbacks.post_event") as mock_post, \
         patch("os.makedirs", side_effect=OSError("permission denied")):
        result = make_transcript_writer(
            bad_path, "agent-x",
            log_level="verbose",
            socket_path="/tmp/fake.sock",
            session_id="sess-1",
        )

    assert result is None
    warning_calls = [
        c for c in mock_post.call_args_list
        if c.args[3] == "bridge:warning"
    ]
    assert len(warning_calls) == 1
    data = warning_calls[0].args[4]
    assert data["kind"] == "transcript_disabled"
    assert data["path"] == bad_path


def test_make_transcript_writer_posts_warning_on_failure_at_trace(tmp_path):
    bad_path = str(tmp_path / "no" / "agent.jsonl")
    with patch("hermes_bridge.callbacks.post_event") as mock_post, \
         patch("os.makedirs", side_effect=OSError("no space")):
        result = make_transcript_writer(
            bad_path, "agent-y",
            log_level="trace",
            socket_path="/tmp/fake.sock",
            session_id="sess-2",
        )

    assert result is None
    warning_calls = [
        c for c in mock_post.call_args_list
        if c.args[3] == "bridge:warning"
    ]
    assert len(warning_calls) == 1


def test_make_transcript_writer_no_warning_at_standard(tmp_path):
    bad_path = str(tmp_path / "no" / "agent.jsonl")
    with patch("hermes_bridge.callbacks.post_event") as mock_post, \
         patch("os.makedirs", side_effect=OSError("no space")):
        result = make_transcript_writer(
            bad_path, "agent-z",
            log_level="standard",
            socket_path="/tmp/fake.sock",
            session_id="sess-3",
        )

    assert result is None
    warning_calls = [
        c for c in mock_post.call_args_list
        if c.args[3] == "bridge:warning"
    ]
    assert len(warning_calls) == 0


def test_make_transcript_writer_no_warning_when_no_socket(tmp_path):
    """When socket_path is empty, warning cannot be posted — no crash."""
    bad_path = str(tmp_path / "no" / "agent.jsonl")
    with patch("hermes_bridge.callbacks.post_event") as mock_post, \
         patch("os.makedirs", side_effect=OSError("no space")):
        result = make_transcript_writer(
            bad_path, "agent-w",
            log_level="verbose",
            socket_path="",
            session_id="sess-4",
        )

    assert result is None
    # No post_event call since socket_path is empty.
    assert mock_post.call_count == 0
