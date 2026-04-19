# Observability: Log Tiers, Unified CLI, and Dashboard API — Implementation Plan

> **Status**: Active | **Created**: 2026-04-19 | **Last Updated**: 2026-04-19 (Phase 3 complete, pending codex review)
> **Design Doc**: `docs/design-docs/2026-04-19-observability-log-tiers-and-api-design.md`
> **Consulted Learnings**: None (LEARNINGS.md not present)
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-04-19 | Design | Three-tier monotonic log model (standard⊂verbose⊂trace) | Separates operator scan-cost from debugger fat-payload need; immutable tier avoids retro-capture gaps |
| 2026-04-19 | Design | Single `belayer logs` command (consolidate `watch`) | Two commands with overlapping semantics was the observed CLI pain |
| 2026-04-19 | Design | Bridge stdout file always-on, NOT an event type | File captures bridge crashes before HTTP round-trip; mirroring into DB would duplicate without value |
| 2026-04-19 | Design | 64 KB inline / spill threshold per event; per-agent fragments rotated at 128 MB; zstd on exit | Keeps SQLite small, bounds per-fragment cost, keeps live tier cheap |
| 2026-04-19 | Design | LLM request/response capture deferred upstream to Hermes | Belayer's callback surface doesn't expose it; file issue on Hermes |
| 2026-04-19 | Design | Per-reader server-side cursors via `reader_cursors` table, 24 h TTL | Lets agents skip UUID bookkeeping on repeat polls |
| 2026-04-19 | Design | Additive schema in `belayer-log/v1` (no v2 bump) | Nothing consumes the contract tag yet; freely updatable |
| 2026-04-19 | Design | Delete `SANDBOXING.md`, replace with `DEPLOYMENT.md` | v7 assumes Belayer is sandboxed from outside; clamshell doc is misleading default |

## Progress

- [x] Phase 1 — Schema + `--log-level trace` + session propagation _(completed 2026-04-19)_
  - [x] Task 1.1: Accept `trace` in ValidateLogLevel _(commit caa7d26)_
  - [x] Task 1.2: trace_* columns + reader_cursors table _(commit b0597fb)_
  - [x] Task 1.3: resolveRunLogLevel + env fallback _(commit 4c5b1d2)_
  - [x] Task 1.4+1.5: /health log_levels + status LOG column _(commit 73044cb)_
- [x] Phase 2 — Bridge stdout file + `/bridges/*` HTTP + `--raw` CLI _(completed 2026-04-19)_
  - [x] Task 2.1: bridgelog package (writer + rotate) _(commits a66e524, 08cc336)_
  - [x] Task 2.2: wire stdout/stderr rotation + race guards _(commits a7dd329, 951351c, b800775, 76610c2, 657ef18)_
  - [x] Task 2.3: GET /sessions/{id}/bridges + /stdout tail/follow _(commits f7512fa, 13fbac6, 0cfd765)_
  - [x] Task 2.4: belayer logs --raw --agent tails bridge stdout _(commits 10dbad1, 5f58c56)_
  - [x] Task 2.5: belayer bridges tail shorthand _(commit 0f374e7)_
- [x] Phase 3 — Trace writer, spill, `full_input`/`full_result`, `trace:fs_snapshot`, `trace:subprocess_exec` _(pending codex review)_
  - [x] Task 3.1: internal/trace/ writer pkg (Fragment, Append, rotate, zstd) _(commit 1ce45f3)_
  - [x] Task 3.2: Store.InsertEventWithSpill + handleLogEvent scrub/spill/truncate + BELAYER_LOG_LEVEL env _(commit 35727fe)_
  - [x] Task 3.3 + 3.4 + 3.5: callbacks.py full_input/full_result, fs_snapshot pre/post, subprocess_exec + env allowlist _(commit b503834)_
  - [x] Task 3.6: daemon/redact.go Scrub() with keyRegex/openaiRegex/bearerRegex _(commit faac269)_
  - [x] Task 3.7: E2E trace session + /sessions/{id}/trace/{agent}/{fragment} slice reader _(commit 640a54a)_
- [ ] Phase 4 — Aggregate endpoints + response headers + compact format + per-reader cursor
- [ ] Phase 5 — SSE filter params + `session_digest` frame
- [ ] Phase 6 — TCP bind + bearer auth + CORS
- [ ] Phase 7 — Artifact + transcript + trace HTTP endpoints
- [ ] Phase 8 — Deprecation removal + doc finalization (LOG_FORMAT rewrite, OBSERVABILITY, DEPLOYMENT, SANDBOXING delete)

## Surprises & Discoveries

