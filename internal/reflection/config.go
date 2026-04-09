package reflection

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ReflectionConfig controls how and when reflection runs.
// Loaded from .belayer/config.yaml or per-template override.
type ReflectionConfig struct {
	// Vendor is the CLI tool used to run the reflection agent (e.g., "claude", "opencode").
	Vendor string `yaml:"vendor" json:"vendor"`

	// Model is the model to use for reflection (e.g., "sonnet", "opus").
	Model string `yaml:"model" json:"model"`

	// Trigger controls when reflection fires.
	// Supported: "post-session" (default), "manual".
	Trigger string `yaml:"trigger" json:"trigger"`

	// Limits are daemon-enforced safety valves.
	Limits ReflectionLimits `yaml:"limits" json:"limits"`
}

// ReflectionLimits are hard safety valves enforced by the daemon, not by LLM judgment.
type ReflectionLimits struct {
	// MaxReviewLoops caps how many times the pilot can cycle implementer→reviewer
	// before the daemon forces the session to stop. 0 means unlimited.
	MaxReviewLoops int `yaml:"max_review_loops" json:"max_review_loops"`

	// MaxSessionDuration caps total session wall-clock time. 0 means unlimited.
	MaxSessionDuration time.Duration `yaml:"max_session_duration" json:"max_session_duration"`

	// MaxSessionTokens caps total token usage across all agents. 0 means unlimited.
	MaxSessionTokens int64 `yaml:"max_session_tokens" json:"max_session_tokens"`
}

// DefaultReflectionConfig returns sensible defaults.
func DefaultReflectionConfig() ReflectionConfig {
	return ReflectionConfig{
		Vendor:  "claude",
		Model:   "sonnet",
		Trigger: "post-session",
		Limits: ReflectionLimits{
			MaxReviewLoops:     10,
			MaxSessionDuration: 4 * time.Hour,
			MaxSessionTokens:   0, // unlimited by default
		},
	}
}

// LoadReflectionConfig reads a ReflectionConfig from a YAML file.
// Returns DefaultReflectionConfig() if the file doesn't exist.
func LoadReflectionConfig(path string) (ReflectionConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultReflectionConfig(), nil
		}
		return ReflectionConfig{}, fmt.Errorf("reflection: read config %q: %w", path, err)
	}

	cfg := DefaultReflectionConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ReflectionConfig{}, fmt.Errorf("reflection: parse config %q: %w", path, err)
	}
	return cfg, nil
}

// Validate checks that the config has valid values.
func (c ReflectionConfig) Validate() error {
	if c.Vendor == "" {
		return fmt.Errorf("reflection config: vendor is required")
	}
	if c.Model == "" {
		return fmt.Errorf("reflection config: model is required")
	}
	switch c.Trigger {
	case "post-session", "manual":
		// valid
	default:
		return fmt.Errorf("reflection config: unknown trigger %q (want post-session or manual)", c.Trigger)
	}
	return nil
}
