"""Tests for _TranscriptWriter and make_transcript_writer in callbacks.py."""

import json
import threading

import pytest

from hermes_bridge.callbacks import _TranscriptWriter, make_transcript_writer


def test_make_transcript_writer_none_returns_none():
    assert make_transcript_writer(None, "agent") is None


def test_make_transcript_writer_empty_string_returns_none():
    assert make_transcript_writer("", "agent") is None


def test_write_turn_produces_jsonl_lines(tmp_path):
    path = str(tmp_path / "agent.jsonl")
    writer = make_transcript_writer(path, "test-agent")
    assert writer is not None

    for _ in range(3):
        writer.write_turn({"foo": "bar"})
    writer.close()

    with open(path, encoding="utf-8") as f:
        lines = [l for l in f.read().splitlines() if l]

    assert len(lines) == 3, f"expected 3 lines, got {len(lines)}"
    for line in lines:
        obj = json.loads(line)
        assert obj["foo"] == "bar", f"expected foo=bar in {obj}"
        assert obj["agent_id"] == "test-agent", f"expected agent_id in {obj}"
        assert "ts" in obj, f"expected ts in {obj}"


def test_write_turn_thread_safety(tmp_path):
    path = str(tmp_path / "concurrent.jsonl")
    writer = make_transcript_writer(path, "concurrent-agent")
    assert writer is not None

    threads = []
    for _ in range(10):
        t = threading.Thread(target=lambda: [writer.write_turn({"x": 1}) for _ in range(100)])
        threads.append(t)
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    writer.close()

    with open(path, encoding="utf-8") as f:
        lines = [l for l in f.read().splitlines() if l]

    assert len(lines) == 1000, f"expected 1000 lines, got {len(lines)}"
    for line in lines:
        obj = json.loads(line)  # raises if malformed
        assert "agent_id" in obj
        assert "ts" in obj
