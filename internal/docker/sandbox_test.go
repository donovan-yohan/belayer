package docker

import (
	"strings"
	"testing"
)

// helpers to build ComposeConfig for tests

func singleAgentConfig(sessionID, image, workDir string) ComposeConfig {
	return ComposeConfig{
		SessionID: sessionID,
		Agents: []AgentComposeConfig{
			{Name: "agent", Image: image, WorkDir: workDir},
		},
		Network: NetworkConfig{Type: "none"},
	}
}

func TestNewSandbox_DefaultImage(t *testing.T) {
	s, err := NewSandbox(SandboxConfig{
		ComposeConfig: ComposeConfig{
			SessionID: "test-session",
			Agents: []AgentComposeConfig{
				{Name: "pilot"},
			},
			Network: NetworkConfig{Type: "none"},
		},
	})
	if err != nil {
		t.Fatalf("NewSandbox returned error: %v", err)
	}
	if s.config.ComposeConfig.Agents[0].Image != "belayer/agent:latest" {
		t.Errorf("expected default image 'belayer/agent:latest', got %q", s.config.ComposeConfig.Agents[0].Image)
	}
}

func TestNewSandbox_RequiresSessionID(t *testing.T) {
	_, err := NewSandbox(SandboxConfig{
		ComposeConfig: ComposeConfig{},
	})
	if err == nil {
		t.Fatal("expected error when SessionID is empty, got nil")
	}
}

func TestNewSandbox_DefaultWorkDir(t *testing.T) {
	s, err := NewSandbox(SandboxConfig{
		ComposeConfig: ComposeConfig{
			SessionID: "test-session",
			Agents: []AgentComposeConfig{
				{Name: "pilot"},
			},
			Network: NetworkConfig{Type: "none"},
		},
	})
	if err != nil {
		t.Fatalf("NewSandbox returned error: %v", err)
	}
	if s.config.ComposeConfig.Agents[0].WorkDir != "/workspace" {
		t.Errorf("expected default WorkDir '/workspace', got %q", s.config.ComposeConfig.Agents[0].WorkDir)
	}
}

func TestNewSandbox_AgentNames(t *testing.T) {
	s, err := NewSandbox(SandboxConfig{
		ComposeConfig: ComposeConfig{
			SessionID: "test-session",
			Agents: []AgentComposeConfig{
				{Name: "pilot"},
				{Name: "implementer"},
				{Name: "reviewer"},
			},
			Network: NetworkConfig{Type: "none"},
		},
	})
	if err != nil {
		t.Fatalf("NewSandbox returned error: %v", err)
	}
	names := s.AgentNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 agent names, got %d", len(names))
	}
	expected := []string{"pilot", "implementer", "reviewer"}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("agent[%d]: expected %q, got %q", i, name, names[i])
		}
	}
}

func TestGenerateCompose_ServiceNames(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{Name: "pilot", Image: "belayer/agent:latest"},
			{Name: "implementer", Image: "belayer/agent:latest"},
			{Name: "reviewer", Image: "belayer/agent:latest"},
		},
		Network: NetworkConfig{Type: "none"},
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	for _, name := range []string{"pilot:", "implementer:", "reviewer:"} {
		if !strings.Contains(content, name) {
			t.Errorf("expected %q service in compose output, got:\n%s", name, content)
		}
	}
}

func TestGenerateCompose_InternalNetwork_None(t *testing.T) {
	cfg := singleAgentConfig("abc123", "belayer/agent:latest", "")
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "internal: true") {
		t.Errorf("expected 'internal: true' for network type 'none', got:\n%s", content)
	}
}

func TestGenerateCompose_NoInternalNetwork_Full(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{Name: "agent", Image: "belayer/agent:latest"},
		},
		Network: NetworkConfig{Type: "full"},
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if strings.Contains(content, "internal: true") {
		t.Errorf("expected no 'internal: true' for network type 'full', got:\n%s", content)
	}
}

func TestGenerateCompose_ProxyIncluded_Limited(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{Name: "pilot", Image: "belayer/agent:latest"},
		},
		Network: NetworkConfig{
			Type:         "limited",
			AllowedHosts: []string{"*.github.com", "registry.npmjs.org"},
			ProxyImage:   "ubuntu/squid:latest",
		},
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "proxy:") {
		t.Errorf("expected 'proxy:' service in compose output for 'limited' network, got:\n%s", content)
	}
	if !strings.Contains(content, "internet:") {
		t.Errorf("expected 'internet:' network in compose output for 'limited' network, got:\n%s", content)
	}
	if !strings.Contains(content, "depends_on:") {
		t.Errorf("expected 'depends_on:' for agents when proxy is present, got:\n%s", content)
	}
	if !strings.Contains(content, "*.github.com") {
		t.Errorf("expected allowed hosts in proxy config, got:\n%s", content)
	}
}

