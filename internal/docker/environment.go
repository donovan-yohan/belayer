package docker

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// EnvironmentConfig describes the Docker infrastructure for a session.
type EnvironmentConfig struct {
	Name       string         `yaml:"name"`
	Type       string         `yaml:"type"`       // "docker-compose"
	Compose    ComposeExtend  `yaml:"compose"`
	Networking NetworkingRule `yaml:"networking"`
	Repos      []RepoRef      `yaml:"repos"`
}

// ComposeExtend describes how to extend an existing Docker Compose setup.
type ComposeExtend struct {
	Include  string   `yaml:"include"`  // path to existing docker-compose.yml
	Profiles []string `yaml:"profiles"` // compose profiles to activate
}

// NetworkingRule describes the network access policy for a sandbox.
type NetworkingRule struct {
	Type                 string   `yaml:"type"` // "none", "limited", "full"
	AllowedHosts         []string `yaml:"allowed_hosts"`
	AllowPackageManagers bool     `yaml:"allow_package_managers"`
}

// RepoRef identifies a repository involved in the session.
type RepoRef struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// LoadEnvironment reads and parses an EnvironmentConfig from the given YAML file path.
func LoadEnvironment(path string) (*EnvironmentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("docker: environment: read %q: %w", path, err)
	}

	var cfg EnvironmentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("docker: environment: parse %q: %w", path, err)
	}

	return &cfg, nil
}

// LoadEnvironmentByName loads an environment config from
// <workspaceDir>/.belayer/environments/<name>.yaml.
func LoadEnvironmentByName(workspaceDir, name string) (*EnvironmentConfig, error) {
	path := filepath.Join(workspaceDir, ".belayer", "environments", name+".yaml")
	return LoadEnvironment(path)
}

// DefaultEnvironment returns a minimal EnvironmentConfig with safe defaults.
func DefaultEnvironment() *EnvironmentConfig {
	return &EnvironmentConfig{
		Type: "docker-compose",
		Networking: NetworkingRule{
			Type: "none",
		},
	}
}

// PackageManagerHosts returns common package manager domains that may be
// added to AllowedHosts when AllowPackageManagers is true.
func PackageManagerHosts() []string {
	return []string{
		"registry.npmjs.org",
		"pypi.org",
		"proxy.golang.org",
		"repo.maven.apache.org",
		"rubygems.org",
	}
}
