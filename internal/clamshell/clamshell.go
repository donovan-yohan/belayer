package clamshell

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

var execCommand = exec.Command

// WorkspaceMount is a host:container workspace mount for a clamshell sandbox.
type WorkspaceMount struct {
	HostPath string
	Target   string
}

// SandboxConfig configures a sandbox creation request.
type SandboxConfig struct {
	Name       string
	PolicyPath string
	Workspaces []WorkspaceMount
	Command    []string
}

// BuildCreateArgs builds the clamshell CLI arguments for sandbox creation.
func BuildCreateArgs(cfg SandboxConfig) ([]string, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("clamshell: sandbox name is required")
	}
	if cfg.PolicyPath == "" {
		return nil, fmt.Errorf("clamshell: policy path is required")
	}
	args := []string{"sandbox", "create", "--name", cfg.Name, "--policy", cfg.PolicyPath}
	for _, mount := range cfg.Workspaces {
		if mount.HostPath == "" || mount.Target == "" {
			return nil, fmt.Errorf("clamshell: workspace mounts require host and target paths")
		}
		args = append(args, "--workspace", mount.HostPath+":"+mount.Target)
	}
	if len(cfg.Command) > 0 {
		args = append(args, "--")
		args = append(args, cfg.Command...)
	}
	return args, nil
}

// CreateSandbox invokes `clamshell sandbox create`.
func CreateSandbox(cfg SandboxConfig, stdout, stderr *os.File) error {
	args, err := BuildCreateArgs(cfg)
	if err != nil {
		return err
	}
	cmd := execCommand("clamshell", args...)
	var stderrBuf bytes.Buffer
	if stdout != nil {
		cmd.Stdout = stdout
	}
	if stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, &stderrBuf)
	} else {
		cmd.Stderr = &stderrBuf
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clamshell create %s: %w: %s", cfg.Name, err, strings.TrimSpace(stderrBuf.String()))
	}
	return nil
}

// ConnectSandbox invokes `clamshell sandbox connect` for interactive attach.
func ConnectSandbox(name string) error {
	if name == "" {
		return fmt.Errorf("clamshell: sandbox name is required")
	}
	cmd := execCommand("clamshell", "sandbox", "connect", "--name", name)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
