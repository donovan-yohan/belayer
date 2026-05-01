"""Tests for plugins.belayer — the Hermes 0.12 plugin entry point.

Verifies the plugin's register(ctx) wiring: which tools it registers, how
env vars drive kind-gating and the BELAYER_TOOLS allowlist, that missing
env vars produce a clean no-op, and that individual tool handlers post the
right requests to the daemon.
"""

from __future__ import annotations

import plugins.belayer as plugin_pkg


def _load_plugin_module():
    """Return the plugin package module, clearing per-turn state so tests
    don't leak mutable buffer state into each other. Cached import — all
    tests share one module object, which matches the real Hermes plugin
    loader's behaviour (one module per subprocess)."""
    plugin_pkg._TURN_MAIL_IDS.clear()
    return plugin_pkg


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


def _set_required_env(monkeypatch, *, agent_id="supervisor", kind="main", tools=""):
    monkeypatch.setenv("BELAYER_SESSION_ID", "sess-1")
    monkeypatch.setenv("BELAYER_AGENT_ID", agent_id)
    monkeypatch.setenv("BELAYER_SOCKET", "/tmp/belayer.sock")
    monkeypatch.setenv("BELAYER_AGENT_KIND", kind)
    monkeypatch.setenv("BELAYER_TOOLS", tools)


# --- register() gating --------------------------------------------------


def test_main_kind_registers_baseline_plus_mail(monkeypatch):
    """Main agent with no allowlist: baseline (report_status, create_artifact)
    plus the mail surface (send_message, broadcast, check_mail)."""
    plugin = _load_plugin_module()
    _set_required_env(monkeypatch, kind="main")

    ctx = _FakeCtx()
    plugin.register(ctx)

    names = {t["name"] for t in ctx.tools}
    assert names == {
        "belayer_send_message",
        "belayer_broadcast",
        "belayer_check_mail",
        "belayer_report_status",
        "belayer_create_artifact",
    }


def test_side_kind_excludes_mail_surface(monkeypatch):
    """Side agents have no mailbox — baseline only."""
    plugin = _load_plugin_module()
    _set_required_env(monkeypatch, kind="side")

    ctx = _FakeCtx()
    plugin.register(ctx)

    names = {t["name"] for t in ctx.tools}
    assert names == {"belayer_report_status", "belayer_create_artifact"}


def test_supervisor_allowlist_adds_role_tools(monkeypatch):
    """Supervisor identity carries spawn + completion + escalate in its
    agent.yaml belayer_tools. Those are added on top of the main baseline."""
    plugin = _load_plugin_module()
    _set_required_env(
        monkeypatch,
        agent_id="supervisor",
        kind="main",
        tools="belayer_spawn_agent,belayer_request_completion,belayer_escalate_to_human",
    )

    ctx = _FakeCtx()
    plugin.register(ctx)

    names = {t["name"] for t in ctx.tools}
    assert "belayer_spawn_agent" in names
    assert "belayer_request_completion" in names
    assert "belayer_escalate_to_human" in names
    # PM-only tools must not leak to supervisor.
    assert "belayer_approve_completion" not in names
    assert "belayer_reject_completion" not in names


def test_pm_allowlist_on_side_kind(monkeypatch):
    """PM is a side agent that carries approve/reject in its allowlist. No
    mail surface. No spawn. Just the completion verdicts + baseline."""
    plugin = _load_plugin_module()
    _set_required_env(
        monkeypatch,
        agent_id="pm",
        kind="side",
        tools="belayer_approve_completion,belayer_reject_completion",
    )

    ctx = _FakeCtx()
    plugin.register(ctx)

    names = {t["name"] for t in ctx.tools}
    assert "belayer_approve_completion" in names
    assert "belayer_reject_completion" in names
    # No mail surface on side.
    assert "belayer_send_message" not in names
    assert "belayer_broadcast" not in names
    assert "belayer_check_mail" not in names
    # No spawn.
    assert "belayer_spawn_agent" not in names


def test_side_agent_allowlisting_drops_mail_tools(monkeypatch):
    """A misconfigured agent.yaml that lists mail tools for a side kind
    should be silently corrected — side agents don't have a mailbox."""
    plugin = _load_plugin_module()
    _set_required_env(
        monkeypatch,
        kind="side",
        tools="belayer_broadcast,belayer_approve_completion",
    )

    ctx = _FakeCtx()
    plugin.register(ctx)

    names = {t["name"] for t in ctx.tools}
    assert "belayer_broadcast" not in names
    assert "belayer_approve_completion" in names