| 2026-04-19 | Task 2.2 | Codex review surfaced 4 successive blockers (rotation race, Stop-can-hang, global shutdown scope, tombstone never cleared). Each was real; fixes landed in 4 follow-up commits. | Tombstone is an acceptable state; residual risk noted if future code path calls `store.UpdateSessionStatus(running)` directly (not current). |
| 2026-04-19 | Task 3.2 | Existing `testDaemon` constructor builds `Daemon` by direct struct literal, leaving `traceWriter` nil. `handleLogEvent` gained a nil-guard that falls through to truncation path. | Acceptable for unit-test scaffolding; real daemons always go through `New()` which initialises the writer. Flagged for Phase 4 when aggregate-endpoint tests need trace capture. |
| 2026-04-19 | Tasks 3.1-3.6 | Executed in parallel via 3 background sonnet subagents after user directive "kick off any parallel phases that you can". File-scope partitioning prevented merge conflicts: Agent A (3.2) → internal/store, internal/daemon/daemon.go, internal/bridge, internal/daemon/agents.go; Agent B (3.3-5) → hermes_bridge/*.py; Agent C (3.6) → internal/daemon/redact*. Agent C scoped intentionally to create-only so Agent A owned all bridge_events.go edits. | Confirmed intra-phase parallelism safe when file partition is strict; worth applying to Phases 4/5/7 where endpoints cluster by file. |
| 2026-04-19 | Task 3.7 | Fragment filenames use `.jsonl` / `.jsonl.zst` extensions — plan assumed bare `0001` paths. Slice handler resolves by stripping both `.zst` and `.jsonl` from input then normalising to 4-digit padded form. | Matches writer's real on-disk layout. Slice URL accepts raw int, padded, or either suffix. |
| 2026-04-19 | Task 3.7 | `testDaemon` fixtures constructed `Daemon` by direct struct literal with empty `Config{}`, so deriving `traceBase` from `filepath.Dir(cfg.DBPath)` resolved to `.` — broke E2E tests. Promoted `traceBase` to an explicit `Daemon` struct field populated in `New()` so fixtures can set it directly without threading DBPath. | Cleaner separation: handler reads from a known field rather than recomputing from config. No runtime impact. |

## Plan Drift

| Task 2.2 | Plan wrote `.belayer/sessions/<id>/bridge.<agent>.log` (flat per-session). | Kept existing `.belayer/runs/<session>/<agent>/bridge-stdout.log` + `bridge-stderr.log` (nested per-agent) layout. | Codebase already had this layout; migrating paths was out of scope and would have broken archiver and transcript logic. |

---

# Plan Detail

**Goal:** Unified, tiered observability for Belayer sessions — one CLI, one HTTP surface, three data tiers (standard/verbose/trace), spill-on-size trace writer, aggregate endpoints for agents, SSE filters + session digests for dashboards, always-on bridge stdout capture.

**Architecture:** Extend `internal/store/` with additive columns + `reader_cursors` table. Add `internal/trace/` package for per-agent spill fragment writer. Add `internal/daemon/bridgelog/` for always-on stdout capture. Extend `internal/daemon/daemon.go` HTTP mux with new endpoints (aggregate, transcripts, traces, bridges, artifacts-get), SSE filter query params, response headers. Rewire `hermes_bridge/callbacks.py` to emit full payloads at trace tier and fire new `trace:fs_snapshot` / `trace:subprocess_exec` events. Consolidate `internal/cli/session.go logs/watch` into one command backed by the HTTP API.

**Tech Stack:** Go (net/http, modernc.org/sqlite, klauspost/compress/zstd), Python 3.12 (hermes_bridge), existing cobra CLI, existing SSE implementation.

**Ground rules for every phase:**
- TDD. Failing test first, minimal code, passing test, commit.
- Exact file paths in each task.
- Every task ends in a commit.
- Phases are additive. No phase breaks an earlier phase's contract.
- Subagent model: `model: "sonnet"` for implementation tasks; `opus` only when a task explicitly requests design judgment.

---

## Phase 1 — Schema and `--log-level trace` propagation

**Goal:** Add `trace` as a valid tier, migrate tables, wire config/flag/env resolution, surface in /health and session APIs. No behavior change beyond accepting the value.

### Task 1.1: Accept `trace` in ValidateLogLevel

**Files:**
- Modify: `internal/daemon/loglevel.go`
- Modify: `internal/daemon/loglevel_test.go`

- [ ] **Step 1: Add failing test for `trace`**

Append to `internal/daemon/loglevel_test.go`:

```go
func TestValidateLogLevel_Trace(t *testing.T) {
    got, err := ValidateLogLevel("trace")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if got != "trace" {
        t.Fatalf("want %q, got %q", "trace", got)
    }
}
```

- [ ] **Step 2: Run — expect FAIL**

`go test ./internal/daemon -run TestValidateLogLevel_Trace -v` → `invalid log_level "trace"`.

- [ ] **Step 3: Implement**

`internal/daemon/loglevel.go`:

```go
const LogLevelTrace = "trace"
```

Switch case:

```go
case LogLevelStandard, LogLevelVerbose, LogLevelTrace:
    return s, nil
default:
    return "", fmt.Errorf("invalid log_level %q: must be one of [standard, verbose, trace]", s)
```

- [ ] **Step 4: Run — expect PASS**

`go test ./internal/daemon -run TestValidateLogLevel -v`.

- [ ] **Step 5: Commit**

```
feat(daemon): accept trace tier in ValidateLogLevel
```

### Task 1.2: Add `trace_file` / `trace_offset` / `trace_length` columns + `reader_cursors` table

**Files:**
- Modify: `internal/store/migrations.go`
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Failing test**

In `internal/store/store_test.go`:

```go
func TestMigrate_TraceColumnsAndReaderCursors(t *testing.T) {
    s, err := Open(":memory:")
    if err != nil { t.Fatal(err) }
    defer s.Close()
    rows, err := s.DB().Query("PRAGMA table_info(events)")
    if err != nil { t.Fatal(err) }
    defer rows.Close()
    cols := map[string]bool{}
    for rows.Next() {
        var cid int; var name, typ string; var notnull, pk int; var dflt sql.NullString
        if err := rows.Scan(&cid,&name,&typ,&notnull,&dflt,&pk); err != nil { t.Fatal(err) }
        cols[name] = true
    }
    for _, c := range []string{"trace_file","trace_offset","trace_length"} {
        if !cols[c] { t.Errorf("events missing column %s", c) }
    }
    if _, err := s.DB().Exec(`INSERT INTO reader_cursors(reader_id,session_id,last_id,updated_at) VALUES('r','s',0,CURRENT_TIMESTAMP)`); err != nil {
        t.Fatalf("reader_cursors insert: %v", err)
    }
}
```

Expose `DB()` on Store if absent (add thin accessor for tests only).

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement migration**

Append three `addColumnIfNotExists` calls for `events`:

```go
if err := addColumnIfNotExists(db, "events", "trace_file", "TEXT"); err != nil { return err }
if err := addColumnIfNotExists(db, "events", "trace_offset", "INTEGER"); err != nil { return err }
if err := addColumnIfNotExists(db, "events", "trace_length", "INTEGER"); err != nil { return err }
```

Append CREATE TABLE to `stmts`:

```sql
CREATE TABLE IF NOT EXISTS reader_cursors (
    reader_id  TEXT NOT NULL,
    session_id TEXT NOT NULL,
    last_id    INTEGER NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (reader_id, session_id)
)
```

- [ ] **Step 4: Run — expect PASS**

`go test ./internal/store -run TestMigrate_TraceColumnsAndReaderCursors -v`.

- [ ] **Step 5: Commit**

```
feat(store): add trace_* columns on events and reader_cursors table
```

### Task 1.3: Plumb `--log-level trace` through CLI + env + config

**Files:**
- Modify: `internal/cli/run.go`
- Modify: `internal/cli/init.go` (ensure default config template mentions trace)
- Modify: `internal/cli/nightshift_test.go` or new integration test confirming trace flag accepted

- [ ] **Step 1: Failing test for `run start --log-level trace`**

New file `internal/cli/run_test.go` (or extend existing):

```go
func TestRunStart_AcceptsTraceLogLevel(t *testing.T) {
    cmd := newRootCmd()
    cmd.SetArgs([]string{"run","start","--log-level","trace","--dry-run"})
    // --dry-run should print the body it would send; assert "trace" appears.
    var buf bytes.Buffer
    cmd.SetOut(&buf); cmd.SetErr(&buf)
    if err := cmd.Execute(); err != nil { t.Fatalf("exec: %v", err) }
    if !strings.Contains(buf.String(), `"log_level":"trace"`) {
        t.Fatalf("expected trace log_level in request body; got:\n%s", buf.String())
    }
}
```

(If `--dry-run` doesn't exist, add it as a 2-line flag that prints the JSON body instead of POSTing.)

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

In `internal/cli/run.go`:

- Add `logLevel string` flag on the subcommand: `cmd.Flags().StringVar(&logLevel, "log-level", "", "Log tier: standard|verbose|trace")`.
- Resolution order: flag → `BELAYER_LOG_LEVEL` → `.belayer/config.yaml log_level:` → `""` (daemon default).
- Include in the `POST /sessions` body as `log_level`.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(cli): --log-level flag on belayer run start
```

### Task 1.4: Surface `log_level` in `/sessions/{id}` + `/sessions` list + `belayer status`

**Files:**
- Modify: `internal/daemon/daemon.go` (`handleGetSession`, `handleListSessions` — include LogLevel in JSON)
- Modify: `internal/cli/session.go` (`newStatusCmd` — add LOG column)
- Modify: `internal/cli/client.go` (session DTO includes LogLevel if missing)

- [ ] **Step 1: Failing test**

`internal/daemon/daemon_test.go`:

```go
func TestGetSession_IncludesLogLevel(t *testing.T) {
    d,cleanup := newTestDaemon(t); defer cleanup()
    id := createSession(t,d,"s1","trace")
    resp := httpGET(t,d,"/sessions/"+id)
    var body map[string]any
    if err := json.NewDecoder(resp.Body).Decode(&body); err != nil { t.Fatal(err) }
    if body["log_level"] != "trace" {
        t.Fatalf("want trace, got %v", body["log_level"])
    }
}
```

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

Ensure the session marshal struct in `daemon.go` has `LogLevel string \`json:"log_level"\``. Same in the CLI DTO.

`belayer status` tab header: `ID\tNAME\tSTATUS\tTEMPLATE\tLOG` and row adds `s.LogLevel`.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(api,cli): surface log_level in session GET/list and status command
```

### Task 1.5: Expose `log_level` in /health capabilities + archive manifest

**Files:**
- Modify: `internal/daemon/health_test.go` or `daemon.go handleHealth`
- Modify: `internal/daemon/archive_manager.go` (manifest session block)
- Modify: `internal/daemon/archive_manager_test.go`

- [ ] **Step 1: Failing tests**

Health test asserts capabilities includes `"log_levels":["standard","verbose","trace"]`.
Archive test creates a `trace` session, archives, reads manifest.json, asserts `session.log_level == "trace"`.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

Add to `/health` response `capabilities.log_levels = []string{"standard","verbose","trace"}`.
Include `LogLevel` in archive manifest session block (JSON key `log_level`).

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(api): advertise log_levels in /health and archive manifest
```

---

## Phase 2 — Bridge stdout file + `/bridges/*` HTTP + `--raw` CLI

**Goal:** Capture every bridge subprocess's stdout/stderr to a per-agent file, rotate at daemon restart, expose via HTTP, add `belayer logs --raw --agent <name>` shorthand.

### Task 2.1: `internal/daemon/bridgelog/` package — append/rotate writer

**Files:**
- Create: `internal/daemon/bridgelog/writer.go`
- Create: `internal/daemon/bridgelog/writer_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestWriter_AppendsToFile(t *testing.T) {
    dir := t.TempDir()
    w, err := New(filepath.Join(dir,"bridge.sup.log"))
    if err != nil { t.Fatal(err) }
    defer w.Close()
    n,err := w.Write([]byte("hello\n")); if err!=nil||n!=6 { t.Fatalf("write: %d %v",n,err) }
    b,_ := os.ReadFile(filepath.Join(dir,"bridge.sup.log"))
    if string(b) != "hello\n" { t.Fatalf("got %q", b) }
}

func TestRotate_KeepsLastN(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir,"bridge.sup.log")
    for i:=0;i<5;i++ {
        w,_ := New(path); w.Write([]byte("x")); w.Close()
        Rotate(path, 3)
    }
    entries,_ := os.ReadDir(dir)
    if len(entries) > 4 { // .log + .log.1..3
        t.Fatalf("want ≤4, got %d", len(entries))
    }
}
```

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

`writer.go`:

```go
package bridgelog

import (
    "fmt"
    "io"
    "os"
    "path/filepath"
    "sync"
)

type Writer struct {
    mu sync.Mutex
    f  *os.File
}

func New(path string) (*Writer, error) {
    if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { return nil, err }
    f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
    if err != nil { return nil, err }
    return &Writer{f: f}, nil
}

func (w *Writer) Write(p []byte) (int, error) {
    w.mu.Lock(); defer w.mu.Unlock()
    return w.f.Write(p)
}

func (w *Writer) Close() error {
    w.mu.Lock(); defer w.mu.Unlock()
    return w.f.Close()
}

// Rotate renames path → path.1 → path.2 … up to keep, drops the oldest.
func Rotate(path string, keep int) error {
    for i := keep; i >= 1; i-- {
        src := fmt.Sprintf("%s.%d", path, i-1)
        if i-1 == 0 { src = path }
        dst := fmt.Sprintf("%s.%d", path, i)
        if _, err := os.Stat(src); err == nil {
            if i == keep { os.Remove(dst) }
            if err := os.Rename(src, dst); err != nil { return err }
        }
    }
    return nil
}

var _ io.WriteCloser = (*Writer)(nil)
```

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(daemon): bridgelog package — per-agent stdout writer with rotation
```

### Task 2.2: Wire bridge subprocess stdout/stderr to the writer

**Files:**
- Modify: `internal/bridge/bridge.go` — accept a `io.Writer` for combined stdout+stderr
- Modify: `internal/daemon/daemon.go` — when spawning a bridge, create `sessions/<id>/bridge.<agent>.log` and pass the writer
- Modify: `internal/daemon/bridge_integration_test.go`

- [ ] **Step 1: Failing integration test**

Extend `bridge_integration_test.go` with a test that spawns a bridge and asserts `.belayer/sessions/<id>/bridge.<agent>.log` exists and contains non-empty bytes after the bridge runs.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

In `bridge.go`, change the `exec.Cmd` construction: set `cmd.Stdout = writer; cmd.Stderr = writer` (tee to daemon log still optional for debug).

In daemon spawn path: build `logPath := filepath.Join(workspaceRoot, ".belayer","sessions", sessionID, "bridge."+agentName+".log")`, call `bridgelog.Rotate(logPath, 3)` before opening, then `bridgelog.New(logPath)`, pass as writer.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(daemon): capture bridge stdout+stderr to per-agent file
```

### Task 2.3: `GET /sessions/{id}/bridges` and `GET /sessions/{id}/bridges/{agent}/stdout`

**Files:**
- Modify: `internal/daemon/daemon.go`
- Create: `internal/daemon/bridges_http.go` (handler methods)
- Create: `internal/daemon/bridges_http_test.go`

- [ ] **Step 1: Failing tests**

List endpoint returns `[]` for no agents; one entry per created log file. Stdout endpoint serves file bytes. `?follow=1` upgrades to a chunked stream that sees appended bytes. `?tail=<bytes>` returns last N bytes.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

Two handlers:

```go
func (d *Daemon) handleListBridges(w http.ResponseWriter, r *http.Request) {
    sid := r.PathValue("id")
    dir := filepath.Join(d.workspaceRoot(), ".belayer","sessions", sid)
    entries, _ := os.ReadDir(dir)
    out := []map[string]any{}
    for _, e := range entries {
        if !strings.HasPrefix(e.Name(), "bridge.") || !strings.HasSuffix(e.Name(), ".log") { continue }
        info, _ := e.Info()
        name := strings.TrimSuffix(strings.TrimPrefix(e.Name(),"bridge."),".log")
        out = append(out, map[string]any{
            "agent": name,
            "log_path": filepath.Join(dir, e.Name()),
            "size": info.Size(),
            "rotated_at": info.ModTime(),
        })
    }
    writeJSON(w, 200, out)
}

func (d *Daemon) handleBridgeStdout(w http.ResponseWriter, r *http.Request) {
    sid := r.PathValue("id"); agent := r.PathValue("agent")
    p := filepath.Join(d.workspaceRoot(), ".belayer","sessions", sid, "bridge."+agent+".log")
    q := r.URL.Query()
    w.Header().Set("Content-Type","text/plain; charset=utf-8")
    if q.Get("follow") == "1" {
        streamFileAppend(r.Context(), w, p, parseInt(q.Get("after_byte")))
        return
    }
    if tail := q.Get("tail"); tail != "" {
        serveFileTail(w, p, parseInt(tail)); return
    }
    http.ServeFile(w, r, p)
}
```

Register routes next to existing archive routes.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(api): GET /sessions/{id}/bridges and /bridges/{agent}/stdout
```

### Task 2.4: `belayer logs --raw --agent <name>` tails bridge file

**Files:**
- Modify: `internal/cli/session.go` — `newLogsCmd`
- Modify: `internal/cli/client.go` — add `BridgeStdoutStream(ctx, sessionID, agent, afterByte)` helper
- Modify: `internal/cli/session_test.go`

- [ ] **Step 1: Failing test**

CLI test: invoke `logs --raw --agent supervisor sess-id -f`, write content to file under test, assert CLI emits the same bytes.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

Extend `logsCmd` flags with `raw bool` and `agent string`. When `raw`, branch: require `agent`; call new client helper, copy bytes to `cmd.OutOrStdout()`.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(cli): belayer logs --raw --agent tails bridge stdout file
```

### Task 2.5: `belayer bridges tail <session> <agent>` shorthand

**Files:**
- Create: `internal/cli/bridges.go`
- Modify: `internal/cli/root.go`
- Create: `internal/cli/bridges_test.go`

- [ ] **Step 1: Failing test**

Invoke `belayer bridges tail s1 supervisor -f`, assert forwards to the same bytes.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

Thin wrapper that calls the same client helper as 2.4 with `follow=true`.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(cli): belayer bridges tail shorthand
```

---

## Phase 3 — Trace writer, spill, `full_input`/`full_result`, `trace:fs_snapshot`, `trace:subprocess_exec`

**Goal:** At trace tier, capture untruncated tool payloads, pre/post file snapshots, subprocess exec events. Payloads ≥64 KB spill to per-agent fragment files rotated at 128 MB, zstd-compressed on rotate/exit.

### Task 3.1: `internal/trace/` package — per-agent spill fragment writer

**Files:**
- Create: `internal/trace/writer.go`
- Create: `internal/trace/writer_test.go`

Interface:

```go
type Fragment struct {
    Path   string
    Offset int64
    Length int64
}

type Writer interface {
    Append(sessionID, agentName string, payload []byte) (Fragment, error)
    CloseAgent(sessionID, agentName string) error // compresses current fragment
    Close() error
}
```

- [ ] **Step 1: Failing tests**

- `TestAppend_WritesAtOffset` — first append offset=0, second offset=len(first)+1 (newline).
- `TestRotate_At128MB` — when current fragment size ≥ threshold, next append opens fragment N+1 and returns its path.
- `TestCloseAgent_CompressesFragments` — after CloseAgent, previous `.jsonl` files replaced by `.jsonl.zst`.
- `TestCrashRecovery_TruncatesPartialTail` — open writer against a file whose last bytes have no terminating `\n`, expect truncation back to last newline.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

Key points:
- Per `(session, agent)` pair: a `*agentFragment` with `currentPath, currentFile, size, fragmentIndex`.
- Append appends `payload\n`, returns `Fragment{Path:currentPath, Offset:offsetBeforeWrite, Length:len(payload)}`.
- On `size >= 128*1024*1024`, close current, kick off zstd compression in a goroutine, open next fragment.
- `CloseAgent` closes + compresses current.
- Crash recovery on first Append: if file exists, seek end, scan backward for last `\n`, truncate.

Use `github.com/klauspost/compress/zstd` for compression.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(trace): per-agent spill fragment writer with rotation and zstd
```

### Task 3.2: Spill branch in event insert path

**Files:**
- Modify: `internal/store/store.go` — extend `InsertEvent` (or add `InsertEventWithSpill`) to accept optional `Fragment`
- Modify: `internal/daemon/bridge_events.go` — when tier=trace and serialized data ≥64 KB, call `traceWriter.Append`, set placeholder data, store fragment refs
- Modify: `internal/daemon/bridge_events_test.go`

- [ ] **Step 1: Failing test**

At trace tier, POST `/sessions/{id}/events` with a 70 KB data blob. Assert:
- DB row's `data` is the placeholder `{"agent":"...","_trace":true}`.
- `trace_file`, `trace_offset`, `trace_length` populated.
- Fragment file on disk contains the full blob at the given offset.

At standard tier, same input truncates to 64 KB plus suffix; no spill.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

In the event handler, after validating body:

```go
tier := session.LogLevel
raw := body.Data // already a string
if len(raw) >= 65536 {
    switch tier {
    case "trace":
        frag, err := d.traceWriter.Append(sid, agent, []byte(raw))
        if err != nil { /* log, fall through to truncate */ }
        placeholder := fmt.Sprintf(`{"agent":%q,"_trace":true}`, agent)
        err := d.store.InsertEventWithSpill(sid, body.Type, placeholder, frag)
        ...
    default:
        raw = raw[:65536] + `…(truncated; upgrade to trace tier to capture)`
    }
}
```

Add `InsertEventWithSpill` to store that sets `trace_file/offset/length` columns when Fragment is non-zero.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(daemon): spill ≥64 KB event payloads to trace fragments at trace tier
```

