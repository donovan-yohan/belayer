"""HTTP client for daemon communication.

Supports Unix socket paths, direct HTTP URLs, and HTTP-CONNECT-proxied URLs.

- Unix socket (e.g. /path/to/daemon.sock): used in noop sandbox mode.
- Direct HTTP URL (e.g. http://172.17.0.1:7523): used when the host gateway
  is directly reachable from the container.
- CONNECT-proxied HTTP URL: used inside clamshell sandboxes where the host
  gateway is not directly routable. Set BELAYER_HTTP_PROXY=http://host:port
  to route daemon calls through an HTTP CONNECT proxy (clamshell transparent
  proxy at 172.31.0.2:3128).
"""

import json
import logging
import os
import http.client
import socket as sock
from urllib.parse import urlparse

log = logging.getLogger("http_client")


def _is_http_url(socket_path: str) -> bool:
    """Return True for plain HTTP URLs. HTTPS is intentionally not supported;
    the daemon is only reached over loopback, a bind-mounted Unix socket, or
    an HTTP-CONNECT proxy, so TLS termination is never needed."""
    return socket_path.startswith("http://")


def _make_conn(socket_path: str) -> http.client.HTTPConnection:
    if _is_http_url(socket_path):
        parsed = urlparse(socket_path)
        proxy_url = os.environ.get("BELAYER_HTTP_PROXY", "")
        if proxy_url:
            proxy = urlparse(proxy_url)
            conn = http.client.HTTPConnection(proxy.hostname, proxy.port or 3128)
            conn.set_tunnel(parsed.hostname, parsed.port or 80)
            return conn
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
