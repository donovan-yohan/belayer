"""Shared Unix socket HTTP client for daemon communication.

All daemon calls go through a Unix socket. Python's http.client supports
this by swapping in a pre-connected AF_UNIX socket. Each call creates a
fresh connection — volume is low enough that pooling isn't worth the
complexity.
"""

import json
import logging
import http.client
import socket as sock

log = logging.getLogger("http_client")


def unix_post(socket_path: str, path: str, body: dict) -> tuple[int, str]:
    """POST JSON body to the daemon over its Unix socket.

    Returns (status_code, response_body_text).
    Returns (0, error_message) on connection/IO failure.
    """
    try:
        conn = http.client.HTTPConnection("localhost")
        s = sock.socket(sock.AF_UNIX, sock.SOCK_STREAM)
        s.connect(socket_path)
        conn.sock = s

        payload = json.dumps(body).encode()
        conn.request(
            "POST",
            path,
            body=payload,
            headers={"Content-Type": "application/json"},
        )
        resp = conn.getresponse()
        resp_body = resp.read().decode()
        conn.close()
        return resp.status, resp_body
    except Exception as e:
        log.debug("unix_post %s failed: %s", path, e)
        return 0, str(e)


def unix_get(socket_path: str, path: str) -> tuple[int, str]:
    """GET from the daemon over its Unix socket.

    Returns (status_code, response_body_text).
    Returns (0, error_message) on failure.
    """
    try:
        conn = http.client.HTTPConnection("localhost")
        s = sock.socket(sock.AF_UNIX, sock.SOCK_STREAM)
        s.connect(socket_path)
        conn.sock = s

        conn.request("GET", path)
        resp = conn.getresponse()
        resp_body = resp.read().decode()
        conn.close()
        return resp.status, resp_body
    except Exception as e:
        log.debug("unix_get %s failed: %s", path, e)
        return 0, str(e)
