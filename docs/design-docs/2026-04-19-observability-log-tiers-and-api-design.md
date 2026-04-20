---
status: current
created: 2026-04-19
branch: feat/capture-at-level
supersedes:
implemented-by:
consulted-learnings: []
---

# Observability: Log Tiers, Unified CLI, and Dashboard API

## Problem

Today Belayer exposes session observability through three separate channels that overlap inconsistently:

1. **Event stream** (SQLite-backed, HTTP+SSE): agent spawn/exit, tool calls (200-char previews), messaging, artifacts, completion, verbose reasoning when opted in.
2. **Daemon log** (stdout/file): mix of daemon process events plus partial session content ("old format only logs aux LLM events").
3. **Bridge subprocess logs**: implicit in daemon stdout, not addressable.

Two CLI commands exist (`belayer logs` HTTP poll; `belayer watch` SSE) that do similar things through different transports. There is no per-agent filter, no way to bypass the event DB when the bridge itself crashes, and no way to ask "just give me this one agent's live thoughts."

Nightshift will sit above Belayer as the fleet consumer: it owns the cross-run web dashboard, session routing, user auth. Belayer needs to expose a clean, single API surface so a browser SPA can show every agent's reasoning, tool calls, and peer messaging live. At the same time, LLM agents (supervisor, PM) query the session themselves — they need token-efficient aggregate endpoints, not raw event dumps.

Several capture gaps:

- No way to see full tool args/results. The 200-char preview helps operators scan but discards the data you actually need when debugging a bad tool call.
- No way to see file content before/after a Write/Edit. Claude Code's `file-history-snapshot` is the gold standard here; we have nothing.
- No way to see subprocess stdout when an agent runs shell commands.
- No way to see the Python bridge's own crash stacks when something explodes before the HTTP round-trip completes.

At the same time we do **not** want:

- Separate "trace" database and "event" database — fragmented store.
- Filesystem plumbing for things Hermes already delivers as structured events via callbacks.
- Webhook push surface — Nightshift will pull when it's ready.
- Mid-session tier changes — creates retro-capture gaps, adds operator footguns.

The 9-agent sessions we run today produce event streams no human can read linearly. The solution has three parts that must be co-designed:

- **Data:** a tiered capture model (standard/verbose/trace) so operators opt into fat payloads only when debugging.
- **Transport:** one HTTP API surface covering sessions, events (live + historical), archives, artifacts, transcripts, traces, and bridge subprocess logs — usable by both CLI and browser.
- **Interaction:** one CLI command (`belayer logs`) that is also a reference consumer for that API, plus the compact/aggregate endpoints agents need to query efficiently.

## Non-goals

- Cross-run fleet dashboard. Nightshift owns that.
- Webhook push delivery. Poll/subscribe only.
- Cross-host session federation. One daemon per workspace, no replication.
- Mutable tier during a session. Set at create, immutable.
- Always-on metrics (Prometheus/OTel). Deferred.
- Replacing LOG_FORMAT.md v1 with a v2. All changes additive within v1.

## Architecture

Three-layer shape:

```
┌────────────────────────────────────────────────────────────────┐
│ Consumers                                                       │
│   CLI (`belayer logs`)      Browser SPA (Nightshift dashboard)  │
│   LLM agents (HTTP from sandbox)                                │
└────────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────────────┐
│ HTTP API (unix socket + opt-in TCP + bearer token)              │
│   /sessions, /events, /events/stream (SSE), /search,            │
│   /sessions/{id}/{outline,tools,conversation,phase},            │
│   /sessions/{id}/{artifacts,transcripts,traces,bridges}/*,      │
│   /sessions/{id}/archive.*, /health                             │
└────────────────────────────────────────────────────────────────┘
                         │
                         ▼
┌────────────────────────────────────────────────────────────────┐
│ Storage                                                         │
│   events.sqlite  (lifecycle + structured events, optional       │
│                   trace_file/offset/length spill refs)          │
│   sessions/<id>/bridge.<agent>.log      (always-on stdout)      │
│   transcripts/<id>/<agent>.jsonl(.zst)  (verbose+)              │
│   traces/<id>/<agent>.NNNN.jsonl(.zst)  (trace, rotated 128 MB) │
│   archive/<id>/                         (terminal-session bundle)│
│   artifacts/<id>/<artifact_id>                                  │
└────────────────────────────────────────────────────────────────┘
```

Three key data invariants:

1. **Event DB is authoritative for structured events.** Callbacks from Hermes post events via HTTP to the daemon. Everything else (transcripts, traces, bridge stdout) is a projection or a fat-payload sidecar, not a parallel source.
2. **Bridge stdout file is authoritative for subprocess-level debug.** It captures failure modes event DB cannot — daemon posts fail if HTTP is unreachable. File is always-on regardless of tier.
3. **Tiers are monotonic.** `verbose` ⊃ `standard`. `trace` ⊃ `verbose`. Consumers targeting tier N always get a superset of what tier N-1 would deliver.