def test_no_env_is_noop(monkeypatch):
    """Plugin enabled outside a Belayer session must register nothing and
    not raise — so an unrelated hermes session stays plain hermes."""
    plugin = _load_plugin_module()
    monkeypatch.delenv("BELAYER_SESSION_ID", raising=False)
    monkeypatch.delenv("BELAYER_AGENT_ID", raising=False)
    monkeypatch.delenv("BELAYER_SOCKET", raising=False)

    ctx = _FakeCtx()
    plugin.register(ctx)

    assert ctx.tools == []
    assert ctx.hooks == []


def test_unknown_kind_defaults_to_main(monkeypatch):
    plugin = _load_plugin_module()
    _set_required_env(monkeypatch, kind="weird-kind")

    ctx = _FakeCtx()
    plugin.register(ctx)

    names = {t["name"] for t in ctx.tools}
    assert "belayer_broadcast" in names  # would be dropped if kind→side


def test_unknown_tool_in_allowlist_is_ignored(monkeypatch):
    """An unknown tool name in BELAYER_TOOLS (stale agent.yaml) should not
    crash — the plugin logs and skips it."""
    plugin = _load_plugin_module()
    _set_required_env(
        monkeypatch,
        kind="main",
        tools="belayer_spawn_agent,belayer_nonexistent_tool",
    )

    ctx = _FakeCtx()
    plugin.register(ctx)

    names = {t["name"] for t in ctx.tools}
    assert "belayer_spawn_agent" in names
    assert "belayer_nonexistent_tool" not in names


# --- handler contracts (one test per tool — exercises closure wiring) ---


def _make_plugin_with_env(monkeypatch, **env_overrides):
    plugin = _load_plugin_module()
    _set_required_env(monkeypatch, **env_overrides)
    return plugin


def _register_and_get(plugin, tool_name):
    """Register via the plugin and return the matching ctx entry."""
    ctx = _FakeCtx()
    plugin.register(ctx)
    matches = [t for t in ctx.tools if t["name"] == tool_name]
    assert matches, f"{tool_name} not registered"
    return matches[0]


def test_send_message_handler_posts_to_messages(monkeypatch):
    plugin = _make_plugin_with_env(monkeypatch, agent_id="supervisor", kind="main")
    seen: list[tuple] = []
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (seen.append(a), (201, ""))[1])

    tool = _register_and_get(plugin, "belayer_send_message")
    result = tool["handler"]({"to": "reviewer-1", "content": "look at diff"})
    assert result == "Message sent to reviewer-1."
    assert seen[0][1] == "/sessions/sess-1/messages"
    assert seen[0][2]["from"] == "supervisor"
    assert seen[0][2]["interrupt"] is False


def test_send_message_interrupt_sets_flag(monkeypatch):
    plugin = _make_plugin_with_env(monkeypatch, kind="main")
    seen: list[tuple] = []
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (seen.append(a), (201, ""))[1])

    tool = _register_and_get(plugin, "belayer_send_message")
    result = tool["handler"]({"to": "pm", "content": "urgent", "interrupt": True})
    assert result == "Interrupt sent to pm."
    assert seen[0][2]["interrupt"] is True


def test_send_message_handler_reports_missing_recipient(monkeypatch):
    plugin = _make_plugin_with_env(monkeypatch, kind="main")
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (410, "agent exited"))

    tool = _register_and_get(plugin, "belayer_send_message")
    result = tool["handler"]({"to": "ghost", "content": "hi"})
    assert result.startswith("[System] Agent 'ghost' is not available")


def test_broadcast_handler_posts_to_broadcast_route(monkeypatch):
    plugin = _make_plugin_with_env(monkeypatch, kind="main")
    seen: list[tuple] = []
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (seen.append(a), (201, ""))[1])

    tool = _register_and_get(plugin, "belayer_broadcast")
    result = tool["handler"]({"content": "heads up"})
    assert result == "Broadcast sent."
    assert seen[0][1] == "/sessions/sess-1/messages/broadcast"


def test_report_status_posts_status_event(monkeypatch):
    plugin = _make_plugin_with_env(monkeypatch, kind="main")
    seen: list[tuple] = []
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (seen.append(a), (201, ""))[1])

    tool = _register_and_get(plugin, "belayer_report_status")
    result = tool["handler"]({"status": "done", "detail": "feature shipped"})
    assert result == "Status reported: done."
    # Event type encodes the status value so daemon can branch cheaply.
    assert seen[0][2]["type"] == "agent_status:done"


def test_create_artifact_posts_to_artifacts(monkeypatch):
    plugin = _make_plugin_with_env(monkeypatch, kind="main")
    seen: list[tuple] = []
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (seen.append(a), (201, ""))[1])

    tool = _register_and_get(plugin, "belayer_create_artifact")
    result = tool["handler"]({"kind": "spec", "path": "spec.md", "summary": "checkout flow"})
    assert "spec at spec.md" in result
    assert seen[0][1] == "/sessions/sess-1/artifacts"
    assert seen[0][2]["producer"] == "supervisor"


