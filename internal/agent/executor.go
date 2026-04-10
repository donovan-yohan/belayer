package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/donovan-yohan/belayer/internal/shell"
)

// RenderError is returned when a tool's command template cannot be rendered,
// typically due to missing or invalid input keys. The daemon maps this to a
// 400 response so callers can distinguish bad input from infrastructure failures.
type RenderError struct {
	err error
}

func (e *RenderError) Error() string { return e.err.Error() }
func (e *RenderError) Unwrap() error { return e.err }

// ExecResult holds the outcome of a tool execution.
type ExecResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	DurationMS int64
}

// Executor runs ToolSpec commands against the correct execution target.
// All user-provided input values are shell-quoted via shell.Quote before
// being substituted into command templates — raw user input never reaches
// the shell unquoted.
type Executor struct {
	// SandboxDir is the directory that contains the docker-compose.yml for the
	// session. For compose-based targets ("agent", "workbench", "infra") this
	// must point to the session's sandbox directory, e.g.:
	//   ~/.belayer/sandboxes/<sessionID>/
	SandboxDir string
}

// Execute renders the tool's command template with the provided input values,
// routes the rendered command to the correct target, and returns the result.
//
// Security contract:
//   - Every {{.field}} substitution uses shell.Quote on the input value.
//   - Host execution is opt-in: only allowed when spec.Exec.Target == "host".
//   - Docker compose targets exec into the named service via sh -c.
func (e *Executor) Execute(ctx context.Context, spec ToolSpec, input map[string]string) (ExecResult, error) {
	if spec.Exec.Target == "" {
		return ExecResult{}, fmt.Errorf("executor: tool %q has empty exec target", spec.Name)
	}
	if !ValidTargets[spec.Exec.Target] {
		return ExecResult{}, fmt.Errorf("executor: tool %q has invalid exec target %q", spec.Name, spec.Exec.Target)
	}

	// Render the command template with shell-safe quoting.
	rendered, err := renderCommand(spec.Exec.Command, input)
	if err != nil {
		return ExecResult{}, &RenderError{err: fmt.Errorf("executor: render command for tool %q: %w", spec.Name, err)}
	}

	// Apply timeout.
	timeout := time.Duration(spec.Exec.EffectiveTimeout()) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	result, err := e.run(execCtx, spec.Exec.Target, rendered)
	result.DurationMS = time.Since(start).Milliseconds()
	return result, err
}

// run dispatches to the appropriate execution target.
func (e *Executor) run(ctx context.Context, target, command string) (ExecResult, error) {
	switch target {
	case "agent", "workbench", "infra":
		return e.runCompose(ctx, target, command)
	case "host":
		return runHost(ctx, command)
	default:
		return ExecResult{}, fmt.Errorf("executor: unknown target %q", target)
	}
}

// runCompose executes command in a docker compose service.
// The compose file is expected at SandboxDir/docker-compose.yml.
func (e *Executor) runCompose(ctx context.Context, service, command string) (ExecResult, error) {
	composePath := filepath.Join(e.SandboxDir, "docker-compose.yml")
	if e.SandboxDir == "" {
		return ExecResult{}, fmt.Errorf("executor: SandboxDir is required for compose target %q", service)
	}
	if _, err := os.Stat(composePath); err != nil {
		return ExecResult{}, fmt.Errorf("executor: compose file not found at %s: %w", composePath, err)
	}

	cmd := exec.CommandContext(ctx, "docker", "compose",
		"-f", composePath,
		"exec", "-T", service,
		"sh", "-c", command,
	)
	return captureCommand(cmd)
}

// runHost executes command directly on the host via sh -c.
// This is opt-in and should only be used when the target is explicitly "host".
func runHost(ctx context.Context, command string) (ExecResult, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	return captureCommand(cmd)
}

// captureCommand runs cmd and returns stdout, stderr, exit code, and any error.
func captureCommand(cmd *exec.Cmd) (ExecResult, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			// Non-zero exit is not an executor error — it's a tool result.
			return result, nil
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			// Context timeout/cancel: set conventional exit code 130 (SIGKILL) so
			// callers and audit logs can distinguish timeouts from infrastructure failures.
			result.ExitCode = 130
			return result, err
		}
		// Process couldn't be started or was killed by signal.
		return result, fmt.Errorf("executor: command failed: %w", err)
	}
	return result, nil
}

// renderCommand renders a Go template command string, quoting every input value
// with shell.Quote to prevent injection.
//
// Template syntax: {{.fieldName}} — field names must match keys in input.
// Example:
//
//	template: "psql $DB -c {{.query}}"
//	input:    {"query": "SELECT 1; DROP TABLE users"}
//	result:   "psql $DB -c 'SELECT 1; DROP TABLE users'"
func renderCommand(tmplStr string, input map[string]string) (string, error) {
	if tmplStr == "" {
		return "", fmt.Errorf("command template is empty")
	}

	// Build a quoted data map: every value is passed through shell.Quote.
	data := make(map[string]string, len(input))
	for k, v := range input {
		data[k] = shell.Quote(v)
	}

	tmpl, err := template.New("cmd").
		Option("missingkey=error").
		Funcs(template.FuncMap{
			// quote is available inside templates for explicit quoting, though
			// values in {{.field}} are pre-quoted via the data map above.
			"quote": shell.Quote,
		}).
		Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse command template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute command template: %w", err)
	}
	return buf.String(), nil
}
