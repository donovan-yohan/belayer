# Belayer Web UI Spec

## Goal
A self-hosted, dark-themed, zero-build-step web dashboard for live monitoring of Belayer agent sessions. Ships inside the Go binary via `go:embed`. Accessible at `/ui/` when the daemon is running.

## Design Principles
- **No build step**: vanilla HTML/CSS/JS, no npm, no bundler.
- **Dark IDE aesthetic**: GitHub-dark palette (`#0d1117`, `#161b22`, `#21262d`), compact density.
- **Three-panel layout**:
  - Left (240px): sessions list + connection status
  - Center (flex): event timeline / message stream (main focus)
  - Right (260px): agent roster + session metadata
- **Color-coded agents**: every agent bubble/badge uses a deterministic color derived from its role/identity.
- **Live streaming**: SSE `GET /events/stream` with auto-reconnect.

## Agent Color Palette
Colors are assigned by agent identity name prefix (first component before `.` or `-`):

| Identity | Color (hex) | Tailwind approx |
|----------|-------------|-----------------|
| supervisor | `#8b5cf6` | violet-500 |
| backend-dev | `#22c55e` | green-500 |
| web-dev | `#f59e0b` | amber-500 |
| qa | `#ef4444` | red-500 |
| pm | `#06b6d4` | cyan-500 |
| reviewer | `#94a3b8` | slate-400 |
| system / daemon | `#64748b` | slate-500 |
| unknown | `#a855f7` | purple-500 |

Render agent name pills with `background-color` at 15% opacity and `border-left: 3px solid <color>`.

## Layout Detail

### Top Bar (48px)
- Left: Belayer logo/wordmark + daemon instance ID (truncated)
- Center: active session name + status badge
- Right: SSE connection status (green dot = connected, red = disconnected, amber = reconnecting)

### Left Sidebar (240px)
- Section: "Sessions"
  - List of sessions from `GET /sessions`
  - Each row: name, status badge, created_at relative time
  - Click to select / load
  - Auto-refresh every 5s
- Section: "Filters" (collapsible)
  - Toggle event type filters: `session_*`, `agent_*`, `bridge:*`, `message_*`, `artifact_*`, `tool_*`
  - Agent checkboxes (populated from roster)

### Center Pane (flex)
- Tab bar: "Events" | "Messages" | "Tool Calls"
- **Events tab**: chronological feed of all `SessionEvent` objects
  - Each row: timestamp | agent pill | event type | collapsed payload
  - Click to expand payload (pretty-printed JSON)
  - Auto-scroll to bottom when near bottom
  - Pause auto-scroll when user scrolls up
- **Messages tab**: dedicated inter-agent messaging view
  - Chat-bubble style: left = inbound to selected agent, right = outbound
  - Or: clean table showing `from → to` with content
  - Highlight @mentions
- **Tool Calls tab**: paired tool invocations from `GET /sessions/{id}/tool-calls`
  - Collapsible rows showing input preview + result preview + duration

### Right Sidebar (260px)
- Section: "Agent Roster"
  - List from `GET /sessions/{id}` agent roster + live status
  - Each agent: colored dot + name + role + status badge
  - Status: `running`, `complete`, `blocked`, `pending_verification`
- Section: "Session Metadata"
  - ID, log_level, created_at, phase
- Section: "Artifacts"
  - List from `GET /sessions/{id}/artifacts`
  - Links to download

## Data Flow
1. On load: `GET /health` → verify capabilities (look for `web_ui: true`)
2. `GET /sessions` → populate left sidebar
3. On session select: `GET /sessions/{id}` → populate metadata + roster
4. `GET /sessions/{id}/events?after=0&limit=1000` → backfill recent events
5. Open SSE: `GET /events/stream?sessions={id}&tier=verbose`
6. Merge backfill + live events into chronological feed
7. `GET /sessions/{id}/tool-calls` → populate tool calls tab
8. Poll `GET /sessions/{id}/outline` every 10s for roster updates

## SSE Reconnect Strategy
- Use `fetch()` + `ReadableStream` parser (NOT native `EventSource`) so we can:
  - Set `Authorization: Bearer <token>` header
  - Control reconnect backoff (500ms → 30s cap)
  - Access `Last-Event-ID` equivalent manually
- On reconnect: resume from last seen event `id` using `?after=<id>`
- On `daemon_draining`: show warning banner, stop reconnecting after 3 attempts
- On `daemon_hello` with new `daemon_instance_id`: clear event buffer and reload from `?after=0`

