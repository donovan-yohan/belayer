# Belayer Log Format — Consumer Contract

Schema tag: **`belayer-log/v1`**. This document is the sole authoritative reference for external consumers (e.g. cragd) of Belayer's event stream, archive files, and search API.

---

## 1. Overview

Belayer is the agent control plane for a single Nightshift worker run — one daemon per workspace, ephemeral per run. It coordinates supervisor and specialist agents via the Hermes bridge subprocess.

A **SessionEvent** is an immutable, monotonically-ordered row inserted into SQLite when something significant happens during a session. Events are the primary observability primitive: streamed live via SSE, queryable via HTTP, and dumped to NDJSON at terminal transition.

**Schema version tag:** `belayer-log/v1`. This tag appears on every `/health` response, every `daemon_hello` SSE control frame, and every archive `manifest.json`. Consumers MUST verify this tag on connect. On version skew, consumers MUST refuse to parse and surface an error to the operator.

---

## 2. SessionEvent Schema

Source: `internal/store/store.go:SessionEvent`.

```json
{
  "id":         <int>,       // monotonic global ID; assigned by the store on insert; never reused
  "session_id": "<string>",  // UUID of the owning session
  "timestamp":  "<RFC3339>", // UTC wall time of insertion
  "type":       "<string>",  // dotted or prefixed name — see Section 3
  "data":       { ... }      // type-specific payload; always a JSON object, never null or array
}
```

**Guarantees:**
- `id` values are strictly increasing across all sessions in the same daemon instance. They are NOT dense — gaps are valid (transaction rollbacks, reserved ranges).
- `data` is always a JSON object. Consumers MUST treat an absent `data` key as `{}`.
- Unknown keys at the top level MUST be ignored. New keys may be added within v1.

---

## 3. Event Type Catalogue

### 3.1 `session_*` — Session lifecycle

| Type | Required `data` fields | Optional `data` fields | Notes |
|------|------------------------|------------------------|-------|
| `session_created` | `name` (string) | `template` (string) | Emitted immediately after row creation |
| `session_status_changed` | `status` (string) | — | Emitted on any explicit status patch; does NOT fire for daemon-internal transitions (use `session_completed`, `session_stalled`) |
| `session_completed` | `approved_by` (string), `report` (string, ≤1000 chars) | — | Emitted when PM approves completion |
| `session_stalled` | `reason` (string) | — | Emitted when all agents exit without PM approval; `reason` is always `"all_agents_exited_without_completion"` in v1 |

### 3.2 `agent_*` — Agent lifecycle

| Type | Required `data` fields | Optional `data` fields | Notes |
|------|------------------------|------------------------|-------|
| `agent_spawned` | `agent` (string), `role` (string), `profile` (string), `transport` (string) | — | `transport` is always `"bridge"` in v1 |
| `agent_finished` | `agent` (string), `status` (string), `summary` (string) | — | `status` is `"complete"`, `"blocked"`, or `"pending_verification"` in v1. Consumers MUST tolerate unknown status strings and treat them as opaque rather than rejecting the event. |
| `agent_escalated` | `agent` (string), `reason` (string) | `detail` (string) | Emitted when agent reports incomplete via `agent_status:incomplete`; `reason` is always `"incomplete"` in v1 |
| `agent_exited_without_finish` | `agent` (string), `status` (string), `exit_err` (string) | — | Bridge process exited without a clean `bridge:finished`; `status` is always `"blocked"` |

`agent_status:incomplete` is the raw event posted by the agent tool; it triggers `agent_escalated` as a side effect. Consumers monitoring agent health SHOULD watch `agent_escalated`, not `agent_status:incomplete`.

### 3.3 `bridge:*` — Hermes bridge subprocess events

**INVARIANT (binding contract):** Every `bridge:*` event MUST carry `data.agent` — a non-empty string naming the agent (e.g. `"web-dev.a"`, `"pm"`). If any `bridge:*` event is emitted without `data.agent`, that is a belayer bug. Consumers rely on this field for per-agent UI routing and MUST reject bridge events missing it.

