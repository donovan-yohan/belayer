# TUI Dashboard Design

**Goal**: 7 — TUI dashboard
**Date**: 2026-03-06
**Status**: Approved (autonomous decision)

## Overview

Implement a bubbletea-based terminal user interface for belayer that provides real-time visibility into instance state, task progress, lead execution, integration verdicts, and task history. The TUI polls SQLite for updates and renders a multi-pane layout with keyboard navigation.

## Acceptance Criteria (from PRD)

- bubbletea TUI shows: instance list, active tasks with per-repo lead progress bars, real-time streaming of lead output (selectable), integration verdicts, task history
- keyboard navigation (j/k, enter, q, tab between panes)
- responsive layout
- updates via SQLite polling

## Architecture

### Package Structure

```
internal/tui/
  model.go       # Main bubbletea Model, Init, Update, View
  store.go       # TUI-specific read-only store (queries SQLite)
  views.go       # View rendering functions for each pane
  styles.go      # lipgloss styles
  keys.go        # Key bindings
```

### Design Decisions

1. **Single-package approach**: All TUI code in `internal/tui/`. No sub-packages — the TUI is a single coherent component. This follows the existing project pattern where each module is a flat package (`internal/lead/`, `internal/coordinator/`).

2. **TUI-specific store**: Create a new read-only `tui.Store` that wraps `*sql.DB` with queries optimized for the dashboard (e.g., get all tasks with lead counts, get events by recency). This avoids coupling to the coordinator/lead store interfaces and allows TUI-specific joins.

3. **SQLite polling**: Use a `tea.Tick` command at 1-second intervals to re-query SQLite. This matches the coordinator's polling pattern and avoids introducing channels or event buses. The TUI is read-only — it never writes to SQLite.

4. **Three-pane layout**:
   - **Left pane**: Task list (filterable by status) with status indicators
   - **Right pane**: Detail view for selected task (lead progress, repo info)
   - **Bottom pane**: Event log / lead output stream (selectable per lead)

5. **View modes**: The TUI has three navigable focus areas:
   - `TaskList` — left pane, j/k to select tasks
   - `TaskDetail` — right pane, j/k to select leads within a task
   - `OutputLog` — bottom pane, scrollable lead output / event stream

6. **Tab between panes**: Tab cycles focus: TaskList → TaskDetail → OutputLog → TaskList

7. **Progress representation**: Lead progress shown as status badge + goal completion fraction (e.g., `[running] 2/5 goals`). No actual progress bar widget — the goal count is more informative for this domain.

8. **Responsive layout**: Use `tea.WindowSizeMsg` to adapt. Left pane gets ~30% width, right pane ~70%. Bottom pane gets ~30% height. Minimum terminal size: 80x24.

9. **Instance selection**: If multiple instances exist, show instance picker on launch. If only one instance (or `--instance` flag provided), skip directly to task dashboard.

## Data Model

### TUI Store Queries

```go
// TaskSummary is a denormalized view for the task list.
type TaskSummary struct {
    Task       model.Task
    RepoCount  int
    LeadCount  int
    LeadsDone  int
    LeadsFailed int
}

// LeadDetail includes the repo name alongside lead info.
type LeadDetail struct {
    Lead     model.Lead
    RepoName string
    Goals    []model.LeadGoal
}

// EventEntry is a recent event for display.
type EventEntry struct {
    Event    model.Event
    RepoName string // resolved from lead -> task_repo
}
```

### Store Methods

```go
func (s *Store) ListTaskSummaries(instanceID string) ([]TaskSummary, error)
func (s *Store) GetLeadDetails(taskID string) ([]LeadDetail, error)
func (s *Store) GetRecentEvents(taskID string, limit int) ([]EventEntry, error)
func (s *Store) ListInstances() ([]model.Instance, error)
```

## Key Bindings

| Key | Action |
|-----|--------|
| `j` / `↓` | Move cursor down in current pane |
| `k` / `↑` | Move cursor up in current pane |
| `enter` | Select item (instance → tasks, task → detail) |
| `tab` | Cycle focus between panes |
| `shift+tab` | Cycle focus backward |
| `q` / `ctrl+c` | Quit |
| `1`/`2`/`3` | Jump to pane by number |
| `r` | Force refresh |
| `f` | Filter tasks by status (toggle) |
| `esc` | Go back / clear filter |

## Styling

Use lipgloss for consistent terminal styling:
- Status badges: colored per status (green=complete, yellow=running, red=failed, gray=pending, orange=stuck)
- Active pane: highlighted border
- Inactive pane: dim border
- Selected item: reverse video
- Timestamps: relative (e.g., "2m ago")

## Dependencies

- `github.com/charmbracelet/bubbletea` — TUI framework
- `github.com/charmbracelet/lipgloss` — styling
- `github.com/charmbracelet/bubbles` — standard components (viewport for scrolling)

## Non-Goals

- The TUI does not write to SQLite (read-only dashboard)
- No inline task creation from TUI (use CLI commands for that)
- No real-time WebSocket/channel — polling is sufficient for this use case
