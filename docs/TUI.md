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
|  Events 42%                                   |
|  2m ago  lead_exec_output [repo-a]           |
|  1m ago  lead_review_output [repo-a]         |
|  just now  lead_progress [repo-a]            |
+----------------------------------------------+
| 3 tasks | 1 active                            |
```

## Key Bindings

| Key | Action |
|-----|--------|
| `j`/`k` or arrows | Navigate (cursor in tasks/detail, scroll in events) |
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
| `model.go` | Main bubbletea Model (Init/Update/View), tick-based polling, viewport management |
| `store.go` | Read-only SQLite queries (`TaskSummary`, `LeadDetail`, `EventEntry`) |
| `views.go` | Per-pane rendering and content generation for viewports |
| `styles.go` | lipgloss style definitions and `StatusBadge` |
| `keys.go` | Key binding definitions |

### Data Flow

```
SQLite (read-only) --[1s tick]--> tui.Store queries --> Model state --> Viewport content --> View render
```

### Focus States

Three panes cycle with tab: `TaskList -> Detail -> Events -> TaskList`.

- **Tasks pane**: Cursor-based navigation with auto-scroll when cursor exceeds visible height
- **Detail pane**: Lead cursor with viewport scrolling; auto-scrolls to keep selected lead visible
- **Events pane**: Viewport-based scrolling (j/k scrolls through events)

### Viewport Scrolling

Detail and Events panes use `bubbles/viewport` for proper content clipping and scrolling:
- Content is generated in full (no height limits) by `renderDetailContent` / `renderEventsContent`
- Content is set on the viewport via `SetContent`, which handles scroll position preservation
- When the viewport has scrollable content, the title shows scroll percentage (e.g., `Detail 42%`)
- Changing tasks resets both viewports to top
- Moving the lead cursor in the detail pane auto-scrolls to keep the selected lead visible

### Lead Audit Events

The events pane surfaces agent-level activity from lead execution loops:
- `lead_exec_output` — output snippet from the execution agent (first 500 chars)
- `lead_review_output` — output snippet from the review agent (first 500 chars)
- `lead_progress` — goal phase transitions (executing, reviewing)
- `goal_verdict` — pass/fail verdict with summary
- Full agent output stored at `output_file` path in event payload
