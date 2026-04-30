"""Belayer Hermes plugin — registers the full belayer_* tool surface.

At Hermes plugin discovery time, register() reads BELAYER_* env vars set
by the Go daemon and registers only the tools permitted by (a) agent kind
(main gets mailbox surfaces; side does not) and (b) the per-agent allowlist
from .belayer/agents/<identity>/agent.yaml (passed through as the
BELAYER_TOOLS comma-separated env var).

The schemas and handler factories live in plugins.belayer.tools so they
can be exercised by unit tests without pulling the Hermes plugin context
machinery into the import graph.

Per-turn state (check_mail's accumulated ack ids) lives at module level so
the bridge subprocess can drain it between turns — see pop_turn_mail_ids
below.
"""

from __future__ import annotations

import logging
import os
from typing import List

from .tools import (
    BASELINE_TOOLS,
    KIND_MAIN,
    KIND_SIDE,
    MAIL_TOOLS,
    TOOL_SPECS,
)

logger = logging.getLogger(__name__)

# Mutable per-turn buffer for check_mail. The plugin handler appends
# consumed message IDs here; the bridge's end-of-turn cleanup imports this
# module by name and calls pop_turn_mail_ids() to drain and ack them.
_TURN_MAIL_IDS: List[str] = []


def pop_turn_mail_ids() -> List[str]:
    """Return (and clear) the list of message IDs consumed by check_mail
    since the last pop. Called by the bridge between turns so it can ack
    them to the daemon in a single batch POST.
    """
    ids = list(_TURN_MAIL_IDS)
    _TURN_MAIL_IDS.clear()
    return ids


def _parse_allowed_tools(raw: str) -> list[str]:
    """Parse BELAYER_TOOLS env var (comma-separated tool names) into a list.

    The daemon sets this from the agent's agent.yaml belayer_tools allowlist.
    Empty string means "no role-specific tools beyond the kind baseline".
    """
    if not raw:
        return []
    return [t.strip() for t in raw.split(",") if t.strip()]


def _register_tool(ctx, tool_name: str, agent_id: str, session_id: str, socket_path: str) -> None:
    schema, factory = TOOL_SPECS[tool_name]
    if tool_name == "belayer_check_mail":
        handler = factory(agent_id, session_id, socket_path, _TURN_MAIL_IDS)
    else:
        handler = factory(agent_id, session_id, socket_path)
    ctx.register_tool(
        name=schema["name"],
        toolset="belayer",
        schema=schema,
        handler=handler,
        description=schema["description"],
    )


def register(ctx) -> None:
    """Plugin entry point — called by Hermes PluginManager at discovery.

    Reads the same env vars the bridge sets at subprocess exec time and
    mirrors the legacy register_belayer_tools() gating logic:

      - main agents get BASELINE_TOOLS + MAIL_TOOLS + allowlist
      - side agents get BASELINE_TOOLS + allowlist (allowlist items in
        MAIL_TOOLS are filtered out — side agents have no mailbox)

    If BELAYER_SESSION_ID / BELAYER_AGENT_ID / BELAYER_SOCKET are unset,
    the plugin returns without registering anything. This makes it safe to
    leave enabled in hermes globally: an interactive `hermes` session with
    no Belayer env is a plain Hermes session.
    """
    session_id = os.environ.get("BELAYER_SESSION_ID", "")
    agent_id = os.environ.get("BELAYER_AGENT_ID", "")
    socket_path = os.environ.get("BELAYER_SOCKET", "")
    kind = (os.environ.get("BELAYER_AGENT_KIND", KIND_MAIN) or KIND_MAIN).strip().lower()
    if kind not in (KIND_MAIN, KIND_SIDE):
        kind = KIND_MAIN
    allowed = _parse_allowed_tools(os.environ.get("BELAYER_TOOLS", ""))

    if not (session_id and agent_id and socket_path):
        logger.debug(
            "belayer plugin: no BELAYER_* env vars; skipping registration (session=%r agent=%r)",
            session_id, agent_id,
        )
        return

    # Compose the enabled tool set: baseline + (mailbox if main) + allowlist.
    enabled: list[str] = list(BASELINE_TOOLS)
    if kind == KIND_MAIN:
        enabled.extend(MAIL_TOOLS)
    for tool in allowed:
        if tool in enabled:
            continue
        if tool not in TOOL_SPECS:
            logger.warning("belayer plugin: unknown tool %r in allowlist; skipping", tool)
            continue
        # Mail-surface tools are main-only regardless of allowlist. A side
        # agent with 'belayer_broadcast' in its agent.yaml is a misconfig
        # we silently correct rather than panic on.
        if tool in MAIL_TOOLS and kind != KIND_MAIN:
            logger.debug("belayer plugin: dropping mail-surface tool %r for side agent", tool)
            continue
        enabled.append(tool)

    for tool in enabled:
        try:
            _register_tool(ctx, tool, agent_id, session_id, socket_path)
        except Exception as exc:
            logger.warning("belayer plugin: failed to register %s: %s", tool, exc)

    logger.info(
        "belayer plugin: registered %d tools for agent=%s session=%s kind=%s tools=%s",
        len(enabled), agent_id, session_id, kind, enabled,
    )