### Task 3.3: Bridge callbacks emit `full_input`/`full_result` at trace tier

**Files:**
- Modify: `hermes_bridge/callbacks.py`
- Modify: `hermes_bridge/callbacks_test.py` (create if absent)

- [ ] **Step 1: Failing test**

```python
def test_tool_start_emits_full_input_at_trace(monkeypatch, tmp_path):
    posted = []
    def fake_post(sock, sid, aid, typ, data=None):
        posted.append((typ, data))
    monkeypatch.setattr(callbacks, "post_event", fake_post)
    cbs = callbacks.make_callbacks("sup","sess","/tmp/sock", log_level="trace")
    cbs["tool_start_callback"]("call-1", "Write", {"path":"/tmp/x","content":"A"*70000})
    t, d = posted[-1]
    assert t == "bridge:tool_started"
    assert "full_input" in d
    assert len(d["full_input"]["content"]) == 70000
```

And the inverse at standard: no `full_input` key.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

Add `log_level: str = "standard"` kwarg to `make_callbacks`. Thread through daemon → bridge spawn via env var `BELAYER_LOG_LEVEL` (daemon already knows session log_level).

In `tool_start_callback`:

```python
event_data = {"tool": tool_name, "input_preview": str(tool_args)[:200]}
if log_level == "trace":
    event_data["full_input"] = tool_args
```

