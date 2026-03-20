# v2 Channels + Hooks — Phase 1

> **Status**: Completed | **Created**: 2026-03-20 | **Last Updated**: 2026-03-20
> **Design Doc**: `docs/designs/v2-channels-integration.md`
> **For Claude:** Use /harness:orchestrate to execute this plan.

## Decision Log

| Date | Phase | Decision | Rationale |
|------|-------|----------|-----------|
| 2026-03-20 | Design | Channels IN + Hooks OUT + CLI flare/fail | Prompt primary, hooks insurance, channels for push events |
| 2026-03-20 | Design | Thin Bun channel adapter, no business logic | All routing/state stays in Go worker |
| 2026-03-20 | Design | Deterministic port assignment via env var | Simpler than file-based port discovery |
| 2026-03-20 | Design | Phase 1 only — prove reliability before Phase 2 MCP tools | Codex review finding: overbuilt for first cut |

## Progress

- [x] Task 1: Belayer MCP channel server (Bun/TypeScript)
- [x] Task 2: Hook scripts (Stop insurance + Notification routing)
- [ ] Task 3: Channel-aware session spawner (deferred — needs testing with real --channels flag)
- [x] Task 4: Temporal workflow — push pipeline events via HTTP
- [ ] Task 5: Observer session spawn in `belayer run` (deferred — needs Task 3)
- [x] Task 6: CLI idempotency for finish/flare/fail
- [x] Task 7: Tests + verification

## Surprises & Discoveries

_None yet._

## Plan Drift

_None yet._

---

### Task 1: Belayer MCP channel server (Bun/TypeScript)

**Goal:** Create a thin MCP channel server that receives HTTP POSTs from the Temporal worker and pushes them into the Claude Code session as channel events.

**Files:**
- `channel/channel.ts` (new — the MCP server)
- `channel/package.json` (new — Bun dependencies)
- `.mcp.json` (new — register the channel server)

**Steps:**

1. Create `channel/` directory at repo root.

2. Create `channel/package.json`:
   ```json
   { "dependencies": { "@modelcontextprotocol/sdk": "latest" } }
   ```
   Run `cd channel && bun install`.

3. Create `channel/channel.ts`:
   - MCP Server with `claude/channel` capability
   - `instructions` field tells Claude about pipeline events and how to interpret them
   - HTTP listener on `BELAYER_CHANNEL_PORT` (default: 8790)
   - Binds to `127.0.0.1` only
   - POSTed JSON body → `mcp.notification()` with content + meta

4. Create `.mcp.json` at repo root for development testing:
   ```json
   {
     "mcpServers": {
       "belayer-channel": {
         "command": "bun",
         "args": ["./channel/channel.ts"],
         "env": { "BELAYER_CHANNEL_PORT": "8790" }
       }
     }
   }
   ```

**Tests:** Manual — start Claude Code with `--dangerously-load-development-channels server:belayer-channel`, POST a test event via curl.

---

### Task 2: Hook scripts (Stop insurance + Notification routing)

**Goal:** Create hook scripts that provide deterministic outbound signaling.

**Files:**
- `channel/hooks/stop-insurance.sh` (new)
- `channel/hooks/notification-router.sh` (new)
- `channel/hooks/session-start.sh` (new)

**Steps:**

1. Create `channel/hooks/stop-insurance.sh`:
   - Reads `BELAYER_TASK_ID`, `BELAYER_ROLE`, `BELAYER_REPO` from env
   - Calls `belayer <role> finish --task-id <id> [--repo <repo>]`
   - Since finish is idempotent, this is always safe
   - Exit 0 (never block the stop)

2. Create `channel/hooks/notification-router.sh`:
   - Reads notification_type from stdin JSON
   - If `permission_prompt`: POST to observer's channel port (`BELAYER_OBSERVER_PORT`)
   - Exit 0

3. Create `channel/hooks/session-start.sh`:
   - Writes BELAYER_TASK_ID, BELAYER_ROLE, BELAYER_REPO to `$CLAUDE_ENV_FILE`
   - Exit 0

**Tests:** Unit test each script with mock JSON input piped to stdin.

---

### Task 3: Channel-aware session spawner

**Goal:** Update the session spawner to start Claude Code with `--channels` and configure hooks.

**Files:**
- `internal/v2/provider/session.go` (modify)
- `internal/v2/provider/session_test.go` (modify)

**Steps:**

1. Add `ChannelPort int` and `ObserverPort int` to `SessionOpts`.

2. Update `ClaudeSessionSpawner.Spawn` to include:
   - `--channels server:belayer-channel` (when channel is available)
   - `--dangerously-load-development-channels server:belayer-channel` (during research preview)
   - Set env vars: `BELAYER_CHANNEL_PORT`, `BELAYER_TASK_ID`, `BELAYER_ROLE`, `BELAYER_REPO`, `BELAYER_OBSERVER_PORT`

