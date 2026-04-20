# Observability Guide

Operator-facing guide for monitoring a Belayer session in real time and postmortem. For the full API contract see `docs/LOG_FORMAT.md`.

---

## Channels

Belayer exposes three observability channels per session. Choose based on what you need:

| Channel | What it captures | Cardinality | Retention | Latency |
|---------|-----------------|-------------|-----------|---------|
| **Events** (`/events/stream`, `/sessions/{id}/events`) | Structured, typed SessionEvent rows: session/agent lifecycle, tool calls, messages, artifacts | One row per meaningful action; bounded at standard tier | Persisted in SQLite for daemon lifetime; archived to `events.ndjson` at session end | Near-zero — emitted at occurrence |
| **Transcripts** (`/sessions/{id}/transcripts/{agent}`) | Untruncated agent reasoning (chain-of-thought) and narration, one JSONL file per agent | One record per model turn; verbose+ only | Append-only file per agent; archived alongside events | One file flush per turn |
| **Traces** (`/sessions/{id}/traces`, `/trace/{agent}/{frag}`) | Untruncated tool payloads, pre/post file snapshots, subprocess exec details | One record per tool invocation; trace tier only | Spilled to zstd fragment files; up to 128 MB per fragment before rotation | Spill on event insert |

**Pick the channel that matches your use case:**
- Dashboard / status panel → Events at `standard` tier.
- Debug agent reasoning → Transcripts at `verbose` tier.
- Reproduce an exact tool call byte-for-byte → Traces at `trace` tier.

---

## Tier Selection

Tiers gate what gets captured. Higher tiers include everything from lower tiers plus more detail. Set once at session creation via `log_level:` in the POST /sessions body or `.belayer/config.yaml`.

```
standard  ←  default; session/agent lifecycle, tool call summaries, messages, artifacts
verbose   ←  standard + bridge tool details, agent reasoning & narration, bridge stdout
trace     ←  verbose + untruncated full_input/full_result, fs_snapshot, subprocess_exec, spill
```

**Decision tree — what do you need to debug?**

- "Did the session complete? Which agents ran?" → `standard`
- "What did the agent say/think during this turn?" → `verbose`
- "What exact bytes did this tool receive and return?" → `trace`
- "What file did the agent read before making that edit?" → `trace`
- "What subprocess did the agent spawn and with what env?" → `trace`

**Cost caution:** trace tier can produce megabytes of spill data per session on busy agents. Use it for targeted debugging, not as a default. Check `manifest.trace_bytes` in the archive to measure actual spill size.

---

## Recipes

### 1. Tail one agent's events

Server-side filter keeps the stream lean:

```bash
SID="<session-id>"
SOCK="$HOME/.belayer/daemon.sock"

belayer logs "$SID" -f --agent pm
# or equivalently via curl:
curl -s --unix-socket "$SOCK" \
  "http+unix/events/stream?sessions=$SID&agent=pm&tier=standard"
```

### 2. Grep tool calls

Find all `edit_file` invocations and their duration:

```bash
curl -s --unix-socket "$SOCK" \
  "http+unix/sessions/$SID/tool-calls" \
  | jq '.[] | select(.tool == "edit_file") | {agent, duration_ms, status}'
```

Or stream compact TSV to awk for large result sets:

```bash
curl -s --unix-socket "$SOCK" \
  "http+unix/sessions/$SID/events?format=compact" \
  | awk -F'\t' '$3 == "bridge:tool_completed" && $4 ~ /edit_file/'
```

### 3. Archive postmortem

After a stall or unexpected exit:

```bash
ARCHIVE="$HOME/.belayer/archive/$SID"

# 1. Check what tier was running and spill volume
jq '{log_level: .session.log_level, trace_count, trace_bytes, final_status}' \
  "$ARCHIVE/manifest.json"

# 2. Browse the event log
grep -m 20 '"type":"bridge:tool' "$ARCHIVE/events.ndjson" | jq .

# 3. If trace tier: list fragment files
curl -s --unix-socket "$SOCK" \
  "http+unix/sessions/$SID/traces" | jq '.'

# 4. Fetch a spilled tool payload (use trace_offset/trace_length from SessionEvent)
curl -s --unix-socket "$SOCK" \
  "http+unix/sessions/$SID/trace/supervisor/0001?offset=0&length=16384"

# 5. Read agent reasoning from transcript
tail -20 "$ARCHIVE/transcripts/supervisor.jsonl" | jq '.text'
```

### 4. Dashboard setup

Capability-negotiate first, then subscribe with bounded cost:

```bash
# 1. Check what the daemon supports
curl -s --unix-socket "$SOCK" "http+unix/health" | jq '.capabilities'

# 2. If sse_filters and session_digest_control_frame are true:
curl -s --unix-socket "$SOCK" \
  "http+unix/events/stream?sessions=$SID&tier=standard&digest=1"
# session_digest frames (every 60s or 50 events) carry agent activity + phase
# without needing to parse every event
```

---

## Dashboard Integration Notes

### Capability Negotiation

Always call `GET /health` before using any feature introduced after the initial release. The `capabilities` block (documented in `docs/LOG_FORMAT.md` §7) is the authoritative feature manifest. Key booleans to check:

| Capability | Feature |
|-----------|---------|
| `sse_filters` | `?agent=`, `?type_prefix=`, `?tier=`, `?digest=` SSE query params |
| `cursor_reader_id` | `?since=<id>` server-side cursor |
| `compact_tsv` | `?format=compact` TSV output |
| `aggregates` | `/outline`, `/tool-calls`, `/conversation`, `/phase` |
| `transcripts` | Transcript endpoints (verbose+) |
| `traces` | Trace endpoints (trace tier) |
| `session_digest_control_frame` | `session_digest` SSE control frames |

### TCP Setup for Remote Dashboards

By default the daemon binds only to a Unix socket at `~/.belayer/daemon.sock`. To expose over TCP:

```bash
belayer daemon \
  --bind 127.0.0.1:7523 \
  --cors-origin https://my-dashboard.internal \
  --auth-token "$BELAYER_API_TOKEN"
```

- `--bind` (alias `--tcp-addr`) enables the TCP listener at the given address.
- `--cors-origin` allowlists an Origin header value for browser dashboards. Requests from non-allowlisted origins are rejected with HTTP 403.
- `--auth-token` requires all TCP requests (except `GET /health`) to carry `Authorization: Bearer <token>`. If `--bind` is set without `--auth-token`, the daemon auto-generates a token and logs it to stderr.

### Token Handling

Pass the token via environment variable on the dashboard side:

```bash
export BELAYER_API_TOKEN="$(cat ~/.belayer.env | grep BELAYER_API_TOKEN | cut -d= -f2)"
```

Never commit tokens. Never hardcode them in dashboard config files. The Unix socket path does not require a token (local trust model). Use the Unix socket when the dashboard runs on the same host.
