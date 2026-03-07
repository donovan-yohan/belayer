# Execution Plan: Project Scaffolding & Core Architecture

**Goal**: 1
**Design Doc**: [2026-03-06-project-scaffolding-design](../../design-docs/2026-03-06-project-scaffolding-design.md)
**Date**: 2026-03-06

## Steps

- [x] **Step 1**: Create directory structure
  - `cmd/belayer/main.go`
  - `internal/cli/`, `internal/config/`, `internal/db/`, `internal/db/migrations/`, `internal/model/`
  - `pkg/` (empty, with `.gitkeep`)

- [x] **Step 2**: Add Go dependencies
  - `go get github.com/spf13/cobra`
  - `go get modernc.org/sqlite`

- [x] **Step 3**: Implement domain model types
  - File: `internal/model/types.go`
  - Types: Instance, Task, TaskRepo, Lead, Event, AgenticDecision
  - Enums for statuses (TaskStatus, LeadStatus, EventType, etc.)

- [x] **Step 4**: Implement SQLite schema and migrations
  - File: `internal/db/migrations/001_initial.sql`
  - File: `internal/db/db.go` — Open, Migrate, Close functions
  - Tables: schema_migrations, instances, tasks, task_repos, leads, events, agentic_decisions

- [x] **Step 5**: Implement config management
  - File: `internal/config/config.go`
  - Functions: Load, Save, DefaultPath, EnsureDir
  - Config struct with JSON tags
  - Creates `~/.belayer/` directory on init

- [x] **Step 6**: Implement CLI commands
  - File: `internal/cli/root.go` — root command with version flag
  - File: `internal/cli/init.go` — creates ~/.belayer/ and config.json
  - File: `internal/cli/instance.go` — `instance create` stub
  - File: `internal/cli/task.go` — `task create` stub
  - File: `internal/cli/status.go` — `status` stub
  - File: `internal/cli/tui.go` — `tui` stub

- [x] **Step 7**: Wire up main.go
  - File: `cmd/belayer/main.go`
  - Calls root command Execute()

- [x] **Step 8**: Write tests
  - `internal/db/db_test.go` — migration, table creation
  - `internal/config/config_test.go` — load, save, defaults

- [x] **Step 9**: Verify
  - `go build -o belayer ./cmd/belayer`
  - `go test ./...`
  - `./belayer --help`
  - `./belayer init` (creates ~/.belayer/)
