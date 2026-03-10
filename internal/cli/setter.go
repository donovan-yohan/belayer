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
	"github.com/donovan-yohan/belayer/internal/belayer"
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

	cfg := belayer.Config{
		InstanceName: name,
		InstanceDir:  instanceDir,
		MaxLeads:     maxLeads,
		PollInterval: pollInterval,
		StaleTimeout: staleTimeout,
	}

	tm := tmux.NewRealTmux()
	sp := lead.NewClaudeSpawner(tm)
	s := belayer.New(cfg, bcfg, globalCfgDir, instanceCfgDir, database.Conn(), tm, sp)

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
