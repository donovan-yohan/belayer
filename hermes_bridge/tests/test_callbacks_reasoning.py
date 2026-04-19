"""Tests for reasoning_callback and interim_assistant_callback in callbacks.py."""

from unittest.mock import MagicMock, patch

from hermes_bridge.callbacks import make_callbacks


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_writer():
    """Return a MagicMock that stands in for a _TranscriptWriter."""
    return MagicMock()


def _build(writer=None):
    """Build a callbacks dict with the given writer (or None)."""
    return make_callbacks(
        agent_id="test-agent",
        session_id="sess-1",
        socket_path="/tmp/fake.sock",
        transcript_writer=writer,
    )


# ---------------------------------------------------------------------------
# reasoning_callback tests
# ---------------------------------------------------------------------------


def test_reasoning_buffers_and_flushes_on_step():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(writer)

        cbs["reasoning_callback"]("chunk1")
        cbs["reasoning_callback"]("chunk2")
        cbs["reasoning_callback"]("chunk3")

        # Writer should NOT have been called before step fires.
        writer.write_turn.assert_not_called()

        cbs["step_callback"](messages=[])

    # One write_turn call with the joined text.
    writer.write_turn.assert_called_once_with({
        "kind": "reasoning",
        "turn": 1,
        "text": "chunk1chunk2chunk3",
    })

    # post_event should have received a bridge:agent_reasoning call.
    reasoning_calls = [
        c for c in mock_post.call_args_list
        if c.args[3] == "bridge:agent_reasoning"
    ]
    assert len(reasoning_calls) == 1
    assert reasoning_calls[0].args[4]["text"] == "chunk1chunk2chunk3"
    assert reasoning_calls[0].args[4]["turn"] == 1


def test_reasoning_empty_after_flush():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event"):
        cbs = _build(writer)

        cbs["reasoning_callback"]("chunk1")
        cbs["step_callback"](messages=[])  # flush

        # Reset call count to isolate the second step.
        writer.write_turn.reset_mock()

        cbs["step_callback"](messages=[])  # no new reasoning

    # No spurious second write.
    writer.write_turn.assert_not_called()


def test_reasoning_no_writer_noop():
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(writer=None)

        cbs["reasoning_callback"]("text")
        cbs["step_callback"](messages=[])

    reasoning_calls = [
        c for c in mock_post.call_args_list
        if c.args[3] == "bridge:agent_reasoning"
    ]
    assert len(reasoning_calls) == 0


def test_empty_reasoning_delta_ignored():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(writer)

        cbs["reasoning_callback"]("")
        cbs["reasoning_callback"](None)
        cbs["step_callback"](messages=[])

    # Buffer was never populated, so no write and no reasoning event.
    writer.write_turn.assert_not_called()
    reasoning_calls = [
        c for c in mock_post.call_args_list
        if c.args[3] == "bridge:agent_reasoning"
    ]
    assert len(reasoning_calls) == 0


# ---------------------------------------------------------------------------
# interim_assistant_callback tests
# ---------------------------------------------------------------------------


def test_narration_fires_immediately():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(writer)

        cbs["interim_assistant_callback"]("hello world", already_streamed=False)

    writer.write_turn.assert_called_once_with({
        "kind": "narration",
        "text": "hello world",
        "already_streamed": False,
    })

    narration_calls = [
        c for c in mock_post.call_args_list
        if c.args[3] == "bridge:agent_narration"
    ]
    assert len(narration_calls) == 1
    assert narration_calls[0].args[4]["text"] == "hello world"
    assert narration_calls[0].args[4]["already_streamed"] is False


def test_narration_streamed_flag_propagates():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(writer)

        cbs["interim_assistant_callback"]("streamed text", already_streamed=True)

    writer.write_turn.assert_called_once_with({
        "kind": "narration",
        "text": "streamed text",
        "already_streamed": True,
    })

    narration_calls = [
        c for c in mock_post.call_args_list
        if c.args[3] == "bridge:agent_narration"
    ]
    assert len(narration_calls) == 1
    assert narration_calls[0].args[4]["already_streamed"] is True


def test_narration_no_writer_noop():
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(writer=None)

        cbs["interim_assistant_callback"]("text")

    narration_calls = [
        c for c in mock_post.call_args_list
        if c.args[3] == "bridge:agent_narration"
    ]
    assert len(narration_calls) == 0


def test_empty_narration_ignored():
    writer = _make_writer()
    with patch("hermes_bridge.callbacks.post_event") as mock_post:
        cbs = _build(writer)

        cbs["interim_assistant_callback"]("")

    writer.write_turn.assert_not_called()
    narration_calls = [
        c for c in mock_post.call_args_list
        if c.args[3] == "bridge:agent_narration"
    ]
    assert len(narration_calls) == 0
