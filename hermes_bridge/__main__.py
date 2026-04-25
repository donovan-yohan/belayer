#!/usr/bin/env python3
"""Hermes bridge subprocess — one per agent.

Launched by the Belayer Go daemon via `python -m hermes_bridge`.
Reads config from env vars, constructs an AIAgent, registers Belayer
coordination tools, wires callbacks, then runs the outer conversation loop.

Exit codes:
    0 — completed or clean stop
    1 — fatal startup error (missing env, hermes not installed, etc.)
"""

import os
import sys
import json
import logging
import queue
import time

# Ensure stdout/stderr are line-buffered when running as a subprocess with piped
# stdio. CPython defaults to block-buffering (~4 KB) on non-TTY file descriptors;
# without this, log output and error strings die in the buffer on crash.
# reconfigure() is available on Python 3.7+; skip gracefully on older runtimes.
try:
    sys.stdout.reconfigure(line_buffering=True)
    sys.stderr.reconfigure(line_buffering=True)
except AttributeError:
    pass

# Hermes imports — conditional so package structure validates without hermes installed.
try:
    from run_agent import AIAgent  # type: ignore[import]
    from hermes_state import SessionDB  # type: ignore[import]
except ImportError:
    print(
        "ERROR: hermes-agent package not found. Install hermes-agent first.",
        file=sys.stderr,
    )
    sys.exit(1)

from hermes_bridge.tools import register_belayer_tools
from hermes_bridge.callbacks import make_callbacks, make_transcript_writer, post_event, start_heartbeat_thread, wire_callbacks
from hermes_bridge.stdin_reader import StdinReader
from hermes_bridge.http_client import unix_get

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [bridge:%(name)s] %(message)s",
)
log = logging.getLogger("main")


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _require_env(name: str) -> str:
    val = os.environ.get(name, "")
    if not val:
        log.error("Required env var %s is not set", name)
        sys.exit(1)
    return val


def fetch_pending_messages(socket_path: str, session_id: str, agent_id: str) -> list[dict]:
    """Pull pending messages addressed to this agent from the daemon."""
    status, body = unix_get(
        socket_path,
        f"/sessions/{session_id}/messages?for={agent_id}&pending=true",
    )
    if status == 200:
        try:
            data = json.loads(body)
            return data if isinstance(data, list) else []
        except json.JSONDecodeError:
            return []
    return []


def fetch_roster(socket_path: str, session_id: str) -> list[dict] | None:
    """Fetch the full agent roster for a session from the daemon.

    Returns None on any error (non-200, JSON decode failure, or a non-list
    payload) so callers can distinguish "roster genuinely empty" from
    "daemon unreachable / response garbled". The idle loop uses this to
    avoid advancing the terminal-only idle countdown during transient
    daemon outages.
    """
    status, body = unix_get(
        socket_path,
        f"/sessions/{session_id}/agents",
    )
    if status != 200:
        return None
    try:
        data = json.loads(body)
    except json.JSONDecodeError:
        return None
    if not isinstance(data, list):
        return None
    return data


def _msg_field(msg: dict, *keys: str) -> str:
    """Return the first non-empty string value found among the given keys.

    The daemon serialises store.Message without json struct tags, so field
    names arrive as PascalCase (Content, SenderID, ID).  Some call sites may
    also pass snake_case dicts (e.g. test fixtures or future tagged versions).
    Checking both casings here keeps the bridge tolerant of either shape.
    """
    for key in keys:
        val = msg.get(key)
        if val is not None and val != "":
            return str(val)
    return ""


def format_messages(messages: list[dict]) -> str:
    """Format a list of pending message dicts into a single user-turn string."""
    parts = []
    for msg in messages:
        sender = _msg_field(msg, "SenderID", "sender_id") or "unknown"
        content = _msg_field(msg, "Content", "content")
        parts.append(f"[Message from {sender}]: {content}")
    return "\n\n".join(parts)


