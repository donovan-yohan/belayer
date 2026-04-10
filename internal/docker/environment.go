package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/agent"
	"gopkg.in/yaml.v3"
)

// EnvironmentConfig describes the Docker infrastructure for a session.
type EnvironmentConfig struct {
	Name        string               `yaml:"name"`
	Description string               `yaml:"description,omitempty"`
	Type        string               `yaml:"type"` // "docker-compose"
	Compose     ComposeExtend        `yaml:"compose"`
	Networking  NetworkingRule       `yaml:"networking"`
	Repos       []RepoRef            `yaml:"repos"`
	Workbench   *WorkbenchConfigSpec `yaml:"workbench"`
	Clamshell   *ClamshellConfig     `yaml:"clamshell,omitempty"`
	Agents      []EnvironmentAgent   `yaml:"agents,omitempty"`
	Tools       []agent.ToolSpec     `yaml:"tools,omitempty"`

	sourcePath string `yaml:"-"`
}

// ComposeExtend describes how to extend an existing Docker Compose setup.
type ComposeExtend struct {
	Include  string   `yaml:"include"`
	Profiles []string `yaml:"profiles"`
}

// NetworkingRule describes the network access policy for a sandbox.
type NetworkingRule struct {
	Type                 string   `yaml:"type"`
	AllowedHosts         []string `yaml:"allowed_hosts"`
	AllowPackageManagers bool     `yaml:"allow_package_managers"`
	ConnectToWorkbench   bool     `yaml:"connect_to_workbench"`
}

// RepoRef identifies a repository involved in the session.
type RepoRef struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// ClamshellConfig points to the policy used by the environment.
type ClamshellConfig struct {
	Policy string `yaml:"policy"`
}

// EnvironmentAgent references a reusable agent template plus optional repo binding.
type EnvironmentAgent struct {
	Template string `yaml:"template"`
	Repo     string `yaml:"repo,omitempty"`
}

// WorkbenchConfigSpec describes the parsed workbench configuration used for provisioning services.
type WorkbenchConfigSpec struct {
	Spec     string        `yaml:"spec,omitempty"`
	Timeout  string        `yaml:"timeout,omitempty"`
	Services []ServiceDecl `yaml:"services"`
}

// UnmarshalYAML supports both a sequence of services and a compose-style mapping keyed by service name.
func (w *WorkbenchConfigSpec) UnmarshalYAML(node *yaml.Node) error {
	type rawWorkbenchConfigSpec struct {
		Spec     string    `yaml:"spec,omitempty"`
		Timeout  string    `yaml:"timeout,omitempty"`
		Services yaml.Node `yaml:"services"`
	}

	var raw rawWorkbenchConfigSpec
	if err := node.Decode(&raw); err != nil {
		return err
	}

	w.Spec = raw.Spec
	w.Timeout = raw.Timeout
	w.Services = nil
	if raw.Services.Kind == 0 {
		return nil
	}

	switch raw.Services.Kind {
	case yaml.SequenceNode:
		var services []ServiceDecl
		if err := raw.Services.Decode(&services); err != nil {
			return err
		}
		w.Services = services
		return nil
	case yaml.MappingNode:
		services := make([]ServiceDecl, 0, len(raw.Services.Content)/2)
		for i := 0; i < len(raw.Services.Content); i += 2 {
			nameNode := raw.Services.Content[i]
			serviceNode := raw.Services.Content[i+1]

			var service ServiceDecl
			if err := serviceNode.Decode(&service); err != nil {
				return err
			}
			if service.Name == "" {
				service.Name = nameNode.Value
			}
			services = append(services, service)
		}
		w.Services = services
		return nil
	default:
		return fmt.Errorf("docker: environment: services must be a sequence or mapping")
	}
}

// BuildDecl describes a service build context.
type BuildDecl struct {
	Context    string `yaml:"context,omitempty"`
	Dockerfile string `yaml:"dockerfile,omitempty"`
}

func (b *BuildDecl) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		return node.Decode(&b.Context)
	case yaml.MappingNode:
		type raw BuildDecl
		return node.Decode((*raw)(b))
	default:
		return fmt.Errorf("docker: environment: invalid build declaration")
	}
}

func (b BuildDecl) Empty() bool {
	return b.Context == "" && b.Dockerfile == ""
}

// ServiceDependency describes an optional compose dependency condition.
type ServiceDependency struct {
	Condition string `yaml:"condition,omitempty"`
}

