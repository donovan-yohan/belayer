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

	"github.com/donovan-yohan/belayer/internal/belayer"
	"github.com/donovan-yohan/belayer/internal/belayerconfig"
	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/pidfile"
	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/spf13/cobra"
)

func newBelayerDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "belayer",
		Short: "Manage the belayer daemon",
		Long:  "Start, stop, and check the status of the belayer daemon that orchestrates problem execution.",
	}

	cmd.AddCommand(
		newBelayerDaemonStartCmd(),
		newBelayerDaemonStopCmd(),
		newBelayerDaemonStatusCmd(),
	)

	return cmd
}

func newBelayerDaemonStartCmd() *cobra.Command {
	var instanceName string
	var foreground bool
	var maxLeads int
	var pollInterval time.Duration
	var staleTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the belayer daemon",
		Long:  "Starts the belayer daemon. By default it runs in the background. Use --foreground to run in the current terminal.",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(name)
			if err != nil {
				return err
			}

			pidPath := filepath.Join(instanceDir, "belayer.pid")

			// Check for already running belayer daemon
			if existingPID, running := pidfile.Check(pidPath); running {
				return fmt.Errorf("belayer daemon already running (PID %d)", existingPID)
			}
			// Clean up stale PID file
			pidfile.Remove(pidPath)

			if foreground {
				return runBelayerDaemonForeground(name, instanceDir, pidPath, maxLeads, pollInterval, staleTimeout)
			}

			return runBelayerDaemonBackground(name, instanceDir, pidPath, maxLeads, pollInterval, staleTimeout)
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Run in the foreground instead of daemonizing")
	cmd.Flags().IntVar(&maxLeads, "max-leads", 8, "Maximum concurrent lead sessions")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 5*time.Second, "Polling interval for new problems")
	cmd.Flags().DurationVar(&staleTimeout, "stale-timeout", 30*time.Minute, "Timeout for stale climb detection")

	return cmd
}

// runBelayerDaemonForeground runs the belayer daemon in the current process (foreground mode).
func runBelayerDaemonForeground(name, instanceDir, pidPath string, maxLeads int, pollInterval, staleTimeout time.Duration) error {
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
		fmt.Println("\nBelayer daemon shutting down...")
		cancel()
	}()

	return s.Run(ctx)
}

// runBelayerDaemonBackground re-execs the belayer daemon in the background with --foreground,
// redirecting output to a log file.
func runBelayerDaemonBackground(name, instanceDir, pidPath string, maxLeads int, pollInterval, staleTimeout time.Duration) error {
	logsDir := filepath.Join(instanceDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("creating logs directory: %w", err)
	}
	logPath := filepath.Join(logsDir, "belayer.log")

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	// Build args for re-exec with --foreground
	args := []string{
		"belayer", "start", "--foreground",
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
		return fmt.Errorf("starting belayer daemon: %w", err)
	}

	// Close our handle — the child owns the file now
	logFile.Close()

	fmt.Printf("Belayer daemon started (PID %d)\n", child.Process.Pid)
	fmt.Printf("Logs: %s\n", logPath)

	// Don't wait for child — let it run detached
	// The child writes its own PID file via --foreground
	child.Process.Release()

	return nil
}

func newBelayerDaemonStopCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the belayer daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(name)
			if err != nil {
				return err
			}

			pidPath := filepath.Join(instanceDir, "belayer.pid")
			pid, running := pidfile.Check(pidPath)
			if !running {
				pidfile.Remove(pidPath) // clean stale
				fmt.Println("No belayer daemon running.")
				return nil
			}

			// Send SIGTERM
			fmt.Printf("Stopping belayer daemon (PID %d)...\n", pid)
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				pidfile.Remove(pidPath)
				return fmt.Errorf("sending SIGTERM to PID %d: %w", pid, err)
			}

			// Wait up to 10s for exit
			for i := 0; i < 20; i++ {
				time.Sleep(500 * time.Millisecond)
				if !pidfile.IsRunning(pid) {
					pidfile.Remove(pidPath)
					fmt.Println("Belayer daemon stopped.")
					return nil
				}
			}

			// Force kill
			fmt.Printf("Belayer daemon did not exit in 10s, sending SIGKILL...\n")
			syscall.Kill(pid, syscall.SIGKILL)
			pidfile.Remove(pidPath)
			fmt.Println("Belayer daemon killed.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")

	return cmd
}

func newBelayerDaemonStatusCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check if the belayer daemon is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(name)
			if err != nil {
				return err
			}

			pidPath := filepath.Join(instanceDir, "belayer.pid")
			pid, running := pidfile.Check(pidPath)
			if running {
				fmt.Printf("Belayer daemon running (PID %d)\n", pid)
			} else {
				if pid > 0 {
					pidfile.Remove(pidPath) // clean stale
				}
				fmt.Println("Belayer daemon not running.")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")

	return cmd
}
