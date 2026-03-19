---
status: implemented
created: 2026-03-10
branch: master
supersedes: 2026-03-09-mail-system-design.md
implemented-by:
---
# Filesystem Mail Store

> **Status**: Approved | **Created**: 2026-03-10

## Problem

The beads-backed mail system spawns a `dolt sql-server` per Claude Code session. Since belayer spawns many concurrent Claude sessions (leads, spotters, anchors), this results in dozens of orphaned Dolt processes consuming memory and ports. Mail is simple ephemeral inter-agent messaging — it doesn't need a database.

## Design

Replace `BeadsStore` with `FileStore` — same interface, pure filesystem. Messages are JSON files in per-address directories with `unread/` and `read/` subdirectories.

## Directory Layout

```
<instanceDir>/mail/
  task/<taskID>/lead/<repo>/<goalID>/
    unread/1741234567000000000-goal_assignment.json
    read/1741234560000000000-feedback.json
  task/<taskID>/anchor/
    unread/
    read/
  setter/
    unread/
    read/
```

Address directories mirror the existing address routing scheme (`task/<id>/lead/<repo>/<goal>`, `task/<id>/anchor`, `setter`).

## Message File Format

Same as the current `Message` struct — no schema change:

```json
{
  "from": "setter",
  "to": "task/t1/lead/api/g1",
  "type": "goal_assignment",
  "subject": "New Goal Assignment",
  "body": "..."
}
```

Filename: `<unix-nanos>-<type>.json` — provides sort ordering and uniqueness.

## FileStore Interface

Replaces `BeadsStore` with identical operations:

| Method | Implementation |
|--------|---------------|
| `Create(title, body, labels)` | Write JSON to `<addr>/unread/<ts>-<type>.json` |
| `List(address)` | `os.ReadDir(<addr>/unread/)`, parse each JSON file |
| `Close(id)` | `os.Rename` from `unread/<file>` to `read/<file>` |

Constructor: `NewFileStore(baseDir)` — no initialization step, no external process, no dependencies.

## Cleanup

`TaskRunner.Cleanup()` adds `os.RemoveAll(<instanceDir>/mail/task/<taskID>/)` to wipe all mail (read and unread) when a task completes. This piggybacks on existing task lifecycle cleanup.

## Files Changed

| File | Change |
|------|--------|
| `internal/mail/beads.go` | **Delete** — replaced by filestore.go |
| `internal/mail/beads_test.go` | **Delete** — replaced by filestore_test.go |
| `internal/mail/filestore.go` | **Create** — FileStore with Create/List/Close |
| `internal/mail/filestore_test.go` | **Create** — tests for all operations |
| `internal/mail/send.go` | Update `*BeadsStore` → `*FileStore` |
| `internal/mail/read.go` | Update `*BeadsStore` → `*FileStore` |
| `internal/cli/mail.go` | Update `mailStore()` to return `*FileStore` |
| `internal/cli/message.go` | Update store initialization |
| `internal/setter/taskrunner.go` | Add mail directory cleanup to `Cleanup()` |
| `docs/DESIGN.md` | Update Mail System section |

## What Stays the Same

- `message.go` — types, addresses, parsing — unchanged
- `delivery.go` — tmux nudge mechanics — unchanged
- `templates.go` — message templates — unchanged
- `send.go` / `read.go` logic — just swap store type in signatures
- CLI commands, flags, UX — identical