## Section 1 — Tier Model

Three tiers. Each includes everything from the lower tier.

| Tier | Captures |
|---|---|
| `standard` | Session lifecycle, agent spawn/exit, messages, artifacts, status changes, warnings, errors, tool call + result **previews** (200-char cap), token usage, completion gate. |
| `verbose` | Above + `bridge:agent_reasoning` (full extended-thinking text) + `bridge:agent_narration` (interim assistant text) + append-only `transcripts/<agent>.jsonl`. |
| `trace` | Above + **untruncated** `full_input` / `full_result` fields on `bridge:tool_started`/`bridge:tool_completed` + new `trace:fs_snapshot` (before/after bytes of every Write/Edit) + new `trace:subprocess_exec` (argv/exit/stdout/stderr of shell tools). Payloads ≥64 KB spill to `traces/<agent>.NNNN.jsonl`. |

### Tier selection

Tier is set at session creation via (in precedence):

1. `belayer run start --log-level <tier>` CLI flag.
2. `BELAYER_LOG_LEVEL` env var.
3. `.belayer/config.yaml` → `log_level: <tier>`.
4. Default: `standard`.

Immutable for the session's lifetime. Recorded in `sessions.log_level` column. Surfaced in `/health` capabilities, `GET /sessions/{id}`, and archive `manifest.session.log_level`. No runtime tier bump in v1.

### Bridge stdout file (always-on, tier-independent)

Every bridge subprocess's stdout+stderr is captured to:

```
.belayer/sessions/<session_id>/bridge.<agent_id>.log
```

Appended live. Plain text (not events, not parsed). Rotated on daemon restart (keep 3). Captured regardless of `log_level`. This covers failure modes the event DB cannot: bridge crashes before the HTTP client is wired, Python import errors, daemon-side write failures, socket unreachable.

`trace:bridge_stdout` is explicitly **not** an event type. The file is authoritative; mirroring into the event DB would duplicate without value.

## Section 2 — CLI Surface

### `belayer logs` — the one command

```
belayer logs [<session>] [flags]
```

Replaces both the existing `belayer logs` (HTTP poll) and `belayer watch` (SSE). `belayer watch` kept as a deprecated alias for one minor release, prints a stderr notice, then removed.

**Session resolution:**

- Omitted: target the active session if exactly one is running. Error otherwise (`"N sessions active: …"`).
- UUID: exact.
- UUID prefix: unique match via existing `lookupSessionID`.
- Name: unique `sessions.name` match, falls back to prefix.

**Flags:**

| Flag | Default | Behavior |
|---|---|---|
| `-f, --follow` | **true** | Backfill then tail via SSE. `--no-follow` for one-shot. |
| `--since <dur>` | `0` | Start from `now − dur`. Mutually exclusive with `--after`. |
| `--after <id>` | `0` | Start from `event.id > N`. For script resumption. |
| `--agent <name>` | all | Server-side filter. Repeatable. |
| `--type <prefix>` | all | Server-side filter. E.g. `--type bridge:` or `--type message_`. |
| `--tier <level>` | all | Filter events to those at or below tier: `standard` / `verbose` / `trace`. |
| `--raw` | false | Bypass event DB; tail `bridge.<agent>.log` directly. Requires `--agent`. |
| `--format <fmt>` | `pretty` | `pretty` / `ndjson` / `json`. |
| `--no-color` | auto | Auto-disabled when stdout is non-TTY. |
| `--tail <n>` | none | Only the last N events before following. |

**Pretty rendering:**

One line per event. Deterministic color per agent (hash → ANSI palette slot). Prefix `[HH:MM:SS] [agent-name] `. Body examples:

- `agent_spawned` → `→ spawned (role=backend-dev, profile=default)`
- `bridge:tool_started` → `▸ Write(/tmp/x.md)`
- `bridge:tool_completed` → `◂ Write completed 142ms`
- `bridge:agent_reasoning` → `💭 <first line, terminal-width truncated>`
- `bridge:agent_narration` → `💬 <text, wrapped>`
- `message_sent` → `→ to=backend-dev.a type=plain: <content>`
- `bridge:turn_usage` → `$ turn: 1.2k in / 380 out / $0.012`
- `artifact_created` → `📎 spec → .belayer/artifacts/.../spec.md`
- `session_completed` → `✓ complete (approved_by=pm)`
- Otherwise → `· <type>: <json summary>`

### Other commands