def test_spawn_agent_forwards_repo_and_branch(monkeypatch):
    plugin = _make_plugin_with_env(
        monkeypatch,
        kind="main",
        tools="belayer_spawn_agent",
    )
    seen: list[tuple] = []
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (seen.append(a), (201, ""))[1])

    tool = _register_and_get(plugin, "belayer_spawn_agent")
    result = tool["handler"]({
        "name": "web-dev-1",
        "identity": "web-dev",
        "profile": "default",
        "message": "build the auth flow",
        "branch": "agent/auth-flow",
        "repo": "frontend",
    })
    assert "web-dev-1" in result
    assert "frontend" in result
    payload = seen[0][2]
    assert payload["repo"] == "frontend"
    assert payload["branch"] == "agent/auth-flow"
    assert payload["identity"] == "web-dev"


def test_request_completion_posts_event(monkeypatch):
    plugin = _make_plugin_with_env(
        monkeypatch,
        agent_id="supervisor",
        kind="main",
        tools="belayer_request_completion",
    )
    seen: list[tuple] = []
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (seen.append(a), (201, ""))[1])

    tool = _register_and_get(plugin, "belayer_request_completion")
    tool["handler"]({"summary": "api extended, tests green", "spec_artifact": "spec.md"})
    assert seen[0][1] == "/sessions/sess-1/events"
    assert seen[0][2]["type"] == "bridge:completion_requested"


def test_approve_completion_posts_event(monkeypatch):
    plugin = _make_plugin_with_env(
        monkeypatch,
        agent_id="pm",
        kind="side",
        tools="belayer_approve_completion",
    )
    seen: list[tuple] = []
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (seen.append(a), (201, ""))[1])

    tool = _register_and_get(plugin, "belayer_approve_completion")
    tool["handler"]({"verification_report": "all spec items verified"})
    assert seen[0][2]["type"] == "bridge:completion_approved"


def test_reject_completion_posts_event(monkeypatch):
    plugin = _make_plugin_with_env(
        monkeypatch,
        agent_id="pm",
        kind="side",
        tools="belayer_reject_completion",
    )
    seen: list[tuple] = []
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (seen.append(a), (201, ""))[1])

    tool = _register_and_get(plugin, "belayer_reject_completion")
    tool["handler"]({"verification_report": "report", "gap_list": "section 3.2 missing"})
    assert seen[0][2]["type"] == "bridge:completion_rejected"


def test_escalate_to_human_enforces_3_attempts(monkeypatch):
    plugin = _make_plugin_with_env(
        monkeypatch,
        kind="main",
        tools="belayer_escalate_to_human",
    )
    # No unix_post needed for the validation-fail path.
    tool = _register_and_get(plugin, "belayer_escalate_to_human")

    result = tool["handler"]({
        "reason": "blocked",
        "blocker": "egress policy",
        "what_tried": ["one thing"],  # only 1 entry — below the 3-attempt floor
    })
    assert "at least 3" in result


def test_escalate_to_human_raises_system_exit_on_success(monkeypatch):
    plugin = _make_plugin_with_env(
        monkeypatch,
        kind="main",
        tools="belayer_escalate_to_human",
    )
    monkeypatch.setattr(plugin.tools, "unix_post", lambda *a: (201, ""))

    import pytest
    tool = _register_and_get(plugin, "belayer_escalate_to_human")
    with pytest.raises(SystemExit) as excinfo:
        tool["handler"]({
            "reason": "infrastructure blocker",
            "blocker": "egress policy blocks every mirror",
            "what_tried": ["pip install x", "conda install x", "vendored wheel"],
        })
    assert excinfo.value.code == 0


# --- check_mail shared buffer contract ---------------------------------


def test_check_mail_appends_to_turn_mail_ids(monkeypatch):
    """check_mail writes consumed message IDs into the plugin's module-
    level _TURN_MAIL_IDS list — the same list the bridge's end-of-turn
    cleanup drains via pop_turn_mail_ids()."""
    plugin = _make_plugin_with_env(monkeypatch, kind="main")
    # Fake the GET to return two messages with IDs.
    def _fake_get(socket_path, path):
        import json
        body = json.dumps([
            {"ID": "m-1", "SenderID": "peer", "Content": "hello"},
            {"ID": "m-2", "SenderID": "peer", "Content": "world"},
        ])
        return 200, body

    monkeypatch.setattr(plugin.tools, "unix_get", _fake_get)

    tool = _register_and_get(plugin, "belayer_check_mail")
    result = tool["handler"]({})
    assert "hello" in result
    assert plugin._TURN_MAIL_IDS == ["m-1", "m-2"]

    drained = plugin.pop_turn_mail_ids()
    assert drained == ["m-1", "m-2"]
    assert plugin._TURN_MAIL_IDS == []
