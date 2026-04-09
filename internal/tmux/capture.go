package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// StartCapture begins streaming all pane output for the named session to
// outputPath by attaching a `tmux pipe-pane` process. The output directory is
// created if it does not already exist.
func StartCapture(session, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("start capture for session %q: create output dir: %w", session, err)
	}

	target := sessionPrefix + session
	pipeCmd := fmt.Sprintf("cat >> '%s'", outputPath)
	cmd := exec.Command("tmux", "pipe-pane", "-t", target, pipeCmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start capture for session %q: tmux pipe-pane: %w: %s", session, err, string(out))
	}
	return nil
}

// StopCapture stops the pipe-pane capture for the named session by sending an
// empty command to `tmux pipe-pane`, which detaches any active pipe.
func StopCapture(session string) error {
	target := sessionPrefix + session
	cmd := exec.Command("tmux", "pipe-pane", "-t", target)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("stop capture for session %q: tmux pipe-pane: %w: %s", session, err, string(out))
	}
	return nil
}
