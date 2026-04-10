package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"
)

var execCommand = exec.Command
var workbenchPollInterval = 5 * time.Second

// WorkbenchConfig holds configuration for creating a workbench.
type WorkbenchConfig struct {
	SessionID     string
	Spec          WorkbenchConfigSpec
	WorktreePaths map[string]string
	Networks      []string
	BaseDir       string // override compose output dir (defaults to ~/.belayer/workbenches)
}

// WorkbenchStatus represents the status of a workbench service.
type WorkbenchStatus struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Health string `json:"health"`
}

// WorkbenchEndpoint is a structured endpoint returned to callers after
// provisioning succeeds.
type WorkbenchEndpoint struct {
	Service string `json:"service"`
	URL     string `json:"url"`
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
	if strings.ContainsAny(cfg.SessionID, "/\\") {
		return nil, fmt.Errorf("docker: workbench: SessionID contains invalid path characters")
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

// Create generates the docker-compose.yml in the workbench directory.
// Uses BaseDir if set, otherwise ~/.belayer/workbenches/<sessionID>/.
// Must be called before Start.
func (w *Workbench) Create() error {
	var dir string
	if w.config.BaseDir != "" {
		dir = filepath.Join(w.config.BaseDir, w.config.SessionID)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("docker: workbench: get home dir: %w", err)
		}
		dir = filepath.Join(home, ".belayer", "workbenches", w.config.SessionID)
	}

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
	cmd := execCommand("docker", "compose", "-f", composePath, "up", "-d")
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
	cmd := execCommand("docker", "compose", "-f", composePath, "ps", "--format", "json")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker: workbench: status: %w", err)
	}

	var statuses []WorkbenchStatus
	if err := json.Unmarshal(out.Bytes(), &statuses); err != nil {
		return nil, fmt.Errorf("docker: workbench: parse status: %w", err)
	}

	return statuses, nil
}

