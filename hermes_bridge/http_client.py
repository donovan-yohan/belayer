"""HTTP client for daemon communication.

Supports both Unix socket paths (e.g. /path/to/daemon.sock) and HTTP URL
addresses (e.g. http://172.17.0.1:7523). Unix sockets are used when belayer
runs with noop sandbox mode; HTTP URLs are used when bridges run inside
clamshell Docker containers and reach the daemon via Docker's host gateway.
"""

import json
import logging
import http.client
import socket as sock
from urllib.parse import urlparse

log = logging.getLogger("http_client")


def _is_http_url(socket_path: str) -> bool:
    return socket_path.startswith("http://") or socket_path.startswith("https://")


def _make_conn(socket_path: str) -> http.client.HTTPConnection:
    if _is_http_url(socket_path):
        parsed = urlparse(socket_path)
        return http.client.HTTPConnection(parsed.hostname, parsed.port or 80)
    conn = http.client.HTTPConnection("localhost")
    s = sock.socket(sock.AF_UNIX, sock.SOCK_STREAM)
    s.connect(socket_path)
    conn.sock = s
    return conn


def unix_post(socket_path: str, path: str, body: dict) -> tuple[int, str]:
    """POST JSON body to the daemon over its Unix socket or TCP address."""
    try:
        conn = _make_conn(socket_path)
        payload = json.dumps(body).encode()
        conn.request("POST", path, body=payload, headers={"Content-Type": "application/json"})
        resp = conn.getresponse()
        resp_body = resp.read().decode()
        conn.close()
        return resp.status, resp_body
    except Exception as e:
        log.debug("unix_post %s failed: %s", path, e)
        return 0, str(e)


def unix_get(socket_path: str, path: str) -> tuple[int, str]:
    """GET from the daemon over its Unix socket or TCP address."""
    try:
        conn = _make_conn(socket_path)
        conn.request("GET", path)
        resp = conn.getresponse()
        resp_body = resp.read().decode()
        conn.close()
        return resp.status, resp_body
    except Exception as e:
        log.debug("unix_get %s failed: %s", path, e)
        return 0, str(e)
