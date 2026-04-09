package agent

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// AgentConfig holds the configuration for a single agent.
type AgentConfig struct {
	Name         string `yaml:"name" json:"name"`
	Vendor       string `yaml:"vendor" json:"vendor"`
	Model        string `yaml:"model" json:"model"`
	Tools        []Tool `yaml:"tools,omitempty" json:"tools,omitempty"`
	SystemPrompt string `yaml:"system_prompt" json:"system_prompt"`
}

// Tool describes a shell-backed tool available to an agent.
type Tool struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	InputSchema string `yaml:"input_schema" json:"input_schema"` // JSON schema as string
	Command     string `yaml:"command" json:"command"`            // shell command to exec
}

// LoadAgentConfig reads and parses a YAML file containing a single AgentConfig.
func LoadAgentConfig(path string) (AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AgentConfig{}, fmt.Errorf("agent: read config %q: %w", path, err)
	}
	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return AgentConfig{}, fmt.Errorf("agent: parse config %q: %w", path, err)
	}
	return cfg, nil
}

// LoadAgentConfigs reads and parses a YAML file containing a list of AgentConfigs.
func LoadAgentConfigs(path string) ([]AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agent: read configs %q: %w", path, err)
	}
	var cfgs []AgentConfig
	if err := yaml.Unmarshal(data, &cfgs); err != nil {
		return nil, fmt.Errorf("agent: parse configs %q: %w", path, err)
	}
	return cfgs, nil
}

// ValidateAgentConfig checks that all required fields of an AgentConfig are valid.
// It returns a descriptive error for the first violation found.
func ValidateAgentConfig(cfg AgentConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("agent: config validation: name is required")
	}
	if cfg.Vendor == "" {
		return fmt.Errorf("agent: config validation: vendor is required")
	}
	return nil
}