// WaitForHealthy polls Status() until all services are healthy or timeout exceeded.
// Services without a healthcheck (health is empty or "none") are considered ready
// when their state is "running".
// Create must be called first.
func (w *Workbench) WaitForHealthy(timeout time.Duration) error {
	if w.composeDir == "" {
		return fmt.Errorf("docker: workbench: must call Create before WaitForHealthy")
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(workbenchPollInterval)
	defer ticker.Stop()

	for {
		statuses, err := w.Status()
		if err != nil {
			return fmt.Errorf("docker: workbench: wait for healthy: %w", err)
		}

		allReady := true
		for _, s := range statuses {
			health := strings.ToLower(strings.TrimSpace(s.Health))
			if health == "" || health == "none" {
				// No healthcheck defined — treat "running" as ready.
				if strings.ToLower(s.State) != "running" {
					allReady = false
					break
				}
			} else if health != "healthy" {
				allReady = false
				break
			}
		}

		if allReady && len(statuses) > 0 {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("docker: workbench: wait for healthy: timeout exceeded")
		}

		<-ticker.C
	}
}

// Stop runs `docker compose down` to tear down the workbench.
// Create must be called first.
func (w *Workbench) Stop() error {
	if w.composeDir == "" {
		return fmt.Errorf("docker: workbench: must call Create before Stop")
	}
	composePath := filepath.Join(w.composeDir, "docker-compose.yml")
	cmd := execCommand("docker", "compose", "-f", composePath, "down")
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
      context: {{ .Build.Context }}
{{ if .Build.Dockerfile }}      dockerfile: {{ .Build.Dockerfile }}
{{ end }}{{ end }}{{ if .Image }}    image: {{ .Image }}
{{ end }}{{ if .Environment }}    environment:
{{ range $k, $v := .Environment }}      {{ $k }}: {{ $v }}
{{ end }}{{ end }}{{ if .DependsOn }}    depends_on:
{{ range .DependsOn }}      {{ .Name }}:
{{ if .Condition }}        condition: {{ .Condition }}
{{ else }}        condition: service_started
{{ end }}{{ end }}{{ end }}{{ if .HealthCheck }}    healthcheck:
      test: {{ .HealthCheck.Test }}
{{ if .HealthCheck.Interval }}      interval: {{ .HealthCheck.Interval }}
{{ end }}{{ if .HealthCheck.Timeout }}      timeout: {{ .HealthCheck.Timeout }}
{{ end }}{{ if .HealthCheck.Retries }}      retries: {{ .HealthCheck.Retries }}
{{ end }}{{ if .HealthCheck.StartPeriod }}      start_period: {{ .HealthCheck.StartPeriod }}
{{ end }}{{ end }}{{ if .Ports }}    ports:
{{ range .Ports }}      - {{ . }}
{{ end }}{{ end }}    networks:
      - workbench-net
{{ range .ExtraNetworks }}      - {{ . }}
{{ end }}
{{ end }}`

type healthCheckTemplateData struct {
	Test        string // pre-formatted as JSON array
	Interval    string
	Timeout     string
	Retries     int
	StartPeriod string
}

type serviceDependencyTemplateData struct {
	Name      string
	Condition string
}

type buildTemplateData struct {
	Context    string
	Dockerfile string
}

type workbenchServiceTemplateData struct {
	Name          string
	Build         *buildTemplateData
	Image         string
	Ports         []string
	Environment   map[string]string
	DependsOn     []serviceDependencyTemplateData
	HealthCheck   *healthCheckTemplateData
	ExtraNetworks []string
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
		var build *buildTemplateData
		if !svc.Build.Empty() {
			contextPath, err := renderWorkbenchString(svc.Build.Context, cfg)
			if err != nil {
				return nil, fmt.Errorf("docker: render build context for %s: %w", svc.Name, err)
			}
			dockerfilePath, err := renderWorkbenchString(svc.Build.Dockerfile, cfg)
			if err != nil {
				return nil, fmt.Errorf("docker: render dockerfile for %s: %w", svc.Name, err)
			}
			build = &buildTemplateData{Context: contextPath, Dockerfile: dockerfilePath}
		}

		safeEnv := make(map[string]string, len(svc.Env))
		for k, v := range svc.Env {
			rendered, err := renderWorkbenchString(v, cfg)
			if err != nil {
				return nil, fmt.Errorf("docker: render env %s for %s: %w", k, svc.Name, err)
			}
			safeEnv[k] = fmt.Sprintf("%q", rendered)
		}

		var hc *healthCheckTemplateData
		if svc.Health != nil {
			testJSON, _ := json.Marshal(svc.Health.Test)
			hc = &healthCheckTemplateData{
				Test:        string(testJSON),
				Interval:    svc.Health.Interval,
				Timeout:     svc.Health.Timeout,
				Retries:     svc.Health.Retries,
				StartPeriod: svc.Health.StartPeriod,
			}
		}

		dependsOn := make([]serviceDependencyTemplateData, 0, len(svc.DependsOn))
		for name, dep := range svc.DependsOn {
			dependsOn = append(dependsOn, serviceDependencyTemplateData{Name: name, Condition: dep.Condition})
		}
		sort.Slice(dependsOn, func(i, j int) bool { return dependsOn[i].Name < dependsOn[j].Name })

		var extraNets []string
		for _, net := range svc.Networks {
			if net != "workbench-net" {
				extraNets = append(extraNets, net)
			}
		}

		services = append(services, workbenchServiceTemplateData{
			Name:          svc.Name,
			Build:         build,
			Image:         svc.Image,
			Ports:         svc.Ports,
			Environment:   safeEnv,
			DependsOn:     dependsOn,
			HealthCheck:   hc,
			ExtraNetworks: extraNets,
		})
	}

	data := workbenchTemplateData{SessionID: cfg.SessionID, Services: services}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("docker: execute workbench template: %w", err)
	}

	return buf.Bytes(), nil
}

func renderWorkbenchString(value string, cfg WorkbenchConfig) (string, error) {
	if value == "" {
		return "", nil
	}
	for name, path := range cfg.WorktreePaths {
		value = strings.ReplaceAll(value, fmt.Sprintf("${WORKTREE_%s}", name), path)
	}
	worktreeRefPattern := regexp.MustCompile(`\{\{\s*\.Worktree\.([A-Za-z0-9._-]+)\s*\}\}`)
	value = worktreeRefPattern.ReplaceAllStringFunc(value, func(match string) string {
		submatches := worktreeRefPattern.FindStringSubmatch(match)
		if len(submatches) != 2 {
			return match
		}
		if path, ok := cfg.WorktreePaths[submatches[1]]; ok {
			return path
		}
		return match
	})
	tmpl, err := template.New("workbench-string").Option("missingkey=error").Parse(value)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	data := struct {
		SessionID string
		Worktree  map[string]string
	}{
		SessionID: cfg.SessionID,
		Worktree:  cfg.WorktreePaths,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// OpenWorkbench re-opens an already-created workbench by session ID so callers can
// query status or stop it without recreating the compose file.
func OpenWorkbench(sessionID, baseDir string) *Workbench {
	root := baseDir
	if root == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			root = filepath.Join(home, ".belayer", "workbenches")
		}
	}
	return &Workbench{
		config:     WorkbenchConfig{SessionID: sessionID, BaseDir: baseDir},
		composeDir: filepath.Join(root, sessionID),
	}
}

// Endpoints returns best-effort service endpoints for callers. For web-style
// ports it emits http:// URLs; otherwise it emits service:port addresses.
func (w *Workbench) Endpoints() map[string]WorkbenchEndpoint {
	endpoints := make(map[string]WorkbenchEndpoint, len(w.config.Spec.Services))
	for _, svc := range w.config.Spec.Services {
		if len(svc.Ports) == 0 {
			continue
		}
		port := svc.Ports[0]
		serviceURL := fmt.Sprintf("%s:%s", svc.Name, port)
		switch port {
		case "80", "3000", "8080", "8081":
			serviceURL = fmt.Sprintf("http://%s:%s", svc.Name, port)
		}
		endpoints[svc.Name] = WorkbenchEndpoint{
			Service: svc.Name,
			URL:     serviceURL,
		}
	}
	return endpoints
}
