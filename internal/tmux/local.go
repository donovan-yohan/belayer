package tmux

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const sessionPrefix = "belayer-"
const pollInterval = 500 * time.Millisecond

// execFunc is the signature used by LocalRunner to execute external commands.
// It defaults to exec.Command and can be overridden in tests.
type execFunc func(name string, args ...string) *exec.Cmd

// LocalRunner implements Runner using the local tmux binary via os/exec.
type LocalRunner struct {
	exec execFunc
}

// NewLocalRunner returns a LocalRunner backed by the system tmux binary.
func NewLocalRunner() *LocalRunner {
	return &LocalRunner{exec: exec.Command}
}

// sessionName returns the fully-qualified tmux session name for the given logical name.
func (r *LocalRunner) sessionName(name string) string {
	return sessionPrefix + name
}

// run executes a tmux subcommand and returns its combined output.
// Errors are wrapped with the tmux arguments for context.
func (r *LocalRunner) run(args ...string) (string, error) {
	cmd := r.exec("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// CreateSession creates a new detached tmux session named belayer-<name> running cmd.
func (r *LocalRunner) CreateSession(name, cmd string) error {
	_, err := r.run("new-session", "-d", "-s", r.sessionName(name), cmd)
	if err != nil {
		return fmt.Errorf("create session %q: %w", name, err)
	}
	return nil
}

// SendKeys sends keystrokes to the session.
// When bracketed is true the keys are wrapped in bracketed-paste escape sequences
// (xterm escape codes \x1b[200~ ... \x1b[201~) which prevents the terminal from
// interpreting pasted content as commands.
func (r *LocalRunner) SendKeys(session, keys string, bracketed bool) error {
	target := r.sessionName(session)
	if bracketed {
		// Send the bracketed-paste start sequence.
		if _, err := r.run("send-keys", "-t", target, "-l", "\x1b[200~"); err != nil {
			return fmt.Errorf("send bracketed start to session %q: %w", session, err)
		}
		// Send the actual payload.
		if _, err := r.run("send-keys", "-t", target, "-l", keys); err != nil {
			return fmt.Errorf("send keys to session %q: %w", session, err)
		}
		// Send the bracketed-paste end sequence.
		if _, err := r.run("send-keys", "-t", target, "-l", "\x1b[201~"); err != nil {
			return fmt.Errorf("send bracketed end to session %q: %w", session, err)
		}
		return nil
	}

	_, err := r.run("send-keys", "-t", target, "-l", keys)
	if err != nil {
		return fmt.Errorf("send keys to session %q: %w", session, err)
	}
	return nil
}

// SendEnter sends an Enter keypress to the session.
func (r *LocalRunner) SendEnter(session string) error {
	_, err := r.run("send-keys", "-t", r.sessionName(session), "Enter")
	if err != nil {
		return fmt.Errorf("send enter to session %q: %w", session, err)
	}
	return nil
}

// CapturePane returns the current visible content of the session's pane.
func (r *LocalRunner) CapturePane(session string) (string, error) {
	out, err := r.run("capture-pane", "-t", r.sessionName(session), "-p")
	if err != nil {
		return "", fmt.Errorf("capture pane for session %q: %w", session, err)
	}
	return out, nil
}

// KillSession terminates the tmux session.
func (r *LocalRunner) KillSession(session string) error {
	_, err := r.run("kill-session", "-t", r.sessionName(session))
	if err != nil {
		return fmt.Errorf("kill session %q: %w", session, err)
	}
	return nil
}

// WaitForSession polls until the session no longer exists or the timeout elapses.
// A nil return means the session exited; a non-nil return indicates timeout or error.
func (r *LocalRunner) WaitForSession(session string, timeout time.Duration) error {
	target := r.sessionName(session)
	deadline := time.Now().Add(timeout)
	for {
		cmd := r.exec("tmux", "has-session", "-t", target)
		if err := cmd.Run(); err != nil {
			// has-session returned non-zero — the session is gone.
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait for session %q: timed out after %s", session, timeout)
		}
		time.Sleep(pollInterval)
	}
}

// ListSessions returns the logical names (without the "belayer-" prefix) of all
// belayer-managed tmux sessions currently running.
func (r *LocalRunner) ListSessions() ([]string, error) {
	out, err := r.run("list-sessions", "-F", "#{session_name}")
	if err != nil {
		// tmux exits non-zero when there are no sessions at all; treat that as
		// an empty list rather than a hard error.
		if strings.Contains(err.Error(), "no server running") ||
			strings.Contains(err.Error(), "no sessions") {
			return []string{}, nil
		}
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, sessionPrefix) {
			sessions = append(sessions, strings.TrimPrefix(line, sessionPrefix))
		}
	}
	return sessions, nil
}
