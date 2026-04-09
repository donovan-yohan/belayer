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
	ComposeConfig ComposeConfig
}

// Sandbox manages the lifecycle of a Docker Compose-based sandbox container.
type Sandbox struct {
	config     SandboxConfig
	agentNames []string // names of agent services for cleanup
	composeDir string   // directory containing the generated docker-compose.yml
}

// NewSandbox validates config, sets defaults, and returns a Sandbox ready for Create.
func NewSandbox(cfg SandboxConfig) (*Sandbox, error) {
	if cfg.ComposeConfig.SessionID == "" {
		return nil, fmt.Errorf("docker: sandbox: SessionID is required")
	}

	// Apply defaults to each agent.
	for i := range cfg.ComposeConfig.Agents {
		if cfg.ComposeConfig.Agents[i].Image == "" {
			cfg.ComposeConfig.Agents[i].Image = "belayer/agent:latest"
		}
		if cfg.ComposeConfig.Agents[i].WorkDir == "" {
			cfg.ComposeConfig.Agents[i].WorkDir = "/workspace"
		}
	}

	agentNames := make([]string, 0, len(cfg.ComposeConfig.Agents))
	for _, a := range cfg.ComposeConfig.Agents {
		agentNames = append(agentNames, a.Name)
	}

	return &Sandbox{config: cfg, agentNames: agentNames}, nil
}

// Create generates the docker-compose.yml in ~/.belayer/sandboxes/<sessionID>/.
// Must be called before Start.
func (s *Sandbox) Create() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("docker: sandbox: get home dir: %w", err)
	}

	dir := filepath.Join(home, ".belayer", "sandboxes", s.config.ComposeConfig.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("docker: sandbox: create compose dir: %w", err)
	}

	content, err := generateCompose(s.config.ComposeConfig)
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
		fmt.Fprintln(os.Stderr, "warning: stopping sandbox for session", s.config.ComposeConfig.SessionID)
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

// Exec runs a command inside the named agent service container and returns its combined output.
// If agentName is empty, the first agent in the config is used.
func (s *Sandbox) Exec(agentName, command string) (string, error) {
	if s.composeDir == "" {
		return "", fmt.Errorf("docker: sandbox: must call Create before Exec")
	}
	if agentName == "" && len(s.agentNames) > 0 {
		agentName = s.agentNames[0]
	}
	if agentName == "" {
		return "", fmt.Errorf("docker: sandbox: no agent name specified and no agents configured")
	}
	composePath := filepath.Join(s.composeDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composePath, "exec", "-T", agentName, "sh", "-c", command)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("docker: sandbox: exec: %w", err)
	}
	return out.String(), nil
}

// AgentNames returns the list of agent service names in this sandbox.
func (s *Sandbox) AgentNames() []string {
	return s.agentNames
}

// ComposeDir returns the directory containing the generated docker-compose.yml.
func (s *Sandbox) ComposeDir() string {
	return s.composeDir
}
