"""Focused tests for hermes_bridge.__main__ runtime behavior."""

from __future__ import annotations

import importlib
import sys
import types
from unittest.mock import MagicMock


def _load_main_module(monkeypatch):
    fake_run_agent = types.ModuleType("run_agent")
    fake_state = types.ModuleType("hermes_state")

    class _PlaceholderAIAgent:
        def __init__(self, **kwargs):
            self.kwargs = kwargs
            self.session_id = "placeholder-session"

        def run_conversation(self, **kwargs):  # pragma: no cover - replaced per test
            return kwargs

    class _PlaceholderSessionDB:
        def __init__(self, *args, **kwargs):
            pass

    fake_run_agent.AIAgent = _PlaceholderAIAgent
    fake_state.SessionDB = _PlaceholderSessionDB
    monkeypatch.setitem(sys.modules, "run_agent", fake_run_agent)
    monkeypatch.setitem(sys.modules, "hermes_state", fake_state)
    monkeypatch.delitem(sys.modules, "hermes_bridge.__main__", raising=False)
    return importlib.import_module("hermes_bridge.__main__")


def _patch_bridge_runtime(monkeypatch, module, result, pending_messages=None, created_agents=None):
    pending_messages = list(pending_messages or [])
    created_agents = created_agents if created_agents is not None else []

    class FakeAIAgent:
        def __init__(self, **kwargs):
            self.kwargs = kwargs
            self.session_id = "hermes-session-123"
            created_agents.append(self)

        def run_conversation(self, **kwargs):
            self.last_run = kwargs
            return result

    class DummyReader:
        def __init__(self, *args, **kwargs):
            self.args = args
            self.kwargs = kwargs

        def start(self):
            return None

        def stop(self):
            return None

    monkeypatch.setattr(module, "AIAgent", FakeAIAgent)
    def _fetch_pending_messages(*args, **kwargs):
        if pending_messages:
            return [pending_messages.pop(0)]
        return []

    monkeypatch.setattr(module, "fetch_pending_messages", _fetch_pending_messages)
    monkeypatch.setattr(module, "make_callbacks", lambda *args, **kwargs: {})
    monkeypatch.setattr(module, "register_belayer_tools", lambda *args, **kwargs: None)
    monkeypatch.setattr(module, "start_heartbeat_thread", lambda *args, **kwargs: types.SimpleNamespace(set=lambda: None))
    monkeypatch.setattr(module, "StdinReader", DummyReader)


def _set_required_env(monkeypatch, max_turns="7"):
    monkeypatch.setenv("BELAYER_SESSION_ID", "sess-1")
    monkeypatch.setenv("BELAYER_AGENT_ID", "agent-1")
    monkeypatch.setenv("BELAYER_SOCKET", "/tmp/belayer.sock")
    monkeypatch.setenv("BELAYER_ROLE", "implementer")
    monkeypatch.setenv("BELAYER_AGENT_KIND", "main")
    monkeypatch.setenv("BELAYER_PROFILE", "default")
    monkeypatch.setenv("BELAYER_MAX_TURNS", max_turns)
    monkeypatch.delenv("BELAYER_MESSAGE", raising=False)
    monkeypatch.delenv("BELAYER_SYSTEM_PROMPT", raising=False)
    monkeypatch.delenv("BELAYER_HERMES_SESSION_ID", raising=False)
    monkeypatch.delenv("BELAYER_EPHEMERAL", raising=False)
    monkeypatch.delenv("BELAYER_TRANSCRIPT_PATH", raising=False)
    monkeypatch.delenv("BELAYER_TOOLS", raising=False)


def test_main_emits_message_ack_for_consumed_pending_messages(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="7")

    created_agents = []
    result = {
        "budget_exhausted": True,
        "turns_used": 1,
        "final_response": "done",
        "last_message": "done",
        "messages": [],
    }
    _patch_bridge_runtime(
        monkeypatch,
        module,
        result,
        pending_messages=[{"ID": "msg-1", "SenderID": "peer", "Content": "hello"}],
        created_agents=created_agents,
    )

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert created_agents[0].kwargs["max_iterations"] == 7
    assert "[Message from peer]: hello" in created_agents[0].last_run["user_message"]

    ack_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:message_ack"]
    assert len(ack_calls) == 1
    assert ack_calls[0].args[4]["ids"] == ["msg-1"]

    budget_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:budget_exhausted"]
    assert len(budget_calls) == 1

    finished_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:finished"]
    assert len(finished_calls) == 1


def test_main_emits_budget_exhausted_and_passes_max_turns(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="9")

    created_agents = []
    result = {
        "turns_used": 9,
        "last_message": "still refactoring",
        "final_response": "still refactoring",
        "completed": False,
        "messages": [],
    }
    _patch_bridge_runtime(
        monkeypatch,
        module,
        result,
        pending_messages=[],
        created_agents=created_agents,
    )

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert created_agents[0].kwargs["max_iterations"] == 9

    budget_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:budget_exhausted"]
    assert len(budget_calls) == 1
    assert budget_calls[0].args[4]["turns_used"] == 9
    assert budget_calls[0].args[4]["max_turns"] == 9
    assert budget_calls[0].args[4]["last_message"] == "still refactoring"

    finished_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:finished"]
    assert len(finished_calls) == 1
    assert finished_calls[0].args[4]["reason"] == "budget_exhausted"


def test_side_kind_skips_mail_polling(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="5")
    monkeypatch.setenv("BELAYER_AGENT_KIND", "side")

    created_agents = []
    result = {
        "completed": True,
        "final_response": "done",
        "messages": [],
    }
    _patch_bridge_runtime(
        monkeypatch,
        module,
        result,
        pending_messages=[{"ID": "msg-side", "SenderID": "peer", "Content": "ignore me"}],
        created_agents=created_agents,
    )

    fetch_calls = []

    def _fail_fetch(*args, **kwargs):
        fetch_calls.append((args, kwargs))
        raise AssertionError("side agents must not poll queued mail")

    monkeypatch.setattr(module, "fetch_pending_messages", _fail_fetch)
    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert created_agents[0].kwargs["ephemeral"] is True
    assert fetch_calls == []

    finished_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:finished"]
    assert len(finished_calls) == 1


def test_bridge_honors_ephemeral_env_override(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="5")
    monkeypatch.setenv("BELAYER_AGENT_KIND", "side")
    monkeypatch.setenv("BELAYER_EPHEMERAL", "false")

    created_agents = []
    result = {
        "budget_exhausted": True,
        "turns_used": 1,
        "final_response": "done",
        "last_message": "done",
        "messages": [],
    }
    _patch_bridge_runtime(
        monkeypatch,
        module,
        result,
        pending_messages=[],
        created_agents=created_agents,
    )

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert created_agents[0].kwargs["ephemeral"] is False