| Type | Key `data` fields | Notes |
|------|-------------------|-------|
| `bridge:started` | `agent`, `hermes_session_id` (string, optional) | Agent bridge process started; Hermes session established |
| `bridge:finished` | `agent`; optional `reason` (string) OR `final_response` (string, ≤500 chars) | Bridge subprocess exited cleanly; daemon marks status `complete`. Known `reason` values: `"idle_timeout"` (idle-loop window elapsed with no peer activity), `"absolute_idle_ceiling"` (1hr hard failsafe, fires even if peers report running), `"escalate_to_human"` (supervisor invoked `belayer_escalate_to_human`), `"interrupted"` (SIGINT / no queued message after interrupt), `"stopped"` (explicit stop command). The success case carries `final_response` instead of `reason` (first 500 chars of the model's final output). Consumers MUST tolerate unknown `reason` values as opaque and MUST treat the event as a clean exit regardless of which field is present. |
| `bridge:failed` | `agent`, `error` (string, optional) | Bridge subprocess failed; daemon marks status `blocked` |
| `bridge:heartbeat` | `agent` | Periodic liveness signal from bridge; no side effects |
| `bridge:step_completed` | `agent` | Intermediate step within an agent turn |
| `bridge:tool_started` | `agent`, `tool` (string), `input_preview` (string, ≤200 chars); optional `path` (string, when the tool args carry a file path) | Tool invocation started |
| `bridge:tool_completed` | `agent`, `tool` (string), `duration_ms` (int), `result_preview` (string, ≤200 chars); optional `path` (string) | Tool invocation completed |
| `bridge:status_change` | `agent`, `status_type` (string) | Agent-reported status change |
| `bridge:turn_usage` | `agent`; any subset of: `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `reasoning_tokens`, `total_tokens`, `api_calls`, `estimated_cost_usd`, `cost_status` | Per-turn token usage; non-`agent` fields present only when the model returned them |
| `bridge:session_usage` | `agent`; any subset of: `session_total_tokens`, `session_input_tokens`, `session_output_tokens`, `session_cache_read_tokens`, `session_cache_write_tokens`, `session_reasoning_tokens`, `session_api_calls`, `session_estimated_cost_usd`, `session_cost_status` | Cumulative session usage emitted on agent exit |
| `bridge:clarification_needed` | `agent`, `question` (string) | Agent needs input; relayed to supervisor |
| `bridge:completion_requested` | `agent`, `summary` (string), `spec_artifact` (string, optional) | Supervisor signalled work complete; triggers PM auto-spawn |
| `bridge:completion_approved` | `agent`, `verification_report` (string) | PM approved; triggers `session_completed` |
| `bridge:completion_rejected` | `agent`, `verification_report` (string), `gap_list` (string) | PM rejected; gap list sent to supervisor for remediation |
| `bridge:agent_reasoning` | `agent`, `text` (string, untruncated), `turn` (int) | **Verbose-only.** Full extended-thinking text for the turn, flushed at turn end. Only emitted when the session's `log_level == "verbose"`. Consumers configured for `log_level == "standard"` MUST NOT expect these events |
| `bridge:agent_narration` | `agent`, `text` (string, untruncated), `already_streamed` (bool) | **Verbose-only.** Mid-turn visible assistant commentary (e.g. "Let me read that file first..."). `already_streamed: true` means the text was delivered token-by-token elsewhere and emitting it is duplicative for streaming UIs; `false` means this event is the only delivery. Only emitted when `log_level == "verbose"` |

`bridge:*` events are forwarded verbatim from the Python hermes_bridge subprocess. Additional fields may appear in `data`; consumers MUST ignore unknown fields.

**Verbose-only events.** `bridge:agent_reasoning` and `bridge:agent_narration` are gated at the bridge on the session's `log_level`. Standard runs emit no such events — consumers that rely on reasoning capture MUST verify the session was created with `log_level: "verbose"` (see Section 6.5).

### 3.4 `message_*` — Inter-agent messaging

| Type | Required `data` fields | Optional / Notes |
|------|------------------------|------------------|
| `message_sent` | `id`, `to`, `from`, `content`, `type`, `interrupt` (bool), `sent_at` (RFC3339) | Logged by send handler |
| `message_broadcast` | `from`, `content`, `type`, `sent_at` (RFC3339) | No `to`; broadcast to all subscribers |
| `message_delivered` | Broker `Message` fields (`session_id`, `sender_id`, `recipient_id`, `type`, `content`, `timestamp`) | Emitted by in-process broker on push delivery; bridge agents use pull-based delivery and this event may not fire for every delivered message |

### 3.5 `artifact_created`

| Required `data` fields | Optional |
|------------------------|----------|
| `kind` (string), `path` (string) | `producer` (string) |

`kind` values in v1: `"spec"`, `"design-doc"`, `"verification-report"`. The canonical spelling uses hyphens. Historical data may contain `"design_doc"` (underscore); consumers SHOULD normalize by replacing `_` with `-` in `kind` before comparison. Extension values are permitted; consumers MUST tolerate unknown kinds.

### 3.6 `tool_*` — Tool registration and execution

| Type | Key `data` fields |
|------|-------------------|
| `tool_registered` | `tool` (string), `target` (string) — `target` ∈ `{"agent","workbench","infra","host"}` |
| `tool_executed` | `tool`, `target`, `input` (object), `exit_code` (int), `duration_ms` (int), `output` (string, ≤4096 chars), `calling_agent` (string, optional), `timestamp` (RFC3339) |

### 3.7 `completion_escalated` / `completion_rejected` / `completion_approved_with_busy_agents` — PM gate flow

| Type | Required `data` fields |
|------|------------------------|
| `completion_rejected` | `rejected_by` (string), `cycle` (string in the exact form `"<attempt>/<max>"` where both are positive integers, e.g. `"1/3"`; regex `^\d+/\d+$`) |
| `completion_escalated` | `reason` (string), `rejections` (string) — emitted when `maxRejectionCycles` (3) is exceeded; session transitions to `needs_human_review` |
| `completion_approved_with_busy_agents` | `approved_by` (string), `busy_agents` (array of strings in the form `"<agent_name>=<status>"`, e.g. `["web-dev.a=running"]`) — emitted when PM approves completion while non-supervisor agents are still in `starting`/`running`/`pending_verification`; their in-flight work will be discarded by the subsequent bridge drain. Advisory event for post-mortems; does NOT transition session status. |

### 3.8 `pm_*` — PM agent errors

| Type | Required `data` fields |
|------|------------------------|
| `pm_spawn_failed` | `error` (string) |

### 3.9 `warning:*` — Advisory conditions

| Type | Required `data` fields |
|------|------------------------|
| `warning:supervisor_exited_early` | `active_agent` (string), `agent_status` (string) |

At most one `warning:supervisor_exited_early` event is emitted per session, for the first active specialist found.

### 3.10 `node_*` — Agent-run internal lifecycle

| Type | Typical `data` fields | Notes |
|------|----------------------|-------|
| `node_started` | `node` (string) | Emitted by agents via `POST /sessions/{id}/events` |
| `node_completed` | `node` (string) | Emitted by agents via `POST /sessions/{id}/events` |

Exact payload shape is agent-defined. Consumers SHOULD treat `data` as opaque beyond `node`.

### 3.11 `gate_scored`

Emitted by agents via `POST /sessions/{id}/events`. Payload is agent-defined. Consumers SHOULD treat `data` as opaque.

### 3.12 `agent_status:*`

Agent-posted events. `agent_status:incomplete` is the only type with daemon-side effects (see Section 3.2). Other subtypes are logged verbatim with no side effects.

### 3.13 `custom_event`

Extension point. Agents may post arbitrary events via `POST /sessions/{id}/events` with `type: "custom_event"`. No daemon-side effects. `data` is agent-defined.

---

## 4. Live Streaming — `GET /events/stream` (SSE)

### Query parameters

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `sessions` | comma-separated IDs | YES | Sessions to subscribe to. Empty string → HTTP 400 |
| `after` | int | NO | Cursor: deliver events with `id > after`. Omit for full backlog (see A0) |

### Cursor contract

**(A0) Default when `after` omitted:** full event backlog is delivered before live events. Consumers MUST NOT assume "stream from now" when `after` is absent.

**(A1) `after=<N>`:** returns events with `id > N`, across the entire `sessions=` subscription set, in global event-ID order. The cursor is a single monotonic integer — not per-session. **This is safe only for a fixed subscription set.** If a consumer reconnects with a superset `sessions=A,B,C` using the same `after` value, session C's events with `id ≤ after` are permanently unreachable via SSE — they must be fetched from the archive or via `GET /sessions/{id}/events?after=0`. **Recommended pattern: one SSE connection per session** for per-session gap-free resume.

### Frame taxonomy

**(A2) Domain frames** — one per SessionEvent. Format:
```
id: <event_id>
event: <type>
data: <json>

```
The `id:` line enables standard `Last-Event-ID` reconnect. `event:` matches `SessionEvent.Type`.

**(A3) Control frames** — not resumable; MUST NOT carry an `id:` line. This is a binding invariant: any future control-frame type added within v1 MUST also omit `id:` so that consumer `Last-Event-ID` cursors remain pure domain-event references and cannot be poisoned by a control frame that happens to land between domain events.

```
event: daemon_hello
data: {"daemon_instance_id":"<uuid>","schema_version":"belayer-log/v1","last_id":<int>}

```

```
event: daemon_draining
data: {"reason":"shutdown","timeout_ms":<int>}

```

`daemon_hello` is always the first frame on every new connection. `last_id` is the current max event ID in the store at connect time. Consumers MUST persist `last_id` from the hello frame (in addition to the `id:` value of every processed domain frame) — it is the recovery cursor used by `daemon_draining` (see below) and by reconnect-from-now flows. Consumers wanting "stream from now" MUST issue their next connect with `?after=<last_id>`.

`daemon_draining` is emitted once on graceful shutdown. The payload's `timeout_ms` is an upper bound on how long the daemon will stay up draining active work before TCP close; it is NOT a guarantee that the archive is on disk by that deadline. Consumers MUST immediately issue `GET /sessions/{id}/events?after=<last_id>` for active sessions in case TCP closes before the terminal SessionEvent arrives. The session archive (`GET /sessions/{id}/archive.ndjson`) is only available AFTER the terminal SessionEvent is written to the store, which may be at or after `daemon_draining`. If no archive materializes within a reasonable grace window (recommended: 2× `timeout_ms`), treat the session as truncated.

**Keepalive comments:** `: keep-alive\n\n` emitted every **15 seconds** to prevent idle-connection drops (source: `internal/daemon/daemon.go`, SSE handler). Consumers MUST tolerate SSE comments (lines starting with `:`).

**Unknown `event:` types:** consumers MUST ignore unrecognized control frame types. New control primitives WILL be added within v1 without a version bump.

### Reconnect strategy (normative recommendation)

- Use `Last-Event-ID` HTTP header first; fall back to explicit `?after=<last_seen_id>`.
- Exponential backoff: start 500ms, cap 30s.
- On `daemon_instance_id` change between connects: the daemon restarted. Flush resume cursors that depended on the old epoch. Resubscribe from `?after=0` (or from your own last archive cursor).

---

## 5. Archive Format — `<workspace>/.belayer/archive/<session-id>/`

Written when a session reaches terminal status while the daemon is alive.

### `events.ndjson`

One SessionEvent JSON object per line, in monotonic `id` order. Written atomically: the file is staged as `events.ndjson.tmp` then renamed. A consumer reading the archive directory MUST treat the file as either (a) complete, or (b) absent — never partially written.

### `transcripts/<agent>.jsonl` (optional; verbose sessions only)

For sessions created with `log_level: "verbose"`, the archive also contains a `transcripts/` subdirectory with one append-only JSONL file per agent (`transcripts/supervisor.jsonl`, `transcripts/web-dev.a.jsonl`, etc.). Each line is a JSON object:

```json
{"ts": <float unix>, "agent_id": "<string>", "kind": "reasoning", "turn": <int>, "text": "<untruncated>"}
{"ts": <float unix>, "agent_id": "<string>", "kind": "narration", "text": "<untruncated>", "already_streamed": <bool>}
```

Transcripts are the durable complement to `bridge:agent_reasoning` / `bridge:agent_narration` events and carry identical payloads. Consumers MUST tolerate `kind` values they don't recognise (future `kind: "tool_turn"` etc. may appear additively within v1). The file may be absent when the session was created with `log_level: "standard"`, when the daemon failed to allocate the transcript path at spawn, or when no turns have flushed yet — treat absence as "no verbose capture available" rather than an error. Consumers SHOULD cross-check `manifest.session.log_level` (see below) to disambiguate.

### `manifest.json`

```json
{
  "schema_version":     "belayer-log/v1",
  "daemon_instance_id": "<uuid>",
  "session": {
    "id":        "<string>",
    "name":      "<string>",
    "workspace": "<string>"
  },
  "agent_roster": [
    {"name": "<string>", "role": "<string>", "profile": "<string>"}
  ],
  "artifacts": [
    {"id": "<string>", "kind": "<string>", "path": "<string>"}
  ],
  "final_status":    "<complete|blocked|failed|cancelled|needs_human_review|stalled>",
  "event_count":     <int>,
  "first_event_id":  <int>,
  "last_event_id":   <int>,
  "archived_at":     "<RFC3339>",
  "partial":         false
}
```

- `daemon_instance_id` is the UUID of the daemon that archived the session. Lets consumers correlate an archive with the SSE epoch they observed.
- `event_count` is the number of JSON lines in `events.ndjson`.
- `first_event_id` / `last_event_id` are the monotonic IDs of the first and last lines. A consumer resuming from `last_event_id + 1` via SSE or `GET /sessions/.../events` MUST see no gap.
- `partial: true` means the daemon's drain timeout elapsed before all events were flushed. `events.ndjson` contains whatever was written before the timeout; `event_count`, `first_event_id`, and `last_event_id` describe the ACTUAL flushed contents (not the intended contents). Consumers MUST treat the session as truncated when `partial` is `true`; they MAY compare `last_event_id` against the latest `id:` they saw on SSE to measure the gap.

Unknown keys in `manifest.json` MUST be ignored. Additive keys may appear within v1.

### Archive HTTP endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/sessions/{id}/archive.ndjson` | Serves `events.ndjson` if the archive has been written. 404 otherwise. `Content-Type: application/x-ndjson` |
| `GET` | `/sessions/{id}/archive/manifest.json` | Serves `manifest.json` if the archive has been written. 404 otherwise. `Content-Type: application/json` |
| `GET` | `/sessions/{id}/archive.tar.gz` | Streams `events.ndjson` + `manifest.json` as a single gzip'd tarball. 404 if the archive has not been written |

All endpoints return `{"error":"<message>"}` on error with no stack trace. 404 when no archive exists (including while the session is still active). Consumers SHOULD prefer `archive.ndjson` for streaming consumption and `archive.tar.gz` for durable snapshots.

### Archive guarantee

An archive is written only for sessions that reached terminal status while the daemon was alive. If the daemon crashes mid-session, no archive is written. Consumers detect this via:
- Absence of `daemon_draining` before TCP close on the SSE stream.
- `daemon_instance_id` mismatch on reconnect (compare with `manifest.daemon_instance_id` if the archive eventually appears).
- 404 on `GET /sessions/{id}/archive.ndjson` after a reasonable grace window post-`daemon_draining`.

---

## 6. Historical Search — `GET /search`

All parameters are optional and combined with AND.

| Param | Type | Description |
|-------|------|-------------|
| `q` | string | FTS5 MATCH against `type` and `data` fields. Standard FTS5 grammar |
| `session` | string | Restrict to one session ID |
| `type_prefix` | string | Matches events where `type` starts with `<prefix>` (e.g. `bridge:` matches all bridge events) |
| `agent` | string | Restrict to events with `data.agent == <name>` |
| `after` | int | Include only events with `id > after` |
| `before` | int | Include only events with `id < before` |

**Guarantees and caps:**

- All predicates AND together regardless of query-string order.
- Result set capped at **1000 rows**. Paginate via `after=<last_id_seen>`.
- Query timeout: **2 seconds**. Longer queries → HTTP 504.
- `q` length cap: **4 KB**. Longer queries → HTTP 400.
- Malformed FTS5 syntax (unbalanced quotes, unbalanced parens, unsupported `NEAR` forms, empty column filters) → HTTP 400 with body `{"error":"<message>"}`. Never a 500 or stack trace.

**No-params behavior:** `GET /search` with no query string returns the most-recent 1000 events across all sessions in reverse-chronological order (`id DESC`). This is a valid query, not an error.

**Error-body shape:** all 4xx / 5xx responses carry a JSON body `{"error":"<clean human message>"}` with no stack trace, no internal field names, and no leaked SQL. Consumers MAY log the error string verbatim to operators.

**Note:** v1 daemon currently implements `q` only. `session`, `type_prefix`, `agent`, `after`, and `before` are specified here as the v1 contract and MUST be implemented before cragd integrates `/search`. Consumers MUST NOT assume these parameters are no-ops if absent from the query string.

---

## 6.5. Session Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/sessions` | List all sessions. Returns JSON array of session summaries (`id`, `name`, `status`, `created_at`, `updated_at`) |
| `GET` | `/sessions/{id}` | Get a single session summary plus agent roster. 404 if unknown |
| `GET` | `/sessions/{id}/events` | Query events for one session. Params: `after` (int, default 0), `before` (int, optional), `limit` (int, capped at 1000). Returns JSON array of SessionEvent |
| `POST` | `/sessions/{id}/events` | Agents post custom events. Body: `{type, data}`. `id`/`timestamp`/`session_id` are assigned by the daemon |

`GET /sessions/{id}/events` is the canonical "catch up on one session" endpoint. Use it:
- After receiving `daemon_hello` to fetch the full backlog for one session without relying on the multi-session SSE cursor.
- After `daemon_draining` + reconnect failure, to pull the tail.
- As a fallback when the archive is not yet present.

Error body shape matches Section 6: `{"error":"<message>"}` with no stack trace.

---

## 7. `GET /health`

```json
{
  "status":            "ok" | "draining",
  "schema_version":    "belayer-log/v1",
  "daemon_instance_id":"<uuid>",
  "draining":          <bool>,
  "capabilities": {
    "search_predicates":  ["q", "session", "type_prefix", "agent", "after", "before"],
    "archive_http":       true,
    "sse_control_frames": ["daemon_hello", "daemon_draining"]
  }
}
```

- `daemon_instance_id`: UUID generated at `daemon.New()`. Stable for the process lifetime. Compare across polls to detect daemon restart (epoch change).
- `draining: true` → HTTP 503. Otherwise HTTP 200.
- `capabilities.search_predicates`: the list of `/search` query params the running daemon honors. Consumers MUST consult this before relying on `session`, `type_prefix`, `agent`, `after`, or `before` — an older daemon may advertise only `["q"]` even though v1 contract names all six. Unrecognized params MUST be silently dropped by the daemon, so absence from `capabilities` means "do not rely on this predicate."
- `capabilities.archive_http`: `true` once `/sessions/{id}/archive.ndjson` and `/sessions/{id}/archive.tar.gz` are live. `false` or missing means consumers should only rely on SSE + `GET /sessions/{id}/events`.
- `capabilities.sse_control_frames`: control-frame types the daemon emits. Consumers MUST treat any frame with `event:` not in this list as ignorable (per the "ignore unknown control frames" rule in Section 4).
- Consumers MAY poll at 5-second intervals. No rate limit in v1.

---

## 8. Version Policy

- Schema tag is `belayer-log/v1`. **v1 is frozen.**
- Breaking changes ship as `belayer-log/v2` at new endpoints (e.g. `/v2/events/stream`). v1 endpoints MUST remain indefinitely for existing consumers.
- **Additive changes within v1:** new keys in `manifest.json`, new fields in event `data`, new control frame types — are permitted provided consumers are required (by this document) to ignore unknown keys and frame types. Both requirements are stated explicitly in Sections 4 and 5.
- The schema tag is the single version discriminant. Do not embed version in individual event payloads.

---

## 9. Worked Examples (Non-Normative)

### 9.1. SSE wire dump

A consumer connects: `GET /events/stream?sessions=9f2b4a11-...&after=0`

```
event: daemon_hello
data: {"daemon_instance_id":"3b1e5c08-4d2f-4e7b-8a9c-0a1b2c3d4e5f","schema_version":"belayer-log/v1","last_id":142}

id: 143
event: session_created
data: {"id":143,"session_id":"9f2b4a11-...","timestamp":"2026-04-17T12:34:56Z","type":"session_created","data":{"name":"build-feature-x"}}

id: 144
event: agent_spawned
data: {"id":144,"session_id":"9f2b4a11-...","timestamp":"2026-04-17T12:34:57Z","type":"agent_spawned","data":{"agent":"supervisor","role":"supervisor","profile":"default","transport":"bridge"}}

: keep-alive

id: 145
event: bridge:tool_started
data: {"id":145,"session_id":"9f2b4a11-...","timestamp":"2026-04-17T12:35:02Z","type":"bridge:tool_started","data":{"agent":"supervisor","tool":"Write","input_preview":"{'file_path': '/tmp/x.md', ...","path":"/tmp/x.md"}}

event: daemon_draining
data: {"reason":"shutdown","timeout_ms":30000}

```

Observations worth highlighting:
- `daemon_hello` is first, no `id:`.
- Domain frames carry `id:`.
- Comment keepalive `: keep-alive` interleaves without disrupting parsing.
- `daemon_draining` has no `id:` — if a consumer's SSE client auto-adopted it as `Last-Event-ID`, the next reconnect would skip ahead of real events. Consumers MUST filter control-event types out of their resume cursor.

### 9.2. Complete `manifest.json`

```json
{
  "schema_version":     "belayer-log/v1",
  "daemon_instance_id": "3b1e5c08-4d2f-4e7b-8a9c-0a1b2c3d4e5f",
  "session": {
    "id":        "9f2b4a11-7e3d-4c5a-b6f8-1234567890ab",
    "name":      "build-feature-x",
    "workspace": "/Users/operator/work/my-repo"
  },
  "agent_roster": [
    {"name": "supervisor",  "role": "supervisor", "profile": "default"},
    {"name": "web-dev.a",   "role": "implementer", "profile": "default"},
    {"name": "pm",          "role": "pm",          "profile": "default"}
  ],
  "artifacts": [
    {"id": "a1",  "kind": "spec",                "path": ".belayer/artifacts/9f2b4a11.../spec.md"},
    {"id": "a2",  "kind": "design-doc",          "path": ".belayer/artifacts/9f2b4a11.../design.md"},
    {"id": "a3",  "kind": "verification-report", "path": ".belayer/artifacts/9f2b4a11.../pm-report.md"}
  ],
  "final_status":    "complete",
  "event_count":     218,
  "first_event_id":  143,
  "last_event_id":   360,
  "archived_at":     "2026-04-17T13:12:44Z",
  "partial":         false
}
```

---

## 10. Consumer Best Practices (Non-Normative)

- Open one SSE connection per session for per-session gap-free resume. Multi-session connections cannot safely resume with a single cursor (see A1).
- Persist the last-seen event ID per session after every successfully processed frame. Write it durably before acting on the event.
- On reconnect: use `Last-Event-ID` header first; fall back to `?after=<id>`.
- On `daemon_instance_id` change: flush all resume cursors that depended on the old epoch. Resubscribe with `?after=0` or your last archived event ID.
- On `daemon_draining`: immediately issue `GET /sessions/{id}/events?after=<last_id>` for each active session. Do not wait for TCP close — drain may complete before the stream ends.
- Never assume event IDs are dense. Gaps are valid; process by `id` order, not by position in the array.
- On `partial: true` in archive manifest: surface the truncation to the operator. Do not silently treat the session as complete.
- Check `data.agent` is non-empty before routing any `bridge:*` event. Belayer guarantees this invariant; treat a missing `data.agent` as a belayer bug and emit an alert.