## Auth & TCP
- The UI is served at `/ui/` and `/ui/*` from embedded static files.
- `authMiddleware` must exempt `/ui/` and `/ui/*` so the HTML/JS/CSS loads without a token.
- `authMiddleware` must also accept `?token=<bearer>` on **GET requests** (including SSE) because browsers cannot set headers on EventSource/fetch without CORS preflight complications, and because the UI needs to pass the token via URL for SSE.
- The UI reads `?token=...` from `window.location.search` on startup and stores it in memory. All `fetch()` calls include `Authorization: Bearer <token>`. The SSE URL also includes `?token=...`.

## Go Implementation Plan

### New: `web/index.html`
Single HTML file that loads `style.css` and `app.js`. All CSS and JS are in separate files for readability, but no build step.

### New: `web/style.css`
Dark theme CSS. ~400 lines. No external dependencies.

### New: `web/app.js`
Vanilla JS. ~800-1000 lines. Modules via IIFE or simple script tags.

### New: `internal/daemon/webui.go`
```go
package daemon

import (
    "embed"
    "io/fs"
    "net/http"
    "strings"
)

//go:embed all:web
var webFS embed.FS

func (d *Daemon) handleWebUI(w http.ResponseWriter, r *http.Request) {
    // Strip /ui prefix
    path := strings.TrimPrefix(r.URL.Path, "/ui")
    if path == "" || path == "/" {
        path = "/index.html"
    }
    // Serve from embedded FS
    content, err := webFS.ReadFile("web" + path)
    if err != nil {
        http.NotFound(w, r)
        return
    }
    // Set content type based on extension
    switch {
    case strings.HasSuffix(path, ".css"):
        w.Header().Set("Content-Type", "text/css")
    case strings.HasSuffix(path, ".js"):
        w.Header().Set("Content-Type", "application/javascript")
    default:
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
    }
    w.Write(content)
}
```

**IMPORTANT**: `embed.FS` is relative to the package directory. Since `internal/daemon/webui.go` cannot embed files from the repo root (`web/`), we have two options:
1. Put `web/` under `internal/daemon/web/` and embed from there
2. Keep `web/` at repo root and have `embed.go` in package `belayer` embed it, then `internal/daemon` imports `github.com/donovan-yohan/belayer` to access the FS

Option 2 follows the existing pattern (`embed.go` already exists at repo root in package `belayer`). We'll add `//go:embed all:web` to `embed.go` and expose a `var WebUI embed.FS`. Then `internal/daemon/webui.go` imports the root package.

### Modified: `embed.go`
```go
//go:embed all:web
var WebUI embed.FS
```

### Modified: `internal/daemon/daemon.go`
Add route:
```go
mux.HandleFunc("GET /ui/{$}", d.handleWebUI)
mux.HandleFunc("GET /ui/{path...}", d.handleWebUI)
```
Also add redirect from `/` to `/ui/` for convenience (optional but nice).

### Modified: `internal/daemon/auth.go`
In `authMiddleware`, exempt `/ui/` and `/ui/*`:
```go
if strings.HasPrefix(r.URL.Path, "/ui/") {
    next.ServeHTTP(w, r)
    return
}
```

Also add query-param token fallback for GET requests:
```go
auth := r.Header.Get("Authorization")
if auth == "" && r.Method == http.MethodGet {
    auth = "Bearer " + r.URL.Query().Get("token")
}
```

### Modified: `internal/daemon/health_test.go`
Add `WebUI bool json:"web_ui"` to capabilities struct and assert it is `true`.

## Capabilities
Add `web_ui: true` to the `/health` capabilities manifest.

## Testing
- `TestWebUI_ServesIndex` — `GET /ui/` returns 200 and HTML containing "Belayer"
- `TestWebUI_ServesCSS` — `GET /ui/style.css` returns 200 with correct Content-Type
- `TestWebUI_ServesJS` — `GET /ui/app.js` returns 200 with correct Content-Type
- `TestAuth_ExemptsWebUI` — `GET /ui/index.html` on TCP without token returns 200
- `TestAuth_QueryParamToken` — `GET /sessions?token=<valid>` on TCP returns 200
- `TestHealth_AdvertisesWebUI` — `/health` contains `web_ui: true`

## Reference UI Projects
- https://github.com/nesquena/hermes-webui — three-panel layout, dark theme, composer footer, context ring
- https://github.com/outsourc-e/hermes-workspace — chat with tool rendering, inspector panel, terminal

## Scope Exclusions (future work)
- Transcript viewer (verbose+ reasoning text)
- Trace fragment viewer (trace tier file snapshots)
- Real-time terminal/PTY
- PWA / offline mode
- Multi-session side-by-side view
