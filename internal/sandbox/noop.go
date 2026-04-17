package sandbox

import (
	"context"
	"fmt"
	"os/exec"
)

func init() {
	Register("noop", &Noop{})
}

// Noop is a no-isolation driver that runs commands via direct exec.
// It has zero overhead and is the default driver for Belayer.
// Use this when no sandbox isolation is needed.
type Noop struct{}

// Create returns a handle immediately using the config name as the ID.
// No environment is actually provisioned — direct exec needs no setup.
func (n *Noop) Create(_ context.Context, cfg Config) (Handle, error) {
	return Handle{ID: cfg.Name}, nil
}

// Exec runs cmd directly on the host using exec.CommandContext and returns
// a Process that wraps the *exec.Cmd so callers get cmd.Wait() semantics
// (stdio pumps drained, CommandContext watcher released).
func (n *Noop) Exec(ctx context.Context, _ Handle, cmd []string, opts ExecOpts) (Process, error) {
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
	return &noopProcess{cmd: c}, nil
}

// Stop is a no-op for the noop driver — there is no sandbox environment to tear down.
func (n *Noop) Stop(_ context.Context, _ Handle) error {
	return nil
}

// noopProcess wraps *exec.Cmd to satisfy Process. Using cmd.Wait (rather than
// cmd.Process.Wait) is required so that stdout/stderr copy goroutines spawned
// by exec when Stdout/Stderr are non-*os.File writers have finished before
// Wait returns.
type noopProcess struct {
	cmd *exec.Cmd
}

func (p *noopProcess) Pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *noopProcess) Wait() error {
	return p.cmd.Wait()
}

func (p *noopProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}
