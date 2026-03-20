package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"syscall"

	"github.com/donovan-yohan/belayer/internal/v2/role"
)

// ExecProvider runs a Type A (pitch) role by shelling out to a command.
// JSON is passed on stdin; JSON is read from stdout.
// Pattern matches internal/envprovider/client.go.
type ExecProvider struct{}

// Execute runs the role's provider command with JSON input on stdin.
func (e *ExecProvider) Execute(ctx context.Context, roleDef role.RoleDef, input json.RawMessage) (json.RawMessage, error) {
	if roleDef.Provider.Command == "" {
		return nil, fmt.Errorf("exec provider for role %q: no command configured", roleDef.Name)
	}

	args := append([]string{}, roleDef.Provider.Args...)
	cmd := exec.CommandContext(ctx, roleDef.Provider.Command, args...)

	// Pass input JSON on stdin.
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}

	// Process group isolation: kill the entire process tree on context cancellation.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := stderr.String()
		if stderrStr != "" {
			return nil, fmt.Errorf("exec provider %q: %w\nstderr: %s", roleDef.Name, err, stderrStr)
		}
		return nil, fmt.Errorf("exec provider %q: %w", roleDef.Name, err)
	}

	out := stdout.Bytes()
	if len(out) == 0 {
		return nil, fmt.Errorf("exec provider %q: no output on stdout", roleDef.Name)
	}

	// Validate output is valid JSON.
	if !json.Valid(out) {
		return nil, fmt.Errorf("exec provider %q: stdout is not valid JSON: %s", roleDef.Name, truncate(string(out), 200))
	}

	return json.RawMessage(out), nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