| Command | Change |
|---|---|
| `belayer watch` | Deprecated alias. Removed next release. |
| `belayer run start` | Adds `--log-level <standard\|verbose\|trace>`. |
| `belayer status` | Adds `log_level` column per session. |
| `belayer roster` | Unchanged. |
| `belayer message send/broadcast/list` | Unchanged. |
| `belayer spawn` / `finish` / `request-completion` | Unchanged. |
| `belayer init` | Scaffolds `.belayer/config.yaml` with `log_level: standard`. |
| `belayer artifact get <session> <id>` | **New.** Downloads artifact content. |
| `belayer bridges tail <session> <agent>` | **New shorthand** for `logs --agent <agent> --raw -f`. |
| `belayer daemon` | Adds `--bind <addr:port>`, `--auth-token <str>` (empty = generate), `--cors-origin <url>` (repeatable). Unix socket active always; TCP only if `--bind` supplied. |

### Help docs — channels block

Every command's `--help` output gains the same footnote:

```
Channels:
  Event stream: session events via belayer daemon. Primary observability source.
  Daemon log:   daemon process log (startup, shutdown, HTTP errors).
                Not session content. See 'belayer daemon --log-file'.
  Bridge log:   per-agent subprocess stdout/stderr.
                Tail with 'belayer logs <sess> --agent <name> --raw'.
                Captures failures the event stream cannot (bridge crash
                before HTTP, daemon unreachable).
```

## Section 3 — HTTP API

Canonical contract. Supersedes LOG_FORMAT.md §§4–7 (rewrite described in Section 6).

### 3.1 Transport

- **Unix socket (always):** `.belayer/belayer.sock`. Local CLI + local agents. No auth.
- **TCP (opt-in):** `belayer daemon --bind <addr:port> [--auth-token <str>] [--cors-origin <url>]`. `Authorization: Bearer <token>` required on every request. Token generated via `crypto/rand` (32 bytes base64url) when `--auth-token` is empty; printed once to daemon stdout. CORS: explicit origin allowlist, credentials disallowed, no `*`.
- `/health` reachable on both without auth (for liveness probes).

### 3.2 Response shape

All JSON. Errors: `{"error":"<message>"}`, no stack trace. Content-Type per resource. Every event-returning endpoint carries response headers listed in §3.6.

### 3.3 Endpoint catalog

**Sessions**

| Method | Path | Purpose |
|---|---|---|
| GET | `/sessions` | List all. Query: `status=…`, `limit=N`. |
| GET | `/sessions/{id}` | Summary + roster. Includes `log_level`. |
| GET | `/sessions/{id}/outline` | Digest for dashboards/agents. |
| GET | `/sessions/{id}/phase` | Derived phase. |
| GET | `/sessions/{id}/tools` | All tool calls, time-ordered. |
| GET | `/sessions/{id}/conversation` | Query: `between=a,b` OR `agent=a`. |

**Events**

| Method | Path | Purpose |
|---|---|---|
| GET | `/sessions/{id}/events` | Query: `after,before,limit,agent,type_prefix,tier,format=json\|compact,since=<reader_id>`. Paginated via `Link`. |
| POST | `/sessions/{id}/events` | Agent-posted event. Body `{type, data}`. Server assigns `id,timestamp,session_id`. |
| GET | `/events/stream` (SSE) | Query: `sessions=a,b,…` (required), `after`, `agent`, `type_prefix`, `tier`, `digest=0\|1`. Domain frames + control frames. |
| GET | `/search` | Query: `q,session,agent,type_prefix,after,before,limit≤1000,format`. FTS5. |

**Archive**

| Method | Path | Purpose |
|---|---|---|
| GET | `/sessions/{id}/archive.ndjson` | Events NDJSON. 404 until written. |
| GET | `/sessions/{id}/archive/manifest.json` | Manifest. |
| GET | `/sessions/{id}/archive.tar.gz` | Full bundle (events + manifest + transcripts/ + traces/ + bridges/ + artifacts/). |

**Artifacts**

| Method | Path | Purpose |
|---|---|---|
| GET | `/sessions/{id}/artifacts` | `[{id,kind,path,producer,created_at}]`. |
| GET | `/sessions/{id}/artifacts/{artifact_id}` | Raw file bytes. Content-Type inferred. `Content-Disposition: inline` when renderable, `attachment` otherwise. |

**Transcripts (verbose+)**

| Method | Path | Purpose |
|---|---|---|
| GET | `/sessions/{id}/transcripts` | `[{agent, path, size, updated_at}]`. 404 if `log_level<verbose`. |
| GET | `/sessions/{id}/transcripts/{agent}.jsonl` | Query: `follow=1`, `tail=<bytes>`. JSONL stream. |

**Traces (trace tier)**

