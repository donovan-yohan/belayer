> **Status:** Design spec — `belayer dashboard` has shipped. This document describes the intended architecture; actual behavior may diverge. Prefer `belayer dashboard --help` for current flags.

# Belayer Dashboard — Multi-Daemon Unified Web UI

## Goal
A single `belayer dashboard` command serves one Web UI that aggregates sessions, events, messages, and agents from multiple Belayer daemons running on the same machine.

## Architecture

```
┌─────────────┐     ┌─────────────────────────────┐     ┌─────────────┐
│   Browser   │────▶│  belayer dashboard :7525   │────▶│  Daemon A   │
│  (UI /ui/)  │     │  - static files             │     │  :7523      │
└─────────────┘     │  - /api/daemons list        │────▶│  Daemon B   │
                    │  - /api/daemons/{n}/...     │     │  :7524      │
                    │    reverse proxy            │     └─────────────┘
                    └─────────────────────────────┘
```

The dashboard is a thin reverse-proxy + static-file server. It does NOT hold state. All session data lives in the individual daemons.

## Backend — New Package `internal/dashboard/`

### Types

```go
package dashboard

type DaemonConfig struct {
    Name  string `yaml:"name"`
    URL   string `yaml:"url"`   // e.g. http://localhost:7523
    Token string `yaml:"token"` // bearer token for that daemon
}

type Server struct {
    daemons []DaemonConfig
    // reverse proxies cached per daemon
    proxies map[string]*httputil.ReverseProxy
}
```

### Routes

| Route | Handler | Description |
|-------|---------|-------------|
| `GET /ui/{$}` | `handleWebUI` | Serve `index.html` from embedded `belayer.WebUI` |
| `GET /ui/{path...}` | `handleWebUI` | Serve CSS/JS/static assets |
| `GET /api/daemons` | `handleListDaemons` | JSON array of `{name, url, healthy}` |
| `GET /api/daemons/{name}/health` | `handleProxy` | Proxy to daemon `/health` |
| `GET /api/daemons/{name}/sessions` | `handleProxy` | Proxy to daemon `/sessions` |
| `GET /api/daemons/{name}/sessions/{id}` | `handleProxy` | Proxy to daemon `/sessions/{id}` |
| `GET /api/daemons/{name}/sessions/{id}/events` | `handleProxy` | Proxy to daemon `/sessions/{id}/events` |
| `GET /api/daemons/{name}/events/stream` | `handleProxySSE` | Proxy SSE stream (no buffering) |
| `GET /api/daemons/{name}/sessions/{id}/tool-calls` | `handleProxy` | Proxy to daemon `/sessions/{id}/tool-calls` |
| `GET /api/daemons/{name}/sessions/{id}/outline` | `handleProxy` | Proxy to daemon `/sessions/{id}/outline` |
| `GET /api/daemons/{name}/sessions/{id}/artifacts` | `handleProxy` | Proxy to daemon `/sessions/{id}/artifacts` |
| `GET /api/daemons/{name}/sessions/{id}/transcripts` | `handleProxy` | Proxy to daemon `/sessions/{id}/transcripts` |
| `GET /api/daemons/{name}/sessions/{id}/transcripts/{agent}` | `handleProxy` | Proxy to daemon `/sessions/{id}/transcripts/{agent}` |
| `GET /api/daemons/{name}/sessions/{id}/conversation` | `handleProxy` | Proxy to daemon `/sessions/{id}/conversation` |
| `GET /api/daemons/{name}/sessions/{id}/phase` | `handleProxy` | Proxy to daemon `/sessions/{id}/phase` |

### Proxy behavior

- For each daemon, create an `httputil.ReverseProxy` with a custom `Director` that:
  1. Rewrites the request URL from `/api/daemons/{name}/...` to `/{...}`
  2. Sets `Authorization: Bearer <token>` header
  3. Sets `X-Forwarded-For` (optional but nice)
