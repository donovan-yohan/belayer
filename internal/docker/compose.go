package docker

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"text/template"
)

// ComposeConfig describes a full session's Docker Compose setup.
type ComposeConfig struct {
	SessionID      string
	Agents         []AgentComposeConfig
	Network        NetworkConfig
	IncludeCompose string // path to user's existing docker-compose.yml to extend
}

// AgentComposeConfig describes a single agent service in the compose file.
type AgentComposeConfig struct {
	Name    string            // e.g., "pilot", "implementer", "reviewer"
	Image   string            // default: "belayer/agent:latest"
	WorkDir string            // host path to mount as /workspace
	EnvFile string            // path to .env file for vendor auth
	EnvVars map[string]string // additional env vars (BELAYER_SESSION_ID, etc.)
}

// NetworkConfig describes the network isolation mode for the session.
type NetworkConfig struct {
	Type         string   // "none", "limited", "full"
	AllowedHosts []string // for "limited" mode
	ProxyImage   string   // default: "ubuntu/squid:latest"
}

const composeTmpl = `{{ if .IncludeCompose }}include:
  - path: {{ .IncludeCompose }}

{{ end }}services:
{{ range .Agents }}  {{ .Name }}:
    image: {{ .Image }}
    working_dir: /workspace
{{ if .WorkDir }}    volumes:
      - {{ .WorkDir }}:/workspace
{{ end }}{{ if .EnvFile }}    env_file:
      - {{ .EnvFile }}
{{ end }}    environment:
{{ range $k, $v := .EnvVars }}      {{ $k }}: {{ $v }}
{{ end }}    networks:
      - session
{{ if $.IncludeProxy }}    depends_on:
      - proxy
{{ end }}
{{ end }}{{ if .IncludeProxy }}  proxy:
    image: {{ .ProxyImage }}
    environment:
      ALLOWED_HOSTS: "{{ .AllowedHosts }}"
    networks:
      - session
      - internet

{{ end }}networks:
  session:
    name: belayer-{{ .SessionID }}
{{ if .InternalNetwork }}    internal: true
{{ end }}{{ if .IncludeProxy }}  internet:
    driver: bridge
{{ end }}`

type composeTemplateData struct {
	SessionID       string
	Agents          []agentTemplateData
	IncludeCompose  string
	IncludeProxy    bool
	ProxyImage      string
	AllowedHosts    string
	InternalNetwork bool
}

type agentTemplateData struct {
	Name    string
	Image   string
	WorkDir string
	EnvFile string
	EnvVars map[string]string
}

// generateCompose returns docker-compose.yml content for the given ComposeConfig.
func generateCompose(cfg ComposeConfig) ([]byte, error) {
	tmpl, err := template.New("compose").Parse(composeTmpl)
	if err != nil {
		return nil, fmt.Errorf("docker: parse compose template: %w", err)
	}

	agents := make([]agentTemplateData, 0, len(cfg.Agents))
	for _, a := range cfg.Agents {
		img := a.Image
		if img == "" {
			img = "belayer/agent:latest"
		}
		agents = append(agents, agentTemplateData{
			Name:    a.Name,
			Image:   img,
			WorkDir: a.WorkDir,
			EnvFile: a.EnvFile,
			EnvVars: a.EnvVars,
		})
	}

	proxyImage := cfg.Network.ProxyImage
	if proxyImage == "" {
		proxyImage = "ubuntu/squid:latest"
	}

	includeProxy := cfg.Network.Type == "limited"
	internalNetwork := cfg.Network.Type != "full"

	data := composeTemplateData{
		SessionID:       cfg.SessionID,
		Agents:          agents,
		IncludeCompose:  cfg.IncludeCompose,
		IncludeProxy:    includeProxy,
		ProxyImage:      proxyImage,
		AllowedHosts:    strings.Join(cfg.Network.AllowedHosts, ","),
		InternalNetwork: internalNetwork,
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