def filter_and_format_messages(
    messages: list[dict],
    socket_path: str,
    session_id: str,
    agent_id: str,
) -> tuple[str, list[str]]:
    """Drop empty-content messages, warn on drops, format the rest, and collect ack IDs."""
    valid = []
    dropped = []
    ack_ids: list[str] = []
    for msg in messages:
        msg_id = _msg_field(msg, "ID", "id")
        content = _msg_field(msg, "Content", "content")
        if not content.strip():
            dropped.append({
                "sender": _msg_field(msg, "SenderID", "sender_id") or "unknown",
                "message_id": msg_id,
            })
            if msg_id:
                ack_ids.append(msg_id)
            continue
        valid.append(msg)
        if msg_id:
            ack_ids.append(msg_id)
    if dropped:
        post_event(
            socket_path, session_id, agent_id,
            "bridge:warning",
            {"kind": "empty_message_dropped", "count": len(dropped), "dropped": dropped},
        )
        log.warning("Dropped %d empty message(s): %s", len(dropped), dropped)
    return format_messages(valid), ack_ids


def post_message_ack(socket_path: str, session_id: str, agent_id: str, ids: list[str]) -> None:
    """Emit a bridge:message_ack event for a turn's consumed message IDs."""
    if not ids:
        return
    post_event(
        socket_path, session_id, agent_id,
        "bridge:message_ack",
        {"ids": list(ids)},
    )


_BUDGET_EXHAUSTED_MARKERS = (
    "Iteration budget exhausted",
    "⚠️ Iteration budget exhausted",
)


def _contains_budget_exhaustion(value: object) -> bool:
    if isinstance(value, str):
        return any(marker in value for marker in _BUDGET_EXHAUSTED_MARKERS)
    if isinstance(value, dict):
        return any(_contains_budget_exhaustion(v) for v in value.values())
    if isinstance(value, (list, tuple)):
        return any(_contains_budget_exhaustion(item) for item in value)
    return False


def result_budget_exhausted(result: dict, max_turns: int | None = None) -> bool:
    """Return True when Hermes exhausted its turn budget, even on the max_iterations path."""
    if result.get("budget_exhausted"):
        return True
    for key in ("_turn_exit_reason", "final_response", "last_message", "messages"):
        if _contains_budget_exhaustion(result.get(key)):
            return True
    if result.get("completed") or result.get("failed"):
        return False
    turns_used = result.get("turns_used")
    if max_turns is not None and turns_used == max_turns:
        log.warning(
            "Treating turns_used=%s at max_turns=%s as budget exhaustion because Hermes did not set budget_exhausted",
            turns_used,
            max_turns,
        )
        return True
    return False


def parse_optional_env_bool(name: str) -> bool | None:
    """Parse a tri-state boolean env var, returning None when unset/invalid."""
    raw = os.environ.get(name)
    if raw is None:
        return None
    value = raw.strip().lower()
    if value in ("1", "true", "yes", "on"):
        return True
    if value in ("0", "false", "no", "off"):
        return False
    log.warning("Ignoring invalid %s=%r", name, raw)
    return None


def fetch_and_format_pending_messages(
    socket_path: str,
    session_id: str,
    agent_id: str,
) -> tuple[str, list[str]]:
    """Fetch pending mail for the next turn and return formatted text plus ack IDs."""
    pending = fetch_pending_messages(socket_path, session_id, agent_id)
    if not pending:
        return "", []
    return filter_and_format_messages(pending, socket_path, session_id, agent_id)


def extract_turn_usage(result: dict) -> dict:
    """Extract token usage fields from a run_conversation() result dict."""
    fields = (
        "input_tokens", "output_tokens", "cache_read_tokens",
        "cache_write_tokens", "reasoning_tokens", "total_tokens",
        "api_calls", "estimated_cost_usd", "cost_status",
    )
    return {k: result[k] for k in fields if k in result}


