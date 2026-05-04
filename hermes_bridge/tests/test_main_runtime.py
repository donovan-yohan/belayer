"""Focused tests for hermes_bridge.__main__ runtime behavior."""

from __future__ import annotations

import importlib
import sys
import types
from unittest.mock import MagicMock

import pytest


def _load_main_module(monkeypatch):
    fake_run_agent = types.ModuleType("run_agent")
    fake_state = types.ModuleType("hermes_state")
    fake_hermes_cli = types.ModuleType("hermes_cli")

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
    fake_hermes_cli.__version__ = "0.12.0"
    monkeypatch.setitem(sys.modules, "run_agent", fake_run_agent)
    monkeypatch.setitem(sys.modules, "hermes_state", fake_state)
    monkeypatch.setitem(sys.modules, "hermes_cli", fake_hermes_cli)
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
    # register_belayer_tools was removed in phase 2 — tools are now owned
    # by the plugins.belayer package which hermes discovers at AIAgent
    # import time. The bridge's fallback `from hermes_plugins import
    # belayer as belayer_plugin` is tolerated to fail in tests (bridge
    # logs a warning and falls back to an empty turn_mail_ids list).
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
    monkeypatch.delenv("BELAYER_ENABLED_TOOLSETS", raising=False)


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
    assert "persist_session" not in created_agents[0].kwargs
    assert "[Message from peer]: hello" in created_agents[0].last_run["user_message"]

    ack_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:message_ack"]
    assert len(ack_calls) == 1
    assert ack_calls[0].args[4]["ids"] == ["msg-1"]

    budget_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:budget_exhausted"]
    assert len(budget_calls) == 1

    finished_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:finished"]
    assert len(finished_calls) == 1


def test_main_requires_hermes_0_12_or_newer(monkeypatch, caplog):
    module = _load_main_module(monkeypatch)
    monkeypatch.setattr(module, "HERMES_VERSION", "0.11.0")

    with pytest.raises(SystemExit) as exc:
        module.require_supported_hermes_version()

    assert exc.value.code == 1
    assert "Hermes 0.12.0 or newer is required" in caplog.text


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
    assert "persist_session" not in created_agents[0].kwargs

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
    # ephemeral is a bridge-local flag, not an AIAgent kwarg (cfbe579).
    # It drives the post-completion idle vs exit logic.
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
    # ephemeral is a bridge-local flag, not an AIAgent kwarg (cfbe579).
    # The override is observable via the post-completion idle vs exit path.
    assert "ephemeral" not in created_agents[0].kwargs


def test_completed_on_last_turn_is_not_marked_budget_exhausted(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="3")
    monkeypatch.setenv("BELAYER_EPHEMERAL", "true")

    created_agents = []
    result = {
        "completed": True,
        "turns_used": 3,
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

    budget_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:budget_exhausted"]
    assert budget_calls == []

    finished_calls = [c for c in post_event.call_args_list if c.args[3] == "bridge:finished"]
    assert len(finished_calls) == 1
    assert "reason" not in finished_calls[0].args[4]


def test_non_positive_max_turns_is_ignored(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="0")
    monkeypatch.setenv("BELAYER_EPHEMERAL", "true")

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
        pending_messages=[],
        created_agents=created_agents,
    )

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert "max_iterations" not in created_agents[0].kwargs


def test_main_honors_enabled_toolsets_env(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="5")
    monkeypatch.setenv("BELAYER_EPHEMERAL", "true")
    monkeypatch.setenv("BELAYER_ENABLED_TOOLSETS", "file, code_execution")

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
        pending_messages=[],
        created_agents=created_agents,
    )

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert created_agents[0].kwargs["enabled_toolsets"] == ["file", "code_execution"]


def test_main_ignores_empty_enabled_toolsets_env(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="5")
    monkeypatch.setenv("BELAYER_EPHEMERAL", "true")
    monkeypatch.setenv("BELAYER_ENABLED_TOOLSETS", "")

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
        pending_messages=[],
        created_agents=created_agents,
    )

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert created_agents[0].kwargs["enabled_toolsets"] == []


def test_main_treats_unset_enabled_toolsets_env_as_unconfigured(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="5")
    monkeypatch.setenv("BELAYER_AGENT_KIND", "side")  # avoid idle loop after completion
    monkeypatch.delenv("BELAYER_ENABLED_TOOLSETS", raising=False)

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
        pending_messages=[],
        created_agents=created_agents,
    )

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert "enabled_toolsets" not in created_agents[0].kwargs


def test_main_ignores_all_sentinel_enabled_toolsets_env(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="5")
    monkeypatch.setenv("BELAYER_EPHEMERAL", "true")
    monkeypatch.setenv("BELAYER_ENABLED_TOOLSETS", "__all__")

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
        pending_messages=[],
        created_agents=created_agents,
    )

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert "enabled_toolsets" not in created_agents[0].kwargs


def test_main_passthrough_provider_envs(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="5")
    monkeypatch.setenv("BELAYER_EPHEMERAL", "true")
    monkeypatch.setenv("BELAYER_API_KEY", "sk-test")
    monkeypatch.setenv("BELAYER_BASE_URL", "https://api.test")
    monkeypatch.setenv("BELAYER_PROVIDER", "test-provider")

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
        pending_messages=[],
        created_agents=created_agents,
    )

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert created_agents[0].kwargs["api_key"] == "sk-test"
    assert created_agents[0].kwargs["base_url"] == "https://api.test"
    assert created_agents[0].kwargs["provider"] == "test-provider"


