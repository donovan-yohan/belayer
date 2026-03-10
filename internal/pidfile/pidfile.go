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