func (d *ServiceDependency) UnmarshalYAML(node *yaml.Node) error {
	var aux struct {
		Condition string `yaml:"condition,omitempty"`
	}
	if err := node.Decode(&aux); err != nil {
		return err
	}
	d.Condition = aux.Condition
	return nil
}

// ServiceDecl describes a service to be provisioned in the workbench.
type ServiceDecl struct {
	Name      string                       `yaml:"name"`
	Build     BuildDecl                    `yaml:"build,omitempty"`
	Image     string                       `yaml:"image,omitempty"`
	Ports     []string                     `yaml:"ports,omitempty"`
	Env       map[string]string            `yaml:"environment,omitempty"`
	DependsOn map[string]ServiceDependency `yaml:"depends_on,omitempty"`
	Health    *HealthDecl                  `yaml:"healthcheck,omitempty"`
	Networks  []string                     `yaml:"networks,omitempty"`
}

// UnmarshalYAML supports both compact and structured forms for build/depends_on/healthcheck.
func (s *ServiceDecl) UnmarshalYAML(node *yaml.Node) error {
	type rawService struct {
		Name      string            `yaml:"name"`
		Build     BuildDecl         `yaml:"build"`
		Image     string            `yaml:"image"`
		Ports     []string          `yaml:"ports"`
		Env       map[string]string `yaml:"environment"`
		DependsOn yaml.Node         `yaml:"depends_on"`
		HealthA   *HealthDecl       `yaml:"health_check"`
		HealthB   *HealthDecl       `yaml:"healthcheck"`
		Networks  []string          `yaml:"networks"`
	}
	var raw rawService
	if err := node.Decode(&raw); err != nil {
		return err
	}

	s.Name = raw.Name
	s.Build = raw.Build
	s.Image = raw.Image
	s.Ports = raw.Ports
	s.Env = raw.Env
	s.Networks = raw.Networks
	if raw.HealthA != nil {
		s.Health = raw.HealthA
	} else {
		s.Health = raw.HealthB
	}

	s.DependsOn = make(map[string]ServiceDependency)
	switch raw.DependsOn.Kind {
	case 0:
	case yaml.SequenceNode:
		var deps []string
		if err := raw.DependsOn.Decode(&deps); err != nil {
			return err
		}
		for _, dep := range deps {
			s.DependsOn[dep] = ServiceDependency{}
		}
	case yaml.MappingNode:
		var deps map[string]ServiceDependency
		if err := raw.DependsOn.Decode(&deps); err != nil {
			return err
		}
		for name, dep := range deps {
			s.DependsOn[name] = dep
		}
	default:
		return fmt.Errorf("docker: environment: invalid depends_on declaration")
	}
	if len(s.DependsOn) == 0 {
		s.DependsOn = nil
	}
	return nil
}

// HealthDecl describes a health check for a service.
type HealthDecl struct {
	Test        []string `yaml:"test"`
	Interval    string   `yaml:"interval"`
	Timeout     string   `yaml:"timeout"`
	Retries     int      `yaml:"retries"`
	StartPeriod string   `yaml:"start_period"`
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
	if cfg.Type == "" {
		cfg.Type = "docker-compose"
	}
	cfg.sourcePath = path

	if cfg.Workbench != nil && cfg.Workbench.Spec != "" && len(cfg.Workbench.Services) == 0 {
		spec, err := LoadWorkbenchSpec(cfg.ResolveWorkbenchSpecPath())
		if err != nil {
			return nil, err
		}
		if cfg.Workbench.Timeout != "" && spec.Timeout == "" {
			spec.Timeout = cfg.Workbench.Timeout
		}
		cfg.Workbench = &spec
	}

	return &cfg, nil
}

