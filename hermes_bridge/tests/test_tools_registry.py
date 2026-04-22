"""Tests for hermes_bridge.tools registry split and compatibility aliases."""

from __future__ import annotations

import sys
import types

import pytest

from hermes_bridge.tools import register_belayer_tools


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
    tools_pkg.__path__ = []  # mark as package for import machinery
    registry_mod = types.ModuleType("tools.registry")
    registry_mod.registry = fake_registry
    monkeypatch.setitem(sys.modules, "tools", tools_pkg)
    monkeypatch.setitem(sys.modules, "tools.registry", registry_mod)
    return fake_registry


def _build_agent():
    return types.SimpleNamespace(tools=[], valid_tool_names=set())


@pytest.mark.parametrize(
    "agent_kind,is_game_master,expected,unexpected",
    [
        (
            "main",
            False,
            {
                "belayer_send",
                "belayer_broadcast",
                "belayer_check_mail",
                "belayer_register_artifact",
                "belayer_report_status",
                "belayer_send_message",
                "belayer_create_artifact",
            },
            {
                "belayer_spawn_main",
                "belayer_summon_side",
                "belayer_finish",
                "belayer_spawn_agent",
                "belayer_request_completion",
                "belayer_approve_completion",
                "belayer_reject_completion",
                "belayer_escalate_to_human",
            },
        ),
        (
            "main",
            True,
            {
                "belayer_send",
                "belayer_broadcast",
                "belayer_check_mail",
                "belayer_register_artifact",
                "belayer_report_status",
                "belayer_send_message",
                "belayer_create_artifact",
                "belayer_spawn_main",
                "belayer_summon_side",
                "belayer_finish",
                "belayer_spawn_agent",
                "belayer_request_completion",
                "belayer_escalate_to_human",
            },
            {
                "belayer_approve_completion",
                "belayer_reject_completion",
            },
        ),
        (
            "side",
            False,
            {
                "belayer_register_artifact",
                "belayer_report_status",
                "belayer_create_artifact",
            },
            {
                "belayer_send",
                "belayer_broadcast",
                "belayer_check_mail",
                "belayer_send_message",
                "belayer_spawn_main",
                "belayer_summon_side",
                "belayer_finish",
                "belayer_spawn_agent",
                "belayer_request_completion",
                "belayer_approve_completion",
                "belayer_reject_completion",
                "belayer_escalate_to_human",
            },
        ),
    ],
)
def test_register_belayer_tools_splits_by_kind(monkeypatch, agent_kind, is_game_master, expected, unexpected):
    fake_registry = _install_fake_tools_registry(monkeypatch)
    agent = _build_agent()

    register_belayer_tools(
        agent,
        agent_id="agent-1",
        session_id="sess-1",
        socket_path="/tmp/belayer.sock",
        agent_kind=agent_kind,
        is_game_master=is_game_master,
    )

    names = {call["name"] for call in fake_registry.calls}
    assert expected <= names
    assert not (unexpected & names)
    assert set(agent.valid_tool_names) == names
    assert {tool["function"]["name"] for tool in agent.tools} == names


def test_register_belayer_tools_allows_pm_legacy_tools_via_allowlist(monkeypatch):
    fake_registry = _install_fake_tools_registry(monkeypatch)
    agent = _build_agent()

    register_belayer_tools(
        agent,
        agent_id="pm-1",
        session_id="sess-1",
        socket_path="/tmp/belayer.sock",
        agent_kind="side",
        is_game_master=False,
        allowed_tools=["belayer_approve_completion", "belayer_reject_completion"],
    )

    names = {call["name"] for call in fake_registry.calls}
    assert "belayer_approve_completion" in names
    assert "belayer_reject_completion" in names
    assert "belayer_request_completion" not in names
    assert "belayer_spawn_agent" not in names
