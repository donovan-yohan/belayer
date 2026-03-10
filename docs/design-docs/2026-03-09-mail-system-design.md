# Mail System Design

Beads-backed inter-agent messaging for belayer. Enables the setter, leads, spotters, and anchors to communicate through a unified mail system, replacing signal files (DONE.json, SPOT.json, VERDICT.json) with typed messages.

## Context

Belayer agents run as interactive Claude Code sessions in tmux. Today, orchestration signals are one-directional: the setter spawns agents with GOAL.json, agents write signal files, the setter polls for them. There is no mechanism for the setter to send messages to running agents, for agents to signal completion through a shared system, or for idle agents to pick up new work.

Gastown solves this with a three-layer system (beads mail + nudge queue + hooks). We take a simpler approach: beads for storage, tmux send-keys for delivery, triggered synchronously at send time.

## Design Decisions

1. **Beads as storage backend** — persistent, queryable, audit trail via git history. One beads database per instance at `~/.belayer/instances/<name>/mail/`.
2. **Sender-driven delivery** — `belayer message` writes to beads AND delivers via tmux send-keys in one atomic operation. No watcher daemon.
3. **All agents use `belayer message`** — unified CLI for sending and reading. Replaces DONE.json/SPOT.json/VERDICT.json signal files.
4. **Environment-based identity** — `BELAYER_MAIL_ADDRESS` env var set in tmux at spawn time. CLI commands derive identity from it automatically.
5. **Deterministic tmux naming** — no session registry needed. Addresses map to tmux targets by convention.
6. **Templates at send time** — message type selects a template that prepends actionable instructions to the body before writing.
7. **Send-keys only for MVP** — no hook-based cooperative delivery. Future enhancement when Claude Code hooks are needed or Codex support is added.

## Architecture

```
belayer message <address> --type <type> --body "..."
    │
    ├─ 1. Load template for --type, render with body
    ├─ 2. Write beads issue (bd create with labels)
    └─ 3. Deliver via tmux send-keys to target session
          "You have a new message. Run 'belayer mail read' to see it."

belayer mail read
    │
    ├─ 1. Read BELAYER_MAIL_ADDRESS from env
    ├─ 2. Query beads: bd list --label "to:<address>" (open issues)
    ├─ 3. Print each message body (already template-rendered)
    └─ 4. Close the beads issues (marks as read)
```

## Beads Database

Initialized per instance:

```
~/.belayer/instances/<name>/mail/
  .beads/          # beads database (dolt-backed)
  .git/            # auto-created by bd init --stealth
```

### Issue-to-Message Mapping

| Beads field | Mail concept |
|-------------|-------------|
| Title | Subject line |
| Description | Template-rendered body |
| Labels | Routing metadata |
| Status | open = unread, closed = read/processed |
| Priority | P1 = urgent, P2 = normal |

### Label Schema

```
to:<address>              # recipient
from:<address>            # sender (auto-populated from BELAYER_MAIL_ADDRESS)
msg-type:<type>           # message type enum
```

## Addresses

Addresses are path-like strings that double as identifiers and map deterministically to tmux targets:

| Address | Tmux session | Tmux window |
|---------|-------------|-------------|
| `setter` | `belayer-setter` | `0` |
| `task/<id>/lead/<repo>/<goal>` | `belayer-task-<id>` | `<repo>-<goal>` |
| `task/<id>/spotter/<repo>/<goal>` | `belayer-task-<id>` | `<repo>-<goal>` |
| `task/<id>/anchor` | `belayer-task-<id>` | `anchor` |

No registry file needed — the mapping is a pure function of the address.

## CLI Commands

### `belayer message` — Send a message

```bash
# Inline body
belayer message <address> --type <type> --body "..."

# Body from file
belayer message <address> --type <type> --file ./payload.json

# Body from stdin
cat payload.json | belayer message <address> --type <type> --stdin

# Optional subject override (templates provide defaults)
belayer message <address> --type <type> --body "..." --subject "Custom subject"
```

