# Execution Plan: TUI Dashboard

**Goal**: 7 — TUI dashboard
**Design Doc**: [2026-03-06-tui-dashboard-design](../../design-docs/2026-03-06-tui-dashboard-design.md)
**Created**: 2026-03-06

## Steps

### 1. Add bubbletea dependencies
- [x] `go get github.com/charmbracelet/bubbletea github.com/charmbracelet/lipgloss github.com/charmbracelet/bubbles`
- **Files**: `go.mod`, `go.sum`

### 2. Create TUI store
- [x] Create `internal/tui/store.go` with read-only SQLite queries
- [x] Define `TaskSummary`, `LeadDetail`, `EventEntry` types
- [x] Implement `ListInstances`, `ListTaskSummaries`, `GetLeadDetails`, `GetRecentEvents`
- **Files**: `internal/tui/store.go`

### 3. Create TUI styles
- [x] Create `internal/tui/styles.go` with lipgloss style definitions
- [x] Status-colored badges, pane borders, selection highlighting
- **Files**: `internal/tui/styles.go`

### 4. Create key bindings
- [x] Create `internal/tui/keys.go` with bubbles/key.Binding definitions
- **Files**: `internal/tui/keys.go`

### 5. Create TUI model and main update/view loop
- [x] Create `internal/tui/model.go` with main bubbletea Model
- [x] Implement Init (load initial data), Update (key handling + tick), View (render panes)
- [x] Three panes: task list (left), task detail (right), event log (bottom)
- [x] Focus cycling with tab
- [x] SQLite polling via tea.Tick at 1s interval
- **Files**: `internal/tui/model.go`

### 6. Create view rendering functions
- [x] Create `internal/tui/views.go` with per-pane rendering
- [x] `renderTaskList` — task list with status badges and lead counts
- [x] `renderTaskDetail` — selected task's leads with goal progress
- [x] `renderEventLog` — recent events / lead output for selected lead
- [x] `renderHeader` — instance name + help hints
- [x] `renderStatusBar` — bottom status line
- **Files**: `internal/tui/views.go`

### 7. Wire TUI into CLI command
- [x] Update `internal/cli/tui.go` to launch bubbletea program with store
- [x] Accept `--instance` flag, resolve instance, open DB, pass to TUI
- **Files**: `internal/cli/tui.go`

### 8. Write tests
- [x] Create `internal/tui/store_test.go` — test store queries with temp-file SQLite
- [x] Create `internal/tui/model_test.go` — test model Update logic (key events, tick refresh)
- **Files**: `internal/tui/store_test.go`, `internal/tui/model_test.go`

### 9. Run full test suite
- [x] `go test ./...` passes
- [x] `go build -o belayer ./cmd/belayer` succeeds

### 10. Update documentation
- [x] Create `docs/TUI.md` with component overview
- [x] Update `docs/ARCHITECTURE.md` code map
- [x] Update `docs/DESIGN.md` with TUI section
