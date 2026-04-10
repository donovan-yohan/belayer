package docker

import (
	"os"
	"path/filepath"
	"testing"
)

const validEnvironmentYAML = `
name: extend-fullstack
type: docker-compose
compose:
  include: ~/Documents/Programs/work/extend-localenv/docker/docker-compose.yml
  profiles: [database, rabbitmq, localstack, redis, spicedb, moto]
networking:
  type: limited
  allowed_hosts:
    - "*.github.com"
    - "registry.npmjs.org"
  allow_package_managers: true
repos:
  - name: extend-api
    path: ~/Documents/Programs/work/extend-api
  - name: extend-app
    path: ~/Documents/Programs/work/extend-app
`

const splitWorkbenchEnvironmentYAML = `
name: extend-fullstack
workbench:
  spec: workbench.yaml
`

const splitWorkbenchSpecYAML = `
timeout: 5m
services:
  - name: extend-api
    image: example/api:latest
    ports: ["8080"]
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 5s
      timeout: 3s
      retries: 10
      start_period: 30s
`

func TestLoadEnvironment_ValidYAML(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "env-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(validEnvironmentYAML); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	cfg, err := LoadEnvironment(f.Name())
	if err != nil {
		t.Fatalf("LoadEnvironment returned error: %v", err)
	}

	if cfg.Name != "extend-fullstack" {
		t.Errorf("expected Name %q, got %q", "extend-fullstack", cfg.Name)
	}
	if cfg.Type != "docker-compose" {
		t.Errorf("expected Type %q, got %q", "docker-compose", cfg.Type)
	}
	if cfg.Compose.Include == "" {
		t.Error("expected Compose.Include to be set")
	}
	if len(cfg.Compose.Profiles) != 6 {
		t.Errorf("expected 6 compose profiles, got %d", len(cfg.Compose.Profiles))
	}
	if cfg.Networking.Type != "limited" {
		t.Errorf("expected Networking.Type %q, got %q", "limited", cfg.Networking.Type)
	}
	if !cfg.Networking.AllowPackageManagers {
		t.Error("expected AllowPackageManagers to be true")
	}
	if len(cfg.Networking.AllowedHosts) != 2 {
		t.Errorf("expected 2 allowed hosts, got %d", len(cfg.Networking.AllowedHosts))
	}
	if len(cfg.Repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(cfg.Repos))
	}
	if cfg.Repos[0].Name != "extend-api" {
		t.Errorf("expected first repo name %q, got %q", "extend-api", cfg.Repos[0].Name)
	}
}

