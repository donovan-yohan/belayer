package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// RuntimeCaps captures the daemon-side concurrency settings parsed from
// .belayer/config.yaml. Zero values mean "unspecified" and should be resolved
// against defaults or legacy compatibility rules by the caller.
type RuntimeCaps struct {
	MaxConcurrentAgents      int
	MaxConcurrentMains       int
	MaxConcurrentSides       int
	MaxSideSummonsPerSession int
}

type runtimeCapsFile struct {
	Runtime struct {
		MaxConcurrentAgents      *int `yaml:"max_concurrent_agents"`
		MaxConcurrentMains       *int `yaml:"max_concurrent_mains"`
		MaxConcurrentSides       *int `yaml:"max_concurrent_sides"`
		MaxSideSummonsPerSession *int `yaml:"max_side_summons_per_session"`
	} `yaml:"runtime"`
}

// LoadRuntimeCaps reads concurrency settings from <workdir>/.belayer/config.yaml.
// Missing files or missing keys are treated as zero values so callers can layer
// in their own defaults and back-compat fallbacks.
func LoadRuntimeCaps(workdir string) (RuntimeCaps, error) {
	if workdir == "" {
		return RuntimeCaps{}, nil
	}

	path := filepath.Join(workdir, ".belayer", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RuntimeCaps{}, nil
		}
		return RuntimeCaps{}, fmt.Errorf("daemon: read runtime caps: %w", err)
	}

	var f runtimeCapsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return RuntimeCaps{}, fmt.Errorf("daemon: parse runtime caps: %w", err)
	}

	out := RuntimeCaps{}
	if f.Runtime.MaxConcurrentAgents != nil {
		out.MaxConcurrentAgents = *f.Runtime.MaxConcurrentAgents
	}
	if f.Runtime.MaxConcurrentMains != nil {
		out.MaxConcurrentMains = *f.Runtime.MaxConcurrentMains
	}
	if f.Runtime.MaxConcurrentSides != nil {
		out.MaxConcurrentSides = *f.Runtime.MaxConcurrentSides
	}
	if f.Runtime.MaxSideSummonsPerSession != nil {
		out.MaxSideSummonsPerSession = *f.Runtime.MaxSideSummonsPerSession
	}
	return out, nil
}
