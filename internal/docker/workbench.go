package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// WorkbenchConfig holds configuration for creating a workbench.
type WorkbenchConfig struct {
	SessionID     string
	Spec          WorkbenchConfigSpec
	WorktreePaths map[string]string
	Networks      []string
}

// WorkbenchStatus represents the status of a workbench service.
type WorkbenchStatus struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Health string `json:"health"`
}

// Workbench manages the lifecycle of a Docker Compose-based workbench.
type Workbench struct {
	config     WorkbenchConfig
	composeDir string
}

// NewWorkbench validates config and returns a Workbench ready for Create.
func NewWorkbench(cfg WorkbenchConfig) (*Workbench, error) {
	if cfg.SessionID == "" {
		return nil, fmt.Errorf("docker: workbench: SessionID is required")
	}
	if len(cfg.Spec.Services) == 0 {
		return nil, fmt.Errorf("docker: workbench: Spec.Services is required")
	}

	// Apply default timeout if not set
	if cfg.Spec.Timeout == "" {
		cfg.Spec.Timeout = "5m"
	}

	// Default networks if not specified
	if len(cfg.Networks) == 0 {
		cfg.Networks = []string{"workbench-net", "infra-net"}
	}

	return &Workbench{config: cfg}, nil
}

// Create generates the docker-compose.yml in ~/.belayer/workbenches/<sessionID>/.
// Must be called before Start.
func (w *Workbench) Create() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("docker: workbench: get home dir: %w", err)
	}

	dir := filepath.Join(home, ".belayer", "workbenches", w.config.SessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("docker: workbench: create compose dir: %w", err)
	}

	content, err := generateWorkbenchCompose(w.config)
	if err != nil {
		return fmt.Errorf("docker: workbench: generate compose: %w", err)
	}

	composePath := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(composePath, content, 0o600); err != nil {
		return fmt.Errorf("docker: workbench: write compose file: %w", err)
	}

	w.composeDir = dir
	return nil
}

// Start runs `docker compose up -d` for the workbench.
// Create must be called first.
func (w *Workbench) Start() error {
	if w.composeDir == "" {
		return fmt.Errorf("docker: workbench: must call Create before Start")
	}
	composePath := filepath.Join(w.composeDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composePath, "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker: workbench: start: %w", err)
	}
	return nil
}

// Status runs `docker compose ps --format json` and parses the output.
// Create must be called first.
func (w *Workbench) Status() ([]WorkbenchStatus, error) {
	if w.composeDir == "" {
		return nil, fmt.Errorf("docker: workbench: must call Create before Status")
	}
	composePath := filepath.Join(w.composeDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composePath, "ps", "--format", "json")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker: workbench: status: %w", err)
	}

	// Docker Compose ps --format json returns a JSON array
	var statuses []WorkbenchStatus
	if err := json.Unmarshal(out.Bytes(), &statuses); err != nil {
		return nil, fmt.Errorf("docker: workbench: parse status: %w", err)
	}

	return statuses, nil
}

// WaitForHealthy polls Status() until all services are healthy or timeout exceeded.
// Create must be called first.
func (w *Workbench) WaitForHealthy(timeout time.Duration) error {
	if w.composeDir == "" {
		return fmt.Errorf("docker: workbench: must call Create before WaitForHealthy")
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			statuses, err := w.Status()
			if err != nil {
				return fmt.Errorf("docker: workbench: wait for healthy: %w", err)
			}

			allHealthy := true
			for _, s := range statuses {
				if s.Health != "healthy" {
					allHealthy = false
					break
				}
			}

			if allHealthy && len(statuses) > 0 {
				return nil
			}

			if time.Now().After(deadline) {
				return fmt.Errorf("docker: workbench: wait for healthy: timeout exceeded")
			}
		}
	}
}

// Stop runs `docker compose down` to tear down the workbench.
// Create must be called first.
func (w *Workbench) Stop() error {
	if w.composeDir == "" {
		return fmt.Errorf("docker: workbench: must call Create before Stop")
	}
	composePath := filepath.Join(w.composeDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composePath, "down")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker: workbench: stop: %w", err)
	}
	return nil
}

// ComposeDir returns the directory containing the generated docker-compose.yml.
func (w *Workbench) ComposeDir() string {
	return w.composeDir
}

const workbenchComposeTmpl = `version: '3.9'

networks:
  workbench-net:
    name: workbench-{{ .SessionID }}
    internal: true
  infra-net:
    name: infra-{{ .SessionID }}
    driver: bridge

services:
{{ range .Services }}  {{ .Name }}:
{{ if .Build }}    build:
      context: {{ .Build }}
{{ end }}{{ if .Image }}    image: {{ .Image }}
{{ end }}{{ if .Environment }}    environment:
{{ range $k, $v := .Environment }}      {{ $k }}: "{{ $v }}"
{{ end }}{{ end }}{{ if .DependsOn }}    depends_on:
{{ range .DependsOn }}      - {{ . }}
{{ end }}{{ end }}{{ if .HealthCheck }}    healthcheck:
      test: {{ .HealthCheck.Test }}
      interval: {{ .HealthCheck.Interval }}
      timeout: {{ .HealthCheck.Timeout }}
      retries: {{ .HealthCheck.Retries }}
{{ end }}    networks:
      - workbench-net
{{ if .IsInfra }}      - infra-net
{{ end }}
{{ end }}`

type workbenchServiceTemplateData struct {
	Name        string
	Build       string
	Image       string
	Ports       []string
	Environment map[string]string
	DependsOn   []string
	HealthCheck *HealthDecl
	Command     []string
	IsInfra     bool
}

type workbenchTemplateData struct {
	SessionID string
	Services  []workbenchServiceTemplateData
}

// generateWorkbenchCompose returns docker-compose.yml content for the given WorkbenchConfig.
func generateWorkbenchCompose(cfg WorkbenchConfig) ([]byte, error) {
	tmpl, err := template.New("workbench").Parse(workbenchComposeTmpl)
	if err != nil {
		return nil, fmt.Errorf("docker: parse workbench template: %w", err)
	}

	services := make([]workbenchServiceTemplateData, 0, len(cfg.Spec.Services))
	for _, svc := range cfg.Spec.Services {
		// Substitute worktree paths in Build field
		build := svc.Build
		if build != "" {
			for name, path := range cfg.WorktreePaths {
				placeholder := fmt.Sprintf("${WORKTREE_%s}", name)
				build = strings.ReplaceAll(build, placeholder, path)
			}
		}

		// Check if this is an infra service (in infra-net)
		isInfra := false
		for _, net := range cfg.Networks {
			if net == "infra-net" && svc.Name == "infra" {
				isInfra = true
				break
			}
		}

		services = append(services, workbenchServiceTemplateData{
			Name:        svc.Name,
			Build:       build,
			Image:       svc.Image,
			Ports:       svc.Ports,
			Environment: svc.Env,
			DependsOn:   svc.Depends,
			HealthCheck: svc.Health,
			IsInfra:     isInfra,
		})
	}

	data := workbenchTemplateData{
		SessionID: cfg.SessionID,
		Services:  services,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("docker: execute workbench template: %w", err)
	}

	return buf.Bytes(), nil
}