3. Update the tmux command construction to include these flags and env vars.

4. Keep backward compatibility: if `ChannelPort == 0`, spawn without channels (TmuxCLI fallback path).

**Tests:**
- Spawn with ChannelPort → command includes --channels flag
- Spawn without ChannelPort → original behavior (no channels)
- Env vars set correctly in the tmux command

---

### Task 4: Temporal workflow — push pipeline events via HTTP

**Goal:** Add HTTP push calls to the workflow at key lifecycle points.

**Files:**
- `internal/v2/temporal/events.go` (new — event types + push logic)
- `internal/v2/temporal/workflow.go` (modify — add event pushes)
- `internal/v2/temporal/fanout.go` (modify — add repo completion events)

**Steps:**

1. Define pipeline event types:
   ```go
   type PipelineEvent struct {
       Event   string            `json:"event"`
       Content string            `json:"content"`
       Meta    map[string]string `json:"meta"`
   }
   ```

2. Create `PushEventActivity` — an activity that HTTP POSTs to a channel port:
   ```go
   func (a *Activities) PushEventActivity(ctx context.Context, input PushEventInput) error {
       resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d", input.Port), "application/json", ...)
       // ...
   }
   ```

3. Add event pushes to RouteWorkflow at:
   - Pipeline started (→ observer)
   - Phase started (→ observer)
   - Role completed (→ observer + dependent workers)
   - Risk gate triggered (→ observer)
   - Flare received (→ observer)
   - Pipeline completed (→ observer)

4. Add event push to fan-out when a repo's lead finishes (→ dependent repos' workers).

**Tests:** Temporal workflow test with mock PushEventActivity verifying correct events at each lifecycle point.

---

### Task 5: Observer session spawn in `belayer run`

**Goal:** `belayer run` opens a Claude Code observer session in the user's terminal.

**Files:**
- `internal/v2/cli/run.go` (modify — spawn observer before starting workflow)

**Steps:**

1. Before starting the Temporal workflow:
   - Assign observer port (8790)
   - Write `.mcp.json` to a temp dir with the belayer-channel server configured
   - Start Claude Code in the foreground (not tmux) with:
     - `--channels server:belayer-channel` (or dev flag)
     - `--append-system-prompt "You are the belayer observer..."`
     - Initial prompt: "Pipeline starting for: {description}"

2. The observer session blocks the terminal. Worker sessions spawn in tmux behind the scenes.

3. When the observer session exits, the pipeline continues (events go to void, but Temporal still tracks state).

**Note:** This changes `belayer run` from a fire-and-forget command to an interactive session. The old behavior (fire-and-forget) moves to `belayer run --background` or `belayer run --detach`.

**Tests:** Build verification. Manual E2E test.

---

### Task 6: CLI idempotency for finish/flare/fail

**Goal:** Make `belayer <role> finish` idempotent — second call is a no-op.

**Files:**
- `internal/v2/cli/role_signal.go` (modify)
- `internal/v2/temporal/workflow.go` (modify — handle duplicate signals)

**Steps:**

1. In the workflow: when a finish signal arrives for a role+repo that already completed, log and ignore (no error, no re-advance).

2. In the CLI: if the Temporal Signal call returns "workflow not found" or "workflow completed", print "already completed" instead of an error.

3. The Stop hook calls finish regardless — idempotency makes this safe.

**Tests:**
- Workflow test: send finish twice → second is no-op, workflow still completes normally
- CLI test: signal to completed workflow → no error

---

### Task 7: Tests + verification

**Goal:** Verify everything compiles, tests pass, and the E2E flow works.

**Steps:**

1. `go build ./...`
2. `go test ./internal/v2/... -count=1`
3. `cd channel && bun install` (verify Bun dependencies)
4. Manual: `belayer run "test"` with Temporal running → observer opens, events arrive

---

## Outcomes & Retrospective

_Filled by /harness:complete when work is done._

**What worked:**
- Thin Bun channel adapter pattern — ~60 lines of TypeScript, no business logic
- Hook scripts are simple bash with env var contracts — easy to test and modify
- PipelineEvent constructors keep event creation clean and consistent in the workflow
- CLI idempotency via error string matching is pragmatic for Temporal's error types

**What didn't:**
- `bun install` from a Bash tool changes CWD unexpectedly — need absolute paths
- Tasks 3+5 (channel-aware spawner + observer) need real `--channels` flag testing before implementation

**Learnings to codify:**
- MCP channel servers in Bun are thin — keep all routing/state in Go
- Stop hooks with idempotent CLI = belt AND suspenders for session completion
- Pipeline events are best-effort (HTTP POST, silent on failure) — workflow state is in Temporal, not in events
