"""Tests for urgent stdin acknowledgements in stdin_reader.py."""

from __future__ import annotations

import io
import json
import queue
import sys
from unittest.mock import MagicMock, patch

from hermes_bridge.stdin_reader import StdinReader


def test_stdin_reader_emits_message_ack_after_interrupt_accept(monkeypatch):
    agent = MagicMock()
    interrupt_queue: queue.Queue = queue.Queue()
    stdin_payload = "\n".join(
        [
            json.dumps({"type": "interrupt", "from": "supervisor", "content": "wake up", "id": "mid-1"}),
            json.dumps({"type": "stop"}),
        ]
    ) + "\n"
    monkeypatch.setattr(sys, "stdin", io.StringIO(stdin_payload))

    with patch("hermes_bridge.stdin_reader.post_event") as mock_post:
        reader = StdinReader(agent, interrupt_queue, "/tmp/belayer.sock", "sess-1", "agent-1")
        reader._read_loop()

    queued = interrupt_queue.get_nowait()
    assert queued["type"] == "interrupt"
    assert queued["id"] == "mid-1"

    ack_calls = [c for c in mock_post.call_args_list if c.args[3] == "bridge:message_ack"]
    assert len(ack_calls) == 1
    assert ack_calls[0].args[4]["ids"] == ["mid-1"]

    agent.interrupt.assert_any_call("wake up")
