package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Phase represents the three-phase model of a belayer session.
type Phase string

const (
	PhaseIntake    Phase = "intake"
	PhaseImplement Phase = "implement"
	PhaseDeliver   Phase = "deliver"
)

// AgentSpec describes a single agent within a session template.
type AgentSpec struct {
	Name         string            `yaml:"name" json:"name"`
	Vendor       string            `yaml:"vendor" json:"vendor"`
	Model        string            `yaml:"model" json:"model"`
	SystemPrompt string            `yaml:"system_prompt" json:"system_prompt"`
	MCPConfig    string            `yaml:"mcp_config,omitempty" json:"mcp_config,omitempty"`
	Settings     string            `yaml:"settings,omitempty" json:"settings,omitempty"`
	Env          map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// SessionTemplate describes the agents for a session phase.
type SessionTemplate struct {
	Name        string      `yaml:"name" json:"name"`
	Phase       Phase       `yaml:"phase" json:"phase"`
	Description string      `yaml:"description" json:"description"`
	Agents      []AgentSpec `yaml:"agents" json:"agents"`
}

// LoadTemplate loads a session template by name. It checks the workspace
// templates directory first (.belayer/templates/<name>.yaml), then falls
// back to built-in defaults. This lets users customize templates by editing
// the YAML files that belayer setup writes.
func LoadTemplate(name string) (SessionTemplate, error) {
	return LoadTemplateFromDir("", name)
}

// LoadTemplateFromDir loads a template, checking templatesDir first.
// If templatesDir is empty, only built-in defaults are used.
func LoadTemplateFromDir(templatesDir, name string) (SessionTemplate, error) {
	// Try workspace YAML first.
	if templatesDir != "" {
		path := filepath.Join(templatesDir, name+".yaml")
		if tmpl, err := LoadTemplateFile(path); err == nil {
			return tmpl, nil
		}
	}

	// Fall back to built-in defaults.
	src, ok := builtinTemplates[name]
	if !ok {
		return SessionTemplate{}, fmt.Errorf("session: unknown template %q", name)
	}
	agents := make([]AgentSpec, len(src.Agents))
	copy(agents, src.Agents)
	tmpl := src
	tmpl.Agents = agents
	return tmpl, nil
}

// LoadTemplateFile reads and parses a single YAML template file.
func LoadTemplateFile(path string) (SessionTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionTemplate{}, fmt.Errorf("session: read template %q: %w", path, err)
	}
	var tmpl SessionTemplate
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		return SessionTemplate{}, fmt.Errorf("session: parse template %q: %w", path, err)
	}
	return tmpl, nil
}

// ListTemplates returns the names of all built-in templates in sorted order.
func ListTemplates() []string {
	names := make([]string, 0, len(builtinTemplates))
	for name := range builtinTemplates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// WriteDefaultTemplates writes all built-in templates as YAML files to dir.
// Existing files are not overwritten.
func WriteDefaultTemplates(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("session: create templates dir: %w", err)
	}
	for name, tmpl := range builtinTemplates {
		path := filepath.Join(dir, name+".yaml")
		if _, err := os.Stat(path); err == nil {
			continue // don't overwrite existing
		}
		data, err := yaml.Marshal(tmpl)
		if err != nil {
			return fmt.Errorf("session: marshal template %q: %w", name, err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("session: write template %q: %w", name, err)
		}
	}
	return nil
}

// ValidateTemplate checks basic sanity of a SessionTemplate.
func ValidateTemplate(tmpl SessionTemplate) error {
	if tmpl.Name == "" {
		return fmt.Errorf("session: template name is required")
	}
	if len(tmpl.Agents) == 0 {
		return fmt.Errorf("session: template %q must have at least one agent", tmpl.Name)
	}
	for _, a := range tmpl.Agents {
		if a.Name == "" {
			return fmt.Errorf("session: template %q has an agent with no name", tmpl.Name)
		}
		if a.Vendor == "" {
			return fmt.Errorf("session: template %q agent %q has no vendor", tmpl.Name, a.Name)
		}
	}
	return nil
}

// built-in templates keyed by name — the defaults written by belayer setup.
var builtinTemplates = map[string]SessionTemplate{
	"intake": {
		Name:        "intake",
		Phase:       PhaseIntake,
		Description: "Intake — single agent generates spec from idea",
		Agents: []AgentSpec{
			{
				Name:         "explorer",
				Vendor:       "claude",
				Model:        "sonnet",
				SystemPrompt: "You are an exploration agent. Analyze the idea and produce a detailed spec.",
			},
		},
	},
	"implement": {
		Name:        "implement",
		Phase:       PhaseImplement,
		Description: "Implementation — pilot + implementer + reviewer trio",
		Agents: []AgentSpec{
			{
				Name:         "pilot",
				Vendor:       "claude",
				Model:        "opus",
				SystemPrompt: "You are the pilot agent. Coordinate the implementer and reviewer. You do NOT write code.",
			},
			{
				Name:         "implementer",
				Vendor:       "opencode",
				Model:        "opencode/claude-sonnet-4-6",
				SystemPrompt: "You are the implementer. Write code as directed by the pilot.",
			},
			{
				Name:         "reviewer",
				Vendor:       "opencode",
				Model:        "opencode/gpt-5.1-codex",
				SystemPrompt: "You are the code reviewer. Review changes for correctness, style, and completeness.",
			},
		},
	},
	"deliver": {
		Name:        "deliver",
		Phase:       PhaseDeliver,
		Description: "Deliver — QA validation, merge, and monitoring",
		Agents: []AgentSpec{
			{
				Name:         "qa",
				Vendor:       "claude",
				Model:        "sonnet",
				SystemPrompt: "You are the QA agent. Run tests and validate the implementation.",
			},
			{
				Name:         "merger",
				Vendor:       "claude",
				Model:        "sonnet",
				SystemPrompt: "You are the merge agent. Create the PR and handle merge logistics.",
			},
		},
	},
}
