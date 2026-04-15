package tmux

import "time"

// Runner is the interface for managing tmux sessions.
// All session names are automatically prefixed with "belayer-" to avoid conflicts.
type Runner interface {
	// CreateSession creates a new detached tmux session running the given command.
	CreateSession(name, cmd string) error

	// SendKeys sends keystrokes to a tmux session.
	// When bracketed is true, the keys are wrapped in bracketed-paste escape sequences.
	SendKeys(session, keys string, bracketed bool) error

	// SendEnter sends an Enter keypress to a tmux session.
	SendEnter(session string) error

	// CapturePane returns the current visible content of the session's pane.
	CapturePane(session string) (string, error)

	// KillSession terminates a tmux session.
	KillSession(session string) error

	// WaitForSession blocks until the session exits or the timeout elapses.
	// Returns nil if the session exited cleanly, error on timeout or other failure.
	WaitForSession(session string, timeout time.Duration) error

	// ListSessions returns the names (without the "belayer-" prefix) of all
	// belayer-managed tmux sessions currently running.
	ListSessions() ([]string, error)
}
