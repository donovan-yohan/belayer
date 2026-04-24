"""Tests for plugins.belayer — the Hermes 0.11 plugin entry point.

Verifies the plugin's register(ctx) wiring: which tools it registers, how
env vars drive kind-gating, and that missing env vars (plugin enabled
outside a Belayer session) produce a clean no-op rather than an error.
"""

from __future__ import annotations

import importlib.util
import sys
import types
from pathlib import Path

import pytest


def _load_plugin_module():
    """Load plugins/belayer/__init__.py as a standalone module.

    We deliberately do not rely on the Hermes plugin loader here — these
    tests exercise register(ctx) in isolation against a fake context.
    """
    plugin_dir = Path(__file__).resolve().parent.parent
    init_file = plugin_dir / "__init__.py"
    spec = importlib.util.spec_from_file_location(
        "belayer_plugin_under_test", init_file
    )
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules["belayer_plugin_under_test"] = module
    spec.loader.exec_module(module)
    return module


class _FakeCtx:
    """Captures ctx.register_tool and ctx.register_hook calls for assertions."""

    def __init__(self) -> None:
        self.tools: list[dict] = []
        self.hooks: list[tuple[str, object]] = []

    def register_tool(self, *, name, toolset, schema, handler, description="", **_ignored):
        self.tools.append(
            {
                "name": name,
                "toolset": toolset,
                "schema": schema,
                "handler": handler,
                "description": description,
            }
        )

    def register_hook(self, hook_name, callback):
        self.hooks.append((hook_name, callback))


def test_register_emits_broadcast_for_main(monkeypatch):
    """Main agents get belayer_broadcast — replaces legacy register_belayer_tools path."""
    plugin = _load_plugin_module()
    monkeypatch.setenv("BELAYER_SESSION_ID", "sess-1")
    monkeypatch.setenv("BELAYER_AGENT_ID", "supervisor")
    monkeypatch.setenv("BELAYER_SOCKET", "/tmp/belayer.sock")
    monkeypatch.setenv("BELAYER_AGENT_KIND", "main")

    ctx = _FakeCtx()
    plugin.register(ctx)

    names = [t["name"] for t in ctx.tools]
    assert "belayer_broadcast" in names, names
    # Toolset is "belayer" so hermes groups plugin tools together in the UI.
    broadcast = next(t for t in ctx.tools if t["name"] == "belayer_broadcast")
    assert broadcast["toolset"] == "belayer"
    # Schema matches the legacy contract so downstream prompts don't break.
    assert broadcast["schema"]["parameters"]["required"] == ["content"]


def test_register_skips_broadcast_for_side(monkeypatch):
    """Side agents have no mailbox — broadcast is a mail surface and must stay gated."""
    plugin = _load_plugin_module()
    monkeypatch.setenv("BELAYER_SESSION_ID", "sess-1")
    monkeypatch.setenv("BELAYER_AGENT_ID", "pm")
    monkeypatch.setenv("BELAYER_SOCKET", "/tmp/belayer.sock")
    monkeypatch.setenv("BELAYER_AGENT_KIND", "side")

    ctx = _FakeCtx()
    plugin.register(ctx)

    names = [t["name"] for t in ctx.tools]
    assert "belayer_broadcast" not in names, names


def test_register_no_env_is_noop(monkeypatch):
    """Plugin enabled outside a Belayer session (e.g. user enables globally for
    introspection) must register nothing — not raise — so unrelated hermes
    sessions keep working."""
    plugin = _load_plugin_module()
    monkeypatch.delenv("BELAYER_SESSION_ID", raising=False)
    monkeypatch.delenv("BELAYER_AGENT_ID", raising=False)
    monkeypatch.delenv("BELAYER_SOCKET", raising=False)

    ctx = _FakeCtx()
    plugin.register(ctx)

    assert ctx.tools == []
    assert ctx.hooks == []


def test_register_unknown_kind_defaults_to_main(monkeypatch):
    """Bogus BELAYER_AGENT_KIND should fall back to main rather than silently
    dropping broadcast — supervisor outranks typos."""
    plugin = _load_plugin_module()
    monkeypatch.setenv("BELAYER_SESSION_ID", "sess-1")
    monkeypatch.setenv("BELAYER_AGENT_ID", "supervisor")
    monkeypatch.setenv("BELAYER_SOCKET", "/tmp/belayer.sock")
    monkeypatch.setenv("BELAYER_AGENT_KIND", "weird-value")

    ctx = _FakeCtx()
    plugin.register(ctx)

    names = [t["name"] for t in ctx.tools]
    assert "belayer_broadcast" in names


def test_broadcast_handler_posts_to_daemon(monkeypatch):
    """End-to-end: registered broadcast handler issues a POST to the expected path."""
    plugin = _load_plugin_module()
    monkeypatch.setenv("BELAYER_SESSION_ID", "sess-42")
    monkeypatch.setenv("BELAYER_AGENT_ID", "supervisor")
    monkeypatch.setenv("BELAYER_SOCKET", "/tmp/fake.sock")
    monkeypatch.setenv("BELAYER_AGENT_KIND", "main")

    seen: list[tuple] = []

    def _fake_post(socket_path, path, payload):
        seen.append((socket_path, path, payload))
        return 201, ""

    monkeypatch.setattr(plugin, "_unix_post", _fake_post)

    ctx = _FakeCtx()
    plugin.register(ctx)
    broadcast = next(t for t in ctx.tools if t["name"] == "belayer_broadcast")

    result = broadcast["handler"]({"content": "party time"})
    assert result == "Broadcast sent."
    assert len(seen) == 1
    sock, path, payload = seen[0]
    assert sock == "/tmp/fake.sock"
    assert path == "/sessions/sess-42/messages/broadcast"
    assert payload == {"content": "party time", "from": "supervisor", "type": "instruction"}


def test_broadcast_handler_reports_error_on_daemon_failure(monkeypatch):
    """A non-201 daemon response must surface as a [System] message the agent
    can reason about — not a silent success."""
    plugin = _load_plugin_module()
    monkeypatch.setenv("BELAYER_SESSION_ID", "sess-1")
    monkeypatch.setenv("BELAYER_AGENT_ID", "supervisor")
    monkeypatch.setenv("BELAYER_SOCKET", "/tmp/fake.sock")
    monkeypatch.setenv("BELAYER_AGENT_KIND", "main")

    monkeypatch.setattr(plugin, "_unix_post", lambda *a, **k: (500, "daemon down"))

    ctx = _FakeCtx()
    plugin.register(ctx)
    broadcast = next(t for t in ctx.tools if t["name"] == "belayer_broadcast")

    result = broadcast["handler"]({"content": "hi"})
    assert result.startswith("[System]")
    assert "daemon down" in result
