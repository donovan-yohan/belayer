# Manage Session Runtime Context

## Problem

`belayer manage` currently injects a system prompt via `--system-prompt` flag. This works but limits the session to a single string — no slash commands, no hooks, no structured knowledge. The agent doesn't "know" it's a belayer session unless explicitly told in every prompt.

## Design

Replace `--system-prompt` with a temp workspace containing a full `.claude/` directory. The manage command creates the workspace, renders templates, and execs `claude` from it.

### Architecture

```
belayer manage -i my-project
  │
  ├── Create temp directory
  ├── Render manage.md template → .claude/CLAUDE.md
  │     (injects instance name, repo names)
  ├── Copy static commands → .claude/commands/
  ├── Set BELAYER_INSTANCE env var
  └── exec claude (working dir = temp dir)
```

### CLAUDE.md (templated)

The key file. Establishes belayer as the session's identity:
- "You are a belayer manage session for instance X with repos Y, Z"
- All user requests interpreted as belayer operations
- Routes to appropriate belayer CLI commands automatically
- CLI reference for available commands
- Task creation workflow: spec.md → goals.json → `belayer task create`
- Mail and messaging context

Lives at: `internal/defaults/claudemd/manage.md`

### Commands (static, 6 total)

| Command | Purpose |
|---------|---------|
| `/status` | Run `belayer status` and present results |
| `/task-create` | Guide spec.md + goals.json creation, then `belayer task create` |
| `/task-list` | Run `belayer task list` and present results |
| `/logs` | Run `belayer logs` and present results |
| `/message` | Send a message to an agent via `belayer message` |
| `/mail` | Check mail inbox via `belayer mail read` |

Live at: `internal/defaults/commands/*.md`

### CLI Enhancement: BELAYER_INSTANCE env var

`belayer task create` gains env var fallback: if `--instance` isn't provided, check `BELAYER_INSTANCE`. Same pattern as `BELAYER_MAIL_ADDRESS`.

The manage command sets this env var when spawning the session, so all commands "just work" without specifying `--instance`.

### File Layout

```
internal/defaults/
  claudemd/
    manage.md              # new — templated CLAUDE.md
    lead.md                # existing
    spotter.md             # existing
    anchor.md              # existing
  commands/
    status.md              # new — static command files
    task-create.md
    task-list.md
    logs.md
    message.md
    mail.md
```

### Changes

1. **`internal/defaults/defaults.go`** — Add `commands/*.md` to embed directive
2. **`internal/cli/manage.go`** — Replace `--system-prompt` with temp dir + `.claude/` setup
3. **`internal/manage/prompt.go`** — Remove or repurpose; template rendering moves to manage.go or a new prepare function
4. **`internal/cli/task.go`** — Add `BELAYER_INSTANCE` env var fallback to `task create`
5. **`CLAUDE.md`** — Add maintenance rule for manage session files

## Key Decision

**Static commands, templated CLAUDE.md.** Commands don't need dynamic data — they reference "the current instance" and Claude fills in the value from CLAUDE.md context. Only CLAUDE.md needs rendering with instance name and repo names.