Flags `--body`, `--file`, `--stdin` are mutually exclusive. The `from` field is auto-populated from `BELAYER_MAIL_ADDRESS`.

### `belayer mail read` — Read your inbox

```bash
belayer mail read          # read and mark all unread messages
belayer mail inbox         # list unread without marking
belayer mail ack <msg-id>  # manually mark one message as read
```

Uses `BELAYER_MAIL_ADDRESS` env var to determine which mailbox to query.

## Message Types

| Type | Direction | Purpose | Replaces |
|------|-----------|---------|----------|
| `goal_assignment` | setter → lead | New or updated work | GOAL.json spawn |
| `done` | lead → setter | Goal completion signal | DONE.json |
| `spot_result` | spotter → setter | Validation result | SPOT.json |
| `verdict` | anchor → setter | Alignment result | VERDICT.json |
| `feedback` | setter → lead | Spotter/anchor feedback for retry | SpotterFeedback in GOAL.json |
| `instruction` | user/setter → any | Ad-hoc command or info | N/A |

## Message Templates

Embedded in `internal/defaults/mail/` via `embed.FS`. Each template prepends actionable instructions to the raw body.

Example `feedback.md.tmpl`:

```
FEEDBACK FROM SPOTTER

Address the following feedback, then signal completion:
  belayer message setter --type done --body '{"status":"complete","summary":"<your summary>"}'

---

{{.Body}}
```

Example `goal_assignment.md.tmpl`:

```
NEW GOAL ASSIGNED

Read .lead/GOAL.json for full context. Begin working on your assignment.
When complete, signal done:
  belayer message setter --type done --body '{"status":"complete","summary":"<your summary>"}'

---

{{.Body}}
```

Templates are applied at send time. The beads issue description contains the final rendered text.

## Tmux Delivery (Send-Keys)

The delivery layer applies lessons from gastown's battle-tested implementation:

### Gotchas Handled