Same shape in `tool_complete_callback` → adds `full_result`.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(bridge): emit full_input/full_result on tool events at trace tier
```

### Task 3.4: `trace:fs_snapshot` pre/post on Write/Edit/NotebookEdit

**Files:**
- Modify: `hermes_bridge/callbacks.py`
- Modify: `hermes_bridge/callbacks_test.py`

- [ ] **Step 1: Failing test**

Two events posted around a `Write` call at trace tier, with `phase:"before"` and `phase:"after"`, correct sha256, correct `size_bytes`, `exists` set from pre-state.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

```python
_MUTATING_TOOLS = frozenset({"Write","Edit","NotebookEdit","write_file","edit_file","create_file"})

def _fs_snapshot(path, phase):
    try:
        with open(path, "rb") as f: data = f.read()
        return {"phase":phase,"path":path,"exists":True,"size_bytes":len(data),
                "sha256":hashlib.sha256(data).hexdigest(),"content":data.decode("utf-8","replace")}
    except FileNotFoundError:
        return {"phase":phase,"path":path,"exists":False,"size_bytes":0,"sha256":"","content":""}
```

Wrap `tool_start_callback` / `tool_complete_callback` — if tier=trace and tool in `_MUTATING_TOOLS` and path present, emit `trace:fs_snapshot` before/after.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(bridge): emit trace:fs_snapshot around mutating tools at trace tier
```

