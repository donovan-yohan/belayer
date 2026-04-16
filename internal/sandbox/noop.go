package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Noop is a no-isolation driver that runs commands via direct exec.
// It has zero overhead and is the default driver for Belayer.
// Use this when no sandbox isolation is needed.
type Noop struct{}

// Create returns a handle immediately using the config name as the ID.
// No environment is actually provisioned — direct exec needs no setup.
func (n *Noop) Create(_ context.Context, cfg Config) (Handle, error) {
	return Handle{ID: cfg.Name}, nil
}

// Exec runs cmd directly on the host using exec.CommandContext.
// The process is started and its *os.Process is returned; the caller is
// responsible for waiting on it.
func (n *Noop) Exec(ctx context.Context, _ Handle, cmd []string, opts ExecOpts) (*os.Process, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("sandbox/noop: exec requires at least one argument")
	}

	//nolint:gosec // cmd is controlled by internal callers, not user input
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.Env = opts.Env
	c.Dir = opts.Dir
	c.Stdin = opts.Stdin
	c.Stdout = opts.Stdout
	c.Stderr = opts.Stderr

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("sandbox/noop: start process: %w", err)
	}
	return c.Process, nil
}

// Stop is a no-op for the noop driver — there is no sandbox environment to tear down.
func (n *Noop) Stop(_ context.Context, _ Handle) error {
	return nil
}
