# TUI Dashboard

bubbletea-based terminal dashboard for monitoring belayer instances.

## Launch

```bash
belayer tui                        # uses default instance
belayer tui --instance my-project  # specific instance
```

## Layout

```
+------- Header (instance name + help) --------+
|  Tasks (left)    |  Detail (right)            |
|  [running] ...   |  task-id [status]          |
|  [complete] ...  |  Leads:                    |
|                  |    repo-a [running] 2/5    |
|                  |    repo-b [complete] 3/3   |
+----------------------------------------------+
|  Events (bottom)                              |
|  2m ago  lead_started [repo-a]               |
|  1m ago  lead_progress [repo-a]              |
+----------------------------------------------+
| 3 tasks | 1 active                            |
```

## Key Bindings

| Key | Action |
|-----|--------|
| `j`/`k` or arrows | Navigate up/down |
| `tab` / `shift+tab` | Cycle panes |
| `1`/`2`/`3` | Jump to pane |
| `enter` | Select (focus detail) |
| `esc` | Go back |
| `r` | Force refresh |
| `q` / `ctrl+c` | Quit |

## Architecture

### Files

| File | Purpose |
|------|---------|
| `model.go` | Main bubbletea Model (Init/Update/View), tick-based polling |
| `store.go` | Read-only SQLite queries (`TaskSummary`, `LeadDetail`, `EventEntry`) |
| `views.go` | Per-pane rendering functions |
| `styles.go` | lipgloss style definitions and `StatusBadge` |
| `keys.go` | Key binding definitions |

### Data Flow

```
SQLite (read-only) --[1s tick]--> tui.Store queries --> Model state --> View render
```

### Focus States

Three panes cycle with tab: `TaskList -> Detail -> Events -> TaskList`.

Each pane has independent cursor/scroll state.
