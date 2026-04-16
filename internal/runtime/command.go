package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const (
	defaultHealthTimeout  = 30 * time.Second
	defaultHealthInterval = 500 * time.Millisecond
)

// ErrHealthTimeout is returned (wrapped) by Up when the health check does not
// succeed within the configured health timeout. Callers can match on it with
// errors.Is to distinguish timeout from cancellation or other failures.
var ErrHealthTimeout = errors.New("runtime: health check timed out")

// Command is a Provider that shells out to user-defined Up/Health/Down commands
// from a Config. Empty commands are treated as no-ops.
type Command struct {
	cfg            Config
	healthTimeout  time.Duration
	healthInterval time.Duration
}

// NewCommand builds a command-based runtime provider from a Config.
// It is the caller's responsibility to ensure cfg is populated — empty
// commands are treated as no-ops (Up runs nothing, Health always succeeds,
// Down runs nothing). Pass zero Config to get behavior equivalent to Noop.
func NewCommand(cfg Config) *Command {
	return &Command{
		cfg:            cfg,
		healthTimeout:  defaultHealthTimeout,
		healthInterval: defaultHealthInterval,
	}
}

// WithHealthTimeout sets the total time Up will spend waiting for Health to
// succeed. Non-positive values are ignored so callers can't accidentally
// disable health polling by passing 0.
func (c *Command) WithHealthTimeout(d time.Duration) *Command {
	if d > 0 {
		c.healthTimeout = d
	}
	return c
}

// WithHealthInterval sets the delay between Health probes during Up.
// Non-positive values are ignored to prevent a tight retry loop.
func (c *Command) WithHealthInterval(d time.Duration) *Command {
	if d > 0 {
		c.healthInterval = d
	}
	return c
}

// Up starts the dev stack by running cfg.Up (if set), then polls Health until
// it succeeds or the health timeout elapses. Returns cfg.Endpoints on success.
func (c *Command) Up(ctx context.Context) ([]Endpoint, error) {
	if c.cfg.Up == "" && c.cfg.Health == "" {
		return c.cfg.Endpoints, nil
	}

	if c.cfg.Up != "" {
		if err := runShell(ctx, c.cfg.Up); err != nil {
			return nil, fmt.Errorf("runtime: up: %w", err)
		}
	}

	pollCtx, cancel := context.WithTimeout(ctx, c.healthTimeout)
	defer cancel()

	ticker := time.NewTicker(c.healthInterval)
	defer ticker.Stop()

	var lastHealthErr error
	for {
		if err := c.Health(pollCtx); err == nil {
			return c.cfg.Endpoints, nil
		} else {
			lastHealthErr = err
		}

		// Parent cancel wins over our derived timeout.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("runtime: up: %w", ctx.Err())
		}
		if pollCtx.Err() != nil {
			if lastHealthErr != nil {
				return nil, fmt.Errorf("%w after %s (last health error: %v)", ErrHealthTimeout, c.healthTimeout, lastHealthErr)
			}
			return nil, fmt.Errorf("%w after %s", ErrHealthTimeout, c.healthTimeout)
		}

		select {
		case <-pollCtx.Done():
			// Loop; the checks above classify parent-cancel vs timeout.
		case <-ticker.C:
		}
	}
}

// Health runs cfg.Health and returns nil on exit 0. If cfg.Health is empty,
// Health always returns nil.
func (c *Command) Health(ctx context.Context) error {
	if c.cfg.Health == "" {
		return nil
	}
	return runShell(ctx, c.cfg.Health)
}

// Down stops the dev stack by running cfg.Down. Returns nil if cfg.Down is empty.
func (c *Command) Down(ctx context.Context) error {
	if c.cfg.Down == "" {
		return nil
	}
	if err := runShell(ctx, c.cfg.Down); err != nil {
		return fmt.Errorf("runtime: down: %w", err)
	}
	return nil
}

// runShell executes cmd via "sh -c <cmd>" in its own process group so that
// context cancellation kills not just the shell but every child it spawned
// (pipelines, background jobs, docker-compose, etc.). WaitDelay bounds how
// long we'll wait after signaling before forcing stdio pipes closed.
func runShell(ctx context.Context, cmd string) error {
	var stderr bytes.Buffer
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Stderr = &stderr
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		// Negative pid targets the process group; this kills every child
		// the shell spawned, not just the shell itself.
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	c.WaitDelay = 5 * time.Second
	if err := c.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", err, stderr.String())
		}
		return err
	}
	return nil
}