def test_main_ignores_unset_provider_envs(monkeypatch):
    module = _load_main_module(monkeypatch)
    _set_required_env(monkeypatch, max_turns="5")
    monkeypatch.setenv("BELAYER_EPHEMERAL", "true")
    monkeypatch.delenv("BELAYER_API_KEY", raising=False)
    monkeypatch.delenv("BELAYER_BASE_URL", raising=False)
    monkeypatch.delenv("BELAYER_PROVIDER", raising=False)

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
        pending_messages=[],
        created_agents=created_agents,
    )

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert created_agents, "expected AIAgent to be constructed"
    assert "api_key" not in created_agents[0].kwargs
    assert "base_url" not in created_agents[0].kwargs
    assert "provider" not in created_agents[0].kwargs


def test_active_peers_uses_lowercase_json_keys(monkeypatch):
    """Regression: roster JSON keys are lowercase (`name`, `status`).

    Reading them as `Name`/`Status` silently classifies every peer as
    terminal, which made the supervisor's idle countdown tick forward
    even while specialists were actively running tools — producing a
    false `agent_status:incomplete` after idle_timeout despite live
    work.
    """
    module = _load_main_module(monkeypatch)
    roster = [
        {"name": "supervisor", "status": "running"},
        {"name": "extend-api-dev", "status": "running"},
        {"name": "research", "status": "starting"},
        {"name": "qa", "status": "exited"},
        {"name": "old-pm", "status": "idle"},
    ]

    rows = module.active_peers(roster, "supervisor")
    names = sorted(r["name"] for r in rows)

    assert names == ["extend-api-dev", "research"], (
        "supervisor should be excluded; only starting/running peers count "
        f"as active, got {names}"
    )


def test_active_peers_ignores_capitalised_keys(monkeypatch):
    """If something feeds capitalised keys, treat them as unknown (not active)."""
    module = _load_main_module(monkeypatch)
    roster = [
        {"Name": "extend-api-dev", "Status": "running"},
    ]

    assert module.active_peers(roster, "supervisor") == []


def _load_main_module_with_session_db_capture(monkeypatch):
    """Like _load_main_module but returns a SessionDB class that records db_path."""
    import pathlib

    fake_run_agent = types.ModuleType("run_agent")
    fake_state = types.ModuleType("hermes_state")
    fake_hermes_cli = types.ModuleType("hermes_cli")

    class _PlaceholderAIAgent:
        def __init__(self, **kwargs):
            self.kwargs = kwargs
            self.session_id = "placeholder-session"

        def run_conversation(self, **kwargs):  # pragma: no cover - replaced per test
            return kwargs

    class _CapturingSessionDB:
        instances: list = []

        def __init__(self, *args, **kwargs):
            _CapturingSessionDB.instances.append(self)
            self.db_path = kwargs.get("db_path")

    fake_run_agent.AIAgent = _PlaceholderAIAgent
    fake_state.SessionDB = _CapturingSessionDB
    fake_hermes_cli.__version__ = "0.12.0"
    monkeypatch.setitem(sys.modules, "run_agent", fake_run_agent)
    monkeypatch.setitem(sys.modules, "hermes_state", fake_state)
    monkeypatch.setitem(sys.modules, "hermes_cli", fake_hermes_cli)
    monkeypatch.delitem(sys.modules, "hermes_bridge.__main__", raising=False)
    _CapturingSessionDB.instances.clear()
    module = importlib.import_module("hermes_bridge.__main__")
    return module, _CapturingSessionDB


def test_session_db_uses_hermes_home_when_set(monkeypatch):
    """SessionDB db_path should follow HERMES_HOME when the env var is set."""
    import pathlib

    monkeypatch.setenv("HERMES_HOME", "/tmp/foo-profile")
    module, CapturingSessionDB = _load_main_module_with_session_db_capture(monkeypatch)
    _set_required_env(monkeypatch)
    # Use side kind so the agent exits immediately after completion without
    # entering the idle polling loop.
    monkeypatch.setenv("BELAYER_AGENT_KIND", "side")
    monkeypatch.setenv("HERMES_HOME", "/tmp/foo-profile")  # ensure it survives _set_required_env

    result = {
        "completed": True,
        "final_response": "done",
        "messages": [],
    }
    _patch_bridge_runtime(monkeypatch, module, result)

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert CapturingSessionDB.instances, "expected SessionDB to be constructed"
    assert CapturingSessionDB.instances[0].db_path == pathlib.Path("/tmp/foo-profile/state.db"), (
        f"expected /tmp/foo-profile/state.db, got {CapturingSessionDB.instances[0].db_path}"
    )


def test_session_db_falls_back_to_dot_hermes_when_hermes_home_unset(monkeypatch):
    """SessionDB db_path should fall back to ~/.hermes/state.db when HERMES_HOME is unset."""
    import pathlib

    monkeypatch.delenv("HERMES_HOME", raising=False)
    module, CapturingSessionDB = _load_main_module_with_session_db_capture(monkeypatch)
    _set_required_env(monkeypatch)
    # Use side kind so the agent exits immediately after completion without
    # entering the idle polling loop.
    monkeypatch.setenv("BELAYER_AGENT_KIND", "side")
    monkeypatch.delenv("HERMES_HOME", raising=False)  # ensure it stays unset after _set_required_env

    result = {
        "completed": True,
        "final_response": "done",
        "messages": [],
    }
    _patch_bridge_runtime(monkeypatch, module, result)

    post_event = MagicMock()
    monkeypatch.setattr(module, "post_event", post_event)

    module.main()

    assert CapturingSessionDB.instances, "expected SessionDB to be constructed"
    expected = pathlib.Path.home() / ".hermes" / "state.db"
    assert CapturingSessionDB.instances[0].db_path == expected, (
        f"expected {expected}, got {CapturingSessionDB.instances[0].db_path}"
    )
