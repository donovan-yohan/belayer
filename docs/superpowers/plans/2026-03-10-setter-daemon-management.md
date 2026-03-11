# Setter Daemon Management Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `belayer setter start/stop/status` subcommands so the setter can run as a background daemon with PID file management.

**Architecture:** Rewrite `internal/cli/setter.go` from a single command to a parent command with three subcommands. PID file utilities live in a small `internal/pidfile` package. The `start` command re-execs itself with `--foreground` to daemonize, redirecting output to a log file.

**Tech Stack:** Go stdlib (`os`, `os/exec`, `syscall`), cobra subcommands

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/pidfile/pidfile.go` | Read/write/check PID files — reusable utility |
| `internal/pidfile/pidfile_test.go` | Tests for PID file operations |
| `internal/cli/setter.go` | Rewrite: parent cmd + `start`, `stop`, `status` subcommands |

## Chunk 1: PID File Utilities

### Task 1: PID file package

**Files:**
- Create: `internal/pidfile/pidfile.go`
- Create: `internal/pidfile/pidfile_test.go`

- [ ] **Step 1: Write failing tests for pidfile package**

Create `internal/pidfile/pidfile_test.go`:

```go
package pidfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	require.NoError(t, Write(path, 12345))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "12345\n", string(data))
}

func TestRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	require.NoError(t, os.WriteFile(path, []byte("42\n"), 0o644))

	pid, err := Read(path)
	require.NoError(t, err)
	assert.Equal(t, 42, pid)
}

func TestReadMissing(t *testing.T) {
	_, err := Read("/nonexistent/path.pid")
	assert.Error(t, err)
}

func TestRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	require.NoError(t, os.WriteFile(path, []byte("1\n"), 0o644))
	Remove(path)
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestIsRunning_OwnProcess(t *testing.T) {
	// Current process is always alive
	assert.True(t, IsRunning(os.Getpid()))
}

func TestIsRunning_DeadProcess(t *testing.T) {
	// PID 0 is not a real process; very high PID unlikely to exist
	assert.False(t, IsRunning(99999999))
}

func TestCheck_NoFile(t *testing.T) {
	pid, running := Check("/nonexistent.pid")
	assert.Equal(t, 0, pid)
	assert.False(t, running)
}

func TestCheck_StaleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	require.NoError(t, os.WriteFile(path, []byte("99999999\n"), 0o644))

	pid, running := Check(path)
	assert.Equal(t, 99999999, pid)
	assert.False(t, running)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pidfile/... -v`
Expected: FAIL — package doesn't exist yet

- [ ] **Step 3: Implement pidfile package**

Create `internal/pidfile/pidfile.go`:

```go
package pidfile

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Write writes a PID to the given file path.
func Write(path string, pid int) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o644)
}

// Read reads a PID from the given file path.
func Read(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parsing PID from %s: %w", path, err)
	}
	return pid, nil
}

// Remove deletes the PID file. No error if it doesn't exist.
func Remove(path string) {
	os.Remove(path)
}

// IsRunning checks if a process with the given PID is alive.
func IsRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// Check reads the PID file and checks if the process is running.
// Returns (0, false) if the file doesn't exist or can't be read.
func Check(path string) (int, bool) {
	pid, err := Read(path)
	if err != nil {
		return 0, false
	}
	return pid, IsRunning(pid)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pidfile/... -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pidfile/
git commit -m "feat: add pidfile package for daemon PID management"
```

## Chunk 2: Setter Subcommands

### Task 2: Rewrite setter.go with start/stop/status subcommands

**Files:**
- Modify: `internal/cli/setter.go` (full rewrite)

- [ ] **Step 1: Rewrite setter.go**

Replace `internal/cli/setter.go` with the parent command + three subcommands:

```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/donovan-yohan/belayer/internal/belayerconfig"
	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/pidfile"
	"github.com/donovan-yohan/belayer/internal/setter"
	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/spf13/cobra"
)

func newSetterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setter",
		Short: "Manage the setter daemon",
		Long:  "Start, stop, and check the status of the setter daemon that orchestrates task execution.",
	}

	cmd.AddCommand(
		newSetterStartCmd(),
		newSetterStopCmd(),
		newSetterStatusCmd(),
	)

	return cmd
}

func newSetterStartCmd() *cobra.Command {
	var instanceName string
	var foreground bool
	var maxLeads int
	var pollInterval time.Duration
	var staleTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the setter daemon",
		Long:  "Starts the setter daemon. By default it runs in the background. Use --foreground to run in the current terminal.",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(name)
			if err != nil {
				return err
			}

			pidPath := filepath.Join(instanceDir, "setter.pid")

			// Check for already running setter
			if existingPID, running := pidfile.Check(pidPath); running {
				return fmt.Errorf("setter already running (PID %d)", existingPID)
			}
			// Clean up stale PID file
			pidfile.Remove(pidPath)

			if foreground {
				return runSetterForeground(name, instanceDir, pidPath, maxLeads, pollInterval, staleTimeout)
			}

			return runSetterBackground(name, instanceDir, pidPath, maxLeads, pollInterval, staleTimeout)
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Run in the foreground instead of daemonizing")
	cmd.Flags().IntVar(&maxLeads, "max-leads", 8, "Maximum concurrent lead sessions")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 5*time.Second, "Polling interval for new tasks")
	cmd.Flags().DurationVar(&staleTimeout, "stale-timeout", 30*time.Minute, "Timeout for stale goal detection")

	return cmd
}

