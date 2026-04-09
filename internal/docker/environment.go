package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// ValidateEnvironment checks an EnvironmentConfig for safety issues.
func ValidateEnvironment(cfg *EnvironmentConfig) error {
	validTypes := map[string]bool{"none": true, "limited": true, "full": true}
	if cfg.Networking.Type != "" && !validTypes[cfg.Networking.Type] {
		return fmt.Errorf("docker: environment: invalid network type %q (must be none, limited, or full)", cfg.Networking.Type)
	}

	for _, host := range cfg.Networking.AllowedHosts {
		if err := validateHost(host); err != nil {
			return fmt.Errorf("docker: environment: invalid allowed host %q: %w", host, err)
		}
	}

	return nil
}

// validateHost checks that a host entry looks like a valid hostname or wildcard pattern.
func validateHost(host string) error {
	if host == "" {
		return fmt.Errorf("empty host")
	}
	// Reject obvious regex patterns that could bypass the proxy
	if host == ".*" || host == "." || host == "*" {
		return fmt.Errorf("overly broad pattern would bypass network isolation")
	}
	// Allow wildcard prefix (e.g., *.github.com)
	h := host
	if strings.HasPrefix(h, "*.") {
		h = h[2:]
	}
	// Each label must be alphanumeric with hyphens, dots as separators
	for _, label := range strings.Split(h, ".") {
		if label == "" {
			return fmt.Errorf("empty label in hostname")
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return fmt.Errorf("invalid character %q in hostname label", c)
			}
		}
	}
	return nil
}

// EscapeHostForRegex converts a hostname or wildcard pattern into a properly
// anchored POSIX extended regex for tinyproxy's filter file.
func EscapeHostForRegex(host string) string {
	// Handle wildcard prefix: *.github.com → [a-zA-Z0-9.-]+\.github\.com
	prefix := ""
	h := host
	if strings.HasPrefix(h, "*.") {
		prefix = "[a-zA-Z0-9.-]+\\."
		h = h[2:]
	}
	// Escape dots in hostname
	escaped := strings.ReplaceAll(h, ".", "\\.")
	return prefix + escaped
}

// ResolveRepoPath finds the filesystem path for a named repo in the environment config.
// Returns the path if found, or empty string if not found.
func (cfg *EnvironmentConfig) ResolveRepoPath(repoName string) string {
	for _, r := range cfg.Repos {
		if r.Name == repoName {
			return r.Path
		}
	}
	return ""
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