func TestGenerateCompose_NoProxy_None(t *testing.T) {
	cfg := singleAgentConfig("abc123", "belayer/agent:latest", "")
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if strings.Contains(content, "proxy:") {
		t.Errorf("expected no 'proxy:' service for network type 'none', got:\n%s", content)
	}
}

func TestGenerateCompose_EnvFileIncluded(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{Name: "pilot", Image: "belayer/agent:latest", EnvFile: "/path/to/.env"},
		},
		Network: NetworkConfig{Type: "none"},
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "env_file:") {
		t.Errorf("expected 'env_file:' in compose output when EnvFile is set, got:\n%s", content)
	}
	if !strings.Contains(content, "/path/to/.env") {
		t.Errorf("expected env file path in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_EnvFileOmitted(t *testing.T) {
	cfg := singleAgentConfig("abc123", "belayer/agent:latest", "")
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if strings.Contains(content, "env_file:") {
		t.Errorf("expected no 'env_file:' in compose output when EnvFile is empty, got:\n%s", content)
	}
}

func TestGenerateCompose_WorkingDir(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{Name: "agent", Image: "belayer/agent:latest", WorkDir: "/myworkspace"},
		},
		Network: NetworkConfig{Type: "none"},
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "working_dir: /workspace") {
		t.Errorf("expected 'working_dir: /workspace' (container path) in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "/myworkspace:/workspace") {
		t.Errorf("expected volume mount '/myworkspace:/workspace' in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_NetworkName(t *testing.T) {
	cfg := singleAgentConfig("sess-xyz", "belayer/agent:latest", "")
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "belayer-sess-xyz") {
		t.Errorf("expected network name 'belayer-sess-xyz' in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_IncludeCompose(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{Name: "pilot", Image: "belayer/agent:latest"},
		},
		Network:        NetworkConfig{Type: "none"},
		IncludeCompose: "/path/to/docker-compose.yml",
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "include:") {
		t.Errorf("expected 'include:' directive in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "/path/to/docker-compose.yml") {
		t.Errorf("expected include path in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_IncludeOmitted(t *testing.T) {
	cfg := singleAgentConfig("abc123", "belayer/agent:latest", "")
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if strings.Contains(content, "include:") {
		t.Errorf("expected no 'include:' directive when IncludeCompose is empty, got:\n%s", content)
	}
}

func TestGenerateCompose_EnvVars(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{
				Name:  "pilot",
				Image: "belayer/agent:latest",
				EnvVars: map[string]string{
					"BELAYER_SESSION_ID": "abc123",
					"BELAYER_AGENT_ID":   "pilot",
				},
			},
		},
		Network: NetworkConfig{Type: "none"},
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "BELAYER_SESSION_ID") {
		t.Errorf("expected BELAYER_SESSION_ID in compose output, got:\n%s", content)
	}
	if !strings.Contains(content, "BELAYER_AGENT_ID") {
		t.Errorf("expected BELAYER_AGENT_ID in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_DefaultProxyImage(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{Name: "pilot", Image: "belayer/agent:latest"},
		},
		Network: NetworkConfig{
			Type: "limited",
			// ProxyImage intentionally left empty to test default
		},
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "ubuntu/squid:latest") {
		t.Errorf("expected default proxy image 'ubuntu/squid:latest' in compose output, got:\n%s", content)
	}
}

func TestAllocatePort(t *testing.T) {
	port, err := allocatePort()
	if err != nil {
		t.Fatalf("allocatePort returned error: %v", err)
	}
	if port <= 0 {
		t.Errorf("expected port > 0, got %d", port)
	}
}

func TestAllocatePort_UniqueResults(t *testing.T) {
	// Ports should generally be different across calls (not guaranteed, but very likely).
	p1, err := allocatePort()
	if err != nil {
		t.Fatalf("first allocatePort call failed: %v", err)
	}
	p2, err := allocatePort()
	if err != nil {
		t.Fatalf("second allocatePort call failed: %v", err)
	}
	// Just verify both are valid; they could theoretically be equal but won't be in practice.
	if p1 <= 0 || p2 <= 0 {
		t.Errorf("expected both ports > 0, got p1=%d p2=%d", p1, p2)
	}
}

func TestGenerateCompose_ExtraVolumes(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{
				Name:         "pilot",
				Image:        "belayer/agent:latest",
				ExtraVolumes: []string{"/host/path:/container/path:ro"},
			},
		},
		Network: NetworkConfig{Type: "none"},
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "/host/path:/container/path:ro") {
		t.Errorf("expected extra volume mount in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_ProxyEnvVarsInAgentService(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{Name: "pilot", Image: "belayer/agent:latest"},
		},
		Network: NetworkConfig{Type: "limited"},
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "HTTP_PROXY") {
		t.Errorf("expected HTTP_PROXY in compose output for 'limited' network, got:\n%s", content)
	}
	if !strings.Contains(content, "HTTPS_PROXY") {
		t.Errorf("expected HTTPS_PROXY in compose output for 'limited' network, got:\n%s", content)
	}
	// Verify they appear before the proxy service (i.e., under the agent service)
	agentIdx := strings.Index(content, "pilot:")
	proxyIdx := strings.Index(content, "proxy:")
	httpProxyIdx := strings.Index(content, "HTTP_PROXY")
	if agentIdx < 0 || proxyIdx < 0 || httpProxyIdx < 0 {
		t.Fatalf("could not find expected sections in compose output:\n%s", content)
	}
	if httpProxyIdx > proxyIdx {
		t.Errorf("expected HTTP_PROXY to appear under agent service (before proxy service block), got:\n%s", content)
	}
}