- For SSE (`/events/stream`), ensure `FlushInterval` is set on the proxy transport so events stream immediately
- The proxy should NOT buffer the response body

### Static file serving

Reuse the exact same pattern as `internal/daemon/webui.go`:
```go
func handleWebUI(w http.ResponseWriter, r *http.Request) {
    path := strings.TrimPrefix(r.URL.Path, "/ui")
    if path == "" || path == "/" { path = "/index.html" }
    content, err := belayer.WebUI.ReadFile("web" + path)
    // ... set Content-Type, write
}
```

### Health check

`GET /api/daemons` should do a lightweight `GET /health` on each daemon (with 2s timeout) and report `healthy: true/false`.

## CLI — `internal/cli/dashboard.go`

New command:
```bash
belayer dashboard --port 7525 \
  --daemon "extend-api=http://localhost:7523:tokenA" \
  --daemon "relay-ide=http://localhost:7524:tokenB"
```

Flags:
- `--port` / `-p` (default `7525`)
- `--daemon` (repeatable) — format `name=url:token`
- `--config` — path to YAML config file with the same daemon list

If `--config` is provided, parse it. Otherwise parse `--daemon` flags. At least one daemon must be configured.

## Frontend — `web/app.js` Multi-Daemon Mode

### Detection
On startup, attempt `GET /api/daemons`:
- **200 with array** → dashboard/multi-daemon mode
- **404** → single-daemon mode (backward compatible)

### Multi-daemon state additions
```js
let daemons = [];        // [{name, url, healthy}]
let activeDaemon = null; // name of daemon owning active session
```

### UI changes

**Sidebar — Sessions section**
Replace the flat session list with grouped list:
```
Sessions
├── extend-api ●
│   ├── session-abc (running)
│   └── session-def (complete)
├── relay-ide ●
│   └── session-xyz (running)
```

Each daemon name has a health dot (green/yellow/red). Sessions are indented under their daemon.

**Top bar**
Show `Dashboard` instead of `Belayer` in brand. Show active daemon name next to session name.

**API helper changes**
```js
function apiUrl(daemonName, path) {
    if (dashboardMode) {
        return `/api/daemons/${encodeURIComponent(daemonName)}${path}`;
    }
    return path; // single-daemon mode
}
```

All `apiGet`, `apiGetText`, and SSE `openSSE` calls must include the daemon name.

**SSE in multi-daemon mode**
```js
function openSSE(daemonName, sessionId) {
    const url = apiUrl(daemonName, `/events/stream?sessions=${encodeURIComponent(sessionId)}&tier=verbose&after=${lastEventId}`);
    // ... rest same as current SSE logic
}
```

**Session selection**
When a session is clicked, we now know both the session ID and its daemon name. Set `activeDaemon` and proceed as before.

**Polling**
`loadSessions` in multi-daemon mode fetches `/api/daemons`, then for each daemon fetches `/api/daemons/{name}/sessions`, and combines results.

### Backward compatibility
Single-daemon mode must work exactly as before. All changes should be additive — the UI detects dashboard mode at runtime.

## Files to create / modify

### New files
- `internal/dashboard/server.go` — dashboard HTTP server + proxy
- `internal/cli/dashboard.go` — CLI command wiring
- `internal/dashboard/server_test.go` — proxy + static file tests

### Modified files
- `cmd/belayer/main.go` — register `dashboard` subcommand
- `web/app.js` — multi-daemon detection + UI
- `web/index.html` — minor: add daemon list container, dashboard brand
- `web/style.css` — minor: daemon group styles, indentation

## CORS considerations

Since the dashboard proxies all requests, the browser only talks to the dashboard origin. No CORS configuration is needed on the individual daemons. The dashboard → daemon requests are server-to-server.

## Security

- The dashboard does not enforce auth itself (it serves static files publicly)
- Auth is delegated to each daemon via the `Authorization: Bearer <token>` header injected by the proxy
- Tokens are configured explicitly by the operator; there is no auto-discovery that could leak tokens
