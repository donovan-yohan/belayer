package session

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ExecSpawner implements Spawner by executing a shell command.
type ExecSpawner struct{}

// Spawn executes opts.Command via sh -c in the background. It sets BELAYER_*
// environment variables and returns a channel that receives an error if the
// process exits non-zero.
func (e *ExecSpawner) Spawn(ctx context.Context, opts SpawnOpts) (<-chan error, error) {
	if opts.Command == "" {
		return nil, fmt.Errorf("node %q: command is empty", opts.NodeName)
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", opts.Command)
	cmd.Dir = opts.WorkDir
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 10 * time.Second

	cmd.Env = append(os.Environ(),
		"BELAYER_TASK_ID="+opts.TaskID,
		"BELAYER_NODE="+opts.NodeName,
		"BELAYER_ATTEMPT="+strconv.Itoa(opts.Attempt),
		"BELAYER_WORK_DIR="+opts.WorkDir,
	)

	var stderrBuf bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command for node %q: %w", opts.NodeName, err)
	}

	exitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			stderr := strings.TrimSpace(stderrBuf.String())
			if stderr != "" {
				exitCh <- fmt.Errorf("node %q command exited: %w\nstderr: %s", opts.NodeName, err, stderr)
			} else {
				exitCh <- fmt.Errorf("node %q command exited: %w", opts.NodeName, err)
			}
		}
		close(exitCh)
	}()

	return exitCh, nil
}
