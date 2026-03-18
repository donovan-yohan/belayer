package envprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// Client shells out to the configured provider command to manage environments.
type Client struct {
	Command    string
	Subcommand string
	CragName   string
}

// NewClient creates a Client that invokes [command subcommand action --flags... --json].
// cragName is appended as --crag <name> to every command when non-empty.
func NewClient(command, subcommand, cragName string) *Client {
	return &Client{Command: command, Subcommand: subcommand, CragName: cragName}
}

// run executes the provider command for the given action and flags, always appending --json.
// On non-zero exit it attempts to parse an ErrorResponse from output and returns a descriptive error.
func (c *Client) run(ctx context.Context, action string, flags ...string) ([]byte, error) {
	baseSize := 2 + len(flags) + 1
	if c.CragName != "" {
		baseSize += 2
	}
	args := make([]string, 0, baseSize)
	args = append(args, c.Subcommand, action)
	if c.CragName != "" {
		args = append(args, "--crag", c.CragName)
	}
	args = append(args, flags...)
	args = append(args, "--json")

	cmd := exec.CommandContext(ctx, c.Command, args...)

	// Process group isolation: kill the entire process tree on context cancellation.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Prefer stdout for JSON error; fall back to stderr.
			payload := out
			if len(payload) == 0 {
				payload = exitErr.Stderr
			}
			var errResp ErrorResponse
			if jsonErr := json.Unmarshal(payload, &errResp); jsonErr == nil && errResp.Error != "" {
				return nil, fmt.Errorf("envprovider %s: %s (code: %s)", action, errResp.Error, errResp.Code)
			}
			if len(payload) == 0 {
				return nil, fmt.Errorf("envprovider %s: exit %d (no output)", action, exitErr.ExitCode())
			}
			return nil, fmt.Errorf("envprovider %s: exit %d: %s", action, exitErr.ExitCode(), string(payload))
		}
		return nil, fmt.Errorf("envprovider %s: %w", action, err)
	}
	return out, nil
}

// CreateEnv creates a new environment with the given name and optional snapshot.
func (c *Client) CreateEnv(ctx context.Context, name, snapshot string) (*CreateEnvResponse, error) {
	flags := []string{"--name", name}
	if snapshot != "" {
		flags = append(flags, "--snapshot", snapshot)
	}
	out, err := c.run(ctx, "create", flags...)
	if err != nil {
		return nil, err
	}
	var resp CreateEnvResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("envprovider create: parse response: %w", err)
	}
	return &resp, nil
}

// AddWorktree adds a worktree to an existing environment.
func (c *Client) AddWorktree(ctx context.Context, envName, repo, branch, baseRef string) (*AddWorktreeResponse, error) {
	flags := []string{"--name", envName, "--repo", repo, "--branch", branch}
	if baseRef != "" {
		flags = append(flags, "--base-ref", baseRef)
	}
	out, err := c.run(ctx, "add-worktree", flags...)
	if err != nil {
		return nil, err
	}
	var resp AddWorktreeResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("envprovider add-worktree: parse response: %w", err)
	}
	return &resp, nil
}

// RemoveWorktree removes a worktree from an environment.
func (c *Client) RemoveWorktree(ctx context.Context, envName, repo, branch string) error {
	out, err := c.run(ctx, "remove-worktree", "--name", envName, "--repo", repo, "--branch", branch)
	if err != nil {
		return err
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("envprovider remove-worktree: parse response: %w", err)
	}
	if resp.Status != "ok" {
		return fmt.Errorf("envprovider remove-worktree: unexpected status %q", resp.Status)
	}
	return nil
}

// ResetEnv resets an environment, optionally restoring a snapshot.
func (c *Client) ResetEnv(ctx context.Context, envName, snapshot string) error {
	flags := []string{"--name", envName}
	if snapshot != "" {
		flags = append(flags, "--snapshot", snapshot)
	}
	out, err := c.run(ctx, "reset", flags...)
	if err != nil {
		return err
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("envprovider reset: parse response: %w", err)
	}
	if resp.Status != "ok" {
		return fmt.Errorf("envprovider reset: unexpected status %q", resp.Status)
	}
	return nil
}

// DestroyEnv tears down an environment.
func (c *Client) DestroyEnv(ctx context.Context, envName string) error {
	out, err := c.run(ctx, "destroy", "--name", envName)
	if err != nil {
		return err
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("envprovider destroy: parse response: %w", err)
	}
	if resp.Status != "ok" {
		return fmt.Errorf("envprovider destroy: unexpected status %q", resp.Status)
	}
	return nil
}

// StatusEnv returns the current status of an environment.
func (c *Client) StatusEnv(ctx context.Context, envName string) (*StatusEnvResponse, error) {
	out, err := c.run(ctx, "status", "--name", envName)
	if err != nil {
		return nil, err
	}
	var resp StatusEnvResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("envprovider status: parse response: %w", err)
	}
	return &resp, nil
}

// LogsEnv returns log lines for a service in an environment.
func (c *Client) LogsEnv(ctx context.Context, envName, service string) (*LogsEnvResponse, error) {
	flags := []string{"--name", envName}
	if service != "" {
		flags = append(flags, "--service", service)
	}
	out, err := c.run(ctx, "logs", flags...)
	if err != nil {
		return nil, err
	}
	var resp LogsEnvResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("envprovider logs: parse response: %w", err)
	}
	return &resp, nil
}

// ListEnvs returns a summary of all environments.
func (c *Client) ListEnvs(ctx context.Context) (*ListEnvsResponse, error) {
	out, err := c.run(ctx, "list")
	if err != nil {
		return nil, err
	}
	var resp ListEnvsResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("envprovider list: parse response: %w", err)
	}
	return &resp, nil
}