| Method | Path | Purpose |
|---|---|---|
| GET | `/sessions/{id}/traces` | `[{agent, fragment, path, size, compressed, updated_at}]`. 404 if `log_level<trace`. |
| GET | `/sessions/{id}/trace/{agent}/{fragment}` | Query: `offset=N&length=M`. Returns one JSON blob (offset+length present) or full fragment (absent). Handles `.zst` transparently on the server side. |

**Bridge logs (always-on)**

| Method | Path | Purpose |
|---|---|---|
| GET | `/sessions/{id}/bridges` | `[{agent, log_path, size, rotated_at}]`. |
| GET | `/sessions/{id}/bridges/{agent}/stdout` | Query: `follow=1`, `tail=<bytes>`, `after_byte=N`. Plain-text stream. |

**Messaging (unchanged routes)**

| Method | Path | Purpose |
|---|---|---|
| GET | `/sessions/{id}/messages` | Query: `for=<agent>`, `pending=true`. |
| POST | `/sessions/{id}/messages/send` | Body `{to, from, content, type?, interrupt?}`. |
| POST | `/sessions/{id}/messages/broadcast` | Body `{from, content, type?}`. |

**Lifecycle (unchanged)**

| Method | Path | Purpose |
|---|---|---|
| POST | `/sessions` | Create. Body `{name, log_level, template?}`. |
| POST | `/sessions/{id}/agents` | Spawn agent. |
| POST | `/sessions/{id}/finish` | Supervisor completion signal. |
| POST | `/sessions/{id}/status` | Patch status. |

### 3.4 Aggregate endpoints — token-efficient abstractions

Four endpoints let agents and dashboards query derived state instead of scanning raw events. Rich detail lives in LOG_FORMAT §6.5 once this spec lands.

- **`/outline`** returns `{ session, agents:[{name,role,status,last_activity,tool_calls,tokens,cost}], artifacts, phases, final_status }`. Sub-1 KB payload vs ~100 KB raw events.
- **`/tools`** returns `[{agent,tool,path?,duration_ms,status,at,id}]`.
- **`/conversation?between=a,b`** returns message timeline between two agents.
- **`/phase`** returns `{phase, since}` derived from session/agent status events: `discover\|plan\|implement\|verify\|complete`.

### 3.5 Compact format

`?format=compact` on any events endpoint returns one line per event, fixed tab-separated shape:

```
<id>\t<agent>\t<type>\t<one-line-summary>
143	supervisor	agent_spawned	role=supervisor
145	supervisor	bridge:tool_started	Write path=/tmp/x.md
146	supervisor	bridge:tool_completed	Write 142ms ok
```

Approximately 80 bytes/event vs ~400 bytes JSON. Newlines and tabs inside content are escaped as `\n` and `\t`. Grep-safe, pipe-safe, parser-free.

### 3.6 Response headers

Every event-returning endpoint:

| Header | Value |
|---|---|
| `X-Belayer-Schema` | `belayer-log/v1` |
| `X-Last-Event-Id` | Max `id` for this session in the store |
| `X-Event-Count` | Count returned in the response body |
| `X-Session-Status` | Current session status |
| `X-Log-Level` | Session's `log_level` at creation |
| `X-Agent-Roster` | Comma-separated agent names |
| `Link` | `<…?after=N>; rel="next"` when more results exist |

Consumers can `curl -I` an events URL for the shape-peek only.

### 3.7 Per-reader cursors

`?since=<reader_id>` on event endpoints.

Belayer persists the cursor in a new `reader_cursors` table. First call with a new `reader_id` returns the full backlog; subsequent calls return only events with `id > last_delivered(reader_id, session_id)`. Server updates the cursor on successful 2xx response. 24-hour TTL; rows swept on daemon start and hourly. Resets are implicit on TTL expiration — caller-side re-subscription recovers.

Lets an agent supervisor pass `?since=supervisor` and skip UUID bookkeeping.

### 3.8 SSE session_digest control frame

Between domain events, at `verbose` or `trace` tier, the server may emit:

```
event: session_digest
data: {"at":143,"agents":{"supervisor":{"tokens":1420,"tools":3,"status":"running"},"backend-dev.a":{"tokens":840,"tools":1,"status":"running"}},"phase":"implement"}
```

No `id:` line (control frame; per LOG_FORMAT A3 invariant). Cadence: every 60 seconds or every 50 events, whichever is first. Suppressible via `?digest=0`. Dashboards heartbeat-update roster widgets without recomputing from events.

### 3.9 Health capabilities

```json
GET /health
{
  "status": "ok",
  "schema_version": "belayer-log/v1",
  "daemon_instance_id": "<uuid>",
  "draining": false,
  "capabilities": {
    "transport": ["unix","tcp"],
    "search_predicates": ["q","session","type_prefix","agent","after","before"],
    "sse_filters": ["sessions","after","agent","type_prefix","tier","digest"],
    "sse_control_frames": ["daemon_hello","daemon_draining","session_digest"],
    "archive_http": true,
    "artifacts_http": true,
    "transcripts_http": true,
    "traces_http": true,
    "bridges_http": true,
    "event_formats": ["json","compact"],
    "response_headers": ["X-Last-Event-Id","X-Event-Count","X-Session-Status","X-Log-Level","X-Agent-Roster"]
  }
}
```

