package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

const (
	defaultHealthTimeout  = 30 * time.Second
	defaultHealthInterval = 500 * time.Millisecond
)

// Command is a Provider that shells out to user-defined Up/Health/Down commands
// from a Config. Empty commands are treated as no-ops.
type Command struct {
	cfg             Config
	healthTimeout   time.Duration
	healthInterval  time.Duration
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

// WithHealthTimeout sets the total time Up will spend waiting for Health to succeed.
func (c *Command) WithHealthTimeout(d time.Duration) *Command {
	c.healthTimeout = d
	return c
}

// WithHealthInterval sets the delay between Health probes during Up.
func (c *Command) WithHealthInterval(d time.Duration) *Command {
	c.healthInterval = d
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

	deadline := time.Now().Add(c.healthTimeout)
	for {
		err := c.Health(ctx)
		if err == nil {
			return c.cfg.Endpoints, nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("runtime: up: %w", ctx.Err())
		default:
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("runtime: health check did not succeed within %s", c.healthTimeout)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("runtime: up: %w", ctx.Err())
		case <-time.After(c.healthInterval):
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

// runShell executes cmd via "sh -c <cmd>", capturing stderr for error messages.
func runShell(ctx context.Context, cmd string) error {
	var stderr bytes.Buffer
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", err, stderr.String())
		}
		return err
	}
	return nil
}