### Task 3.5: `trace:subprocess_exec` on Bash tool

**Files:**
- Modify: `hermes_bridge/callbacks.py`
- Modify: `hermes_bridge/callbacks_test.py`

- [ ] **Step 1: Failing test**

At trace tier, after Bash tool completes, assert one `trace:subprocess_exec` event with `cmd`, `exit_code`, `stdout`, `stderr`, `env_subset` containing only allowlisted keys.

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

Env allowlist constant:

```python
_ENV_ALLOWLIST = {"PATH","PWD","HOME","USER","LANG","TERM","NODE_ENV","CI"}

def _filtered_env():
    out = {k:v for k,v in os.environ.items() if k in _ENV_ALLOWLIST or k.startswith("BELAYER_")}
    return out
```

In `tool_complete_callback`, when tool in `{"Bash","run_shell","bash"}` and tier=trace, emit `trace:subprocess_exec` using tool_args / tool_result contents.

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(bridge): emit trace:subprocess_exec on shell tool completion at trace tier
```

### Task 3.6: Redaction scrubber on trace writes

**Files:**
- Create: `internal/daemon/redact.go`
- Create: `internal/daemon/redact_test.go`
- Modify: `internal/daemon/bridge_events.go` — call `redact.Scrub(data)` before spill or inline insert when tier=trace

- [ ] **Step 1: Failing tests**

```go
func TestScrub_ReplacesAuthKeys(t *testing.T) {
    in := `{"api_key":"sk-abc123def456ghi789","authorization":"Bearer xyz","other":"value"}`
    got := Scrub(in)
    if strings.Contains(got, "sk-abc123def456ghi789") { t.Fatal("api key leaked") }
    if strings.Contains(got, "Bearer xyz") { t.Fatal("bearer leaked") }
    if !strings.Contains(got, `"other":"value"`) { t.Fatal("non-secret field removed") }
}
```

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

```go
var keyRegex = regexp.MustCompile(`(?i)"(api[_-]?key|authorization|bearer|password|secret|token|credential)"\s*:\s*"[^"]*"`)
var openaiRegex = regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)
var bearerRegex = regexp.MustCompile(`Bearer [A-Za-z0-9._-]+`)