### 3.10 Rate limits and error codes

- SSE keepalive every 15s (existing).
- `/search`: 2s timeout, 1000-row cap, 4 KB query cap.
- Trace slice reads: unbounded on unix socket; TCP capped at 16 MB per request (use `offset+length` for random access).
- No per-IP throttle in v1.

Error codes: 400 (bad query), 401 (missing/invalid token on TCP), 403 (CORS origin denied), 404 (session/artifact/tier-gated endpoint missing), 409 (`daemon_instance_id` epoch mismatch), 503 (draining), 504 (`/search` timeout).

## Section 4 — Storage Layout

On-disk under `<workspace>/.belayer/`:

```
.belayer/
  belayer.sock
  daemon.log              # daemon process log only (infra, not sessions)
  daemon.pid
  config.yaml             # workspace defaults
  events.sqlite           # global event store
  sessions/
    <session_id>/
      bridge.<agent>.log       # always-on subprocess stdout+stderr
      bridge.<agent>.log.1     # rotated at daemon restart (keep 3)
  transcripts/
    <session_id>/
      <agent>.jsonl            # verbose tier, append-only
      <agent>.jsonl.zst        # compressed on agent exit
  traces/
    <session_id>/
      <agent>.0001.jsonl       # trace tier, raw JSONL live
      <agent>.0001.jsonl.zst   # compressed on rotation OR agent exit
      <agent>.0002.jsonl       # rotated at 128 MB
  archive/
    <session_id>/
      events.ndjson
      manifest.json
      transcripts/<agent>.jsonl
      traces/<agent>.NNNN.jsonl.zst
      bridges/<agent>.log
  artifacts/
    <session_id>/<artifact_id>
```

### 4.1 SQLite changes

New columns on `events` (all nullable):

```sql
ALTER TABLE events ADD COLUMN trace_file   TEXT;
ALTER TABLE events ADD COLUMN trace_offset INTEGER;
ALTER TABLE events ADD COLUMN trace_length INTEGER;
```

New column on `sessions`:

```sql
ALTER TABLE sessions ADD COLUMN log_level TEXT NOT NULL DEFAULT 'standard';
```

Existing rows receive `'standard'` retroactively. No backfill for trace columns (NULL means "no spill payload").

New table `reader_cursors`:

```sql
CREATE TABLE reader_cursors (
  reader_id   TEXT NOT NULL,
  session_id  TEXT NOT NULL,
  last_id     INTEGER NOT NULL,
  updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (reader_id, session_id)
);
```

### 4.2 Spill semantics

| Serialized event `data` size | Session tier | Behavior |
|---|---|---|
| < 64 KB | any | Store inline in `events.data` as today. |
| ≥ 64 KB | `trace` | Write payload to current trace fragment; fsync; INSERT row with `trace_file/offset/length`; set `events.data` to thin placeholder `{"agent":"…","_trace":true}`. Consumers reading `compact`/`pretty` see the placeholder; full-data consumers resolve via `/sessions/{id}/trace/{agent}/{fragment}?offset=N&length=M`. |
| ≥ 64 KB | `standard` or `verbose` | Truncate `events.data` to 64 KB, append `…(truncated; upgrade to trace tier to capture)`. No spill. |

### 4.3 Rotation, compression, crash recovery

**Bridge stdout file:** captured via Go `cmd.Stdout`/`cmd.Stderr` writer pair (merged into one file). Rotated on daemon restart by renaming `.log` → `.log.1`, keep 3. No size-based rotation during lifetime (expected small). Copied into `archive/<session>/bridges/` at session close.

**Transcript file:** existing writer (`_TranscriptWriter` in `hermes_bridge/callbacks.py`). Append-only, flushed per write, zstd-compressed on agent exit. No change to write path.

**Trace fragment writer (new):** lives in the daemon (not the bridge). Receives a spill write request from the event insert path. Opens `<agent>.0001.jsonl` on first spill; rotates at 128 MB to `<agent>.0002.jsonl`; compresses previous fragment in the background on rotation and on agent exit.

**Atomicity:** payload written to file first, fsync, then SQL `INSERT` with `trace_file/offset/length`. Orphan payloads tolerated and cleaned by GC.

**Crash recovery:** on daemon start, for each active trace fragment the writer truncates to the last complete newline and logs the cut to `daemon.log`. Rows with `trace_offset` pointing inside the truncated region will return 409 on read (epoch mismatch on reconnect catches most cases; 409 for the tail).