// LoadEnvironmentByName loads an environment config from either:
//
//	<baseDir>/.belayer/environments/<name>.yaml
//	<baseDir>/.belayer/environments/<name>/environment.yaml
//	<baseDir>/environments/<name>.yaml
//	<baseDir>/environments/<name>/environment.yaml
func LoadEnvironmentByName(baseDir, name string) (*EnvironmentConfig, error) {
	candidates := []string{
		filepath.Join(baseDir, ".belayer", "environments", name+".yaml"),
		filepath.Join(baseDir, ".belayer", "environments", name, "environment.yaml"),
		filepath.Join(baseDir, "environments", name+".yaml"),
		filepath.Join(baseDir, "environments", name, "environment.yaml"),
	}
	var lastErr error
	for _, candidate := range candidates {
		cfg, err := LoadEnvironment(candidate)
		if err == nil {
			return cfg, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("docker: environment: %q not found", name)
}

// DefaultEnvironment returns a minimal EnvironmentConfig with safe defaults.
func DefaultEnvironment() *EnvironmentConfig {
	return &EnvironmentConfig{
		Type:       "docker-compose",
		Networking: NetworkingRule{Type: "none"},
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
	if cfg.Workbench != nil {
		if cfg.Workbench.Timeout != "" {
			if _, err := time.ParseDuration(cfg.Workbench.Timeout); err != nil {
				return fmt.Errorf("docker: environment: invalid workbench timeout %q: %w", cfg.Workbench.Timeout, err)
			}
		}
		if cfg.Workbench.Spec != "" {
			if _, err := os.Stat(cfg.ResolveWorkbenchSpecPath()); err != nil {
				return fmt.Errorf("docker: environment: read workbench spec %q: %w", cfg.ResolveWorkbenchSpecPath(), err)
			}
		}
	}
	return nil
}

func validateHost(host string) error {
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if host == ".*" || host == "." || host == "*" {
		return fmt.Errorf("overly broad pattern would bypass network isolation")
	}
	h := host
	if strings.HasPrefix(h, "*.") {
		h = h[2:]
	}
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

// EscapeHostForRegex converts a hostname or wildcard pattern into a properly anchored POSIX extended regex.
func EscapeHostForRegex(host string) string {
	prefix := ""
	h := host
	if strings.HasPrefix(h, "*.") {
		prefix = "[a-zA-Z0-9.-]+\\."
		h = h[2:]
	}
	escaped := strings.ReplaceAll(h, ".", "\\.")
	return prefix + escaped
}

// ResolveRepoPath finds the filesystem path for a named repo in the environment config.
func (cfg *EnvironmentConfig) ResolveRepoPath(repoName string) string {
	for _, r := range cfg.Repos {
		if r.Name == repoName {
			return r.Path
		}
	}
	return ""
}

// ResolveWorkbenchSpecPath resolves a relative nested workbench spec path.
func (cfg *EnvironmentConfig) ResolveWorkbenchSpecPath() string {
	if cfg == nil || cfg.Workbench == nil || cfg.Workbench.Spec == "" {
		return ""
	}
	if filepath.IsAbs(cfg.Workbench.Spec) {
		return cfg.Workbench.Spec
	}
	if cfg.sourcePath == "" {
		return cfg.Workbench.Spec
	}
	return filepath.Join(filepath.Dir(cfg.sourcePath), cfg.Workbench.Spec)
}

// ResolveWorkbenchSpec returns the effective workbench spec, whether inline or split to a separate file.
func (cfg *EnvironmentConfig) ResolveWorkbenchSpec() (WorkbenchConfigSpec, error) {
	return cfg.LoadWorkbenchConfig()
}

// SourcePath returns the file path the environment was loaded from.
func (cfg *EnvironmentConfig) SourcePath() string {
	if cfg == nil {
		return ""
	}
	return cfg.sourcePath
}

// LoadWorkbenchConfig returns the effective workbench configuration for the environment.
func (cfg *EnvironmentConfig) LoadWorkbenchConfig() (WorkbenchConfigSpec, error) {
	if cfg == nil || cfg.Workbench == nil {
		return WorkbenchConfigSpec{}, fmt.Errorf("docker: environment: workbench is not configured")
	}
	if len(cfg.Workbench.Services) > 0 {
		return *cfg.Workbench, nil
	}
	return LoadWorkbenchSpec(cfg.ResolveWorkbenchSpecPath())
}

// LoadWorkbenchSpec reads a standalone workbench spec YAML file.
func LoadWorkbenchSpec(path string) (WorkbenchConfigSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return WorkbenchConfigSpec{}, fmt.Errorf("docker: environment: read workbench spec %q: %w", path, err)
	}
	var spec WorkbenchConfigSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return WorkbenchConfigSpec{}, fmt.Errorf("docker: environment: parse workbench spec %q: %w", path, err)
	}
	if spec.Timeout == "" {
		spec.Timeout = "5m"
	}
	return spec, nil
}

// PackageManagerHosts returns common package manager domains that may be added to AllowedHosts.
func PackageManagerHosts() []string {
	return []string{
		"registry.npmjs.org",
		"pypi.org",
		"proxy.golang.org",
		"repo.maven.apache.org",
		"rubygems.org",
	}
}