// runSetterForeground runs the setter in the current process (foreground mode).
func runSetterForeground(name, instanceDir, pidPath string, maxLeads int, pollInterval, staleTimeout time.Duration) error {
	dbPath := filepath.Join(instanceDir, "belayer.db")
	database, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer database.Close()

	if err := database.Migrate(); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	globalDir, err := config.Dir()
	if err != nil {
		return fmt.Errorf("getting global config dir: %w", err)
	}
	globalCfgDir := filepath.Join(globalDir, "config")
	instanceCfgDir := filepath.Join(instanceDir, "config")

	bcfg, err := belayerconfig.Load(globalCfgDir, instanceCfgDir)
	if err != nil {
		return fmt.Errorf("loading belayer config: %w", err)
	}

	cfg := setter.Config{
		InstanceName: name,
		InstanceDir:  instanceDir,
		MaxLeads:     maxLeads,
		PollInterval: pollInterval,
		StaleTimeout: staleTimeout,
	}

	tm := tmux.NewRealTmux()
	sp := lead.NewClaudeSpawner(tm)
	s := setter.New(cfg, bcfg, globalCfgDir, instanceCfgDir, database.Conn(), tm, sp)

	// Write PID file and clean up on exit
	if err := pidfile.Write(pidPath, os.Getpid()); err != nil {
		return fmt.Errorf("writing PID file: %w", err)
	}
	defer pidfile.Remove(pidPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nSetter shutting down...")
		cancel()
	}()

	return s.Run(ctx)
}

// runSetterBackground re-execs the setter in the background with --foreground,
// redirecting output to a log file.
func runSetterBackground(name, instanceDir, pidPath string, maxLeads int, pollInterval, staleTimeout time.Duration) error {
	logsDir := filepath.Join(instanceDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("creating logs directory: %w", err)
	}
	logPath := filepath.Join(logsDir, "setter.log")

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	// Build args for re-exec with --foreground
	args := []string{
		"setter", "start", "--foreground",
		"--instance", name,
		"--max-leads", fmt.Sprintf("%d", maxLeads),
		"--poll-interval", pollInterval.String(),
		"--stale-timeout", staleTimeout.String(),
	}

	executable, err := os.Executable()
	if err != nil {
		logFile.Close()
		return fmt.Errorf("finding executable path: %w", err)
	}

	child := exec.Command(executable, args...)
	child.Stdout = logFile
	child.Stderr = logFile
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from terminal

	if err := child.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting setter daemon: %w", err)
	}

	// Close our handle — the child owns the file now
	logFile.Close()

	fmt.Printf("Setter started (PID %d)\n", child.Process.Pid)
	fmt.Printf("Logs: %s\n", logPath)

	// Don't wait for child — let it run detached
	// The child writes its own PID file via --foreground
	child.Process.Release()

	return nil
}

func newSetterStopCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the setter daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(name)
			if err != nil {
				return err
			}

			pidPath := filepath.Join(instanceDir, "setter.pid")
			pid, running := pidfile.Check(pidPath)
			if !running {
				pidfile.Remove(pidPath) // clean stale
				fmt.Println("No setter running.")
				return nil
			}

			// Send SIGTERM
			fmt.Printf("Stopping setter (PID %d)...\n", pid)
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				pidfile.Remove(pidPath)
				return fmt.Errorf("sending SIGTERM to PID %d: %w", pid, err)
			}

			// Wait up to 10s for exit
			for i := 0; i < 20; i++ {
				time.Sleep(500 * time.Millisecond)
				if !pidfile.IsRunning(pid) {
					pidfile.Remove(pidPath)
					fmt.Println("Setter stopped.")
					return nil
				}
			}

			// Force kill
			fmt.Printf("Setter did not exit in 10s, sending SIGKILL...\n")
			syscall.Kill(pid, syscall.SIGKILL)
			pidfile.Remove(pidPath)
			fmt.Println("Setter killed.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")

	return cmd
}

func newSetterStatusCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check if the setter daemon is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(name)
			if err != nil {
				return err
			}

			pidPath := filepath.Join(instanceDir, "setter.pid")
			pid, running := pidfile.Check(pidPath)
			if running {
				fmt.Printf("Setter running (PID %d)\n", pid)
			} else {
				if pid > 0 {
					pidfile.Remove(pidPath) // clean stale
				}
				fmt.Println("Setter not running.")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")

	return cmd
}
```

- [ ] **Step 2: Build and verify**

Run: `go build -o belayer ./cmd/belayer`
Expected: Compiles without errors

- [ ] **Step 3: Verify CLI help output**

Run: `./belayer setter --help`
Expected: Shows `start`, `stop`, `status` subcommands

Run: `./belayer setter start --help`
Expected: Shows `--foreground`, `--instance`, `--max-leads`, `--poll-interval`, `--stale-timeout` flags

- [ ] **Step 4: Run full test suite**

Run: `go test ./... -v`
Expected: All existing tests pass (no CLI tests exist for setter)

- [ ] **Step 5: Commit**

```bash
git add internal/cli/setter.go
git commit -m "feat: setter start/stop/status subcommands with daemon support"
```

### Task 3: Update manage session docs if they reference `belayer setter`

**Files:**
- Check: `internal/defaults/claudemd/manage.md`
- Check: `docs/ARCHITECTURE.md`

- [ ] **Step 1: Search for references to the old `belayer setter` command**

Run: `grep -r "belayer setter" docs/ internal/defaults/`
Update any references from `belayer setter` to `belayer setter start`.

- [ ] **Step 2: Commit if changes were made**

```bash
git add -A
git commit -m "docs: update references from 'belayer setter' to 'belayer setter start'"
```
