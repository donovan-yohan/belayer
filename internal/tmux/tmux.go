package tmux

import (
	"fmt"
	"os/exec"
	"strings"
)

// TmuxManager provides an interface for managing tmux sessions and windows.
// Used by the setter daemon to manage tmux sessions for lead execution.
type TmuxManager interface {
	// HasSession checks if a tmux session exists.
	HasSession(name string) bool
	// NewSession creates a new detached tmux session.
	NewSession(name string) error
	// KillSession kills a tmux session.
	KillSession(name string) error
	// NewWindow creates a new window in a session.
	NewWindow(session, windowName string) error
	// KillWindow kills a specific window in a session.
	KillWindow(session, windowName string) error
	// SendKeys sends keys to a specific window.
	SendKeys(session, windowName, keys string) error
	// ListWindows returns window names in a session.
	ListWindows(session string) ([]string, error)
	// PipePane enables pipe-pane logging for a window to a log file.
	PipePane(session, windowName, logPath string) error
	// SetRemainOnExit configures a window to keep the pane open after the process exits.
	SetRemainOnExit(session, windowName string, enabled bool) error
	// IsPaneDead checks if the process in a window has exited.
	IsPaneDead(session, windowName string) (bool, error)
	// CapturePaneContent captures the last N lines of visible pane content.
	CapturePaneContent(session, windowName string, lines int) (string, error)
	// SetEnvironment sets a tmux environment variable for a session.
	SetEnvironment(session, key, value string) error
	// SendKeysLiteral sends literal text (no Enter) to a specific target.
	SendKeysLiteral(target, text string) error
	// SendKeysRaw sends a raw key name (like "Enter", "Escape") to a target.
	SendKeysRaw(target, key string) error
}

// RealTmux implements TmuxManager by shelling out to the tmux CLI.
type RealTmux struct{}

// NewRealTmux returns a new RealTmux instance.
func NewRealTmux() *RealTmux { return &RealTmux{} }

// HasSession checks if a tmux session exists by running tmux has-session.
func (r *RealTmux) HasSession(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// NewSession creates a new detached tmux session with the given name.
func (r *RealTmux) NewSession(name string) error {
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-session -d -s %s: %s: %w", name, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// KillSession kills the tmux session with the given name.
func (r *RealTmux) KillSession(name string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux kill-session -t %s: %s: %w", name, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// NewWindow creates a new window with the given name in the specified session.
func (r *RealTmux) NewWindow(session, windowName string) error {
	cmd := exec.Command("tmux", "new-window", "-t", session, "-n", windowName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-window -t %s -n %s: %s: %w", session, windowName, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// KillWindow kills a specific window in a session.
func (r *RealTmux) KillWindow(session, windowName string) error {
	target := session + ":" + windowName
	cmd := exec.Command("tmux", "kill-window", "-t", target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux kill-window -t %s: %s: %w", target, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// SendKeys sends keys to a specific window in a session, followed by Enter.
func (r *RealTmux) SendKeys(session, windowName, keys string) error {
	target := session + ":" + windowName
	cmd := exec.Command("tmux", "send-keys", "-t", target, keys, "Enter")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys -t %s: %s: %w", target, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// ListWindows returns the names of all windows in the given session.
func (r *RealTmux) ListWindows(session string) ([]string, error) {
	cmd := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_name}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tmux list-windows -t %s: %s: %w", session, strings.TrimSpace(string(output)), err)
	}

	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

// PipePane enables pipe-pane logging for a window, appending output to the given log file.
func (r *RealTmux) PipePane(session, windowName, logPath string) error {
	target := session + ":" + windowName
	pipeCmd := fmt.Sprintf("cat >> %s", logPath)
	cmd := exec.Command("tmux", "pipe-pane", "-t", target, pipeCmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux pipe-pane -t %s: %s: %w", target, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// SetRemainOnExit configures a window to keep the pane open after the process exits.
func (r *RealTmux) SetRemainOnExit(session, windowName string, enabled bool) error {
	target := session + ":" + windowName
	val := "off"
	if enabled {
		val = "on"
	}
	cmd := exec.Command("tmux", "set-option", "-t", target, "remain-on-exit", val)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux set-option remain-on-exit -t %s: %s: %w", target, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// IsPaneDead checks if the process in a window has exited.
func (r *RealTmux) IsPaneDead(session, windowName string) (bool, error) {
	target := session + ":" + windowName
	cmd := exec.Command("tmux", "display-message", "-t", target, "-p", "#{pane_dead}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("tmux display-message -t %s: %s: %w", target, strings.TrimSpace(string(output)), err)
	}
	return strings.TrimSpace(string(output)) == "1", nil
}

// CapturePaneContent captures the last N lines of visible pane content.
func (r *RealTmux) CapturePaneContent(session, windowName string, lines int) (string, error) {
	target := session + ":" + windowName
	startLine := fmt.Sprintf("-%d", lines)
	cmd := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", startLine)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane -t %s: %s: %w", target, strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}

// SetEnvironment sets a tmux environment variable for a session.
func (r *RealTmux) SetEnvironment(session, key, value string) error {
	cmd := exec.Command("tmux", "set-environment", "-t", session, key, value)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux set-environment -t %s %s: %s: %w", session, key, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// SendKeysLiteral sends literal text (no Enter) to a specific target.
func (r *RealTmux) SendKeysLiteral(target, text string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", target, "-l", text)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys -l to %s: %s: %w", target, strings.TrimSpace(string(output)), err)
	}
	return nil
}

// SendKeysRaw sends a raw key name (like "Enter", "Escape") to a target.
func (r *RealTmux) SendKeysRaw(target, key string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", target, key)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys %s to %s: %s: %w", key, target, strings.TrimSpace(string(output)), err)
	}
	return nil
}
