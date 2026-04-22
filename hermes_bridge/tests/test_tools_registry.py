"""Tests for hermes_bridge.tools registry split by kind and allowlist."""

from __future__ import annotations

import sys
import types

from hermes_bridge.tools import SEND_MESSAGE_SCHEMA, register_belayer_tools


class _FakeRegistry:
    def __init__(self) -> None:
        self.calls: list[dict] = []

    def register(self, *, name, toolset, schema, handler):
        self.calls.append(
            {
                "name": name,
                "toolset": toolset,
                "schema": schema,
                "handler": handler,
            }
        )


def _install_fake_tools_registry(monkeypatch):
    fake_registry = _FakeRegistry()
    tools_pkg = types.ModuleType("tools")
    tools_pkg.__path__ = []
    registry_mod = types.ModuleType("tools.registry")
    registry_mod.registry = fake_registry
    monkeypatch.setitem(sys.modules, "tools", tools_pkg)
    monkeypatch.setitem(sys.modules, "tools.registry", registry_mod)
    return fake_registry


def _build_agent():
    return types.SimpleNamespace(tools=[], valid_tool_names=set())


def test_register_belayer_tools_for_main(monkeypatch):
    fake_registry = _install_fake_tools_registry(monkeypatch)
    agent = _build_agent()

    register_belayer_tools(
        agent,
        agent_id="agent-1",
        session_id="sess-1",
        socket_path="/tmp/belayer.sock",
        agent_kind="main",
    )

    names = {call["name"] for call in fake_registry.calls}
    assert names == {
        "belayer_send_message",
        "belayer_broadcast",
        "belayer_check_mail",
        "belayer_create_artifact",
        "belayer_report_status",
    }


def test_register_belayer_tools_for_side(monkeypatch):
    fake_registry = _install_fake_tools_registry(monkeypatch)
    agent = _build_agent()

    register_belayer_tools(
        agent,
        agent_id="agent-1",
        session_id="sess-1",
        socket_path="/tmp/belayer.sock",
        agent_kind="side",
    )

    names = {call["name"] for call in fake_registry.calls}
    assert names == {
        "belayer_create_artifact",
        "belayer_report_status",
    }


def test_register_belayer_tools_honors_supervisor_allowlist(monkeypatch):
    fake_registry = _install_fake_tools_registry(monkeypatch)
    agent = _build_agent()

    register_belayer_tools(
        agent,
        agent_id="supervisor",
        session_id="sess-1",
        socket_path="/tmp/belayer.sock",
        agent_kind="main",
        allowed_tools=[
            "belayer_spawn_agent",
            "belayer_request_completion",
            "belayer_escalate_to_human",
        ],
    )

    names = {call["name"] for call in fake_registry.calls}
    assert "belayer_spawn_agent" in names
    assert "belayer_request_completion" in names
    assert "belayer_escalate_to_human" in names
    assert "belayer_approve_completion" not in names
    assert "belayer_reject_completion" not in names


def test_register_belayer_tools_honors_pm_allowlist(monkeypatch):
    fake_registry = _install_fake_tools_registry(monkeypatch)
    agent = _build_agent()

    register_belayer_tools(
        agent,
        agent_id="pm-1",
        session_id="sess-1",
        socket_path="/tmp/belayer.sock",
        agent_kind="side",
        allowed_tools=["belayer_approve_completion", "belayer_reject_completion"],
    )

    names = {call["name"] for call in fake_registry.calls}
    assert "belayer_approve_completion" in names
    assert "belayer_reject_completion" in names
    assert "belayer_send_message" not in names
    assert "belayer_spawn_agent" not in names


def test_send_message_schema_exposes_interrupt_flag():
    interrupt = SEND_MESSAGE_SCHEMA["parameters"]["properties"]["interrupt"]
    assert interrupt["type"] == "boolean"
