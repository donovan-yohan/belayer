package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// ExecSpawner implements Spawner by executing a shell command.
type ExecSpawner struct{}

// Spawn executes opts.Command via sh -c in the background. It sets BELAYER_*
// environment variables and returns a channel that fires if the process exits
// non-zero before a completion file is written.
func (e *ExecSpawner) Spawn(_ context.Context, opts SpawnOpts) (<-chan error, error) {
	if opts.Command == "" {
		return nil, fmt.Errorf("node %q: command is empty", opts.NodeName)
	}

	cmd := exec.Command("sh", "-c", opts.Command)
	cmd.Dir = opts.WorkDir
	cmd.Env = append(os.Environ(),
		"BELAYER_TASK_ID="+opts.TaskID,
		"BELAYER_NODE="+opts.NodeName,
		"BELAYER_ATTEMPT="+strconv.Itoa(opts.Attempt),
		"BELAYER_WORK_DIR="+opts.WorkDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start command for node %q: %w", opts.NodeName, err)
	}

	exitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			exitCh <- fmt.Errorf("node %q command exited: %w", opts.NodeName, err)
		}
		close(exitCh)
	}()

	return exitCh, nil
}