func TestLoadEnvironment_NonexistentFile(t *testing.T) {
	_, err := LoadEnvironment("/nonexistent/path/env.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLoadEnvironmentByName(t *testing.T) {
	dir := t.TempDir()
	envDir := filepath.Join(dir, ".belayer", "environments")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("create env dir: %v", err)
	}

	envPath := filepath.Join(envDir, "myenv.yaml")
	if err := os.WriteFile(envPath, []byte(validEnvironmentYAML), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cfg, err := LoadEnvironmentByName(dir, "myenv")
	if err != nil {
		t.Fatalf("LoadEnvironmentByName returned error: %v", err)
	}
	if cfg.Name != "extend-fullstack" {
		t.Errorf("expected Name %q, got %q", "extend-fullstack", cfg.Name)
	}
}

func TestLoadEnvironmentByName_NonexistentName(t *testing.T) {
	_, err := LoadEnvironmentByName(t.TempDir(), "does-not-exist")
	if err == nil {
		t.Fatal("expected error for nonexistent environment name, got nil")
	}
}

func TestLoadEnvironment_LoadsSeparateWorkbenchSpec(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "environment.yaml")
	specPath := filepath.Join(dir, "workbench.yaml")
	if err := os.WriteFile(envPath, []byte(splitWorkbenchEnvironmentYAML), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	if err := os.WriteFile(specPath, []byte(splitWorkbenchSpecYAML), 0o644); err != nil {
		t.Fatalf("write workbench spec: %v", err)
	}

	cfg, err := LoadEnvironment(envPath)
	if err != nil {
		t.Fatalf("LoadEnvironment returned error: %v", err)
	}
	if cfg.Workbench == nil {
		t.Fatal("expected Workbench to be loaded")
	}
	if len(cfg.Workbench.Services) != 1 {
		t.Fatalf("expected 1 workbench service, got %d", len(cfg.Workbench.Services))
	}
	if cfg.Workbench.Services[0].Health == nil || cfg.Workbench.Services[0].Health.StartPeriod != "30s" {
		t.Fatalf("expected health start period from split workbench spec, got %#v", cfg.Workbench.Services[0].Health)
	}
}

func TestDefaultEnvironment(t *testing.T) {
	cfg := DefaultEnvironment()
	if cfg == nil {
		t.Fatal("DefaultEnvironment returned nil")
	}
	if cfg.Type != "docker-compose" {
		t.Errorf("expected Type %q, got %q", "docker-compose", cfg.Type)
	}
	if cfg.Networking.Type != "none" {
		t.Errorf("expected Networking.Type %q, got %q", "none", cfg.Networking.Type)
	}
}

func TestPackageManagerHosts(t *testing.T) {
	hosts := PackageManagerHosts()
	if len(hosts) == 0 {
		t.Fatal("expected non-empty PackageManagerHosts slice")
	}

	expected := map[string]bool{
		"registry.npmjs.org":    false,
		"pypi.org":              false,
		"proxy.golang.org":      false,
		"repo.maven.apache.org": false,
		"rubygems.org":          false,
	}

	for _, h := range hosts {
		if _, ok := expected[h]; ok {
			expected[h] = true
		}
	}

	for domain, found := range expected {
		if !found {
			t.Errorf("expected domain %q in PackageManagerHosts, not found", domain)
		}
	}
}

func TestValidateEnvironment_ValidConfig(t *testing.T) {
	cfg := &EnvironmentConfig{
		Networking: NetworkingRule{
			Type:         "limited",
			AllowedHosts: []string{"api.anthropic.com", "*.github.com"},
		},
	}
	if err := ValidateEnvironment(cfg); err != nil {
		t.Errorf("expected no error for valid config, got: %v", err)
	}
}

func TestValidateEnvironment_InvalidNetworkType(t *testing.T) {
	cfg := &EnvironmentConfig{
		Networking: NetworkingRule{Type: "yolo"},
	}
	if err := ValidateEnvironment(cfg); err == nil {
		t.Error("expected error for invalid network type")
	}
}

func TestValidateEnvironment_BroadPattern(t *testing.T) {
	tests := []string{".*", ".", "*"}
	for _, host := range tests {
		cfg := &EnvironmentConfig{
			Networking: NetworkingRule{
				Type:         "limited",
				AllowedHosts: []string{host},
			},
		}
		if err := ValidateEnvironment(cfg); err == nil {
			t.Errorf("expected error for broad pattern %q", host)
		}
	}
}

func TestValidateEnvironment_InvalidHostChars(t *testing.T) {
	cfg := &EnvironmentConfig{
		Networking: NetworkingRule{
			Type:         "limited",
			AllowedHosts: []string{"evil.com|good.com"},
		},
	}
	if err := ValidateEnvironment(cfg); err == nil {
		t.Error("expected error for host with pipe character")
	}
}

func TestEscapeHostForRegex_Simple(t *testing.T) {
	got := EscapeHostForRegex("api.anthropic.com")
	want := "api\\.anthropic\\.com"
	if got != want {
		t.Errorf("EscapeHostForRegex: got %q, want %q", got, want)
	}
}

func TestEscapeHostForRegex_Wildcard(t *testing.T) {
	got := EscapeHostForRegex("*.github.com")
	want := "[a-zA-Z0-9.-]+\\.github\\.com"
	if got != want {
		t.Errorf("EscapeHostForRegex wildcard: got %q, want %q", got, want)
	}
}

func TestEnvironmentConfig_ResolveRepoPath(t *testing.T) {
	cfg := &EnvironmentConfig{
		Repos: []RepoRef{
			{Name: "extend-api", Path: "/work/extend-api"},
			{Name: "extend-app", Path: "/work/extend-app"},
		},
	}

	if got := cfg.ResolveRepoPath("extend-api"); got != "/work/extend-api" {
		t.Errorf("expected /work/extend-api, got %q", got)
	}
	if got := cfg.ResolveRepoPath("extend-app"); got != "/work/extend-app" {
		t.Errorf("expected /work/extend-app, got %q", got)
	}
	if got := cfg.ResolveRepoPath("nonexistent"); got != "" {
		t.Errorf("expected empty for nonexistent repo, got %q", got)
	}
}
