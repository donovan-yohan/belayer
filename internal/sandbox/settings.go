package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// DefaultMode is the driver name resolved when .belayer/config.yaml has no
// sandbox section (or no sandbox.mode). The noop driver ships with belayer
// and is registered unconditionally.
const DefaultMode = "noop"

// Settings holds the sandbox section of .belayer/config.yaml. A zero Settings
// signals default-noop behavior; see Settings.ModeOrDefault.
type Settings struct {
	Mode   string `yaml:"mode"`
	Policy string `yaml:"policy"`
}

// ModeOrDefault returns Mode when non-empty, or DefaultMode otherwise.
func (s Settings) ModeOrDefault() string {
	if s.Mode == "" {
		return DefaultMode
	}
	return s.Mode
}

type settingsFile struct {
	Sandbox Settings `yaml:"sandbox"`
}

// LoadSettings reads the sandbox section from <workdir>/.belayer/config.yaml.
// A missing file, empty workdir, or missing section returns a zero Settings
// and nil error; callers treat an empty Mode as DefaultMode via ModeOrDefault.
func LoadSettings(workdir string) (Settings, error) {
	if workdir == "" {
		return Settings{}, nil
	}
	path := filepath.Join(workdir, ".belayer", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, fmt.Errorf("sandbox: read config: %w", err)
	}
	var f settingsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Settings{}, fmt.Errorf("sandbox: parse config: %w", err)
	}
	return f.Sandbox, nil
}
