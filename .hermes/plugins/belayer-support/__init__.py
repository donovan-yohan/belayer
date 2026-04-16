"""Belayer support plugin for project-local Nightshift runs."""

from __future__ import annotations

import logging
import os
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)


def _run_dir() -> Path | None:
    value = os.getenv("BELAYER_RUN_DIR", "").strip()
    return Path(value) if value else None


def _finish_marker() -> Path | None:
    rd = _run_dir()
    if not rd:
        return None
    return rd / ".belayer-finished"


def _extract_command(params: dict[str, Any] | None) -> str:
    if not isinstance(params, dict):
        return ""
    for key in ("command", "input", "text", "data"):
        value = params.get(key)
        if isinstance(value, str):
            return value
    return ""


def _post_tool_call(tool_name, params, result, task_id=None, **kwargs):
    if tool_name not in {"terminal", "mcp_terminal", "bash", "shell"}:
        return
    command = _extract_command(params)
    if "belayer finish" not in command:
        return
    marker = _finish_marker()
    if not marker:
        return
    marker.parent.mkdir(parents=True, exist_ok=True)
    marker.write_text("finished\n", encoding="utf-8")
    logger.info("Belayer finish marker written: %s", marker)


def register(ctx):
    plugin_dir = Path(__file__).parent
    ctx.register_skill(
        "belayer-communication",
        str(plugin_dir / "skills" / "belayer-communication" / "SKILL.md"),
    )
    ctx.register_hook("post_tool_call", _post_tool_call)
