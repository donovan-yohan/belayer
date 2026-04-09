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
		"registry.npmjs.org":      false,
		"pypi.org":                false,
		"proxy.golang.org":        false,
		"repo.maven.apache.org":   false,
		"rubygems.org":            false,
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
