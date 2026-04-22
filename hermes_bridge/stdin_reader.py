"""Background stdin reader for daemon -> bridge commands.

The daemon writes newline-delimited JSON to the bridge subprocess's stdin
to deliver out-of-band control signals without a second socket connection.

Two command types:
    {"type": "interrupt", "from": "<agent_id>", "content": "<message>"}
    {"type": "stop"}

On "interrupt": the message is enqueued for the main loop and the agent's
interrupt() method is called so the current LLM turn terminates early.

On "stop": the agent is interrupted with a shutdown notice and the reader
exits, allowing the main loop to wind down cleanly.

Stdin EOF (pipe broken / daemon died) is treated as an implicit stop.
"""

import sys
import json
import logging
import threading

from hermes_bridge.callbacks import post_event

log = logging.getLogger("stdin_reader")


class StdinReader:
    def __init__(self, agent, interrupt_queue, socket_path: str, session_id: str, agent_id: str):
        """
        Args:
            agent: AIAgent instance. Must have an .interrupt(message) method.
            interrupt_queue: queue.Queue where interrupt payloads are deposited
                             so the main loop can inject them as user messages.
            socket_path: daemon event channel endpoint used for ack events.
            session_id: session identifier for the bridge event stream.
            agent_id: session-local agent name for the bridge event stream.
        """
        self.agent = agent
        self.interrupt_queue = interrupt_queue
        self.socket_path = socket_path
        self.session_id = session_id
        self.agent_id = agent_id
        self._thread: threading.Thread | None = None
        self._stop = threading.Event()

    def start(self) -> None:
        """Start the background reader thread."""
        self._thread = threading.Thread(
            target=self._read_loop,
            daemon=True,
            name="stdin-reader",
        )
        self._thread.start()

    def stop(self) -> None:
        """Signal the reader thread to exit (non-blocking)."""
        self._stop.set()

    # ------------------------------------------------------------------
    # Internal
    # ------------------------------------------------------------------

    def _read_loop(self) -> None:
        while not self._stop.is_set():
            try:
                line = sys.stdin.readline()
            except Exception as e:
                if not self._stop.is_set():
                    log.error("stdin read error: %s", e)
                break

            if not line:
                # EOF — daemon closed the pipe or died.
                log.info("stdin EOF — treating as implicit stop")
                self._handle_stop()
                break

            line = line.strip()
            if not line:
                continue

            try:
                cmd = json.loads(line)
            except json.JSONDecodeError as e:
                log.warning("Invalid JSON on stdin (ignored): %s | raw=%r", e, line[:120])
                continue

            cmd_type = cmd.get("type", "")

            if cmd_type == "stop":
                log.info("Received stop command")
                self._handle_stop()
                break
            elif cmd_type == "interrupt":
                sender = cmd.get("from", "unknown")
                log.info("Received interrupt from %s", sender)
                self.interrupt_queue.put(cmd)
                try:
                    self._ack_interrupt(cmd)
                except Exception as exc:
                    log.warning("message_ack emission failed: %s", exc)
                # Interrupt the current LLM turn; main loop will inject the message.
                try:
                    self.agent.interrupt(cmd.get("content", "Interrupt requested."))
                except Exception as exc:
                    log.warning("agent.interrupt() raised: %s", exc)
            else:
                log.warning("Unknown stdin command type %r (ignored)", cmd_type)

    def _handle_stop(self) -> None:
        """Interrupt the agent cleanly on stop/EOF."""
        try:
            self.agent.interrupt("Shutting down.")
        except Exception as exc:
            log.warning("agent.interrupt() raised during stop: %s", exc)
        self._stop.set()

    def _ack_interrupt(self, cmd: dict) -> None:
        """Emit a bridge:message_ack event once an interrupt has been accepted."""
        ack_ids: list[str] = []
        raw_ids = cmd.get("ids")
        if isinstance(raw_ids, list):
            ack_ids = [str(v).strip() for v in raw_ids if str(v).strip()]
        else:
            ack_id = str(cmd.get("id", "")).strip()
            if ack_id:
                ack_ids = [ack_id]
        if not ack_ids:
            return
        post_event(
            self.socket_path,
            self.session_id,
            self.agent_id,
            "bridge:message_ack",
            {"ids": ack_ids},
        )