### 4.4 GC

Background goroutine at daemon start and hourly:

- Trace fragment byte ranges referenced by no SQL row → delete (age-gated to avoid racing a just-written payload).
- `reader_cursors` rows with `updated_at < now − 24h` → delete.
- `bridge.<agent>.log.N` beyond retention depth → delete.
- Archive tarballs older than `config.yaml: archive_retention_days` → delete. Default: unlimited.

Emitted to `daemon.log` only, not the event stream (infra, not session content).

## Section 5 — Event Schema Changes

All additive within `belayer-log/v1`.

### 5.1 Modified existing events

**`bridge:tool_started`** — adds trace-only `full_input`:

```json
{
  "agent":         "<string>",
  "tool":          "<string>",
  "input_preview": "<string, ≤200>",
  "full_input":    "<object | string>",
  "path":          "<string, optional>"
}
```

**`bridge:tool_completed`** — adds trace-only `full_result`:

```json
{
  "agent":          "<string>",
  "tool":           "<string>",
  "duration_ms":    <int>,
  "result_preview": "<string, ≤200>",
  "full_result":    "<object | string>",
  "path":           "<string, optional>"
}
```

Verbose/standard consumers never see `full_input`/`full_result`. Trace consumers see them inline (<64 KB) or referenced via `trace_file/offset/length` (≥64 KB).

### 5.2 New trace-tier events

**`trace:fs_snapshot`** — pre/post snapshot around Write/Edit/NotebookEdit tool invocations:

```json
{
  "agent":      "<string>",
  "tool":       "Write | Edit | NotebookEdit | …",
  "path":       "<absolute path>",
  "phase":      "before | after",
  "size_bytes": <int>,
  "sha256":     "<hex>",
  "exists":     <bool>,
  "content":    "<string>"
}
```

Two events per modification. Non-existent file → `exists:false, size_bytes:0, content:""`. Read-only tools (`Read`, `Grep`, `Glob`) NOT snapshotted — volume/value ratio unfavorable.

**`trace:subprocess_exec`** — per-invocation for Bash and scripted subprocess tools:

```json
{
  "agent":       "<string>",
  "cmd":         "<string>",
  "argv":        ["<string>", ...],
  "cwd":         "<string>",
  "env_subset":  { "<KEY>": "<VALUE>" },
  "exit_code":   <int>,
  "duration_ms": <int>,
  "stdout":      "<string>",
  "stderr":      "<string>"
}
```

`env_subset` is an allowlist: `PATH, PWD, HOME, USER, LANG, TERM, NODE_ENV, CI, BELAYER_*`. Everything else is dropped (secret by default).

### 5.3 Dropped proposals

- `trace:bridge_stdout` — replaced by the always-on bridge log file. No duplication.
- `trace:llm_turn` — deferred until Hermes exposes request/response hooks upstream.
- `trace:fs_access` for reads — too noisy; Write/Edit are the interesting cases.

### 5.4 Redaction

Before writing any `full_input`, `full_result`, `stdout`, `stderr`, or `env_subset` payload the daemon applies a regex scrubber:

- Keys matching `(?i)(api[_-]?key|authorization|bearer|password|secret|token|credential)` → value replaced with `"<redacted>"`.
- Values matching `sk-[A-Za-z0-9]{20,}` (OpenAI-style) or `Bearer [A-Za-z0-9._-]+` → substring replaced.

Best-effort only, not a security boundary. Documented as non-guaranteed in LOG_FORMAT.

## Section 6 — Documentation Updates

### 6.1 `docs/LOG_FORMAT.md` — rewrite, stays `belayer-log/v1`

- §1 — note tier grading and `/health` capability negotiation.
- §2 — add `trace_file`, `trace_offset`, `trace_length` optional SessionEvent fields with inline-vs-spill semantics (64 KB).
- §3 — modify `bridge:tool_started/completed` entries with trace-only fields; add §3.14 `trace:fs_snapshot` and `trace:subprocess_exec`; add §3.15 prefix recipes for filter composition.
- §4 — add `agent`, `type_prefix`, `tier`, `digest` SSE query params; add `session_digest` control frame to A3; reinforce control frames omit `id:`.
- §5 — document `archive/<session>/{transcripts,traces,bridges,artifacts}/`; extend `manifest.json` with `log_level`, `trace_count`, `trace_bytes`.
- §6 — unchanged.
- §6.5 — add `/outline`, `/phase`, `/tools`, `/conversation`, `/artifacts`, `/transcripts`, `/traces`, `/bridges` endpoints.
- New §6.6 — catalog of response headers.
- New §6.7 — per-reader cursor semantics and TTL.
- New §6.8 — compact-format TSV shape and escape rules.
- §7 — updated capabilities block.
- §9 — add worked examples: compact format, per-reader cursor flow, trace spill reader flow, `session_digest` frame sample.
- §10 — prefer `/outline` + `/tools` + `/conversation` over raw events; negotiate `capabilities` before depending on tier/trace features.

