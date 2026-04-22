package bridge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// ProjectConfig holds the bridge section of .belayer/config.yaml.
type ProjectConfig struct {
	// SkipOpenRouterProbe suppresses the HERMES_SKIP_OPENROUTER_PROBE env var
	// injection when false. Default true: most providers do not need OpenRouter
	// metadata; suppressing the probe eliminates 20+ proxy-denied CONNECTs per
	// run when a sandbox egress policy does not whitelist openrouter.ai.
	//
	// Set to false only when using a vendor that explicitly requires OpenRouter
	// metadata at startup (e.g. a routing layer that resolves model IDs via
	// the OpenRouter catalog).
	SkipOpenRouterProbe bool `yaml:"skip_openrouter_probe"`

	// MaxConcurrentAgents is the daemon-enforced upper bound for live agents
	// in a session. It is read from runtime.max_concurrent_agents in the
	// project config and defaults to 15 when unset.
	MaxConcurrentAgents int `yaml:"max_concurrent_agents"`
}

// bridgeConfigFile is the top-level shape of .belayer/config.yaml that we
// care about — unknown sections are silently ignored by yaml.v3.
type bridgeConfigFile struct {
	Bridge  bridgeConfigSection  `yaml:"bridge"`
	Runtime runtimeConfigSection `yaml:"runtime"`
}

// bridgeConfigSection mirrors the yaml shape so we can apply the default
// before unmarshalling (SkipOpenRouterProbe defaults to true when the key is
// absent; yaml.v3 zero-initialises missing keys to false, so we pre-set it).
type bridgeConfigSection struct {
	SkipOpenRouterProbe *bool `yaml:"skip_openrouter_probe"`
}

type runtimeConfigSection struct {
	MaxConcurrentAgents *int `yaml:"max_concurrent_agents"`
}

// LoadProjectConfig reads the bridge section from <workdir>/.belayer/config.yaml.
// If the file does not exist, or the bridge section is absent, it returns the
// default config (SkipOpenRouterProbe = true). A missing key inside an existing
// bridge section also defaults to true.
func LoadProjectConfig(workdir string) (ProjectConfig, error) {
	defaults := ProjectConfig{
		SkipOpenRouterProbe: true,
		MaxConcurrentAgents: 15,
	}
	if workdir == "" {
		return defaults, nil
	}

	path := filepath.Join(workdir, ".belayer", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaults, nil
		}
		return defaults, fmt.Errorf("bridge: read config: %w", err)
	}

	var f bridgeConfigFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return defaults, fmt.Errorf("bridge: parse config: %w", err)
	}

	out := defaults
	if f.Bridge.SkipOpenRouterProbe != nil {
		out.SkipOpenRouterProbe = *f.Bridge.SkipOpenRouterProbe
	}
	if f.Runtime.MaxConcurrentAgents != nil && *f.Runtime.MaxConcurrentAgents > 0 {
		out.MaxConcurrentAgents = *f.Runtime.MaxConcurrentAgents
	}
	return out, nil
}
