package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const (
	temporalGRPCPort = "7233"
	temporalWebPort  = "8233"
)

func newTemporalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "temporal",
		Short: "Manage the Temporal dev server",
	}

	cmd.AddCommand(
		newTemporalStartCmd(),
		newTemporalStopCmd(),
		newTemporalStatusCmd(),
	)

	return cmd
}

func newTemporalStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the Temporal dev server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return temporalStart()
		},
	}
}

func newTemporalStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the Temporal dev server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return temporalStop()
		},
	}
}

func newTemporalStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check if the Temporal dev server is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			return temporalStatus()
		},
	}
}

func temporalStart() error {
	// Check if already running.
	if isTemporalRunning() {
		fmt.Println("Temporal dev server is already running.")
		fmt.Printf("  gRPC: localhost:%s\n", temporalGRPCPort)
		fmt.Printf("  Web:  http://localhost:%s\n", temporalWebPort)
		return nil
	}

	// Check if temporal CLI is installed.
	temporalPath, err := exec.LookPath("temporal")
	if err != nil {
		fmt.Println("Temporal CLI not found. Install it:")
		fmt.Println("  brew install temporal")
		fmt.Println("  or: https://docs.temporal.io/cli#install")
		return fmt.Errorf("temporal CLI not found")
	}

	// Start the dev server in the background.
	serverCmd := exec.Command(temporalPath, "server", "start-dev",
		"--db-filename", filepath.Join(belayerDir(), "temporal.db"),
	)
	serverCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	serverCmd.Stdout = nil
	serverCmd.Stderr = nil

	if err := serverCmd.Start(); err != nil {
		return fmt.Errorf("failed to start Temporal dev server: %w", err)
	}

	// Save PID.
	pidFile := filepath.Join(belayerDir(), "temporal.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(serverCmd.Process.Pid)), 0o644); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}

	// Wait for server to be reachable.
	fmt.Print("Starting Temporal dev server")
	for i := 0; i < 30; i++ {
		if isTemporalRunning() {
			fmt.Println(" ready!")
			fmt.Printf("  gRPC: localhost:%s\n", temporalGRPCPort)
			fmt.Printf("  Web:  http://localhost:%s\n", temporalWebPort)
			return nil
		}
		fmt.Print(".")
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("Temporal dev server did not become reachable within 15 seconds")
}

func temporalStop() error {
	pid, err := readTemporalPID()
	if err != nil {
		fmt.Println("Temporal dev server is not running (no PID file).")
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		cleanupPIDFile()
		fmt.Println("Temporal dev server is not running (process not found).")
		return nil
	}

	// Send SIGTERM first, then SIGKILL after timeout.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		cleanupPIDFile()
		fmt.Println("Temporal dev server stopped (was already dead).")
		return nil
	}

	// Wait briefly for graceful shutdown.
	time.Sleep(2 * time.Second)

	// Force kill if still running.
	_ = proc.Signal(syscall.SIGKILL)
	cleanupPIDFile()

	fmt.Println("Temporal dev server stopped.")
	return nil
}

func temporalStatus() error {
	if isTemporalRunning() {
		pid, _ := readTemporalPID()
		fmt.Println("Temporal dev server: running")
		if pid > 0 {
			fmt.Printf("  PID:  %d\n", pid)
		}
		fmt.Printf("  gRPC: localhost:%s\n", temporalGRPCPort)
		fmt.Printf("  Web:  http://localhost:%s\n", temporalWebPort)
	} else {
		fmt.Println("Temporal dev server: not running")
		fmt.Println("Start with: belayer v2 temporal start")
	}
	return nil
}

func isTemporalRunning() bool {
	conn, err := net.DialTimeout("tcp", "localhost:"+temporalGRPCPort, time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func readTemporalPID() (int, error) {
	data, err := os.ReadFile(filepath.Join(belayerDir(), "temporal.pid"))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func cleanupPIDFile() {
	_ = os.Remove(filepath.Join(belayerDir(), "temporal.pid"))
}

func belayerDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".belayer")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}