### 6.2 `docs/AGENT_ARCHITECTURE.md`

Add "Observability for Agents" section: three APIs agents should reach for (`/outline`, `/since=<name>` cursor, `/conversation?between=`). Note: supervisor must not assume `full_result` exists — check `X-Log-Level` or `/health` first.

### 6.3 `docs/PHILOSOPHY.md`

One-sentence addendum to the Session interface: session observability may be tiered with monotonic inclusion, but a runtime must not redefine tier semantics mid-session.

### 6.4 `docs/SANDBOXING.md` — delete

Replaced by new `docs/DEPLOYMENT.md`; see Section 7a.

### 6.5 `CLAUDE.md`

- Extend CLI surface block with new flags/commands.
- Add bullet: "Log tiers: standard/verbose/trace — see `docs/LOG_FORMAT.md`."
- Replace `docs/SANDBOXING.md` reference with `docs/DEPLOYMENT.md`.

### 6.6 New: `docs/OBSERVABILITY.md`

One-page operator guide:

- Three channels table (daemon log / event stream / bridge log) with when-to-read-which.
- Tier selection decision tree.
- Common recipes (tail one agent, grep tool calls, download archive for postmortem).
- Dashboard integration notes: capability negotiation, TCP bind setup, token handling.

Cross-linked from `CLAUDE.md`, `LOG_FORMAT.md`, and command help text.

### 6.7 New: `docs/DEPLOYMENT.md`

Replaces `SANDBOXING.md`. See Section 7a.

## Section 7 — Migration, Testing, Rollout

### 7.1 Schema migration

Single forward-only migration:

```sql
ALTER TABLE events  ADD COLUMN trace_file   TEXT;
ALTER TABLE events  ADD COLUMN trace_offset INTEGER;
ALTER TABLE events  ADD COLUMN trace_length INTEGER;
ALTER TABLE sessions ADD COLUMN log_level TEXT NOT NULL DEFAULT 'standard';
CREATE TABLE reader_cursors (...);
```

Bumps internal DB schema version, not the `belayer-log/` contract tag. Existing sessions become `log_level='standard'`.

### 7.2 Deprecation

`belayer watch` remains as alias of `belayer logs -f` for one minor release, printing a stderr deprecation notice. Removed in the release after.

### 7.3 Testing strategy

**Go unit:**
- Store: tier-gated writes, 64 KB spill path selects the file writer, reader-cursor TTL, migration up-only.
- HTTP handlers: filter combos on `/events` and SSE, compact encoding with `\t\n` edge cases, bearer auth, CORS.
- Trace writer: rotation at 128 MB boundary, zstd on rotate, crash-truncate-to-last-newline recovery.
- GC: orphan-fragment removal, reader_cursor TTL.

**Python bridge unit:**
- Callbacks drop truncation caps at trace tier; keep preview caps at standard/verbose.
- `trace:fs_snapshot` emitted pre/post mutation tools, skipped for read-only tools.
- `trace:subprocess_exec` env_subset allowlist, secret scrubbing.
- Bridge stdout capture: Go-side tests verify file creation, append, and rotation.

**Integration:**
- E2E at each tier: full session, assert event catalog matches tier guarantees.
- SSE round-trip: consumer with `agent=X&tier=verbose`, verify only matching events.
- Archive completeness: verbose archive contains `transcripts/`; trace archive contains `traces/` + spilled refs resolvable post-archive.
- CLI: `belayer logs` implicit resolution (1 running), error 0/N, `--raw` tails bridge log while event DB empty, `--format compact` grep-safe.
- TCP + token: request without token → 401, with valid token → 200, CORS preflight works.

**Load:**
- 9-agent trace-tier session for 30 min. Assert: spill threshold triggers, fragments rotate at 128 MB, SQLite size stays bounded, SSE delivery keeps up (drain backlog < 1 min).
- Same session at standard tier — event volume ≤ 20% of trace.

**Contract:**
- Golden NDJSON fixtures per tier.
- `/health` capability snapshot test matching LOG_FORMAT §7 exactly.

### 7.4 Rollout phases

