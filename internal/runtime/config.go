package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Config holds the runtime section of .belayer/config.yaml.
// An empty Config (all zero values) signals that no runtime config is present,
// and the caller should fall back to the Noop provider.
type Config struct {
	Up        string     `yaml:"up"`
	Health    string     `yaml:"health"`
	Down      string     `yaml:"down"`
	Endpoints []Endpoint `yaml:"endpoints"`
}

// Empty reports whether the Config has no runtime behavior configured.
// Callers use this to decide whether to fall back to the Noop provider.
func (c Config) Empty() bool {
	return c.Up == "" && c.Health == "" && c.Down == "" && len(c.Endpoints) == 0
}

// NewFromConfig picks a Provider to match the given Config. An Empty config
// yields a Noop provider; any other config yields a Command provider wrapping
// cfg. This is the single construction point the daemon CLI uses.
func NewFromConfig(cfg Config) Provider {
	if cfg.Empty() {
		return &Noop{}
	}
	return NewCommand(cfg)
}

// file is the top-level shape of .belayer/config.yaml.
// Unknown sections are ignored by yaml.v3's default behavior.
type file struct {
	Runtime Config `yaml:"runtime"`
}

// LoadConfig reads the runtime section from <workdir>/.belayer/config.yaml.
// If the file does not exist, or the file has no runtime section, it returns
// a zero Config and nil error. The caller should treat a zero Config as an
// indication to use the Noop provider.
func LoadConfig(workdir string) (Config, error) {
	path := filepath.Join(workdir, ".belayer", "config.yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("runtime: read config: %w", err)
	}

	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Config{}, fmt.Errorf("runtime: parse config: %w", err)
	}

	return f.Runtime, nil
}
