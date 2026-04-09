package docker

import (
	"bytes"
	"fmt"
	"net"
	"text/template"
)

const composeTmpl = `services:
  agent:
    image: {{ .Image }}
    command: ["sleep", "infinity"]
    working_dir: {{ .WorkDir }}
    networks:
      - belayer
{{ if .EnvFile }}    env_file:
      - /run/secrets/env
    volumes:
      - {{ .EnvFile }}:/run/secrets/env:ro
{{ end }}{{ if .AllowedDomains }}    # Allowed domains (configure firewall rules manually):
{{ range .AllowedDomains }}    #   - {{ . }}
{{ end }}{{ end }}
networks:
  belayer:
    name: belayer-{{ .SessionID }}
    internal: true
`

type composeData struct {
	Image          string
	WorkDir        string
	EnvFile        string
	SessionID      string
	AllowedDomains []string
}

// generateCompose returns docker-compose.yml content for the given SandboxConfig.
func generateCompose(cfg SandboxConfig) ([]byte, error) {
	tmpl, err := template.New("compose").Parse(composeTmpl)
	if err != nil {
		return nil, fmt.Errorf("docker: parse compose template: %w", err)
	}

	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "/workspace"
	}

	data := composeData{
		Image:          cfg.Image,
		WorkDir:        workDir,
		EnvFile:        cfg.EnvFile,
		SessionID:      cfg.SessionID,
		AllowedDomains: cfg.AllowedDomains,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("docker: execute compose template: %w", err)
	}

	return buf.Bytes(), nil
}

// allocatePort finds an available TCP port by binding to :0 and reading the
// assigned port from the OS.
func allocatePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("docker: allocate port: %w", err)
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("docker: allocate port: unexpected address type")
	}
	return addr.Port, nil
}