func Scrub(s string) string {
    s = keyRegex.ReplaceAllStringFunc(s, func(m string) string {
        i := strings.Index(m, ":"); return m[:i+1] + `"<redacted>"`
    })
    s = openaiRegex.ReplaceAllString(s, "<redacted>")
    s = bearerRegex.ReplaceAllString(s, "Bearer <redacted>")
    return s
}
```

- [ ] **Step 4: Run — expect PASS**

- [ ] **Step 5: Commit**

```
feat(daemon): redact auth/secret fields before writing trace payloads
```

### Task 3.7: Integration — 9-agent trace session E2E

**Files:**
- Create: `internal/daemon/trace_e2e_test.go`

- [ ] **Step 1: Failing test**

Spin test daemon, create trace session, post 5 synthetic large tool events. Assert:
- fragment file exists and contains all five payloads.
- each event row has correct trace_file/offset/length triple.
- GET `/sessions/{id}/trace/{agent}/0001?offset=O&length=L` returns exact payload.
- At standard tier, same volume produces no fragments.

- [ ] **Step 2–5: implement handler for trace slice + pass + commit**

`/sessions/{id}/trace/{agent}/{fragment}?offset=N&length=M`: read file range, decompress transparently if `.zst`. 404 if tier < trace.

```
feat(api,daemon): /sessions/{id}/trace/{agent}/{fragment} slice reader + E2E
```

---

## Phase 4 — Aggregate endpoints + response headers + compact format + per-reader cursor

### Task 4.1: `/sessions/{id}/outline`

**Files:**
- Create: `internal/daemon/outline.go`
- Create: `internal/daemon/outline_test.go`
- Modify: `daemon.go` register route

- [ ] Five steps per TDD. Compute `{session, agents:[{name,role,status,last_activity,tool_calls,tokens,cost}], artifacts, phases, final_status}` from a single `events` + `agent_runs` + `artifacts` scan. Target <1 KB JSON for a typical 9-agent session.

Commit: `feat(api): /sessions/{id}/outline aggregate`.

### Task 4.2: `/sessions/{id}/tools`

Compute from `bridge:tool_started`/`bridge:tool_completed` events, time-ordered. Include `{agent,tool,path,duration_ms,status,at,id}`.

Commit: `feat(api): /sessions/{id}/tools aggregate`.

### Task 4.3: `/sessions/{id}/conversation?between=a,b|agent=a`

Scan `messages` table filtered, return ordered array.

Commit: `feat(api): /sessions/{id}/conversation aggregate`.

### Task 4.4: `/sessions/{id}/phase`

Derive `{phase, since}` from most recent `agent_status:*` / `session_*` events by mapping table: `*:discovering → discover`, `*:planning → plan`, `*:implementing → implement`, etc.

Commit: `feat(api): /sessions/{id}/phase derived phase`.

### Task 4.5: Response headers on every event-returning endpoint

**Files:**
- Create: `internal/daemon/response_headers.go`
- Modify: handlers for `/events`, `/events/stream`, `/search`, `/outline`, `/tools`, `/conversation`, `/phase`

Helper:

```go
func (d *Daemon) writeEventHeaders(w http.ResponseWriter, sessionID string, count int) {
    s, _ := d.store.GetSession(sessionID)
    w.Header().Set("X-Belayer-Schema","belayer-log/v1")
    lastID, _ := d.store.LastEventID(sessionID)
    w.Header().Set("X-Last-Event-Id", strconv.FormatInt(lastID,10))
    w.Header().Set("X-Event-Count", strconv.Itoa(count))
    w.Header().Set("X-Session-Status", s.Status)
    w.Header().Set("X-Log-Level", s.LogLevel)
    roster := d.rosterCSV(sessionID)
    w.Header().Set("X-Agent-Roster", roster)
}
```

Wire before writing body.

Commit: `feat(api): X-Belayer-Schema, X-Last-Event-Id, X-Session-Status, X-Log-Level, X-Agent-Roster headers`.

### Task 4.6: `?format=compact` on events + search

Emit one line per event: `id\tagent\ttype\tsummary\n`. Escape `\n` and `\t` inside summary to `\\n` and `\\t`. Content-Type `text/tab-separated-values`.

Commit: `feat(api): compact TSV format for event endpoints`.

### Task 4.7: `?since=<reader_id>` per-reader cursor

**Files:**
- Modify: `internal/store/store.go` — `LookupCursor / UpdateCursor`
- Modify: `internal/daemon/daemon.go handleGetEvents`
- Modify: `internal/daemon/daemon.go` — GC sweep at start + hourly

- [ ] TDD: repeat call with same `since=r1` sees only new events. 24 h TTL expires → fresh backlog.

Commit: `feat(api): per-reader cursor via ?since=<reader_id> with 24h TTL`.

### Task 4.8: `Link: rel="next"` pagination header

Emit `Link: <…?after=N>; rel="next"` when returned page ≥ limit.

Commit: `feat(api): Link rel=next pagination header on /events`.

---

## Phase 5 — SSE filter params + `session_digest` frame

### Task 5.1: `?agent=`, `?type_prefix=`, `?tier=` filters on `/events/stream`

**Files:**
- Modify: `internal/daemon/daemon.go handleStreamEvents`
- Modify: `internal/daemon/sse_test.go`

- [ ] TDD: subscribe with `agent=backend-dev`, assert supervisor events filtered out. Same for `type_prefix=bridge:`.

Commit: `feat(api): SSE filter params agent, type_prefix, tier`.

### Task 5.2: `session_digest` control frame

**Files:**
- Modify: `internal/daemon/daemon.go` — SSE loop emits every 60 s or 50 events whichever first
- Modify: `internal/daemon/sse_test.go`

Control frame shape:

```
event: session_digest
data: {"at":143,"agents":{...},"phase":"implement"}
```

No `id:` line (control frame invariant).

Suppressible via `?digest=0`.

Commit: `feat(api): SSE session_digest control frame`.

---

## Phase 6 — TCP bind + bearer auth + CORS

### Task 6.1: `--bind <addr:port>` opt-in TCP listener

**Files:**
- Modify: `internal/cli/daemon.go` — flags `--bind`, `--auth-token`, `--cors-origin` (repeatable)
- Modify: `internal/daemon/daemon.go` — honor `TCPAddr` (already present), add middleware

- [ ] TDD: daemon starts with `--bind 127.0.0.1:0`, record chosen port from listener, `GET /health` via TCP returns 200 without auth.

Commit: `feat(daemon): --bind opt-in TCP listener`.

### Task 6.2: Bearer token auth on TCP

**Files:**
- Create: `internal/daemon/auth.go`
- Modify: `daemon.go` — wrap TCP mux with auth middleware, skip `/health`

- [ ] TDD: unauthenticated TCP `GET /sessions` → 401; with `Authorization: Bearer <token>` → 200.

Token generated via `crypto/rand` 32 bytes base64url when `--auth-token=""`. Printed once to stdout:

```
[belayer] TCP listener at 127.0.0.1:7523, token: AbC...xYz (keep this secret)
```

Commit: `feat(daemon): bearer token auth on TCP listener`.

### Task 6.3: CORS origin allowlist

- [ ] TDD: preflight `OPTIONS` with allowed origin returns 200 + `Access-Control-Allow-Origin: <origin>`. Disallowed origin → 403. Never `*`. Credentials header disallowed.

Commit: `feat(daemon): --cors-origin allowlist on TCP listener`.

---

## Phase 7 — Artifact + transcript + trace HTTP endpoints

### Task 7.1: `GET /sessions/{id}/artifacts/{artifact_id}` raw bytes

- [ ] TDD: upload artifact via existing POST, GET returns file bytes with correct Content-Type (inferred from extension, fallback `application/octet-stream`). `Content-Disposition: inline` for `text/*`/`image/*`/`application/json`, else `attachment`.

Commit: `feat(api): GET /sessions/{id}/artifacts/{id} serves artifact bytes`.

### Task 7.2: `GET /sessions/{id}/transcripts` and `/transcripts/{agent}.jsonl`

**Files:**
- Create: `internal/daemon/transcripts_http.go`

- [ ] TDD:
  - 404 if session `log_level < verbose`.
  - List returns `[{agent,path,size,updated_at}]`.
  - `?follow=1` streams appended bytes.
  - `?tail=<bytes>` returns last N.

Commit: `feat(api): /sessions/{id}/transcripts list + stream`.

### Task 7.3: `GET /sessions/{id}/traces` list + slice reader hardening

Implemented the slice reader in 3.7; add the list endpoint.

- [ ] TDD: list returns `[{agent,fragment,path,size,compressed,updated_at}]`. 404 if `log_level<trace`.

Commit: `feat(api): /sessions/{id}/traces list`.

### Task 7.4: `belayer artifact get <session> <id>` CLI

**Files:**
- Modify: `internal/cli/artifact.go`
- Create: `internal/cli/artifact_test.go` additions

- [ ] TDD: invoking the command writes bytes to stdout or `-o <path>`.

Commit: `feat(cli): belayer artifact get downloads artifact`.

---

## Phase 8 — Deprecation + documentation finalization

### Task 8.1: Consolidate `belayer logs` into SSE-backed single command

**Files:**
- Modify: `internal/cli/session.go`
- Modify: `internal/cli/session_test.go`

- [ ] TDD:
  - `logs <sess>` with no `-f` backfills and exits.
  - `logs <sess> -f` backfills then subscribes to SSE (replaces the historical `watch`).
  - Session resolution: omitted + 1 running → target; omitted + 0 or ≥2 running → error; UUID prefix → lookup.
  - `--agent`, `--type`, `--tier` server-side filters propagate to the SSE subscribe URL.
  - `--format {pretty,ndjson,json}` honored.
  - `--tail N` honored.
  - `--no-color` auto when non-TTY.
  - `belayer watch` still works, prints stderr deprecation notice.

- [ ] Implement — delete duplicated path from `newWatchCmd` and point its RunE to the new logs command. Add a `--since <dur>` flag that converts to `--after` semantics.

- [ ] Commit:

```
refactor(cli): consolidate `belayer logs` and `belayer watch` into one command
```

### Task 8.2: `belayer init` scaffolds `.belayer/config.yaml` with `log_level` + `.gitignore` for `.belayer/`

**Files:**
- Modify: `internal/cli/init.go`
- Modify: `internal/cli/init_test.go`

- [ ] TDD: after init, `.belayer/config.yaml` contains `log_level: standard`, and `.gitignore` in workspace root contains `.belayer/`.

Commit: `feat(cli): belayer init writes default log_level config and gitignore entry`.

### Task 8.3: Help `Channels:` footnote on every command

**Files:**
- Modify: `internal/cli/root.go` — shared long-description footer

- [ ] TDD: golden-string test that `logs --help`, `bridges --help`, `daemon --help` all contain the `Channels:` block verbatim.

Commit: `docs(cli): add Channels footnote to command help`.

### Task 8.4: Rewrite `docs/LOG_FORMAT.md`

**Files:**
- Modify: `docs/LOG_FORMAT.md`

- [ ] No test — documentation task. Apply the §6.1 changes from the spec verbatim:
  - §2 add `trace_file/trace_offset/trace_length` optional SessionEvent fields + inline-vs-spill semantics.
  - §3 modify `bridge:tool_started/completed` entries with trace-only fields; new §3.14 `trace:fs_snapshot` + `trace:subprocess_exec`; new §3.15 prefix recipes.
  - §4 document `agent`, `type_prefix`, `tier`, `digest` SSE query params + `session_digest` control frame in A3.
  - §5 document `archive/<session>/{transcripts,traces,bridges,artifacts}/`; extend manifest.json with `log_level`, `trace_count`, `trace_bytes`.
  - §6.5 new aggregate endpoints.
  - New §6.6 response headers, §6.7 per-reader cursor, §6.8 compact TSV.
  - §7 capabilities block.
  - §9 worked examples.
  - §10 preference for `/outline` + `/tools` + `/conversation` over raw events.

Commit: `docs(log-format): document tier model, trace fields, aggregates, cursors, compact format`.

### Task 8.5: New `docs/OBSERVABILITY.md`

**Files:**
- Create: `docs/OBSERVABILITY.md`

Contents: three-channels table, tier selection decision tree, recipes (tail one agent, grep tool calls, archive postmortem), dashboard integration notes (capability negotiation, TCP setup, token handling).

Cross-link from `CLAUDE.md`, `LOG_FORMAT.md`, `logs --help`.

Commit: `docs: add OBSERVABILITY.md operator guide`.

### Task 8.6: New `docs/DEPLOYMENT.md` replacing `docs/SANDBOXING.md`

**Files:**
- Create: `docs/DEPLOYMENT.md`
- Delete: `docs/SANDBOXING.md`
- Modify: `CLAUDE.md` — swap SANDBOXING → DEPLOYMENT reference
- Modify: `docs/PHILOSOPHY.md` — §3 Sandbox addendum: "Belayer does not impose isolation; the outer deployment topology does."

Contents (~60 lines):
- Statement: Belayer is the control plane for ONE run. No isolation boundary.
- Topologies table: host-native (dev), Nightshift worker (prod), clamshell (legacy).
- Trust model: working directory is trusted; trace captures file content; outer sandbox is the boundary.
- Credentials: `~/.belayer.env` + `.belayer/.belayer.env`, workspace wins, redaction best-effort.
- Ports/sockets: unix always; TCP opt-in with token + CORS; bridge stdout at `sessions/<id>/bridge.<agent>.log`.

Commit: `docs: replace SANDBOXING.md with DEPLOYMENT.md`.

### Task 8.7: Remove `belayer watch` alias (one release after 8.1)

- [ ] Delete the deprecated alias, any tests asserting its existence, and the stderr deprecation notice.

Commit: `chore(cli): remove deprecated belayer watch alias`.

### Task 8.8: Update `CLAUDE.md`

**Files:**
- Modify: `CLAUDE.md`

- Extend CLI surface section with new flags and commands.
- Add bullet: "Log tiers: standard/verbose/trace — see `docs/LOG_FORMAT.md`."
- Replace `docs/SANDBOXING.md` reference with `docs/DEPLOYMENT.md`.
- Add `docs/OBSERVABILITY.md` to the docs list.

Commit: `docs: update CLAUDE.md with observability surface and DEPLOYMENT doc`.

### Task 8.9: File upstream issue for `trace:llm_turn`

- [ ] Not a code task. Open a Hermes issue asking for a request/response hook surface. Link the design doc. Track in `docs/design-docs/index.md` open-questions section. Done when issue URL is recorded in the retro.

---

## Load + contract tests (run once at end)

Run these before declaring the plan complete:

- 9-agent trace-tier session for 30 min. Assert: spill triggers, fragments rotate, SQLite size bounded, SSE drain backlog <1 min.
- Same session at standard tier — event volume ≤ 20 % of trace.
- Golden NDJSON fixtures per tier (regenerated under `testdata/observability/{standard,verbose,trace}.ndjson`).
- `/health` capability snapshot exactly matches `docs/LOG_FORMAT.md` §7.

Commit: `test: 9-agent trace load + golden fixtures + capabilities snapshot`.

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
-

**What didn't:**
-

**Learnings to codify:**
-