def extract_session_usage(agent: object) -> dict:
    """Extract cumulative session usage from the AIAgent instance."""
    attrs = (
        "session_total_tokens", "session_input_tokens", "session_output_tokens",
        "session_cache_read_tokens", "session_cache_write_tokens",
        "session_reasoning_tokens", "session_api_calls",
        "session_estimated_cost_usd", "session_cost_status",
    )
    usage = {}
    for attr in attrs:
        val = getattr(agent, attr, None)
        if val is not None:
            usage[attr] = val
    return usage


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------


def main() -> None:
    # --- Config from environment -------------------------------------------
    session_id = _require_env("BELAYER_SESSION_ID")
    agent_id = _require_env("BELAYER_AGENT_ID")
    socket_path = _require_env("BELAYER_SOCKET")
    log.info("BELAYER_SOCKET=%s (is_http=%s)", socket_path, socket_path.startswith("http"))

    # Multiline env values are escaped by the Go-side writeEnvFile() as the
    # two-character sequence `\n` so docker --env-file stays parseable. Decode
    # any BELAYER_* value that can legitimately contain newlines here.
    def _decode_nl(val: str) -> str:
        return val.replace(r"\n", "\n")

    run_dir = os.environ.get("BELAYER_RUN_DIR", "")
    role = os.environ.get("BELAYER_ROLE", "specialist")
    agent_kind = os.environ.get("BELAYER_AGENT_KIND", "main").strip().lower() or "main"
    if agent_kind not in ("main", "side"):
        agent_kind = "main"
    is_supervisor = role.lower() == "supervisor"
    profile = os.environ.get("BELAYER_PROFILE", "")
    model = os.environ.get("BELAYER_MODEL", "")
    max_turns_env = os.environ.get("BELAYER_MAX_TURNS", "")
    max_turns = None
    if max_turns_env:
        try:
            parsed = int(max_turns_env)
            if parsed > 0:
                max_turns = parsed
            else:
                log.warning("Ignoring non-positive BELAYER_MAX_TURNS=%r", max_turns_env)
        except ValueError:
            log.warning("Ignoring invalid BELAYER_MAX_TURNS=%r", max_turns_env)
    initial_message = _decode_nl(os.environ.get("BELAYER_MESSAGE", ""))
    system_prompt = _decode_nl(os.environ.get("BELAYER_SYSTEM_PROMPT", ""))
    hermes_session_id = os.environ.get("BELAYER_HERMES_SESSION_ID", "")
    ephemeral_override = parse_optional_env_bool("BELAYER_EPHEMERAL")
    if ephemeral_override is None:
        ephemeral = agent_kind != "main"
    else:
        ephemeral = ephemeral_override

    log.info(
        "Starting bridge agent=%s session=%s role=%s kind=%s supervisor=%s profile=%s ephemeral=%s",
        agent_id, session_id, role, agent_kind, is_supervisor, profile or "(none)", ephemeral,
    )

    # --- Construct AIAgent -------------------------------------------------
    # Hermes profiles work by setting HERMES_HOME to ~/.hermes/profiles/{name}.
    # AIAgent doesn't accept a "profile" kwarg — it reads HERMES_HOME at construction.
    if profile and profile != "default":
        profile_home = os.path.expanduser(f"~/.hermes/profiles/{profile}")
        if os.path.isdir(profile_home):
            os.environ["HERMES_HOME"] = profile_home
            log.info("Set HERMES_HOME=%s for profile %s", profile_home, profile)
        else:
            log.warning("Profile dir %s not found, using default", profile_home)

    # --- Resolve passthroughs and construct AIAgent --------------------------
    # Hermes 0.11+ loads credentials, base_url, provider, and api_mode from
    # the active profile (HERMES_HOME) via the transport layer. Auth refresh
    # (OAuth, token rotation) is handled by the transport, not the bridge.
    # We only override via BELAYER_* env vars when the daemon explicitly
    # injects them for sandboxed/container deployments that lack a profile.

    # Parse enabled_toolsets from env (comma-separated list of toolset names).
    # Missing env var or __all__ sentinel means "not configured" → no restriction.
    # Empty string means explicitly empty list → zero toolsets.
    # Non-empty list restricts to those toolsets.
    enabled_toolsets_env = os.environ.get("BELAYER_ENABLED_TOOLSETS")
    enabled_toolsets = None
    if enabled_toolsets_env is None or enabled_toolsets_env == "__all__":
        pass  # not configured; no restriction
    elif enabled_toolsets_env == "":
        # explicit empty list (e.g. enabled_toolsets: [])
        enabled_toolsets = []
    else:
        enabled_toolsets = [t.strip() for t in enabled_toolsets_env.split(",") if t.strip()]

    # --- Shared SessionDB for persistence and resume -----------------------
    from pathlib import Path
    session_db = SessionDB(db_path=Path.home() / ".hermes" / "state.db")

    agent_kwargs: dict = {
        "quiet_mode": True,
        "persist_session": True,
        "session_db": session_db,
    }
    if system_prompt:
        agent_kwargs["ephemeral_system_prompt"] = system_prompt
    if model:
        agent_kwargs["model"] = model
    if max_turns is not None:
        # Hermes constrains turn budget via max_iterations on AIAgent.
        agent_kwargs["max_iterations"] = max_turns
    if hermes_session_id:
        agent_kwargs["session_id"] = hermes_session_id
    if enabled_toolsets is not None:
        agent_kwargs["enabled_toolsets"] = enabled_toolsets
        log.info("Restricting agent to toolsets: %s", enabled_toolsets)

    # Sandbox passthrough: daemon injects these when the container user has
    # no Hermes profile. Only pass through when explicitly set.
    _PROVIDER_OVERRIDES = (
        ("api_key", "BELAYER_API_KEY"),
        ("base_url", "BELAYER_BASE_URL"),
        ("provider", "BELAYER_PROVIDER"),
    )
    for key, env_name in _PROVIDER_OVERRIDES:
        val = os.environ.get(env_name, "")
        if val:
            agent_kwargs[key] = val
            log.info("Using %s for LLM %s", env_name, key)

    try:
        agent = AIAgent(**agent_kwargs)
    except Exception as exc:
        log.error("Failed to construct AIAgent: %s", exc)
        post_event(socket_path, session_id, agent_id, "bridge:failed", {"error": str(exc)})
        sys.exit(1)

    # TODO(upstream-hermes): Remove once transports handle HTTPS_PROXY.
    # Hermes 0.11's transport ABC should eventually handle proxy config per-
    # transport. Until then, monkey-patch _create_openai_client to inject a
    # proxy-aware httpx.Client for OpenAI-format transports.
    _proxy_url = os.environ.get("HTTPS_PROXY", "") or os.environ.get("https_proxy", "")
    if _proxy_url and hasattr(agent, "_create_openai_client"):
        try:
            import httpx as _httpx
            import socket as _socket
            _sock_opts = [(_socket.SOL_SOCKET, _socket.SO_KEEPALIVE, 1)]
            if hasattr(_socket, "TCP_KEEPIDLE"):
                _sock_opts.extend([
                    (_socket.IPPROTO_TCP, _socket.TCP_KEEPIDLE, 30),
                    (_socket.IPPROTO_TCP, _socket.TCP_KEEPINTVL, 10),
                    (_socket.IPPROTO_TCP, _socket.TCP_KEEPCNT, 3),
                ])
            _proxy_url_cap = _proxy_url
            _sock_opts_cap = _sock_opts
            _orig_create = agent._create_openai_client  # bound method

            def _proxy_create(client_kwargs, *, reason, shared):
                fresh = dict(client_kwargs)
                fresh.pop("http_client", None)
                fresh["http_client"] = _httpx.Client(
                    proxy=_httpx.Proxy(_proxy_url_cap),
                    transport=_httpx.HTTPTransport(socket_options=_sock_opts_cap),
                )
                return _orig_create(fresh, reason=reason, shared=shared)

            agent._create_openai_client = _proxy_create
            # Rebuild now so the initial client also gets the proxy. Close the
            # pre-patch client first so its httpx transport pool is released —
            # otherwise the original client's sockets hang around until GC.
            old_client = getattr(agent, "client", None)
            agent.client = agent._create_openai_client(
                agent._client_kwargs, reason="proxy_inject", shared=True
            )
            if old_client is not None:
                try:
                    old_client.close()
                except Exception:  # noqa: BLE001 - best-effort cleanup
                    pass
            log.info("Patched _create_openai_client for proxy+keepalive (proxy=%s)", _proxy_url)
        except Exception as _e:
            log.warning("Could not patch proxy client into AIAgent: %s", _e)

    # TODO(upstream-hermes): `max_retries = 3` is a local var inside
    # AIAgent.send_message (run_agent.py ~L8752) with ~2+4+8s backoff, so a
    # single connection-level failure burns ~50s before the bridge surfaces
    # it. For local E2E UX this dominates Step 4's 60s budget. File an
    # upstream PR to expose max_retries via env var (HERMES_MAX_RETRIES) or
    # as an AIAgent ctor kwarg; until then, Step 4 polls for `stalled` to
    # short-circuit this loop on the daemon side (see belayer status output
    # after bridge exits without completion).

    # --- Register Belayer tools --------------------------------------------
    allowed_tools_env = os.environ.get("BELAYER_TOOLS", "")
    allowed_tools = [t.strip() for t in allowed_tools_env.split(",") if t.strip()] if allowed_tools_env else None
    turn_mail_ids: list[str] = []
    try:
        register_belayer_tools(
            agent,
            agent_id,
            session_id,
            socket_path,
            allowed_tools=allowed_tools,
            agent_kind=agent_kind,
            turn_mail_ids=turn_mail_ids,
        )
    except Exception as exc:
        log.error("Failed to register Belayer tools: %s", exc)
        post_event(socket_path, session_id, agent_id, "bridge:failed", {"error": str(exc)})
        sys.exit(1)

    # --- Open transcript writer (verbose sessions only) --------------------
    # Must be created before make_callbacks so the writer can be passed into
    # the closure — reasoning_callback and interim_assistant_callback gate
    # on transcript_writer being non-None.
    transcript_path = os.environ.get("BELAYER_TRANSCRIPT_PATH") or None
    log_level = os.environ.get("BELAYER_LOG_LEVEL", "standard")
    transcript_writer = make_transcript_writer(
        transcript_path, agent_id,
        log_level=log_level,
        socket_path=socket_path,
        session_id=session_id,
    )

    # --- Wire callbacks ----------------------------------------------------
    callbacks = make_callbacks(
        agent_id, session_id, socket_path,
        transcript_writer=transcript_writer,
        log_level=log_level,
    )
    wired = wire_callbacks(agent, callbacks)
    missing = [k for k, v in wired.items() if v == "missing"]
    if missing:
        log.warning("Missing callbacks on AIAgent: %s", ", ".join(missing))

    # --- Start stdin reader ------------------------------------------------
    stdin_queue: queue.Queue = queue.Queue()
    stdin_reader = StdinReader(agent, stdin_queue, socket_path, session_id, agent_id)
    stdin_reader.start()

    # --- Start heartbeat thread -------------------------------------------
    heartbeat_stop = start_heartbeat_thread(socket_path, session_id, agent_id)

    # --- Resume from prior Hermes session if provided ----------------------
    conversation_history: list[dict] | None = None
    if hermes_session_id:
        try:
            history = session_db.get_messages_as_conversation(hermes_session_id)
            conversation_history = [m for m in history if m.get("role") != "session_meta"]
            if conversation_history:
                log.info(
                    "Resumed hermes session %s with %d messages",
                    hermes_session_id,
                    len(conversation_history),
                )
            else:
                log.warning("Hermes session %s found but has no messages", hermes_session_id)
                conversation_history = None
        except Exception as exc:
            log.warning("Could not resume hermes session %s: %s", hermes_session_id, exc)

    # --- Signal readiness (include hermes_session_id for daemon to store) --
    post_event(
        socket_path, session_id, agent_id,
        "bridge:started",
        {
            "role": role,
            "kind": agent_kind,
            "profile": profile,
            "hermes_session_id": agent.session_id,
        },
    )

    # --- Outer conversation loop -------------------------------------------
    if initial_message:
        base_user_message = initial_message
    elif agent_kind == "main":
        if is_supervisor:
            base_user_message = "You are the supervisor main agent. Begin your work."
        else:
            base_user_message = f"You are the {role} main agent. Begin your work."
    else:
        base_user_message = f"You are the {role} side agent. Begin your work."

    terminal_event_emitted = False

    def _post_terminal_event(event_type: str, data: dict) -> None:
        nonlocal terminal_event_emitted
        if terminal_event_emitted:
            log.debug("Skipping duplicate terminal event %s", event_type)
            return
        terminal_event_emitted = True
        post_event(socket_path, session_id, agent_id, event_type, data)

    def _assemble_turn_user_message(base_message: str) -> str:
        if agent_kind != "main":
            return base_message
        formatted, mail_ids = fetch_and_format_pending_messages(socket_path, session_id, agent_id)
        if mail_ids:
            turn_mail_ids.extend(mail_ids)
        if not formatted:
            return base_message
        valid_count = formatted.count("\n\n") + 1
        log.info("Prepending %d pending mail message(s) to this turn", valid_count)
        if base_message:
            return f"{base_message}\n\n{formatted}"
        return formatted

    user_message = base_user_message

    while True:
        turn_user_message = _assemble_turn_user_message(user_message)
        try:
            result = agent.run_conversation(
                user_message=turn_user_message,
                conversation_history=conversation_history,
            )
        except Exception as exc:
            log.error("run_conversation crashed: %s", exc)
            _post_terminal_event("bridge:failed", {"error": str(exc)})
            break

        # Carry history forward for subsequent turns.
        conversation_history = result.get("messages", [])

        # Report per-turn token usage.
        turn_usage = extract_turn_usage(result)
        if turn_usage:
            post_event(socket_path, session_id, agent_id, "bridge:turn_usage", turn_usage)

        if turn_mail_ids:
            post_message_ack(socket_path, session_id, agent_id, turn_mail_ids)
            turn_mail_ids.clear()

        if result_budget_exhausted(result, max_turns=max_turns):
            turns_used = result.get("turns_used")
            final_response = str(result.get("final_response", ""))[:500]
            last_message = str(result.get("last_message", result.get("final_response", "")))[:500]
            post_event(
                socket_path, session_id, agent_id,
                "bridge:budget_exhausted",
                {
                    "turns_used": turns_used,
                    "max_turns": max_turns,
                    "last_message": last_message,
                },
            )
            _post_terminal_event(
                "bridge:finished",
                {
                    "reason": "budget_exhausted",
                    "final_response": final_response,
                    "last_message": last_message,
                },
            )
            break

        # --- Interrupted turn: check for urgent message from stdin ---------
        if result.get("interrupted"):
            try:
                interrupt_msg = stdin_queue.get_nowait()
                sender = interrupt_msg.get("from", "system")
                content = interrupt_msg.get("content", "") or ""
                if not content.strip():
                    post_event(
                        socket_path, session_id, agent_id,
                        "bridge:warning",
                        {"kind": "empty_message_dropped", "count": 1,
                         "dropped": [{"sender": sender, "message_id": ""}]},
                    )
                    log.warning("Dropped empty interrupt message from %s", sender)
                    # Treat as no interrupt — fall through to terminal state checks.
                else:
                    user_message = f"[Urgent from {sender}]: {content}"
                    log.info("Continuing after interrupt from %s", sender)
                    continue
            except queue.Empty:
                # Interrupted but no pending stdin command — treat as stop.
                log.info("Interrupted with no queued message; treating as stop")
                _post_terminal_event(
                    "bridge:finished",
                    {"reason": "interrupted"},
                )
                break

        # --- Terminal states -----------------------------------------------
        if result.get("failed"):
            _post_terminal_event(
                "bridge:failed",
                {"final_response": str(result.get("final_response", ""))[:500]},
            )
            break

        if result.get("completed"):
            if ephemeral:
                _post_terminal_event(
                    "bridge:finished",
                    {"final_response": str(result.get("final_response", ""))[:500]},
                )
                break

            # Non-ephemeral agent: stay alive and wait for more work.
            post_event(
                socket_path, session_id, agent_id,
                "bridge:idle",
                {"final_response": str(result.get("final_response", ""))[:500]},
            )
            log.info("Non-ephemeral agent idle, polling for messages...")
            idle_poll_interval = 5  # seconds
            # idle_timeout fires when the idle counter reaches it — but the
            # counter only advances while every peer is in a terminal state.
            # This is the "everyone's done and nobody pinged me back" ceiling.
            idle_timeout = 900  # 15 min
            # absolute_timeout is the failsafe for the case where a peer is
            # marked running in the roster but is actually hung, crashed
            # without the daemon noticing, or — worst case — idle-waiting for
            # a message from *this* supervisor (deadlock). It advances every
            # tick regardless of peer state and only resets when the idle
            # loop breaks out on a real message/interrupt. If this trips
            # while peers are still "running", suspect a hang and escalate.
            absolute_timeout = 3600  # 1 hr
            waited = 0
            absolute_waited = 0
            was_waiting_on_peers = False
            while waited < idle_timeout and absolute_waited < absolute_timeout:
                time.sleep(idle_poll_interval)
                absolute_waited += idle_poll_interval

                # Check stdin for interrupt/stop commands first.
                try:
                    interrupt_msg = stdin_queue.get_nowait()
                    if interrupt_msg.get("type") == "stop":
                        log.info("Received stop command while idle")
                        _post_terminal_event(
                            "bridge:finished",
                            {"reason": "stopped"},
                        )
                        break
                    sender = interrupt_msg.get("from", "system")
                    content = interrupt_msg.get("content", "") or ""
                    if not content.strip():
                        post_event(
                            socket_path, session_id, agent_id,
                            "bridge:warning",
                            {"kind": "empty_message_dropped", "count": 1,
                             "dropped": [{"sender": sender, "message_id": ""}]},
                        )
                        log.warning("Dropped empty idle-interrupt message from %s", sender)
                        continue
                    user_message = (
                        "[System] You have been idle. An urgent message arrived. "
                        "Process it and continue coordinating.\n\n"
                        f"[Message from {sender}]: {content}"
                    )
                    log.info("Resuming from idle with message from %s", sender)
                    break
                except queue.Empty:
                    pass

                # Poll for pending messages.
                formatted, mail_ids = fetch_and_format_pending_messages(socket_path, session_id, agent_id)
                if mail_ids:
                    turn_mail_ids.extend(mail_ids)
                if formatted:
                    valid_count = formatted.count("\n\n") + 1
                    user_message = (
                        "[System] You have been idle waiting for specialist agents. "
                        "One or more have reported in. Process their updates and continue "
                        "coordinating the session (verify work, merge branches, spawn next agents, create PRs, etc.).\n\n"
                        + formatted
                    )
                    log.info("Resuming from idle with %d valid pending message(s)", valid_count)
                    break
                if mail_ids:
                    log.info("Idle poll: all %d pending message(s) had empty content; continuing to wait", len(mail_ids))

                # Peer-awareness: only advance the idle counter when all peer
                # agents are in a terminal state. While any peer is still
                # running or starting, reset the counter so we don't
                # prematurely kill the supervisor.
                #
                # Identity note: BELAYER_AGENT_ID is the agent *name*
                # (e.g. "supervisor"), not a UUID — the daemon sets
                # AgentID = req.Name when spawning the bridge (see
                # internal/daemon/agents.go). The roster JSON exposes both
                # `ID` (UUID) and `Name`; compare against Name.
                roster = fetch_roster(socket_path, session_id)
                if roster is None:
                    # Daemon unreachable or malformed response. We can't
                    # tell whether peers are terminal, so stay conservative:
                    # don't advance the idle counter, but let the absolute
                    # ceiling continue to tick so we don't wait forever on
                    # a truly dead daemon.
                    log.debug("Roster fetch failed; holding idle counter steady")
                    active_peers = []  # unknown, treated as none-known for logging
                    waited = 0
                else:
                    peers = [r for r in roster if r.get("Name") != agent_id]
                    # Active = anything the daemon considers not-yet-terminal.
                    # The daemon emits "starting" during sandbox boot / hermes
                    # warm-up, then flips to "running" once the bridge reports
                    # in, and "pending_verification" while the PM adjudicates;
                    # all three count as active so we don't idle-timeout a
                    # supervisor while a peer is booting, executing, or awaiting
                    # verification.
                    active_peers = [
                        r for r in peers
                        if r.get("Status") in ("starting", "running", "pending_verification")
                    ]
                    peers_still_active = len(active_peers) > 0

                    if peers_still_active:
                        if not was_waiting_on_peers:
                            was_waiting_on_peers = True
                        waited = 0  # reset; don't count time while specialists are running
                    else:
                        if was_waiting_on_peers:
                            log.info(
                                "All %d peer agent(s) now terminal; idle countdown started",
                                len(peers),
                            )
                            was_waiting_on_peers = False
                        waited += idle_poll_interval
            else:
                # Idle loop exited without a message/interrupt — one of the
                # two ceilings tripped. Distinguish which so operators can
                # tell a clean "everyone's done" stall from a suspected hang.
                # Either way this is not a successful completion; mark as
                # incomplete so the session transitions to needs_human_review.
                if waited >= idle_timeout:
                    detail = (
                        f"Idle timeout after {idle_timeout}s; all peers terminal "
                        "and no messages received"
                    )
                    reason = "idle_timeout"
                else:
                    active_count = len(active_peers)
                    detail = (
                        f"Absolute idle ceiling ({absolute_timeout}s) reached with "
                        f"{active_count} peer(s) still marked running. Suspected "
                        "hang or deadlock — no messages received despite peers "
                        "reporting active."
                    )
                    reason = "absolute_idle_ceiling"
                log.info("Idle loop exiting: %s", detail)
                post_event(
                    socket_path, session_id, agent_id,
                    "agent_status:incomplete",
                    {"status": "incomplete", "detail": detail},
                )
                _post_terminal_event(
                    "bridge:finished",
                    {"reason": reason},
                )
                break
            continue

        # --- Unknown/partial return: nudge agent to continue ---------------
        log.debug("run_conversation returned without terminal state; nudging agent")
        user_message = "[System] Your previous turn ended without completion. Continue your work."

    # Report cumulative session usage before exiting.
    session_usage = extract_session_usage(agent)
    if session_usage:
        post_event(socket_path, session_id, agent_id, "bridge:session_usage", session_usage)

    heartbeat_stop.set()
    if transcript_writer:
        transcript_writer.close()
    stdin_reader.stop()
    log.info("Bridge exiting for agent=%s session=%s", agent_id, session_id)


if __name__ == "__main__":
    main()