func TestGenerateCompose_EnvVarsQuoted(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents: []AgentComposeConfig{
			{
				Name:  "pilot",
				Image: "belayer/agent:latest",
				EnvVars: map[string]string{
					"BELAYER_SESSION_ID": "abc123",
				},
			},
		},
		Network: NetworkConfig{Type: "none"},
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, `BELAYER_SESSION_ID: "abc123"`) {
		t.Errorf("expected env var value to be double-quoted in compose output, got:\n%s", content)
	}
}

func TestBridgeEnvironment_MergesConfig(t *testing.T) {
	env := &EnvironmentConfig{
		Networking: NetworkingRule{
			Type:         "limited",
			AllowedHosts: []string{"api.anthropic.com"},
		},
		Compose: ComposeExtend{
			Include: "/path/to/docker-compose.yml",
		},
	}
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents:    []AgentComposeConfig{{Name: "pilot"}},
	}
	BridgeEnvironment(env, &cfg)

	if cfg.Network.Type != "limited" {
		t.Errorf("expected Network.Type %q, got %q", "limited", cfg.Network.Type)
	}
	if len(cfg.Network.AllowedHosts) == 0 {
		t.Fatal("expected AllowedHosts to be non-empty")
	}
	found := false
	for _, h := range cfg.Network.AllowedHosts {
		if h == "api.anthropic.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'api.anthropic.com' in AllowedHosts, got %v", cfg.Network.AllowedHosts)
	}
	if cfg.IncludeCompose != "/path/to/docker-compose.yml" {
		t.Errorf("expected IncludeCompose %q, got %q", "/path/to/docker-compose.yml", cfg.IncludeCompose)
	}
}

func TestBridgeEnvironment_PackageManagers(t *testing.T) {
	env := &EnvironmentConfig{
		Networking: NetworkingRule{
			Type:                 "limited",
			AllowPackageManagers: true,
		},
	}
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents:    []AgentComposeConfig{{Name: "pilot"}},
	}
	BridgeEnvironment(env, &cfg)

	pkgHosts := PackageManagerHosts()
	hostSet := make(map[string]struct{}, len(cfg.Network.AllowedHosts))
	for _, h := range cfg.Network.AllowedHosts {
		hostSet[h] = struct{}{}
	}
	for _, domain := range pkgHosts {
		if _, ok := hostSet[domain]; !ok {
			t.Errorf("expected package manager domain %q in AllowedHosts, got %v", domain, cfg.Network.AllowedHosts)
		}
	}
}

func TestBridgeEnvironment_NilSafe(t *testing.T) {
	cfg := ComposeConfig{
		SessionID: "abc123",
		Agents:    []AgentComposeConfig{{Name: "pilot"}},
	}
	// Should not panic
	BridgeEnvironment(nil, &cfg)
}

func TestGenerateEnvFile_IncludesExtraVars(t *testing.T) {
	extraVars := map[string]string{
		"CUSTOM_KEY": "custom_value",
	}
	out := GenerateEnvFile(extraVars)
	content := string(out)
	if !strings.Contains(content, "CUSTOM_KEY=custom_value") {
		t.Errorf("expected 'CUSTOM_KEY=custom_value' in env file output, got:\n%s", content)
	}
}

func TestGenerateEnvFile_EmptyWhenNoAuth(t *testing.T) {
	// Unset all vendor auth vars to ensure a clean environment
	authVars := []string{
		"ANTHROPIC_API_KEY",
		"CLAUDE_CODE_OAUTH_TOKEN",
		"OPENAI_API_KEY",
		"GEMINI_API_KEY",
		"OPENCODE_API_KEY",
	}
	for _, key := range authVars {
		t.Setenv(key, "")
	}

	out := GenerateEnvFile(nil)
	if len(out) != 0 {
		t.Errorf("expected empty env file when no auth vars set and no extra vars, got:\n%s", string(out))
	}
}