1. **ESC + Enter timing** — Send Escape, wait 600ms (exceeds bash readline's 500ms keyseq-timeout), then send Enter. Without this, ESC+Enter becomes M-Enter which doesn't submit.
2. **Copy mode interception** — Check `#{pane_in_mode}`, send `-X cancel` to exit before delivering.
3. **Message chunking** — Messages >512 bytes sent in chunks with 10ms inter-chunk delays to avoid argument length limits.
4. **Control character sanitization** — Strip ESC (0x1b), CR (0x0d), BS (0x08), DEL (0x7f). Replace TAB with space. Preserve newlines and printable characters.
5. **Per-session nudge lock** — Channel semaphore (`chan struct{}` size 1) with 30s timed acquisition prevents interleaved messages from concurrent senders.
6. **Cold startup retry** — Exponential backoff (500ms → 750ms → 1125ms → 2s cap) on transient "not in a mode" errors, up to 10s timeout. Non-transient errors fail fast.
7. **Detached session wake** — Resize-window dance (+1, sleep 50ms, restore) triggers SIGWINCH to wake Claude Code's TUI in detached sessions. Reset `window-size` to "latest" afterward.
8. **Multi-pane targeting** — Resolve correct pane when sessions have multiple panes (e.g., spotter reusing lead's window).

### Idle Detection

For future `wait-idle` delivery mode:
- Capture last 5 lines of pane via `capture-pane`
- Look for prompt prefix `❯` (U+276F) with NBSP normalization
- Check status bar for `⏵⏵` + "esc to interrupt" as busy signal
- Require 2 consecutive idle polls 200ms apart (filters transient prompt appearances between tool calls)

Not used in MVP — all delivery is immediate. Listed here for future reference.

## Environment Setup

When the setter spawns any agent session:

1. **Set tmux environment**: `tmux set-environment -t <session> BELAYER_MAIL_ADDRESS "<address>"`
2. **Add to CLAUDE.md** (appended to role-specific instructions):

```markdown
## Mail

You can receive messages from the orchestration system.
When prompted, run `belayer mail read` to check your messages.
When you complete your work, signal completion:
  belayer message setter --type done --body '{"status":"complete","summary":"<describe what you did>"}'
```

## Message Flow Examples

### Setter assigns goal to lead

```
Setter:
  belayer message task/abc/lead/frontend/goal-1 \
    --type goal_assignment \
    --body "Implement dark mode toggle in the navbar"

→ Beads: creates issue with labels to:task/abc/lead/frontend/goal-1, from:setter, msg-type:goal_assignment
→ Tmux: sends "You have a new message. Run 'belayer mail read' to see it." to belayer-task-abc window frontend-goal1

Lead (in tmux):
  belayer mail read

→ Prints:
  NEW GOAL ASSIGNED
  Read .lead/GOAL.json for full context. Begin working on your assignment.
  When complete, signal done:
    belayer message setter --type done --body '{"status":"complete","summary":"<your summary>"}'
  ---
  Implement dark mode toggle in the navbar

→ Closes the beads issue (marks read)
```

### Lead signals completion

```
Lead:
  belayer message setter --type done \
    --body '{"status":"complete","summary":"Added dark mode toggle with localStorage persistence"}'

→ Beads: creates issue with labels to:setter, from:task/abc/lead/frontend/goal-1, msg-type:done
→ Tmux: delivers nudge to belayer-setter session

Setter:
  belayer mail read

→ Reads done signal, processes it (spawn spotter, mark goal complete, etc.)
```

### User sends instruction

```
User (terminal):
  belayer message setter --type instruction --body "Add dark mode to the task"

→ Beads: creates issue with labels
→ Tmux: delivers to setter session
→ Setter reads, decomposes into goals, sends goal_assignment messages to leads
```

## Failure Modes

| Scenario | Behavior |
|----------|----------|
| Target session not running | Beads issue created (durable). Delivery fails silently. Agent reads on next `belayer mail read`. |
| Target session busy (mid-tool-call) | Message typed into prompt. Claude Code processes it at next turn boundary. |
| `belayer message` crashes after beads write but before delivery | Message persists in beads. Agent picks it up via `belayer mail read` at startup. |
| Agent never runs `belayer mail read` | Messages accumulate as open beads issues. Setter's stuck detection catches non-responsive agents. |
| Concurrent messages to same session | Per-session nudge lock serializes delivery. Messages arrive in order. |

## Module Structure

```
internal/mail/
  message.go       # Message type, label constants, address parsing
  send.go          # Send: template + beads write + tmux delivery
  read.go          # Read: beads query + print + close
  templates.go     # embed.FS for mail templates
  delivery.go      # Tmux send-keys with all gotcha handling
  registry.go      # Address → tmux target resolution (deterministic)

internal/defaults/mail/
  goal_assignment.md.tmpl
  done.md.tmpl
  spot_result.md.tmpl
  verdict.md.tmpl
  feedback.md.tmpl
  instruction.md.tmpl

internal/cli/
  message.go       # belayer message command
  mail.go          # belayer mail read/inbox/ack commands
```

## Future Enhancements (Post-MVP)

- **Hook-based cooperative delivery**: Drain unread messages at turn boundary via Claude Code's UserPromptSubmit hook. Avoids interrupting in-flight work.
- **Wait-idle delivery mode**: Poll for idle prompt before delivering. Falls back to immediate.
- **Watcher daemon**: Background process sweeping for undelivered messages. Safety net for crash recovery.
- **Priority-based framing**: Urgent messages interrupt, normal messages queued for next boundary.
- **Codex CLI support**: Since Codex has no hooks, all delivery via send-keys (same as MVP).
- **Message TTL/expiry**: Auto-close stale messages after configurable timeout.
- **Channel broadcasts**: Send to all leads in a task, all agents of a type, etc.
