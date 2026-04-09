package docker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// SandboxConfig holds configuration for a Docker sandbox.
type SandboxConfig struct {
	SessionID      string
	Image          string          // default: "ubuntu:24.04"
	EnvFile        string          // path to .env file to mount (optional)
	AllowedDomains []string        // domains for network allowlisting (config marker only)
	WorkDir        string          // working directory inside container (default: /workspace)
	Ports          map[int]int     // host:container port mappings
}

// Sandbox manages the lifecycle of a Docker Compose-based sandbox container.
type Sandbox struct {
	config     SandboxConfig
	composeDir string // directory containing the generated docker-compose.yml
}

// NewSandbox validates config, sets defaults, and returns a Sandbox ready for Create.
func NewSandbox(cfg SandboxConfig) (*Sandbox, error) {
	if cfg.SessionID == "" {
		return nil, fmt.Errorf("docker: sandbox: SessionID is required")
	}
	if cfg.Image == "" {
		cfg.Image = "ubuntu:24.04"
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = "/workspace"
	}
	return &Sandbox{config: cfg}, nil
}

// Create generates the docker-compose.yml in ~/.belayer/sandboxes/<sessionID>/.
// Must be called before Start.
func (s *Sandbox) Create() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("docker: sandbox: get home dir: %w", err)
	}

	dir := filepath.Join(home, ".belayer", "sandboxes", s.config.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("docker: sandbox: create compose dir: %w", err)
	}

	content, err := generateCompose(s.config)
	if err != nil {
		return fmt.Errorf("docker: sandbox: generate compose: %w", err)
	}

	composePath := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(composePath, content, 0o644); err != nil {
		return fmt.Errorf("docker: sandbox: write compose file: %w", err)
	}

	s.composeDir = dir
	return nil
}

// Start runs `docker compose up -d` for the sandbox.
// Create must be called first.
func (s *Sandbox) Start() error {
	if s.composeDir == "" {
		return fmt.Errorf("docker: sandbox: must call Create before Start")
	}
	composePath := filepath.Join(s.composeDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composePath, "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker: sandbox: start: %w", err)
	}
	return nil
}

// Stop runs `docker compose down` to tear down the sandbox.
// If warn is true, a warning message is printed to stderr before stopping.
func (s *Sandbox) Stop(warn bool) error {
	if s.composeDir == "" {
		return fmt.Errorf("docker: sandbox: must call Create before Stop")
	}
	if warn {
		fmt.Fprintln(os.Stderr, "warning: stopping sandbox for session", s.config.SessionID)
	}
	composePath := filepath.Join(s.composeDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composePath, "down")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker: sandbox: stop: %w", err)
	}
	return nil
}

// Exec runs a command inside the "agent" service container and returns its combined output.
func (s *Sandbox) Exec(command string) (string, error) {
	if s.composeDir == "" {
		return "", fmt.Errorf("docker: sandbox: must call Create before Exec")
	}
	composePath := filepath.Join(s.composeDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composePath, "exec", "-T", "agent", "sh", "-c", command)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("docker: sandbox: exec: %w", err)
	}
	return out.String(), nil
}

// ComposeDir returns the directory containing the generated docker-compose.yml.
func (s *Sandbox) ComposeDir() string {
	return s.composeDir
}