| Phase | Scope |
|---|---|
| 1 | Schema migration + `log_level` column + `belayer run start --log-level` flag. All existing runs default `standard`; behavior unchanged. |
| 2 | Bridge stdout file + `--raw` CLI + `/bridges/*` HTTP. Always-on regardless of tier. |
| 3 | Trace writer + spill + `trace:fs_snapshot` + `trace:subprocess_exec` + `full_input`/`full_result` fields. Opt-in via `--log-level trace`. |
| 4 | Aggregate endpoints (`/outline`, `/tools`, `/conversation`, `/phase`) + response headers + `?format=compact` + per-reader cursor. |
| 5 | SSE filter params + `session_digest` frame. |
| 6 | TCP bind + bearer auth + CORS. |
| 7 | Artifact + transcript + trace HTTP endpoints. |
| 8 | `belayer watch` removal, CLAUDE.md / LOG_FORMAT / OBSERVABILITY / DEPLOYMENT finalized, SANDBOXING.md deleted. |

Each phase is additive — opt-in surfaces only; `log_level` gates data, `--bind` gates network.

### 7.5 Security

- Bearer token: `crypto/rand`, 32 bytes base64url. Printed once to stdout on auto-generation. Not persisted in logs. Rotation = daemon restart.
- TCP + CORS: explicit origin allowlist required; no `*`. Credentials header disallowed.
- Redaction is best-effort only. Documented as such.
- Trace files may contain source code, env values, and tool outputs. Default `.gitignore` entry for `.belayer/` emitted by `belayer init`.

## Section 7a — Deployment Topology Doc

### 7a.1 Delete `docs/SANDBOXING.md`

The current 162-line doc details clamshell Docker mode (per-bridge container, MITM proxy, egress allowlist, process attestation). Matches the v6 "belayer-in-clamshell" direction. v7's philosophy — "one Belayer session per worker, run wherever the outer layer puts us" — makes that detail misleading as a default.

### 7a.2 New `docs/DEPLOYMENT.md`

Short (~60 lines). Contents:

- Statement: Belayer is the control plane for ONE run. It doesn't impose an isolation boundary.
- **Topologies** table: host-native (dev), Nightshift worker (prod), clamshell (legacy). Nightshift worker is the production default.
- **Trust model:** Belayer trusts its working directory. Agents execute tools inside it. Trace captures file content — anything sensitive must live outside belayer's working dir, or tier must stay below `trace`. Outer sandbox (VM, container, chroot, clamshell) is the real boundary.
- **Credentials:** `~/.belayer.env` merged with `.belayer/.belayer.env`; workspace wins. Daemon passes subset to bridges. Redaction applied to trace writes. No guarantees for trace tier + secret-bearing tool args.
- **Ports and sockets:** unix socket always; TCP opt-in with token and CORS; bridge stdout at `sessions/<id>/bridge.<agent>.log`.

### 7a.3 Fallout

- `docs/PHILOSOPHY.md` §3 Sandbox gets one sentence: "Belayer does not impose isolation; the outer deployment topology does."
- `CLAUDE.md` doc list swaps SANDBOXING → DEPLOYMENT.
- Existing clamshell-specific design docs (`docs/design-docs/2026-04-17-belayer-in-clamshell-design.md` and related) remain as historical record.

## Interfaces and isolation

Summary of what lives where:

- **`internal/store/`** — SQLite schema + migration; event insert path gains spill branch.
- **`internal/daemon/`** — new trace writer; bridge stdout capture wiring; GC goroutine; daemon-log separation (remove session-content emissions from daemon log).
- **`internal/cli/`** — revamp `session.go logsCmd`; delete `watchCmd` or alias; new `artifactGetCmd`, `bridgesTailCmd`; daemon flags `--bind` / `--auth-token` / `--cors-origin`; init command emits config and gitignore entries.
- **`internal/httpapi/`** (new or existing) — new endpoints: `/outline`, `/phase`, `/tools`, `/conversation`, `/artifacts/*`, `/transcripts/*`, `/traces/*`, `/bridges/*`; SSE filter params; response headers; compact format; per-reader cursor.
- **`hermes_bridge/callbacks.py`** — emit full payloads at trace tier; new `trace:fs_snapshot` pre/post hook around mutation tools; new `trace:subprocess_exec` around shell tools; no change for verbose/standard.
- **`docs/`** — rewrite `LOG_FORMAT.md`; new `OBSERVABILITY.md` and `DEPLOYMENT.md`; delete `SANDBOXING.md`.

Each component has one clear job; HTTP handlers are thin projections over the store; storage is append-only with spill; CLI is a reference client of the HTTP API, not a parallel code path.

## Open questions (carry into plan)

- Config precedence for multi-agent overrides: should an agent definition (`agents/<name>/agent.yaml`) be able to *force* verbose for just that agent while the session is standard? **Current answer:** no. Session tier governs everything.
- Retention defaults for trace archives: unlimited by default, or bounded at e.g. 14 days? Current spec leaves unbounded until Nightshift explicitly configures.
- `trace:llm_turn` upstream path: file an issue on Hermes once this spec lands; unblocks reconstructing model choices in postmortems.
